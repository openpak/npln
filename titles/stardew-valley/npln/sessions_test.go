package npln

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
)

func bearerCtx(pid uint64, uid string) context.Context {
	tok := newToken(pid, Tenant+"/users/"+uid, Tenant)
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "bearer "+tok.AccessToken))
}

// One check over the session trust boundary: the caller is named by the bearer and not by the
// uid header, only the host may sync a farm, and an expired token is no identity at all.
func TestSessionIdentityIsTheBearer(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NPLN_TURN_SECRET", "s")
	g := newSessionServer()

	// A uid header with no bearer is refused.
	spoof := metadata.NewIncomingContext(context.Background(), metadata.Pairs("uid", "u-victim"))
	if _, err := g.CreateGameSessionCreationTicket(spoof, &mmpb.CreateGameSessionCreationTicketRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("uid header alone created a farm: %v", err)
	}

	host := bearerCtx(1, "u-host")
	tk, err := g.CreateGameSessionCreationTicket(host, &mmpb.CreateGameSessionCreationTicketRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var gsName string
	for id := range g.sessions {
		gsName = Tenant + "/gameSessions/" + id
	}
	if mem := g.members[lastSeg(gsName)]; len(mem) != 1 || lastSeg(mem[0].GetUser()) != "u-host" {
		t.Fatalf("host not named by its bearer: %+v (ticket %s)", mem, tk.GetName())
	}

	// A joiner claiming the host's uid in metadata is still the joiner.
	joinTok := newToken(2, Tenant+"/users/u-joiner", Tenant)
	joiner := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "bearer "+joinTok.AccessToken, "uid", "u-host"))
	if _, err := g.JoinGameSession(joiner, &mmpb.JoinGameSessionRequest{Name: gsName}); err != nil {
		t.Fatal(err)
	}
	sync := &mmpb.SyncGameSessionRequest{GameSession: &mmpb.GameSession{Name: gsName, MaxParticipantCount: 1}}
	if _, err := g.SyncGameSession(joiner, sync); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("joiner synced the host's farm: %v", err)
	}
	if _, err := g.SyncGameSession(host, sync); err != nil {
		t.Fatalf("host sync refused: %v", err)
	}

	// Expiry is enforced on every use.
	expired := signJWT(map[string]any{"alg": "ES256"}, map[string]any{
		"exp": time.Now().Add(-time.Second).Unix(), "sub": "u-host", "npln": map[string]any{"ext_id": "1"},
	})
	if _, ok := verifyJWT(expired); ok {
		t.Fatal("expired token verified")
	}
}
