package npln

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonpb "github.com/openpak/npln/proto/common"
	gspb "github.com/openpak/npln/proto/gamesync/v1"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
)

func bearerCtx(pid uint64, uid string) context.Context {
	tok := newToken(pid, Tenant+"/users/"+uid, Tenant)
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "bearer "+tok.GetAccessToken()))
}

// One check over the room trust boundary: only the host syncs a room, a joiner claiming the
// host's uid in metadata stays the joiner, a full room refuses, and an expired token is nobody.
func TestRoomIdentityIsTheBearer(t *testing.T) {
	resetStores()
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	gss := &gameSessionService{}
	host := bearerCtx(1, "u-host")
	tk, err := gss.CreateGameSessionCreationTicket(host, &mmpb.CreateGameSessionCreationTicketRequest{Parent: Tenant,
		GameSessionCreationTicket: &mmpb.GameSessionCreationTicket{GameSession: &mmpb.GameSession{MaxParticipantCount: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	name := tk.GetGameSession().GetName()
	joinTok := newToken(2, Tenant+"/users/u-joiner", Tenant)
	joiner := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "bearer "+joinTok.GetAccessToken(), "uid", "u-host"))
	j, err := gss.JoinGameSession(joiner, &mmpb.JoinGameSessionRequest{Name: name})
	if err != nil || lastSeg(j.GetMatchedUserSessions()[0].GetUserDefinition().GetUser()) != "u-joiner" {
		t.Fatalf("joiner not named by its bearer: %+v %v", j, err)
	}
	if _, err := gss.JoinGameSession(bearerCtx(3, "u-late"), &mmpb.JoinGameSessionRequest{Name: name}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("full room accepted a third player: %v", err)
	}
	sync := &mmpb.SyncGameSessionRequest{GameSession: &mmpb.GameSession{Name: name, MaxParticipantCount: 4}}
	if _, err := gss.SyncGameSession(joiner, sync); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("joiner synced the host's room: %v", err)
	}
	if _, err := gss.SyncGameSession(host, sync); err != nil {
		t.Fatalf("host sync refused: %v", err)
	}
	expired := signJWT(map[string]any{"alg": "ES256"}, map[string]any{
		"exp": time.Now().Add(-time.Second).Unix(), "sub": "u-host", "npln": map[string]any{"ext_id": "1"}})
	if _, ok := verifyJWT(expired); ok {
		t.Fatal("expired token verified")
	}
}

func resetStores() {
	store.sessions = map[string]*mmpb.GameSession{}
	store.alias = map[string]string{}
	store.idToken = map[string]string{}
	store.pending = map[string][]*mmpb.MatchmakingTicket{}
	store.tickets = map[string]*mmpb.MatchmakingTicket{}
	mail.docs = map[string]*gspb.Document{}
	mail.watchers = map[int]*watcher{}
}

// The pool only forms a match at NPLN_MATCH_SIZE tickets: below it every ticket stays SEARCHING (a
// battle refuses a roster below the mode's size), at it they all resolve to ONE game session with a
// per-user id-token gamesync can exchange.
func TestMatchmakingPoolFormsAtSize(t *testing.T) {
	resetStores()
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NPLN_MATCH_SIZE", "3")
	mm := &matchmaker{}

	// A ticket is for the bearer; a uid header alone is nobody.
	spoof := metadata.NewIncomingContext(context.Background(), metadata.Pairs("uid", "u-victim"))
	if _, err := mm.CreateMatchmakingTicket(spoof, &mmpb.CreateMatchmakingTicketRequest{Parent: Tenant}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("uid header alone opened a ticket: %v", err)
	}

	t1, _ := mm.CreateMatchmakingTicket(bearerCtx(1, "u-a"), &mmpb.CreateMatchmakingTicketRequest{Parent: Tenant})
	t2, _ := mm.CreateMatchmakingTicket(bearerCtx(2, "u-b"), &mmpb.CreateMatchmakingTicketRequest{Parent: Tenant})
	if t1.State != mmpb.MatchmakingTicket_SEARCHING || t2.State != mmpb.MatchmakingTicket_SEARCHING {
		t.Fatalf("below size the tickets must keep searching: %v %v", t1.State, t2.State)
	}

	// The third ticket completes the pool; all three resolve to the same session.
	mm.CreateMatchmakingTicket(bearerCtx(3, "u-c"), &mmpb.CreateMatchmakingTicketRequest{Parent: Tenant})
	for _, name := range []string{t1.Name, t2.Name} {
		tk := store.tickets[name]
		if tk.State != mmpb.MatchmakingTicket_SUCCEEDED {
			t.Fatalf("ticket %s state %v, want SUCCEEDED", short(name), tk.State)
		}
		if tk.GameSession == nil || len(tk.MatchedUserSessions) != 1 || tk.MatchedUserSessions[0].MatchmakingIdToken == "" {
			t.Fatalf("succeeded ticket missing session/id-token: %+v", tk)
		}
	}
	if store.tickets[t1.Name].GameSession.Name != store.tickets[t2.Name].GameSession.Name {
		t.Fatal("pooled tickets landed in different sessions")
	}

	// The id-token the ticket carries must resolve to that session — gamesync depends on it.
	idTok := store.tickets[t1.Name].MatchedUserSessions[0].MatchmakingIdToken
	if gs, ok := sessionForIDToken(idTok); !ok || gs.Name != store.tickets[t1.Name].GameSession.Name {
		t.Fatalf("id-token did not resolve to the match session")
	}
}

// AllocateIceServerSet hands back a concrete STUN + TURN set, with the TURN password derived by
// coturn's REST scheme: base64(HMAC-SHA1(secret, "<expiry>:<user>")).
func TestAllocateIceServerSetCoturnCreds(t *testing.T) {
	resetStores()
	t.Setenv("NPLN_TURN_SECRET", "s3cr3t")
	t.Setenv("NPLN_STUN_HOST", "stun.example")
	gss := &gameSessionService{}
	set, err := gss.AllocateIceServerSet(context.Background(), &mmpb.AllocateIceServerSetRequest{Tenant: Tenant, User: Tenant + "/users/u-abc"})
	if err != nil || set.StunServer == nil || len(set.TurnServers) == 0 {
		t.Fatalf("incomplete ICE set: %+v %v", set, err)
	}
	turn := set.TurnServers[0]
	mac := hmac.New(sha1.New, []byte("s3cr3t"))
	mac.Write([]byte(turn.Username))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); turn.Password != want {
		t.Fatalf("TURN password %q, want %q for username %q", turn.Password, want, turn.Username)
	}
	if !strings.HasSuffix(turn.Username, ":u-abc") {
		t.Fatalf("TURN username %q should end in the user id", turn.Username)
	}
}

