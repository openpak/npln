package npln

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authpb "github.com/openpak/npln/proto/auth/v1"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
)

// An id token naming a version below nx-baas's latest is refused with FAILED_PRECONDITION
// (2321-3072); the latest passes and is remembered; an nx-baas that cannot answer refuses no one.
func TestOutdatedVersionIsRefused(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	latest := "00180000"
	adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/switch/identity":
			fmt.Fprint(w, `{"pid":7,"baas_user_id":"9198f6a6930b5fd1","nickname":"P"}`)
		case "/internal/title-version":
			if latest == "" {
				http.NotFound(w, r)
				return
			}
			av := r.URL.Query().Get("version")
			fmt.Fprintf(w, `{"latest":%q,"update_required":%v}`, latest, len(av) == 8 && av < latest)
		}
	}))
	defer adapter.Close()
	old := adapterURL
	adapterURL = adapter.URL
	defer func() { adapterURL = old }()

	tok := func(av string) *authpb.ExternalIdToken {
		jwt := "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"openpak":"nx2.a.b","nintendo":{"ai":"01000ca004dca000","av":"`+av+`"}}`)) + ".sig"
		return &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: jwt}}
	}
	if _, _, err := gatedIdentity(tok("00170000"), ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("old version: %v, want FAILED_PRECONDITION", err)
	}
	_, path, err := gatedIdentity(tok("00180000"), "")
	if err != nil {
		t.Fatalf("latest version: %v", err)
	}
	if v := appVersionOf(lastSeg(path)); v != "00180000" {
		t.Fatalf("remembered version %q", v)
	}
	latest = ""
	if _, _, err := gatedIdentity(tok("00170000"), ""); err != nil {
		t.Fatalf("unanswerable check refused: %v", err)
	}
}

// Rooms and pools keep versions apart; an unknown version meets everyone.
func TestVersionsNeverShareASession(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	placeDelay = 0
	rememberAppVersion("u-new", "00180000")
	rememberAppVersion("u-new2", "00180000")
	rememberAppVersion("u-old", "00170000")
	rememberAppVersion("u-unknown", "")
	g := newSessionServer()
	if _, err := g.CreateGameSessionCreationTicket(bearerCtx(1, "u-new"), &mmpb.CreateGameSessionCreationTicketRequest{}); err != nil {
		t.Fatal(err)
	}
	var gsName string
	for _, s := range g.sessions {
		gsName = s.GetName()
	}
	listed := func(uid string) int {
		r, err := g.QueryGameSessions(bearerCtx(2, uid), &mmpb.QueryGameSessionsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return len(r.GetGameSessions())
	}
	if listed("u-new2") != 1 || listed("u-old") != 0 || listed("u-unknown") != 1 {
		t.Fatalf("listing: same %d, other %d, unknown %d", listed("u-new2"), listed("u-old"), listed("u-unknown"))
	}
	if _, err := g.JoinGameSession(bearerCtx(3, "u-old"), &mmpb.JoinGameSessionRequest{Name: gsName}); status.Code(err) != codes.NotFound {
		t.Fatalf("other version joined by name: %v", err)
	}
	if _, err := g.JoinGameSession(bearerCtx(2, "u-new2"), &mmpb.JoinGameSessionRequest{Name: gsName}); err != nil {
		t.Fatalf("same version refused: %v", err)
	}

	m := newMatchmaker(g)
	a := track(t, m, 4, "u-new", "CourseMatch_20221202", map[string]string{"MatchingKey": "k"})
	b := track(t, m, 5, "u-old", "CourseMatch_20221202", map[string]string{"MatchingKey": "k"})
	c := track(t, m, 6, "u-new2", "CourseMatch_20221202", map[string]string{"MatchingKey": "k"})
	if a.GetGameSession().GetName() == b.GetGameSession().GetName() {
		t.Fatal("two versions placed in one pool")
	}
	if a.GetGameSession().GetName() != c.GetGameSession().GetName() {
		t.Fatal("one version split across pools")
	}
}
