package npln

// gamesync — nn.npln.gamesync.v1.Gamesync, the signalling mailbox.
//
// Splatoon 3 RELAYS. This service is a small document store the consoles watch, and its whole job
// is to copy each console's Pia contact blob into the documents its peers are watching. Each
// console writes its own blob with WriteDocuments; each console watches a target with
// KeepUserSession and is pushed its peers' blobs. The match then runs console-to-console over Pia
// — nothing here holds game state. (docs/design.md, docs/provenance.md.)
//
// Two measured invariants shape the code:
//
//   - The host does not write first: after subscribing it WAITS for its own user-session document.
//     A silent subscription parks it on a connecting screen forever, so the target's existing
//     documents are enumerated immediately, followed by a TargetChange so the client knows the
//     enumeration is current.
//   - A document must be pushed as EXISTING, with a concrete typed value for every field. An
//     empty-fields document was measured crashing the game's worker (2162-0001), so the seeded
//     session document below carries only concrete Values, never an empty map.

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "openpak/splatoon-3/proto/common"
	gspb "openpak/splatoon-3/proto/gamesync/v1"
)

// concrete typed Values. Every field of a pushed document must be one of these — never a bare,
// type-less field, which crashes the worker.
func sv(s string) *commonpb.Value {
	return &commonpb.Value{ValueType: &commonpb.Value_StringValue{StringValue: s}}
}
func iv(i int64) *commonpb.Value {
	return &commonpb.Value{ValueType: &commonpb.Value_IntegerValue{IntegerValue: i}}
}
func bv(b bool) *commonpb.Value {
	return &commonpb.Value{ValueType: &commonpb.Value_BooleanValue{BooleanValue: b}}
}

// ---- the mailbox ----

type watcher struct {
	targets map[string]*gspb.Target // target id -> target
	ch      chan *gspb.KeepUserSessionResponse
	seen    map[string]bool // document names already pushed to this watcher as EXIST
}

// mailbox is the shared document store the consoles rendezvous through. In memory, because a relay
// keeps no durable state: the documents live only as long as the session that is signalling.
//
// ponytail: one lock, a bounded per-watcher channel that drops under backpressure. A lobby will
// not saturate it; give a watcher a real queue if a match ever does.
var mail = struct {
	sync.Mutex
	docs     map[string]*gspb.Document
	watchers map[int]*watcher
	next     int
}{docs: map[string]*gspb.Document{}, watchers: map[int]*watcher{}}

func matchesTarget(t *gspb.Target, name string) bool {
	if d := t.GetDocuments(); d != nil {
		for _, n := range d.GetDocuments() {
			if n == name {
				return true
			}
		}
	}
	if c := t.GetCollection(); c != nil {
		return strings.HasPrefix(name, c.GetCollection()+"/")
	}
	return false
}

// pushDoc notifies every watcher whose target matches a written/changed document. Caller holds the
// lock. EXIST the first time a watcher sees a document, UPDATED afterwards.
func pushDoc(doc *gspb.Document) {
	for _, w := range mail.watchers {
		for id, t := range w.targets {
			if !matchesTarget(t, doc.Name) {
				continue
			}
			ct := gspb.DocumentChange_UPDATED
			if !w.seen[doc.Name] {
				ct = gspb.DocumentChange_EXIST
				w.seen[doc.Name] = true
			}
			trySend(w, &gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_DocumentChange{
				DocumentChange: &gspb.DocumentChange{TargetId: id, DocumentChangeType: ct, Document: doc},
			}})
		}
	}
}

func pushDelete(name string) {
	for _, w := range mail.watchers {
		for id, t := range w.targets {
			if matchesTarget(t, name) && w.seen[name] {
				delete(w.seen, name)
				trySend(w, &gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_DocumentChange{
					DocumentChange: &gspb.DocumentChange{TargetId: id, DocumentChangeType: gspb.DocumentChange_DELETED, Document: &gspb.Document{Name: name}},
				}})
			}
		}
	}
}

func trySend(w *watcher, r *gspb.KeepUserSessionResponse) {
	select {
	case w.ch <- r:
	default: // watcher is behind; drop rather than block the writer
	}
}