// The mailbox relay: a watcher on a collection is pushed a peer's document the moment it is written,
// as a concrete-fielded DocumentChange — this is the copy-each-peer's-blob behaviour the consoles
// rendezvous through.
func TestMailboxRelaysPeerDoc(t *testing.T) {
	resetStores()
	w := &watcher{targets: map[string]*gspb.Target{}, ch: make(chan *gspb.KeepUserSessionResponse, 8), seen: map[string]bool{}}
	mail.watchers[1] = w
	target := &gspb.Target{Name: "target-1", TargetType: &gspb.Target_Collection{Collection: &gspb.CollectionTarget{Collection: Tenant + "/rooms/r1/contacts"}}}
	enumerateTarget(w, target)
	// enumeration of an empty collection: only the TargetChange LISTED so far.
	if r := <-w.ch; r.GetTargetChange() == nil || r.GetTargetChange().TargetChangeType != gspb.TargetChange_LISTED {
		t.Fatalf("expected a LISTED target change, got %+v", r)
	}

	// A peer writes its contact blob into the watched collection.
	putDoc(&gspb.Document{Name: Tenant + "/rooms/r1/contacts/peerA", Fields: mustFields()})
	r := <-w.ch
	dc := r.GetDocumentChange()
	if dc == nil || dc.DocumentChangeType != gspb.DocumentChange_EXIST || !strings.HasSuffix(dc.Document.Name, "/peerA") {
		t.Fatalf("watcher did not receive the peer's doc as EXIST: %+v", r)
	}
	if dc.Document.Fields == nil || len(dc.Document.Fields.Fields) == 0 {
		t.Fatal("relayed document has no concrete fields")
	}

	// A doc outside the collection must not reach this watcher.
	putDoc(&gspb.Document{Name: Tenant + "/rooms/OTHER/contacts/peerB", Fields: mustFields()})
	select {
	case r := <-w.ch:
		t.Fatalf("watcher received a doc outside its collection: %+v", r)
	default:
	}
}

func mustFields() *commonpb.MapValue {
	return &commonpb.MapValue{Fields: map[string]*commonpb.Value{"k": sv("v")}}
}
