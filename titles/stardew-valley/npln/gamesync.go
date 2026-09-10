package npln

// gamesync — nn.npln.gamesync.v1.Gamesync, the session transport. After a farm is created or
// joined, the client's Pia/NPLN layer opens a second connection (SNI gamesync.npln.nintendo.net)
// to GameSession.host:port, exchanges its matchmaking id-token for a session token
// (IssueToken) and holds a bidirectional KeepUserSession stream: it sends echos (heartbeats) and
// watch targets; we answer with document snapshots. The NPLN layer waits for its own
// UserSession document and the session's mutable data before it considers itself connected —
// those are synthesized here from the farm session; everything the game writes itself
// (WriteDocuments) is stored and relayed to every watcher of that farm.
//
// Document names and field keys are the NPLN SDK's (measured by the family's reference server):
//   …/__us/<uss>, …/__pus/<uss>, …/__gs/s   UserSession: uid ussid ucsid upcsid pgn st tn att ltc
//   …/__stg/All                            GameSessionMutableData: gsid addr p mcn maxu rs{…}
//   …/__stu/<uss>                          per-user state: suid susid sussid suscid pl(bytes)
//
// ponytail: in-memory, one mutex; a restart drops every live farm.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/openpak/npln/proto/common"
	gspb "github.com/openpak/npln/proto/gamesync/v1"
)

type gsSession struct {
	uid, gsName, gsid, uss string
	rank                   int // 1-based order of arrival in the farm: ussid/ucsid/upcsid must differ per player
	team                   string
	att                    *commonpb.MapValue
}

type watcher struct {
	gsid string
	wake chan struct{}
	// push delivers one document to this stream if (and only if) its content changed since the
	// last delivery on that target. Nintendo emits a document change ONCE per change; re-pushing
	// unchanged mailbox documents (docs/__pgn/All/__stu/<uss>) made the consoles re-process old
	// messages and answer each duplicate — a feedback loop measured at hundreds of writes/s.
	// force (the write path, mailbox documents only) delivers even when the bytes are unchanged:
	// Pia re-writes an unacked roster verbatim once a second, and a joiner that dropped the first
	// copy (participant list not yet processed) needs the retransmit, or it times out (2318-1201).
	push    func(tid, kind, name string, m *commonpb.MapValue, force bool) error
	tmu     *sync.Mutex
	targets map[string]*target
}

type target struct {
	tid, coll string
	docs      []string
}

// deliver pushes a just-written (kind UPDATED) or just-removed (kind DELETED) document, once, to
// every stream of the farm that watches it (by name, or through a collection target). Caller holds
// g.mu. DELETED matters: the host's Pia session keeps a departed joiner as a ghost station (and
// keeps writing NAT-traversal messages into its dead mailbox) until it sees the participant's
// __pus document go away, and every later joiner then gets no roster and times out (2318-1201).
func (g *gamesyncServer) deliver(gsid, name, kind string, m *commonpb.MapValue) {
	for w := range g.watchers {
		if w.gsid != gsid || w.push == nil || w.tmu == nil {
			continue
		}
		w.tmu.Lock()
		var hits []string
		for _, t := range w.targets {
			if t.coll != "" && strings.HasPrefix(name, t.coll+"/") {
				hits = append(hits, t.tid)
			}
			for _, d := range t.docs {
				if d == name {
					hits = append(hits, t.tid)
				}
			}
		}
		w.tmu.Unlock()
		force := kind == "UPDATED" && strings.Contains(name, "/__stu/")
		for _, tid := range hits {
			go w.push(tid, kind, name, m, force) //nolint:errcheck // stream errors end the stream itself
		}
	}
}

type gamesyncServer struct {
	gspb.UnimplementedGamesyncServer
	mm *sessionServer

	mu       sync.Mutex
	sess     map[string]*gsSession         // by user-session uuid
	store    map[string]*commonpb.MapValue // gsid + "|" + doc name -> fields written by the game
	watchers map[*watcher]struct{}
}

func newGamesync(mm *sessionServer) *gamesyncServer {
	return &gamesyncServer{mm: mm, sess: map[string]*gsSession{}, store: map[string]*commonpb.MapValue{}, watchers: map[*watcher]struct{}{}}
}

func gsStr(s string) *commonpb.Value {
	return &commonpb.Value{ValueType: &commonpb.Value_StringValue{StringValue: s}}
}
func gsInt(n int64) *commonpb.Value {
	return &commonpb.Value{ValueType: &commonpb.Value_IntegerValue{IntegerValue: n}}
}
func gsBool(b bool) *commonpb.Value {
	return &commonpb.Value{ValueType: &commonpb.Value_BooleanValue{BooleanValue: b}}
}
func gsMap(f map[string]*commonpb.Value) *commonpb.Value {
	if f == nil {
		f = map[string]*commonpb.Value{}
	}
	return &commonpb.Value{ValueType: &commonpb.Value_MapValue{MapValue: &commonpb.MapValue{Fields: f}}}
}