// putDoc stores a document and notifies watchers. Fields must already be concrete.
func putDoc(doc *gspb.Document) {
	now := timestamppb.Now()
	mail.Lock()
	if old := mail.docs[doc.Name]; old != nil {
		doc.CreateTime = old.CreateTime
	} else {
		doc.CreateTime = now
	}
	doc.UpdateTime = now
	mail.docs[doc.Name] = doc
	pushDoc(doc)
	mail.Unlock()
}

type gamesync struct {
	gspb.UnimplementedGamesyncServer
}

// IssueToken exchanges the matchmaking id-token for a session token, and seeds the session
// document the host is about to wait for. The seeded document carries the game session's mutable
// data with a concrete typed value per key — this is the document whose absence, or whose empty
// fields, leaves the host stuck or crashes it.
func (g *gamesync) IssueToken(_ context.Context, req *gspb.IssueTokenRequest) (*gspb.IssueTokenResponse, error) {
	us := req.GetUserSession()
	gs, ok := sessionForIDToken(req.GetMatchmakingIdToken())
	if ok && us != "" {
		doc := &gspb.Document{
			Name: us + "/SessionInfo",
			Fields: &commonpb.MapValue{Fields: map[string]*commonpb.Value{
				"game_session":         sv(gs.GetName()),
				"host":                 sv(gs.GetHost()),
				"port":                 iv(int64(gs.GetPort())),
				"max_participants":     iv(int64(gs.GetMaxParticipantCount())),
				"current_participants": iv(int64(gs.GetCurrentParticipantCount())),
				"is_public":            bv(gs.GetIsPublic()),
				"has_password":         bv(gs.GetPassword() != ""),
			}},
		}
		putDoc(doc)
		log.Printf("[gamesync] IssueToken user_session=%s -> seeded %s (%d fields)", short(us), doc.Name, len(doc.Fields.Fields))
	} else {
		log.Printf("[gamesync] IssueToken user_session=%s: no session for id-token (mmtoken=%.12s…)", short(us), req.GetMatchmakingIdToken())
	}
	return &gspb.IssueTokenResponse{Token: &gspb.Token{
		UserSession:  us,
		AccessToken:  newID(),
		RefreshToken: newID(),
		Ttl:          durationpb.New(2 * time.Hour),
	}}, nil
}

func (g *gamesync) RefreshToken(_ context.Context, req *gspb.RefreshTokenRequest) (*gspb.RefreshTokenResponse, error) {
	return &gspb.RefreshTokenResponse{Token: &gspb.Token{
		UserSession: req.GetUserSession(), AccessToken: newID(), RefreshToken: newID(), Ttl: durationpb.New(2 * time.Hour),
	}}, nil
}

// GetDocument stays unary and answers NotFound for an absent document — two attempts to imitate an
// observed empty success both crashed the game, so a missing document is a real NotFound.
func (g *gamesync) GetDocument(_ context.Context, req *gspb.GetDocumentRequest) (*gspb.Document, error) {
	mail.Lock()
	doc := mail.docs[req.GetName()]
	mail.Unlock()
	if doc == nil {
		log.Printf("[gamesync] GetDocument %q -> NotFound", short(req.GetName()))
		return nil, status.Errorf(codes.NotFound, "document %q", req.GetName())
	}
	return doc, nil
}

func (g *gamesync) ReadDocuments(_ context.Context, req *gspb.ReadDocumentsRequest) (*gspb.ReadDocumentsResponse, error) {
	out := &gspb.ReadDocumentsResponse{ReadTime: timestamppb.Now()}
	mail.Lock()
	for _, n := range req.GetDocuments() {
		if doc := mail.docs[n]; doc != nil {
			out.Results = append(out.Results, &gspb.ReadResult{Result: &gspb.ReadResult_Found{Found: doc}})
		} else {
			out.Results = append(out.Results, &gspb.ReadResult{Result: &gspb.ReadResult_Missing{Missing: n}})
		}
	}
	mail.Unlock()
	return out, nil
}

