package npln

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonpb "github.com/openpak/npln/proto/common"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
)

func bearerCtx(pid uint64, uid string) context.Context {
	tok := newToken(pid, Tenant+"/users/"+uid, Tenant, AppID)
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
	joinTok := newToken(2, Tenant+"/users/u-joiner", Tenant, AppID)
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

// The room-code answer spells the session with the real tenant, never the client's
// "tenants/current" alias, and the code resolves back to it.
func TestShortAliasNamesTheRealTenant(t *testing.T) {
	g := newSessionServer()
	a, err := g.CreateGameSessionShortAlias(context.Background(), &mmpb.CreateGameSessionShortAliasRequest{
		GameSessionShortAlias: &mmpb.GameSessionShortAlias{GameSession: "tenants/current/gameSessions/g1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.GetName(), Tenant+"/GameSessionShortAliases/") {
		t.Fatalf("name = %q: the SDK parses the collection as GameSessionShortAliases", a.GetName())
	}
	if want := Tenant + "/gameSessions/g1"; a.GetGameSession() != want {
		t.Fatalf("game_session = %q, want %q", a.GetGameSession(), want)
	}
	code := lastSeg(a.GetName())
	if len(code) != 6 || strings.Trim(code, roomCodeLetters) != "" || strings.ContainsAny(code, "IOZ") {
		t.Fatalf("room code %q: Dinkum's screen takes 6 capitals", code)
	}
	b, err := g.GetGameSessionShortAlias(context.Background(), &mmpb.GetGameSessionShortAliasRequest{Name: "tenants/current/GameSessionShortAliases/" + strings.ToLower(code)})
	if err != nil || b.GetGameSession() != a.GetGameSession() {
		t.Fatalf("lookup: %v %v", b, err)
	}
}

// Peace Walker's config names carry the lobby size; a size the host asks for still wins, and
// names outside the Mmc_ family keep the default.
func TestCapacityFromConfigName(t *testing.T) {
	for cfg, want := range map[string]int32{
		"tenants/current/matchmakingConfigs/Mmc_CoOps2":  2,
		"tenants/current/matchmakingConfigs/Mmc_CoOps4":  4,
		"tenants/current/matchmakingConfigs/Mmc_CoOps":   0,
		"tenants/current/matchmakingConfigs/Mmc_Big99":   0,
		"tenants/current/matchmakingConfigs/Farm4Player": 0,
		"tenants/current/matchmakingConfigs/Match2":      0,
	} {
		if got := capacityFromConfig(cfg); got != want {
			t.Errorf("%s: %d, want %d", cfg, got, want)
		}
	}

	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	create := func(cfg string, max int32) *mmpb.GameSession {
		t.Helper()
		g := newSessionServer()
		if _, err := g.CreateGameSessionCreationTicket(bearerCtx(1, "u-host"), &mmpb.CreateGameSessionCreationTicketRequest{GameSessionCreationTicket: &mmpb.GameSessionCreationTicket{
			MatchmakingConfig: cfg, GameSession: &mmpb.GameSession{MaxParticipantCount: max}}}); err != nil {
			t.Fatal(err)
		}
		for _, s := range g.sessions {
			return s
		}
		t.Fatal("session not stored")
		return nil
	}
	if s := create("tenants/current/matchmakingConfigs/Mmc_CoOps2", 0); s.GetMaxParticipantCount() != 2 {
		t.Fatalf("Mmc_CoOps2 room holds %d", s.GetMaxParticipantCount())
	}
	if s := create("tenants/current/matchmakingConfigs/Mmc_CoOps2", 3); s.GetMaxParticipantCount() != 3 {
		t.Fatalf("the host's own size was overridden: %d", s.GetMaxParticipantCount())
	}
	if s := create("tenants/current/matchmakingConfigs/Farm4Player", 0); s.GetMaxParticipantCount() != 4 {
		t.Fatalf("Stardew's farm holds %d", s.GetMaxParticipantCount())
	}
}

// Config properties are stamped only for a title that asks for them; Stardew's joiners drop a
// farm that carries them.
func TestConfigPropertiesAreATitleOption(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	defer func() { titleOptions = Options{} }()
	for _, on := range []bool{false, true} {
		titleOptions = Options{ConfigProperties: on}
		g := newSessionServer()
		_, err := g.CreateGameSessionCreationTicket(bearerCtx(1, "u-host"), &mmpb.CreateGameSessionCreationTicketRequest{GameSessionCreationTicket: &mmpb.GameSessionCreationTicket{
			MatchmakingConfig: "tenants/current/matchmakingConfigs/Mmc_CoOps2",
			GameSession: &mmpb.GameSession{Properties: &commonpb.MapValue{Fields: map[string]*commonpb.Value{
				"MatchMode": {ValueType: &commonpb.Value_StringValue{StringValue: "COOPS"}}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range g.sessions {
			f := s.GetProperties().GetFields()
			if f["MatchMode"].GetStringValue() != "COOPS" {
				t.Fatalf("host property lost: %v", f)
			}
			if _, has := f["_BaseConfigName"]; has != on || (on && f["_BaseConfigName"].GetStringValue() != "Mmc_CoOps2") {
				t.Fatalf("ConfigProperties=%v: properties %v", on, f)
			}
		}
	}
}

// A join answers with the joiner's own matched session only, carrying its token; the host's
// entry has none, and a client handed it cannot open gamesync.
func TestJoinAnswersOnlyTheJoiner(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	g := newSessionServer()
	if _, err := g.CreateGameSessionCreationTicket(bearerCtx(1, "u-host"), &mmpb.CreateGameSessionCreationTicketRequest{}); err != nil {
		t.Fatal(err)
	}
	var gsName string
	for _, s := range g.sessions {
		gsName = s.GetName()
	}
	resp, err := g.JoinGameSession(bearerCtx(2, "u-guest"), &mmpb.JoinGameSessionRequest{Name: gsName})
	if err != nil {
		t.Fatal(err)
	}
	m := resp.GetMatchedUserSessions()
	if len(m) != 1 || lastSeg(m[0].GetUserDefinition().GetUser()) != "u-guest" || m[0].GetMatchmakingIdToken() == "" {
		t.Fatalf("matched sessions = %v, want the guest's alone with a token", m)
	}
	if !strings.HasPrefix(m[0].GetUserSession(), gsName+"/userSessions/") {
		t.Fatalf("user session %q is not under %q", m[0].GetUserSession(), gsName)
	}
	if n := len(resp.GetGameSession().GetUserSessions()); n != 2 {
		t.Fatalf("the session itself lists %d members, want both", n)
	}
}

// FriendSearch looks only in friends' rooms: a stranger named in it is refused, a friend's
// room is found, and an unset property is "any".
func TestFriendSearch(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NX_INTERNAL_KEY", "k")
	accounts := map[uint64]string{
		1: `{"pid":1,"baas_user_id":"1111111111111111","friends":[{"pid":2,"baas_user_id":"2222222222222222"}]}`,
		2: `{"pid":2,"baas_user_id":"2222222222222222","friends":[{"pid":1,"baas_user_id":"1111111111111111"}]}`,
		3: `{"pid":3,"baas_user_id":"3333333333333333","friends":[]}`,
	}
	adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ PID uint64 }
		json.NewDecoder(r.Body).Decode(&in)
		fmt.Fprint(w, accounts[in.PID])
	}))
	defer adapter.Close()
	oldURL := adapterURL
	adapterURL = adapter.URL
	defer func() { adapterURL = oldURL }()

	host, friend, stranger := userID("1111111111111111"), userID("2222222222222222"), userID("3333333333333333")
	g := newSessionServer()
	str := func(s string) *commonpb.Value {
		return &commonpb.Value{ValueType: &commonpb.Value_StringValue{StringValue: s}}
	}
	if _, err := g.CreateGameSessionCreationTicket(bearerCtx(1, host), &mmpb.CreateGameSessionCreationTicketRequest{GameSessionCreationTicket: &mmpb.GameSessionCreationTicket{
		MatchmakingConfig: "tenants/current/matchmakingConfigs/Mmc_CoOps2",
		GameSession:       &mmpb.GameSession{Properties: &commonpb.MapValue{Fields: map[string]*commonpb.Value{"MatchingKey": str("k1")}}}}}); err != nil {
		t.Fatal(err)
	}
	search := func(ctx context.Context, user string, props map[string]*commonpb.Value) (*mmpb.QueryGameSessionsResponse, error) {
		return g.QueryGameSessions(ctx, &mmpb.QueryGameSessionsRequest{
			GameSessionSearchConfig: "tenants/current/gameSessionSearchConfigs/FriendSearch",
			Users:                   []string{"tenants/current/users/" + user},
			Properties:              &commonpb.MapValue{Fields: props},
		})
	}

	got, err := search(bearerCtx(2, friend), host, map[string]*commonpb.Value{"MatchingKey": str("k1"), "Mode": {}})
	if err != nil || len(got.GetGameSessions()) != 1 {
		t.Fatalf("friend's search: %v, %v", got, err)
	}
	if got, err := search(bearerCtx(2, friend), host, map[string]*commonpb.Value{"MatchingKey": str("other")}); err != nil || len(got.GetGameSessions()) != 0 {
		t.Fatalf("MatchingKey filter ignored: %v, %v", got, err)
	}
	if _, err := search(bearerCtx(3, stranger), host, nil); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a stranger searched the host's rooms: %v", err)
	}
	if _, err := g.QueryGameSessions(bearerCtx(2, friend), &mmpb.QueryGameSessionsRequest{GameSessionSearchConfig: "tenants/current/gameSessionSearchConfigs/FriendSearch"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("FriendSearch without a user: %v", err)
	}
}