// sessionClaims decodes one of OUR gss tokens (matchmaking id-token or gamesync access token).
func sessionClaims(tok string) (uid, gsid, usid, tid string, ok bool) {
	payload, ok := verifyJWT(tok)
	if !ok {
		return
	}
	var m struct {
		Sub      string                                `json:"sub"`
		Gamesync struct{ GSID, USID, UID, TID string } `json:"gamesync"`
	}
	if json.Unmarshal(payload, &m) != nil || m.Gamesync.GSID == "" {
		return "", "", "", "", false
	}
	uid = m.Gamesync.UID
	if uid == "" {
		uid = m.Sub
	}
	return uid, m.Gamesync.GSID, m.Gamesync.USID, m.Gamesync.TID, true
}

func callerUss(ctx context.Context) string {
	a := strings.TrimSpace(mdGet(ctx, "authorization"))
	a = strings.TrimPrefix(strings.TrimPrefix(a, "Bearer "), "bearer ")
	_, _, usid, _, _ := sessionClaims(a)
	return usid
}

// ussFromDocPath: the user-session uuid when the document name ends with one.
func ussFromDocPath(name string) string {
	if last := lastSeg(name); len(last) >= 8 && strings.Contains(last, "-") {
		return last
	}
	return ""
}

func (g *gamesyncServer) lookup(uss string) *gsSession {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sess[uss]
}

// participants of a farm, ordered by rank. Caller holds no lock.
func (g *gamesyncServer) participants(gsid string) []*gsSession {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []*gsSession
	for _, s := range g.sess {
		if s.gsid == gsid {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rank < out[j].rank })
	return out
}

func (g *gamesyncServer) IssueToken(ctx context.Context, req *gspb.IssueTokenRequest) (*gspb.IssueTokenResponse, error) {
	uid, gsid, usid, tid, ok := sessionClaims(req.GetMatchmakingIdToken())
	if !ok {
		log.Printf("[GS] IssueToken user_session=%q: matchmaking id-token not ours -> refused", req.GetUserSession())
		return nil, grpc.Errorf(16, "matchmaking id token not recognised") //nolint:staticcheck
	}
	uss := lastSeg(req.GetUserSession())
	if usid != "" {
		uss = usid
	}
	tenant := "tenants/" + tid
	gsName := tenant + "/gameSessions/" + gsid
	s := &gsSession{uid: uid, gsName: gsName, gsid: gsid, uss: uss}
	g.mm.mu.Lock()
	for _, m := range g.mm.members[gsid] {
		if lastSeg(m.GetName()) == uss {
			s.att = m.GetAttributes()
		}
	}
	g.mm.mu.Unlock()

	g.mu.Lock()
	for u, other := range g.sess { // a player opening a new session has abandoned the old one
		if u != uss && other.uid == uid {
			delete(g.sess, u)
		}
	}
	if old := g.sess[uss]; old != nil {
		s.rank = old.rank
	} else {
		for _, other := range g.sess {
			if other.gsid == gsid && other.rank > s.rank {
				s.rank = other.rank
			}
		}
		s.rank++
	}
	g.sess[uss] = s
	g.mu.Unlock()
	log.Printf("[GS] IssueToken uss=%s uid=%s farm=%s rank=%d", uss, uid, gsid, s.rank)
	tok := mintSessionToken(uid, tenant, gsName, gsName+"/userSessions/"+uss, s.team, attrJSON(s.att), "{\"latencies\":{}}")
	return &gspb.IssueTokenResponse{Token: &gspb.Token{UserSession: req.GetUserSession(), AccessToken: tok, RefreshToken: tok, Ttl: durationpb.New(tokenTTL)}}, nil
}

func (g *gamesyncServer) RefreshToken(ctx context.Context, req *gspb.RefreshTokenRequest) (*gspb.RefreshTokenResponse, error) {
	uid, gsid, usid, tid, ok := sessionClaims(req.GetRefreshToken())
	if !ok {
		return nil, grpc.Errorf(16, "refresh token not recognised") //nolint:staticcheck
	}
	gsName := "tenants/" + tid + "/gameSessions/" + gsid
	tok := mintSessionToken(uid, "tenants/"+tid, gsName, gsName+"/userSessions/"+usid, "", "{}", "{\"latencies\":{}}")
	return &gspb.RefreshTokenResponse{Token: &gspb.Token{UserSession: req.GetUserSession(), AccessToken: tok, RefreshToken: tok, Ttl: durationpb.New(tokenTTL)}}, nil
}

