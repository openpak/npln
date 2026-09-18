package npln

// presence — nn.npln.friends.v1.PresenceService, ported from Splatoon 3's (titles/splatoon-3).
//
//   - KeepAlive is bidirectional. We open with a heartbeat, then answer every ping and keep
//     what the client publishes about itself (Dinkum: status=host, sessionId=<room code>).
//   - SubscribePresences reports the caller's friends back, and keeps reporting as their state
//     CHANGES rather than sending one snapshot. It is how a friend sees "hosting" and joins.

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/types/known/durationpb"

	commonpb "github.com/openpak/npln/proto/common"
	friendspb "github.com/openpak/npln/proto/friends/v1"
)

// The interval/deadline the real service announces; the client pings on one and gives up after
// the other (as in Splatoon 3).
const (
	presenceInterval = 30 * time.Second
	presenceDeadline = 50 * time.Second
)

var presenceHeartbeat = &friendspb.Heartbeat{
	Interval: durationpb.New(presenceInterval),
	Deadline: durationpb.New(presenceDeadline),
}

// live is every player holding a KeepAlive stream open, with what they last published. A player
// counts as online only while talking to THIS server: claiming otherwise sends friends into a
// join that cannot succeed.
//
// ponytail: one mutex over the whole map, touched once per ping per player; shard it if a
// lobby ever makes that a contention point.
var live = struct {
	sync.Mutex
	attrs map[string]map[string]*commonpb.Value
}{attrs: map[string]map[string]*commonpb.Value{}}

// publish merges an update: the client sends partial updates, so replacing the map wholesale
// would drop attributes it set earlier and still considers current.
func publish(uid string, attrs map[string]*commonpb.Value) {
	if uid == "" {
		return
	}
	live.Lock()
	defer live.Unlock()
	cur := live.attrs[uid]
	if cur == nil {
		cur = map[string]*commonpb.Value{}
		live.attrs[uid] = cur
	}
	for k, v := range attrs {
		cur[k] = v
	}
}

func goOnline(uid string) {
	if uid == "" {
		return
	}
	live.Lock()
	defer live.Unlock()
	if live.attrs[uid] == nil {
		live.attrs[uid] = map[string]*commonpb.Value{}
	}
}

func goOffline(uid string) {
	live.Lock()
	defer live.Unlock()
	delete(live.attrs, uid)
}

func stateOf(uid string) (friendspb.State, map[string]*commonpb.Value) {
	live.Lock()
	defer live.Unlock()
	a, ok := live.attrs[uid]
	if !ok {
		return friendspb.State_OFFLINE, nil
	}
	out := make(map[string]*commonpb.Value, len(a))
	for k, v := range a {
		out[k] = v
	}
	return friendspb.State_ONLINE, out
}

type presenceServer struct {
	friendspb.UnimplementedPresenceServiceServer
}

// KeepAlive opens with a heartbeat, then answers every ping: the client gives up when its own
// pings go unanswered, even if we heartbeat on a timer of our own.
//
// The opening heartbeat is what makes the presence client "connected". Dinkum opens the stream
// and sends nothing; without it, SetPresenceHosting fails (2026-09-18).
func (p *presenceServer) KeepAlive(stream friendspb.PresenceService_KeepAliveServer) error {
	_, uid, _ := bearer(stream.Context())
	goOnline(uid)
	defer goOffline(uid)
	if err := stream.Send(&friendspb.KeepAliveResponse{Heartbeat: presenceHeartbeat}); err != nil {
		return nil
	}
	for {
		req, err := stream.Recv()
		if err != nil {
			return nil
		}
		if up := req.GetUpdatePresence(); up != nil {
			attrs := up.GetPresence().GetAttributes()
			publish(uid, attrs)
			log.Printf("[Presence] UpdatePresence from %s: %d attribute(s)", uid, len(attrs))
		}
		if err := stream.Send(&friendspb.KeepAliveResponse{Heartbeat: presenceHeartbeat}); err != nil {
			return nil
		}
	}
}

// presencesFor builds one Presence per friend of the caller.
func presencesFor(ctx context.Context) []*friendspb.Presence {
	pid, ok := callerPID(ctx)
	if !ok {
		return nil
	}
	me, err := lookupAccount(pid)
	if err != nil {
		log.Printf("[Presence] account lookup for pid=%d: %v", pid, err)
		return nil
	}
	tenant := tenantFromCtx(ctx)
	out := make([]*friendspb.Presence, 0, len(me.Friends))
	for _, f := range me.Friends {
		state, attrs := stateOf(f.UserID())
		out = append(out, &friendspb.Presence{
			Name:       tenant + "/users/" + f.UserID() + "/presence",
			State:      state,
			Attributes: attrs,
		})
	}
	return out
}

