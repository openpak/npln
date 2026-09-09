package npln

// presence — nn.npln.friends.v1.PresenceService.
//
// Two streams, and the game's friend list depends on both:
//
//   - KeepAlive is bidirectional. The client pings, and PUBLISHES its own state on the same
//     stream; we must answer every ping and keep what it publishes.
//   - SubscribePresences reports the caller's friends back, and must keep reporting as their
//     state CHANGES rather than sending one snapshot.

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/types/known/durationpb"

	commonpb "openpak/splatoon-3/proto/common"
	friendspb "openpak/splatoon-3/proto/friends/v1"
)

// The heartbeat contract the client times itself against: it pings on the interval and gives up
// after the deadline. Both values are the ones the real service announces.
const (
	presenceInterval = 30 * time.Second
	presenceDeadline = 50 * time.Second
)

func presenceHeartbeat() *friendspb.Heartbeat {
	return &friendspb.Heartbeat{
		Interval: durationpb.New(presenceInterval),
		Deadline: durationpb.New(presenceDeadline),
	}
}

// ---- who is online, and what they are doing ----

// live is every player currently holding a KeepAlive stream open, with whatever they last
// published about themselves. A player counts as online only while they are talking to THIS
// server: claiming otherwise sends their friends into a join that cannot succeed.
//
// ponytail: one mutex over the whole map. This is a per-title server and the map is touched once
// per ping per player; shard it if a lobby ever makes that a contention point.
var live = struct {
	sync.Mutex
	attrs map[string]map[string]*commonpb.Value
}{attrs: map[string]map[string]*commonpb.Value{}}

// publish merges an update into what we already know. The client sends partial updates, so
// replacing the map wholesale would drop attributes it set earlier and still considers current.
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

// stateOf reports a player's presence and the attributes they published.
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

// presencesFor builds one Presence per friend of the caller.
func presencesFor(ctx context.Context) []*friendspb.Presence {
	me, ok := callerAccount(ctx)
	if !ok {
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

// fingerprint is what we compare to decide a presence CHANGED. Attributes are part of it: the
// friend list's "Join" affordance appears on an attribute transition, not on ONLINE alone.
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

// resumeToken describes the ENUMERATED SET, never a delta. The client cross-references it with
// the set it holds; a token describing only what just changed makes the two diverge, which has
// been measured emptying a large friend list with a communication error.
func resumeToken(ps []*friendspb.Presence) string {
	var raw []byte
	for _, p := range ps {
		uid := lastSeg(trimSuffix(p.GetName(), "/presence"))
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

func trimSuffix(s, suf string) string {
	if len(s) >= len(suf) && s[len(s)-len(suf):] == suf {
		return s[:len(s)-len(suf)]
	}
	return s
}

func (p *presenceServer) SubscribePresences(req *friendspb.SubscribePresencesRequest, stream friendspb.PresenceService_SubscribePresencesServer) error {
	ctx := stream.Context()
	send := func(r *friendspb.SubscribePresencesResponse) bool { return stream.Send(r) == nil }
	heartbeat := func() bool {
		return send(&friendspb.SubscribePresencesResponse{
			Response: &friendspb.SubscribePresencesResponse_Heartbeat{Heartbeat: presenceHeartbeat()},
		})
	}

	// The opening heartbeat comes first: it is where the client learns the contract.
	if !heartbeat() {
		return nil
	}

	presences := presencesFor(ctx)
	// A targeted subscription asks for named presences. Answering with the whole list instead
	// diverges the cursor the client keeps on its side.
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
	log.Printf("[Presence] SubscribePresences user=%q asked=%d -> %d presence(s)",
		short(req.GetUser()), len(req.GetPresences()), len(presences))

	// An EMPTY presences message is not sent at all — it does not occur in observed traffic, and
	// a fresh account goes straight from the heartbeat to enumeration_done.
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

	// Without "I have finished enumerating" the client waits forever for the rest of the list.
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

	t := time.NewTicker(presenceInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			// Push what CHANGED. A subscription that only ever sends its opening snapshot leaves
			// two friends with opposite views of each other, neither of which ever corrects.
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
			if !heartbeat() {
				return nil
			}
		}
	}
}

// KeepAlive answers EVERY ping. Draining the client's pings and heartbeating on our own timer
// instead kills the stream: the client gives up after two intervals with no answer to its own
// ping, and the game reports a communication error although nothing was disconnected.
func (p *presenceServer) KeepAlive(stream friendspb.PresenceService_KeepAliveServer) error {
	ctx := stream.Context()
	// This stream arrives with the uid in metadata and not always with a usable token, so the
	// pairing recorded at authentication is what names the player here.
	uid := mdGet(ctx, "uid")
	if uid == "" {
		if pid, ok := callerPID(ctx); ok {
			uid = uidForPID(pid)
		}
	}
	if uid == "" {
		log.Printf("[Presence] KeepAlive with no identity: neither a usable token nor a known pairing")
	}
	goOnline(uid)
	defer goOffline(uid)

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
		if err := stream.Send(&friendspb.KeepAliveResponse{Heartbeat: presenceHeartbeat()}); err != nil {
			return nil
		}
	}
}