// ---- document synthesis ----

func userSessionFields(s *gsSession) *commonpb.MapValue {
	uid, rank, team, att := "", 1, "", gsMap(nil)
	if s != nil {
		uid, team = s.uid, s.team
		if s.rank > 0 {
			rank = s.rank
		}
		if s.att != nil {
			att = gsMap(s.att.GetFields())
		}
	}
	return &commonpb.MapValue{Fields: map[string]*commonpb.Value{
		"uid": gsStr(uid), "ussid": gsInt(int64(rank)), "ucsid": gsInt(int64(rank)), "upcsid": gsInt(int64(rank)),
		"pgn": gsStr("All"), "st": gsInt(2), "tn": gsStr(team), "att": att, "ltc": gsMap(nil),
	}}
}

func stateUserFields(s *gsSession, uss string) *commonpb.MapValue {
	uid := ""
	if s != nil {
		uid = s.uid
	}
	return &commonpb.MapValue{Fields: map[string]*commonpb.Value{
		"suid": gsStr(uid), "susid": gsStr(uss), "sussid": gsInt(1), "suscid": gsInt(1),
		"pl": {ValueType: &commonpb.Value_BytesValue{BytesValue: nil}},
	}}
}

func (g *gamesyncServer) mutableFields(s *gsSession) *commonpb.MapValue {
	gsid, addr, port, maxp, pw, pub := "", envOr("NPLN_RELAY_HOST", "127.0.0.1"), envInt("NPLN_RELAY_PORT", 21010), int32(4), "", true
	var prp *commonpb.MapValue
	if s != nil {
		gsid = s.gsid
		g.mm.mu.Lock()
		if room := g.mm.sessions[s.gsid]; room != nil {
			addr, port, maxp, pw, pub, prp = room.GetHost(), room.GetPort(), room.GetMaxParticipantCount(), room.GetPassword(), room.GetIsPublic(), room.GetProperties()
		}
		g.mm.mu.Unlock()
	}
	return &commonpb.MapValue{Fields: map[string]*commonpb.Value{
		"gsid": gsStr(gsid), "addr": gsStr(addr), "p": gsInt(int64(port)), "mcn": gsStr("Farm4Player"), "maxu": gsInt(int64(maxp)),
		"rs": gsMap(map[string]*commonpb.Value{
			"cp": gsBool(true), "ip": gsBool(pub && pw == ""), "pw": gsStr(pw), "ebf": gsBool(false),
			"bfmin": gsInt(1), "bfmax": gsInt(int64(maxp)), "prp": gsMap(prp.GetFields()),
		}),
	}}
}

// fields returns what a document name currently holds: the game's own write if there is one,
// else the SDK-level synthesis for that document family, else a present-but-empty document.
func (g *gamesyncServer) fields(name, streamUss string) *commonpb.MapValue {
	uss := ussFromDocPath(name)
	if uss == "" {
		uss = streamUss
	}
	s := g.lookup(uss)
	gsid := ""
	if s != nil {
		gsid = s.gsid
	}
	g.mu.Lock()
	stored := g.store[gsid+"|"+name]
	g.mu.Unlock()
	if stored != nil {
		if strings.Contains(name, "/__pus/") || strings.Contains(name, "/__us/") {
			return g.withStation(gsid, uss, stored)
		}
		return stored
	}
	switch {
	case strings.Contains(name, "/__stu/"):
		return stateUserFields(s, uss)
	case strings.Contains(name, "/__pus/"), strings.Contains(name, "/__us/"), strings.HasSuffix(name, "/__gs/s"):
		return g.withStation(gsid, uss, userSessionFields(s))
	case strings.Contains(name, "/__stg/"):
		return g.mutableFields(s)
	case strings.Contains(name, "/__gs/"):
		if m := g.roomFields(lastSeg(name), s); m != nil {
			return m
		}
	}
	return &commonpb.MapValue{Fields: map[string]*commonpb.Value{"id": gsStr("")}}
}

// withStation relays a participant's Pia station blob (`pl`, written by the console into its own
// docs/__pgn/All/__stu/<uss>) into that participant's member document, which every other
// participant watches through the __pus collection. Nintendo's captures show consoles reading
// back station blobs they never wrote, so the server merges peers' blobs into watched documents;
// without it each console knows only itself and the mesh join times out at "connecting".
func (g *gamesyncServer) withStation(gsid, uss string, m *commonpb.MapValue) *commonpb.MapValue {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.withStationLocked(gsid, uss, m)
}

