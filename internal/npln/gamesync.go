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
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/NextendoNetwork/stardew-nextendo/proto/common"
	gspb "github.com/NextendoNetwork/stardew-nextendo/proto/gamesync/v1"
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
	gsid, addr, port, maxp, pw, pub := "", envOr("NPLN_RELAY_HOST", "127.0.0.1"), envInt("NPLN_RELAY_PORT", 18501), int32(4), "", true
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
		return stored
	}
	switch {
	case strings.Contains(name, "/__stu/"):
		return stateUserFields(s, uss)
	case strings.Contains(name, "/__pus/"), strings.Contains(name, "/__us/"), strings.HasSuffix(name, "/__gs/s"):
		return userSessionFields(s)
	case strings.Contains(name, "/__stg/"):
		return g.mutableFields(s)
	case strings.Contains(name, "/__gs/"):
		if m := g.roomFields(lastSeg(name), s); m != nil {
			return m
		}
	}
	return &commonpb.MapValue{Fields: map[string]*commonpb.Value{"id": gsStr("")}}
}

// roomFields are the room documents docs/__gs/{f,m,r,n,ck} in the schema the reference server
// measured on Nintendo's own answers: f = the session address block, m = the room settings,
// r = participant counters, n = lifetime, ck = the password key.
func (g *gamesyncServer) roomFields(sub string, s *gsSession) *commonpb.MapValue {
	gsid, addr, port, maxp, pw := "", envOr("NPLN_RELAY_HOST", "127.0.0.1"), envInt("NPLN_RELAY_PORT", 18501), int32(4), ""
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

	type target struct {
		tid, coll string
		docs      []string
	}
	var tmu sync.Mutex
	targets := map[string]*target{}
	w := &watcher{wake: make(chan struct{}, 8)}
	if s := g.lookup(streamUss); s != nil {
		w.gsid = s.gsid
	}
	g.mu.Lock()
	g.watchers[w] = struct{}{}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.watchers, w)
		delete(g.sess, streamUss) // the stream IS the session: closed stream = player gone
		g.mu.Unlock()
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
			now := timestamppb.Now()
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
					if send(change(t.tid, gspb.DocumentChange_UPDATED, doc(n, g.fields(n, streamUss), now))) != nil {
						return
					}
				}
				if t.coll != "" {
					_ = send(targetChange(t.tid, gspb.TargetChange_UPDATED))
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
			now := timestamppb.Now()
			names := t.docs
			if t.coll != "" {
				names = g.collectionDocs(t.coll, streamUss)
			}
			for _, n := range names {
				if err := send(change(tid, gspb.DocumentChange_EXIST, doc(n, g.fields(n, streamUss), now))); err != nil {
					return err
				}
			}
			if err := send(targetChange(tid, gspb.TargetChange_LISTED)); err != nil {
				return err
			}
			tmu.Lock()
			targets[tid] = t
			tmu.Unlock()
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

func mergeFields(dst, src *commonpb.MapValue) *commonpb.MapValue {
	if dst == nil || dst.Fields == nil {
		dst = &commonpb.MapValue{Fields: map[string]*commonpb.Value{}}
	}
	for k, v := range src.GetFields() {
		dst.Fields[k] = v
	}
	return dst
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
		log.Printf("[GS] write farm=%s %s fields=%s", gsid, opName(op), fieldKeys(g.store[k]))
	}
	g.mu.Unlock()
	return results, gsid
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