func (g *gamesync) ListDocuments(_ context.Context, req *gspb.ListDocumentsRequest) (*gspb.ListDocumentsResponse, error) {
	out := &gspb.ListDocumentsResponse{}
	prefix := req.GetParent() + "/"
	mail.Lock()
	for name, doc := range mail.docs {
		if strings.HasPrefix(name, prefix) {
			out.Documents = append(out.Documents, doc)
		}
	}
	mail.Unlock()
	return out, nil
}

func (g *gamesync) QueryCollectionIds(_ context.Context, req *gspb.QueryCollectionIdsRequest) (*gspb.QueryCollectionIdsResponse, error) {
	seen := map[string]bool{}
	out := &gspb.QueryCollectionIdsResponse{}
	prefix := req.GetDocument() + "/"
	mail.Lock()
	for name := range mail.docs {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			if i := strings.IndexByte(rest, '/'); i > 0 && !seen[rest[:i]] {
				seen[rest[:i]] = true
				out.CollectionIds = append(out.CollectionIds, rest[:i])
			}
		}
	}
	mail.Unlock()
	return out, nil
}

// WriteDocuments applies the console's writes to the mailbox. The write that matters is the
// console depositing its own Pia contact blob; storing it pushes it to every peer watching.
func (g *gamesync) WriteDocuments(_ context.Context, req *gspb.WriteDocumentsRequest) (*gspb.WriteDocumentsResponse, error) {
	out := &gspb.WriteDocumentsResponse{CommitTime: timestamppb.Now()}
	for _, op := range req.GetWriteOperations() {
		out.WriteResults = append(out.WriteResults, applyWrite(op))
	}
	return out, nil
}

func applyWrite(op *gspb.WriteOperation) *gspb.WriteResult {
	switch {
	case op.GetUpdateDocument() != nil:
		putDoc(op.GetUpdateDocument().GetDocument())
		return &gspb.WriteResult{ResultType: &gspb.WriteResult_UpdateResult{UpdateResult: &gspb.UpdateResult{}}}
	case op.GetMergeDocument() != nil:
		mergeDoc(op.GetMergeDocument().GetDocument())
		return &gspb.WriteResult{ResultType: &gspb.WriteResult_UpdateResult{UpdateResult: &gspb.UpdateResult{}}}
	case op.GetDeleteDocument() != nil:
		name := op.GetDeleteDocument().GetName()
		mail.Lock()
		delete(mail.docs, name)
		pushDelete(name)
		mail.Unlock()
		return &gspb.WriteResult{ResultType: &gspb.WriteResult_DeleteResult{DeleteResult: &gspb.DeleteResult{}}}
	default:
		// TransformDocument (server-value/increment/min/max) is not part of the mailbox flow; accept
		// it so the write set commits rather than erroring.
		return &gspb.WriteResult{ResultType: &gspb.WriteResult_UpdateResult{UpdateResult: &gspb.UpdateResult{}}}
	}
}

// mergeDoc merges fields into an existing document rather than replacing it.
func mergeDoc(doc *gspb.Document) {
	if doc == nil {
		return
	}
	mail.Lock()
	cur := mail.docs[doc.Name]
	mail.Unlock()
	if cur == nil || cur.Fields == nil {
		putDoc(doc)
		return
	}
	merged := &gspb.Document{Name: doc.Name, Fields: &commonpb.MapValue{Fields: map[string]*commonpb.Value{}}}
	for k, v := range cur.Fields.GetFields() {
		merged.Fields.Fields[k] = v
	}
	for k, v := range doc.GetFields().GetFields() {
		merged.Fields.Fields[k] = v
	}
	putDoc(merged)
}

func (g *gamesync) LazyWriteDocuments(_ context.Context, req *gspb.LazyWriteDocumentsRequest) (*gspb.LazyWriteDocumentsResponse, error) {
	for _, op := range req.GetWriteOperations() {
		switch {
		case op.GetMergeDocument() != nil:
			mergeDoc(op.GetMergeDocument().GetDocument())
		}
	}
	return &gspb.LazyWriteDocumentsResponse{CommitTime: timestamppb.Now()}, nil
}