// withStationLocked: caller holds g.mu. Every member document that leaves the server goes
// through here — the initial listing, the wake re-pushes AND the write-path delivery — so a
// peer never sees the console's placeholder ids, not even for one push.
func (g *gamesyncServer) withStationLocked(gsid, uss string, m *commonpb.MapValue) *commonpb.MapValue {
	st := g.store[gsid+"|docs/__pgn/All/__stu/"+uss]
	out := mergeFields(nil, m)
	// The session ids are server-owned. A console writes its own member document with
	// upcsid=0 (later an empty map) and only ucsid/ussid set; Pia's NplnPlugin keys the
	// NAT/TURN station tables on upcsid (the lowest one is the host), so a peer served the
	// console's placeholder builds a station with id 0, never sets up its relay to the host,
	// and the join times out at WaitSetupRelayAddress (2318-1201). Overlay the rank.
	if s := g.sess[uss]; s != nil && s.rank > 0 {
		r := gsInt(int64(s.rank))
		out.Fields["ussid"], out.Fields["ucsid"], out.Fields["upcsid"] = r, r, r
	}
	// docs/__pgn/All/__stu/<uss> is that player's MAILBOX — other consoles write relay
	// messages (roster, acks, NAT traversal) into it — not a station blob the player published.
	// Merging its `pl` into the member document handed a rejoining console the previous
	// joiner's last message glued onto the host's entry, and the roster was then dropped
	// (measured 2026-09-04 21:27: identical listings except for that field). Never merge it.
	_ = st
	return out
}

// roomFields are the room documents docs/__gs/{f,m,r,n,ck} in the schema the reference server
// measured on Nintendo's own answers: f = the session address block, m = the room settings,
// r = participant counters, n = lifetime, ck = the password key.
func (g *gamesyncServer) roomFields(sub string, s *gsSession) *commonpb.MapValue {
	gsid, addr, port, maxp, pw := "", envOr("NPLN_RELAY_HOST", "127.0.0.1"), envInt("NPLN_RELAY_PORT", 21010), int32(4), ""
	var prp *commonpb.MapValue
	count := 1
	if s != nil {
		gsid = s.gsid
		count = len(g.participants(s.gsid))
		g.mm.mu.Lock()
		if room := g.mm.sessions[s.gsid]; room != nil {
			addr, port, maxp, pw, prp = room.GetHost(), room.GetPort(), room.GetMaxParticipantCount(), room.GetPassword(), room.GetProperties()
		}
		g.mm.mu.Unlock()
	}
	if count < 1 {
		count = 1
	}
	settings := func() *commonpb.MapValue {
		bfmax := maxp - 3
		if bfmax < 1 {
			bfmax = 1
		}
		return &commonpb.MapValue{Fields: map[string]*commonpb.Value{
			"bfmax": gsInt(int64(bfmax)), "bfmin": gsInt(1), "cp": gsBool(true), "ebf": gsBool(false),
			"ip": gsBool(true), "pw": gsStr(pw), "prp": gsMap(prp.GetFields()),
		}}
	}
	switch sub {
	case "f":
		sum := sha256.Sum256([]byte("stardew-gs-rs:" + gsid))
		return &commonpb.MapValue{Fields: map[string]*commonpb.Value{
			"addr": gsStr(addr), "gsid": gsStr(gsid), "maxu": gsInt(int64(maxp)), "mcn": gsStr("Farm4Player"),
			"p": gsInt(int64(port)), "rs": {ValueType: &commonpb.Value_BytesValue{BytesValue: sum[:]}}, "tid": gsStr(lastSeg(Tenant)),
		}}
	case "m", "s":
		return settings()
	case "r":
		return &commonpb.MapValue{Fields: map[string]*commonpb.Value{"ipc": gsInt(int64(count)), "dpc": gsInt(0)}}
	case "n":
		return &commonpb.MapValue{Fields: map[string]*commonpb.Value{
			"etn": gsBool(false),
			"ett": {ValueType: &commonpb.Value_TimestampValue{TimestampValue: timestamppb.New(time.Now().Add(24 * time.Hour))}},
		}}
	case "ck":
		return &commonpb.MapValue{Fields: map[string]*commonpb.Value{"k": gsStr(pw)}}
	}
	return nil
}

func doc(name string, f *commonpb.MapValue, now *timestamppb.Timestamp) *gspb.Document {
	return &gspb.Document{Name: name, Fields: f, CreateTime: now, UpdateTime: now}
}