// fingerprint decides a presence CHANGED. Attributes are part of it: the friend list's "Join"
// appears on an attribute transition, not on ONLINE alone.
func fingerprint(p *friendspb.Presence) string {
	attrs := p.GetAttributes()
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := fmt.Sprintf("%d", p.GetState())
	for _, k := range keys {
		s += "|" + k + "=" + attrs[k].String()
	}
	return s
}

// resumeToken describes the ENUMERATED SET, never a delta: a token describing only what just
// changed makes the client's set diverge (measured emptying a friend list, Splatoon 3).
func resumeToken(ps []*friendspb.Presence) string {
	var raw []byte
	for _, p := range ps {
		uid := lastSeg(strings.TrimSuffix(p.GetName(), "/presence"))
		if uid == "" {
			continue
		}
		var entry []byte
		entry = protowire.AppendTag(entry, 1, protowire.BytesType)
		entry = protowire.AppendString(entry, uid)
		entry = protowire.AppendTag(entry, 2, protowire.BytesType)
		entry = protowire.AppendBytes(entry, nil)
		raw = protowire.AppendTag(raw, 1, protowire.BytesType)
		raw = protowire.AppendBytes(raw, entry)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func (p *presenceServer) SubscribePresences(req *friendspb.SubscribePresencesRequest, stream friendspb.PresenceService_SubscribePresencesServer) error {
	ctx := stream.Context()
	send := func(r *friendspb.SubscribePresencesResponse) bool { return stream.Send(r) == nil }
	heartbeat := func() bool {
		return send(&friendspb.SubscribePresencesResponse{
			Response: &friendspb.SubscribePresencesResponse_Heartbeat{Heartbeat: presenceHeartbeat},
		})
	}
	if !heartbeat() { // the opening heartbeat is where the client learns the contract
		return nil
	}

	presences := presencesFor(ctx)
	// A targeted subscription gets only the presences it named: the whole list instead diverges
	// the cursor the client keeps.
	if want := req.GetPresences(); len(want) > 0 {
		keep := make(map[string]bool, len(want))
		for _, w := range want {
			keep[w] = true
		}
		var filtered []*friendspb.Presence
		for _, pr := range presences {
			if keep[pr.GetName()] {
				filtered = append(filtered, pr)
			}
		}
		presences = filtered
	}
	log.Printf("[Presence] SubscribePresences asked=%d -> %d presence(s)", len(req.GetPresences()), len(presences))

	// An empty presences message is never sent: a fresh account goes from heartbeat to done.
	if len(presences) > 0 {
		if !send(&friendspb.SubscribePresencesResponse{
			Response: &friendspb.SubscribePresencesResponse_Presences_{
				Presences: &friendspb.SubscribePresencesResponse_Presences{
					Presences: presences, ResumeToken: resumeToken(presences),
				},
			},
		}) {
			return nil
		}
	}
	// Without "finished enumerating" the client waits forever for the rest of the list.
	if !send(&friendspb.SubscribePresencesResponse{
		Response: &friendspb.SubscribePresencesResponse_EnumerationDone{
			EnumerationDone: &friendspb.SubscribePresencesResponse_PresenceEnumerationDone{},
		},
	}) {
		return nil
	}

	known := make(map[string]string, len(presences))
	for _, pr := range presences {
		known[pr.GetName()] = fingerprint(pr)
	}
	// ponytail: changes are noticed on a 5 s poll; a friend going "host" shows within 5 s.
	// Push from publish() if that lag ever matters.
	poll := time.NewTicker(5 * time.Second)
	defer poll.Stop()
	beat := time.NewTicker(presenceInterval)
	defer beat.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-beat.C:
			if !heartbeat() {
				return nil
			}
		case <-poll.C:
			all := presencesFor(ctx)
			var changed []*friendspb.Presence
			for _, pr := range all {
				if f := fingerprint(pr); known[pr.GetName()] != f {
					known[pr.GetName()] = f
					changed = append(changed, pr)
				}
			}
			if len(changed) > 0 {
				log.Printf("[Presence] %d change(s) across %d friend(s)", len(changed), len(all))
				if !send(&friendspb.SubscribePresencesResponse{
					Response: &friendspb.SubscribePresencesResponse_Presences_{
						Presences: &friendspb.SubscribePresencesResponse_Presences{
							Presences: changed, ResumeToken: resumeToken(all),
						},
					},
				}) {
					return nil
				}
			}
		}
	}
}