// Transactions: the mailbox is a single-writer-per-document rendezvous, not a database, so a
// transaction is just its writes applied on commit. A synthetic token satisfies the client.
func (g *gamesync) BeginTransaction(_ context.Context, _ *gspb.BeginTransactionRequest) (*gspb.BeginTransactionResponse, error) {
	return &gspb.BeginTransactionResponse{Transaction: []byte(newID())}, nil
}

func (g *gamesync) CommitTransaction(_ context.Context, req *gspb.CommitTransactionRequest) (*gspb.CommitTransactionResponse, error) {
	out := &gspb.CommitTransactionResponse{CommitTime: timestamppb.Now()}
	for _, op := range req.GetWriteOperations() {
		out.WriteResults = append(out.WriteResults, applyWrite(op))
	}
	return out, nil
}

func (g *gamesync) RollbackTransaction(_ context.Context, _ *gspb.RollbackTransactionRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

// CreateRound is a match-round marker in the reference's Splatfest/telemetry flow. We keep no round
// state; echoing the round back keeps the call succeeding.
func (g *gamesync) CreateRound(_ context.Context, req *gspb.CreateRoundRequest) (*gspb.Round, error) {
	return req.GetRound(), nil
}

// KeepUserSession is the watch: a bidirectional stream that holds the session connected and pushes
// document changes as they happen. All Sends go through the single select loop below — a gRPC
// stream rejects concurrent Sends — while a reader goroutine turns each request into responses
// posted on the same channel.
func (g *gamesync) KeepUserSession(stream gspb.Gamesync_KeepUserSessionServer) error {
	ctx := stream.Context()
	w := &watcher{targets: map[string]*gspb.Target{}, ch: make(chan *gspb.KeepUserSessionResponse, 32), seen: map[string]bool{}}
	mail.Lock()
	id := mail.next
	mail.next++
	mail.watchers[id] = w
	mail.Unlock()
	defer func() {
		mail.Lock()
		delete(mail.watchers, id)
		mail.Unlock()
	}()

	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				return
			}
			switch {
			case req.GetUpdateTarget() != nil:
				t := req.GetUpdateTarget().GetTarget()
				enumerateTarget(w, t)
			case req.GetDeleteTarget() != nil:
				name := req.GetDeleteTarget().GetName()
				mail.Lock()
				delete(w.targets, name)
				mail.Unlock()
				trySend(w, &gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_TargetChange{
					TargetChange: &gspb.TargetChange{TargetId: name, TargetChangeType: gspb.TargetChange_DELETED},
				}})
			default:
				if _, ok := req.GetRequestType().(*gspb.KeepUserSessionRequest_Echo); ok {
					trySend(w, &gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_Echo{Echo: req.GetEcho()}})
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case r := <-w.ch:
			if stream.Send(r) != nil {
				return nil
			}
		}
	}
}

// enumerateTarget registers a watch and immediately pushes the documents that already match it,
// then a TargetChange LISTED so the client knows the enumeration is current — the difference
// between the host reading its session data and parking on a connecting screen.
func enumerateTarget(w *watcher, t *gspb.Target) {
	if t == nil {
		return
	}
	mail.Lock()
	w.targets[t.GetName()] = t
	var existing []*gspb.Document
	for _, doc := range mail.docs {
		if matchesTarget(t, doc.Name) {
			existing = append(existing, doc)
		}
	}
	for _, doc := range existing {
		w.seen[doc.Name] = true
		trySend(w, &gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_DocumentChange{
			DocumentChange: &gspb.DocumentChange{TargetId: t.GetName(), DocumentChangeType: gspb.DocumentChange_EXIST, Document: doc},
		}})
	}
	mail.Unlock()
	trySend(w, &gspb.KeepUserSessionResponse{ResponseType: &gspb.KeepUserSessionResponse_TargetChange{
		TargetChange: &gspb.TargetChange{TargetId: t.GetName(), TargetChangeType: gspb.TargetChange_LISTED},
	}})
	log.Printf("[gamesync] watch target %q -> %d existing document(s)", short(t.GetName()), len(existing))
}