func change(tid string, t gspb.DocumentChange_DocumentChangeType, d *gspb.Document) *gspb.KeepUserSessionResponse {
	return &gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_DocumentChange{DocumentChange: &gspb.DocumentChange{TargetId: tid, DocumentChangeType: t, Document: d}}}
}

func targetChange(tid string, t gspb.TargetChange_TargetChangeType) *gspb.KeepUserSessionResponse {
	return &gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_TargetChange{TargetChange: &gspb.TargetChange{TargetId: tid, TargetChangeType: t}}}
}

// collectionDocs: every document under a collection — the farm's participants for the SDK's
// UserSession collections, plus whatever the game wrote there.
func (g *gamesyncServer) collectionDocs(coll, streamUss string) []string {
	coll = strings.TrimRight(coll, "/")
	seen := map[string]bool{}
	var names []string
	if strings.HasSuffix(coll, "__us") || strings.HasSuffix(coll, "__pus") {
		if s := g.lookup(streamUss); s != nil {
			for _, p := range g.participants(s.gsid) {
				names = append(names, coll+"/"+p.uss)
				seen[coll+"/"+p.uss] = true
			}
		}
	}
	gsid := ""
	if s := g.lookup(streamUss); s != nil {
		gsid = s.gsid
	}
	g.mu.Lock()
	for k := range g.store {
		if strings.HasPrefix(k, gsid+"|"+coll+"/") {
			if n := strings.TrimPrefix(k, gsid+"|"); !seen[n] && !strings.Contains(strings.TrimPrefix(n, coll+"/"), "/") {
				names = append(names, n)
			}
		}
	}
	g.mu.Unlock()
	sort.Strings(names[len(seen):])
	return names
}

func (g *gamesyncServer) KeepUserSession(stream grpc.BidiStreamingServer[gspb.KeepUserSessionRequest, gspb.KeepUserSessionResponse]) error {
	ctx := stream.Context()
	streamUss := callerUss(ctx)
	log.Printf("[GS] KeepUserSession opened uss=%s", streamUss)
	var sendMu sync.Mutex
	send := func(r *gspb.KeepUserSessionResponse) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(r)
	}

	var tmu sync.Mutex
	targets := map[string]*target{}
	w := &watcher{wake: make(chan struct{}, 8)}
	if s := g.lookup(streamUss); s != nil {
		w.gsid = s.gsid
	}
	var lastMu sync.Mutex
	last := map[string][]byte{} // tid|name -> deterministic bytes of the last delivered fields
	w.push = func(tid, kind, name string, m *commonpb.MapValue, force bool) error {
		b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		k := tid + "|" + name
		lastMu.Lock()
		same := !force && kind == "UPDATED" && string(last[k]) == string(b)
		last[k] = b
		lastMu.Unlock()
		if same {
			return nil
		}
		logPush(streamUss, tid, kind, name, m)
		dc := gspb.DocumentChange_UPDATED
		switch kind {
		case "EXIST":
			dc = gspb.DocumentChange_EXIST
		case "DELETED":
			dc = gspb.DocumentChange_DELETED
		}
		return send(change(tid, dc, doc(name, m, timestamppb.Now())))
	}
	g.mu.Lock()
	g.watchers[w] = struct{}{}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.watchers, w)
		gone := g.sess[streamUss]
		delete(g.sess, streamUss) // the stream IS the session: closed stream = player gone
		for _, n := range []string{"docs/__pgn/All/__pus/" + streamUss, "docs/__us/" + streamUss} {
			delete(g.store, w.gsid+"|"+n)
			g.deliver(w.gsid, n, "DELETED", nil) // tell the others (the host) this station is gone
		}
		g.mu.Unlock()
		if gone != nil {
			g.mm.dropMember(gone.gsid, gone.uid) // matchmaking membership follows the gamesync session
		}
		g.wakeFarm(w.gsid)
		log.Printf("[GS] KeepUserSession closed uss=%s", streamUss)
	}()

	// re-push every watched document when the farm changes (a write, a join, a leave)
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			case <-w.wake:
			}
			tmu.Lock()
			list := make([]*target, 0, len(targets))
			for _, t := range targets {
				list = append(list, t)
			}
			tmu.Unlock()
			for _, t := range list {
				names := t.docs
				if t.coll != "" {
					names = g.collectionDocs(t.coll, streamUss)
				}
				for _, n := range names {
					if w.push(t.tid, "UPDATED", n, g.fields(n, streamUss), false) != nil {
						return
					}
				}
			}
		}
	}()

	for {
		req, err := stream.Recv()
		if err != nil {
			return nil
		}
		if e := req.GetEcho(); e != "" {
			if err := send(&gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_Echo{Echo: e}}); err != nil {
				return err
			}
			continue
		}
		if ut := req.GetUpdateTarget(); ut != nil && ut.GetTarget() != nil {
			tg := ut.GetTarget()
			tid := lastSeg(tg.GetName())
			t := &target{tid: tid}
			if d := tg.GetDocuments(); d != nil {
				t.docs = d.GetDocuments()
			}
			if c := tg.GetCollection(); c != nil {
				t.coll = c.GetCollection()
			}
			log.Printf("[GS] update_target %s docs=%v collection=%q", tid, t.docs, t.coll)
			_ = send(targetChange(tid, gspb.TargetChange_UPDATED))
			names := t.docs
			if t.coll != "" {
				names = g.collectionDocs(t.coll, streamUss)
			}
			for _, n := range names {
				if err := w.push(tid, "EXIST", n, g.fields(n, streamUss), false); err != nil {
					return err
				}
			}
			log.Printf("[GS] target %s LISTED to=%s", tid, lastSeg(streamUss))
			if err := send(targetChange(tid, gspb.TargetChange_LISTED)); err != nil {
				return err
			}
			tmu.Lock()
			targets[tid] = t
			tmu.Unlock()
			w.tmu, w.targets = &tmu, targets
			continue
		}
		if dt := req.GetDeleteTarget(); dt != nil {
			tid := lastSeg(dt.GetName())
			tmu.Lock()
			delete(targets, tid)
			tmu.Unlock()
			_ = send(targetChange(tid, gspb.TargetChange_DELETED))
		}
	}
}

// logPush traces mailbox/station document deliveries (docs/__pgn/All/__stu/<uss>) so a lost
// signaling message can be told apart from one the client ignored.
func logPush(streamUss, tid, kind, name string, m *commonpb.MapValue) {
	if strings.Contains(name, "/__pus/") {
		keys := make([]string, 0, len(m.GetFields()))
		for k := range m.GetFields() {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		log.Printf("[GS] push to=%s tid=%s %s %s fields=%s upcsid=%v", lastSeg(streamUss), tid, kind, lastSeg(name), strings.Join(keys, ","), m.GetFields()["upcsid"].GetIntegerValue())
		return
	}
	if !strings.Contains(name, "/__stu/") && kind != "DELETED" {
		return
	}
	pl := m.GetFields()["pl"].GetBytesValue()
	log.Printf("[GS] push to=%s tid=%s %s %s from=%s pl=%dB", streamUss, tid, kind, lastSeg(name), lastSeg(m.GetFields()["susid"].GetStringValue()), len(pl))
}

func (g *gamesyncServer) wakeFarm(gsid string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for w := range g.watchers {
		if w.gsid == gsid || gsid == "" {
			select {
			case w.wake <- struct{}{}:
			default:
			}
		}
	}
}

// ---- store ----

// mergeFields returns a NEW map holding dst's fields overlaid with src's. It never mutates dst:
// stored documents are handed to gRPC marshalling on other goroutines outside the lock, and
// mutating them in place raced with that ("concurrent map iteration and map write" — the server
// died mid-join, 2026-09-02). Copy-on-write makes every stored MapValue immutable once published.
func mergeFields(dst, src *commonpb.MapValue) *commonpb.MapValue {
	out := &commonpb.MapValue{Fields: make(map[string]*commonpb.Value, len(dst.GetFields())+len(src.GetFields()))}
	for k, v := range dst.GetFields() {
		out.Fields[k] = v
	}
	for k, v := range src.GetFields() {
		out.Fields[k] = v
	}
	return out
}

func transformValue(ft *gspb.FieldTransform) *commonpb.Value {
	switch x := ft.GetTransformType().(type) {
	case *gspb.FieldTransform_SetServerValue:
		return &commonpb.Value{ValueType: &commonpb.Value_TimestampValue{TimestampValue: timestamppb.Now()}}
	case *gspb.FieldTransform_Increment:
		return x.Increment
	case *gspb.FieldTransform_Maximum:
		return x.Maximum
	case *gspb.FieldTransform_Minimum:
		return x.Minimum
	}
	return gsMap(nil)
}

func fieldKeys(m *commonpb.MapValue) string {
	keys := make([]string, 0, len(m.GetFields()))
	for k := range m.GetFields() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func opName(op *gspb.WriteOperation) string {
	switch {
	case op.GetUpdateDocument() != nil:
		return op.GetUpdateDocument().GetDocument().GetName()
	case op.GetMergeDocument() != nil:
		return op.GetMergeDocument().GetDocument().GetName()
	case op.GetTransformDocument() != nil:
		return op.GetTransformDocument().GetName()
	case op.GetDeleteDocument() != nil:
		return op.GetDeleteDocument().GetName()
	}
	return ""
}

func (g *gamesyncServer) apply(ctx context.Context, ops []*gspb.WriteOperation) ([]*gspb.WriteResult, string) {
	uss := callerUss(ctx)
	for _, op := range ops {
		if u := ussFromDocPath(opName(op)); u != "" && uss == "" {
			uss = u
		}
	}
	gsid := ""
	if s := g.lookup(uss); s != nil {
		gsid = s.gsid
	}
	results := make([]*gspb.WriteResult, 0, len(ops))
	g.mu.Lock()
	for _, op := range ops {
		k := gsid + "|" + opName(op)
		switch {
		case op.GetUpdateDocument() != nil:
			g.store[k] = mergeFields(g.store[k], op.GetUpdateDocument().GetDocument().GetFields())
			results = append(results, &gspb.WriteResult{ResultType: &gspb.WriteResult_UpdateResult{UpdateResult: &gspb.UpdateResult{}}})
		case op.GetMergeDocument() != nil:
			g.store[k] = mergeFields(g.store[k], op.GetMergeDocument().GetDocument().GetFields())
			results = append(results, &gspb.WriteResult{ResultType: &gspb.WriteResult_UpdateResult{UpdateResult: &gspb.UpdateResult{}}})
		case op.GetTransformDocument() != nil:
			fts := op.GetTransformDocument().GetFieldTransforms()
			vals := make([]*commonpb.Value, len(fts))
			cur := mergeFields(g.store[k], nil)
			for i, ft := range fts {
				vals[i] = transformValue(ft)
				cur.Fields[strings.Trim(ft.GetFieldPath(), "`")] = vals[i] // the game quotes field paths in backticks
			}
			g.store[k] = cur
			results = append(results, &gspb.WriteResult{ResultType: &gspb.WriteResult_TransformResult{TransformResult: &gspb.TransformResult{Results: vals}}})
		case op.GetDeleteDocument() != nil:
			delete(g.store, k)
			results = append(results, &gspb.WriteResult{ResultType: &gspb.WriteResult_DeleteResult{DeleteResult: &gspb.DeleteResult{}}})
		default:
			results = append(results, &gspb.WriteResult{})
		}
		kind := "UPDATED"
		if op.GetDeleteDocument() != nil {
			kind = "DELETED"
		}
		log.Printf("[GS] write farm=%s %s %s fields=%s", gsid, kind, opName(op), fieldKeys(g.store[k]))
		doc := g.store[k]
		if n := opName(op); doc != nil && (strings.Contains(n, "/__pus/") || strings.Contains(n, "/__us/")) {
			doc = g.withStationLocked(gsid, ussFromDocPath(n), doc) // never deliver placeholder ids (upcsid=0)
		}
		g.deliver(gsid, opName(op), kind, doc)
		if os.Getenv("NPLN_GS_DIAG") != "" {
			log.Printf("[GS][DIAG] %s = %s", opName(op), prototext.MarshalOptions{Multiline: false}.Format(g.store[k]))
		}
		g.mirrorProps(gsid, g.store[k])
	}
	g.mu.Unlock()
	return results, gsid
}

// mirrorProps republishes a written document's `prp` map (the Pia session properties — the
// host's UpdateNetworkProperty job writes `prp._Pia_SystemData` with the lobby application data
// appended, plus `ip`) into the farm's GameSession, so QueryGameSessions hands searchers the
// updated blob. Nintendo's backend does the same (reference server: __gs/m.prp).
// Caller holds g.mu; lock order g.mu -> g.mm.mu as in roomFields.
func (g *gamesyncServer) mirrorProps(gsid string, doc *commonpb.MapValue) {
	prp, ip := doc.GetFields()["prp"].GetMapValue(), doc.GetFields()["ip"]
	if gsid == "" || (prp == nil && ip == nil) {
		return
	}
	g.mm.mu.Lock()
	defer g.mm.mu.Unlock()
	s := g.mm.sessions[gsid]
	if s == nil {
		return
	}
	if prp != nil {
		s.Properties = mergeFields(s.Properties, prp)
		log.Printf("[GS] mirror prp -> farm %s properties=%s", gsid, fieldKeys(s.Properties))
	}
	if ip != nil {
		s.IsPublic = ip.GetBooleanValue()
	}
}

func (g *gamesyncServer) WriteDocuments(ctx context.Context, req *gspb.WriteDocumentsRequest) (*gspb.WriteDocumentsResponse, error) {
	res, gsid := g.apply(ctx, req.GetWriteOperations())
	g.wakeFarm(gsid)
	return &gspb.WriteDocumentsResponse{WriteResults: res, CommitTime: timestamppb.Now()}, nil
}

func (g *gamesyncServer) LazyWriteDocuments(ctx context.Context, req *gspb.LazyWriteDocumentsRequest) (*gspb.LazyWriteDocumentsResponse, error) {
	var ops []*gspb.WriteOperation
	for _, l := range req.GetWriteOperations() {
		switch x := l.GetOperationType().(type) {
		case *gspb.LazyWriteOperation_MergeDocument:
			ops = append(ops, &gspb.WriteOperation{OperationType: &gspb.WriteOperation_MergeDocument{MergeDocument: x.MergeDocument}})
		case *gspb.LazyWriteOperation_TransformDocument:
			ops = append(ops, &gspb.WriteOperation{OperationType: &gspb.WriteOperation_TransformDocument{TransformDocument: x.TransformDocument}})
		}
	}
	_, gsid := g.apply(ctx, ops)
	g.wakeFarm(gsid)
	return &gspb.LazyWriteDocumentsResponse{CommitTime: timestamppb.Now()}, nil
}

func (g *gamesyncServer) BeginTransaction(ctx context.Context, req *gspb.BeginTransactionRequest) (*gspb.BeginTransactionResponse, error) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &gspb.BeginTransactionResponse{Transaction: b}, nil
}

func (g *gamesyncServer) CommitTransaction(ctx context.Context, req *gspb.CommitTransactionRequest) (*gspb.CommitTransactionResponse, error) {
	res, gsid := g.apply(ctx, req.GetWriteOperations())
	g.wakeFarm(gsid)
	return &gspb.CommitTransactionResponse{WriteResults: res, CommitTime: timestamppb.Now()}, nil
}

func (g *gamesyncServer) RollbackTransaction(ctx context.Context, req *gspb.RollbackTransactionRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (g *gamesyncServer) GetDocument(ctx context.Context, req *gspb.GetDocumentRequest) (*gspb.Document, error) {
	log.Printf("[GS] GetDocument %s", req.GetName())
	return doc(req.GetName(), g.fields(req.GetName(), callerUss(ctx)), timestamppb.Now()), nil
}

func (g *gamesyncServer) ReadDocuments(ctx context.Context, req *gspb.ReadDocumentsRequest) (*gspb.ReadDocumentsResponse, error) {
	now := timestamppb.Now()
	out := &gspb.ReadDocumentsResponse{ReadTime: now}
	for _, n := range req.GetDocuments() {
		out.Results = append(out.Results, &gspb.ReadResult{Result: &gspb.ReadResult_Found{Found: doc(n, g.fields(n, callerUss(ctx)), now)}})
	}
	log.Printf("[GS] ReadDocuments %v", req.GetDocuments())
	return out, nil
}

func (g *gamesyncServer) ListDocuments(ctx context.Context, req *gspb.ListDocumentsRequest) (*gspb.ListDocumentsResponse, error) {
	now := timestamppb.Now()
	out := &gspb.ListDocumentsResponse{}
	uss := callerUss(ctx)
	for _, n := range g.collectionDocs(req.GetParent(), uss) {
		out.Documents = append(out.Documents, doc(n, g.fields(n, uss), now))
	}
	log.Printf("[GS] ListDocuments %s -> %d", req.GetParent(), len(out.Documents))
	return out, nil
}

func (g *gamesyncServer) QueryCollectionIds(ctx context.Context, req *gspb.QueryCollectionIdsRequest) (*gspb.QueryCollectionIdsResponse, error) {
	gsid := ""
	if s := g.lookup(callerUss(ctx)); s != nil {
		gsid = s.gsid
	}
	prefix := gsid + "|" + strings.TrimRight(req.GetDocument(), "/") + "/"
	ids := map[string]bool{}
	g.mu.Lock()
	for k := range g.store {
		if strings.HasPrefix(k, prefix) {
			ids[strings.SplitN(strings.TrimPrefix(k, prefix), "/", 2)[0]] = true
		}
	}
	g.mu.Unlock()
	out := &gspb.QueryCollectionIdsResponse{}
	for id := range ids {
		out.CollectionIds = append(out.CollectionIds, id)
	}
	sort.Strings(out.CollectionIds)
	return out, nil
}

func (g *gamesyncServer) CreateRound(ctx context.Context, req *gspb.CreateRoundRequest) (*gspb.Round, error) {
	if r := req.GetRound(); r != nil {
		return r, nil
	}
	return &gspb.Round{}, nil
}
