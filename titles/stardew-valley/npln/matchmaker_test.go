package npln

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonpb "github.com/openpak/npln/proto/common"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
)

func mmTicketReq(cfg string, attrs map[string]string) *mmpb.CreateMatchmakingTicketRequest {
	f := map[string]*commonpb.Value{}
	for k, v := range attrs {
		f[k] = gsStr(v)
	}
	return &mmpb.CreateMatchmakingTicketRequest{MatchmakingTicket: &mmpb.MatchmakingTicket{
		MatchmakingConfig: "tenants/current/matchmakingConfigs/" + cfg,
		UserDefinitions:   []*mmpb.UserDefinition{{User: "tenants/current/users/current", Attributes: &commonpb.MapValue{Fields: f}}},
	}}
}

// track creates and tracks a ticket for uid and returns the final ticket.
func track(t *testing.T, m *matchmaker, pid uint64, uid, cfg string, attrs map[string]string) *mmpb.MatchmakingTicket {
	t.Helper()
	ctx := bearerCtx(pid, uid)
	tk, err := m.CreateMatchmakingTicket(ctx, mmTicketReq(cfg, attrs))
	if err != nil {
		t.Fatalf("create %s for %s: %v", cfg, uid, err)
	}
	s := newFakeStream[mmpb.MatchmakingTicket](ctx)
	if err := m.TrackMatchmakingTicket(&mmpb.TrackMatchmakingTicketRequest{Name: tk.GetName(), IncludeIdTokenUsers: []string{"tenants/current/users/current"}}, s); err != nil {
		t.Fatalf("track: %v", err)
	}
	got := s.got()
	if len(got) != 3 || got[0].GetState() != mmpb.MatchmakingTicket_SEARCHING || got[1].GetState() != mmpb.MatchmakingTicket_PLACING || got[1].GetGameSession() != nil {
		t.Fatalf("phases = %v", got)
	}
	return got[2]
}

// Tickets agreeing on config and MatchingKey share a session until it is full; another key
// never joins them; only the caller's entry carries a token.
func TestMatchmakerPools(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	placeDelay = 0
	g := newSessionServer()
	m := newMatchmaker(g)

	a := track(t, m, 1, "u-a", "CourseMatch_20221202", map[string]string{"MatchingKey": "course-1"})
	if a.GetState() != mmpb.MatchmakingTicket_SUCCEEDED || a.GetGameSession().GetMaxParticipantCount() != 4 {
		t.Fatalf("first ticket: %v", a)
	}
	p := a.GetGameSession().GetProperties().GetFields()
	if p["_BaseConfigName"].GetStringValue() != "CourseMatch_20221202" || p["MatchingKey"].GetStringValue() != "course-1" {
		t.Fatalf("pool properties: %v", p)
	}
	b := track(t, m, 2, "u-b", "CourseMatch_20221202", map[string]string{"MatchingKey": "course-1"})
	if b.GetGameSession().GetName() != a.GetGameSession().GetName() {
		t.Fatal("same config and key landed in two sessions")
	}
	if n := len(b.GetMatchedUserSessions()); n != 2 {
		t.Fatalf("matched sessions = %d, want both members", n)
	}
	for _, ms := range b.GetMatchedUserSessions() {
		if mine := lastSeg(ms.GetUserDefinition().GetUser()) == "u-b"; mine != (ms.GetMatchmakingIdToken() != "") {
			t.Fatalf("token on %s: %q", ms.GetUserDefinition().GetUser(), ms.GetMatchmakingIdToken())
		}
	}
	if again := track(t, m, 2, "u-b", "CourseMatch_20221202", map[string]string{"MatchingKey": "course-1"}); len(again.GetMatchedUserSessions()) != 2 {
		t.Fatal("a repeated ticket took a second seat")
	}
	other := track(t, m, 3, "u-c", "CourseMatch_20221202", map[string]string{"MatchingKey": "course-2"})
	if other.GetGameSession().GetName() == a.GetGameSession().GetName() {
		t.Fatal("a different MatchingKey shared the session")
	}
	for i, u := range []string{"u-d", "u-e"} {
		track(t, m, uint64(4+i), u, "CourseMatch_20221202", map[string]string{"MatchingKey": "course-1"})
	}
	if full := track(t, m, 9, "u-f", "CourseMatch_20221202", map[string]string{"MatchingKey": "course-1"}); full.GetGameSession().GetName() == a.GetGameSession().GetName() {
		t.Fatal("a fifth player joined a 4-seat course")
	}
	if w := track(t, m, 10, "u-g", "WorldMapMatch_20221202", map[string]string{"MatchingKey": "w"}); w.GetGameSession().GetMaxParticipantCount() != 12 {
		t.Fatalf("world map pool holds %d", w.GetGameSession().GetMaxParticipantCount())
	}

	// A pool has no host: it closes with its last member, not its first.
	gsid := lastSeg(a.GetGameSession().GetName())
	for _, u := range []string{"u-a", "u-b", "u-d"} {
		g.dropMember(gsid, u)
		if g.sessionOf(gsid) == nil {
			t.Fatalf("pool closed when %s left", u)
		}
	}
	g.dropMember(gsid, "u-e")
	if g.sessionOf(gsid) != nil {
		t.Fatal("empty pool still open")
	}

	// Someone else's ticket is not the caller's to track or cancel.
	tk, _ := m.CreateMatchmakingTicket(bearerCtx(1, "u-a"), mmTicketReq("CourseMatch_20221202", nil))
	if _, err := m.CancelMatchmakingTicket(bearerCtx(2, "u-b"), &mmpb.CancelMatchmakingTicketRequest{Name: tk.GetName()}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cancel of another's ticket: %v", err)
	}
	bad := mmTicketReq("CourseMatch_20221202", nil)
	bad.MatchmakingTicket.UserDefinitions[0].User = "tenants/current/users/u-victim"
	if _, err := m.CreateMatchmakingTicket(bearerCtx(1, "u-a"), bad); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ticket naming another player: %v", err)
	}
}

// A friend pool is entered only by members of its base room, is private, and is never listed
// or joinable by name.
func TestFriendPools(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	placeDelay = 0
	g := newSessionServer()
	m := newMatchmaker(g)
	if _, err := g.CreateGameSessionCreationTicket(bearerCtx(1, "u-host"), &mmpb.CreateGameSessionCreationTicketRequest{GameSessionCreationTicket: &mmpb.GameSessionCreationTicket{
		MatchmakingConfig: "tenants/current/matchmakingConfigs/FriendMatch"}}); err != nil {
		t.Fatal(err)
	}
	var room *mmpb.GameSession
	for _, s := range g.sessions {
		room = s
	}
	if room.GetMaxParticipantCount() != 12 || room.GetIsPublic() {
		t.Fatalf("FriendMatch room: max=%d public=%v", room.GetMaxParticipantCount(), room.GetIsPublic())
	}
	if _, err := g.JoinGameSession(bearerCtx(2, "u-friend"), &mmpb.JoinGameSessionRequest{Name: room.GetName()}); err != nil {
		t.Fatal(err)
	}
	roomID := lastSeg(room.GetName())

	if _, err := m.CreateMatchmakingTicket(bearerCtx(3, "u-outsider"), mmTicketReq("FriendWorldMapMatch_20230428", map[string]string{"FriendGameSessionId": roomID})); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("outsider entered the friend pool: %v", err)
	}
	if _, err := m.CreateMatchmakingTicket(bearerCtx(1, "u-host"), mmTicketReq("FriendWorldMapMatch_20230428", nil)); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("friend config without a room: %v", err)
	}
	if _, err := m.CreateMatchmakingTicket(bearerCtx(1, "u-host"), mmTicketReq("CourseMatch_20221202", map[string]string{"FriendGameSessionId": roomID})); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("public config naming a room: %v", err)
	}

	host := track(t, m, 1, "u-host", "FriendWorldMapMatch_20230428", map[string]string{"FriendGameSessionId": roomID, "MatchingKey": "k"})
	friend := track(t, m, 2, "u-friend", "FriendWorldMapMatch_20230428", map[string]string{"FriendGameSessionId": roomID, "MatchingKey": "k"})
	pool := host.GetGameSession()
	if friend.GetGameSession().GetName() != pool.GetName() || pool.GetIsPublic() || pool.GetProperties().GetFields()["FriendGameSessionId"].GetStringValue() != roomID {
		t.Fatalf("friend pool: %v", friend.GetGameSession())
	}
	q, err := g.QueryGameSessions(bearerCtx(3, "u-outsider"), &mmpb.QueryGameSessionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range q.GetGameSessions() {
		if s.GetName() == pool.GetName() {
			t.Fatal("friend pool listed as a room")
		}
	}
	if _, err := g.JoinGameSession(bearerCtx(3, "u-outsider"), &mmpb.JoinGameSessionRequest{Name: pool.GetName()}); status.Code(err) != codes.NotFound {
		t.Fatalf("outsider joined the friend pool by name: %v", err)
	}
}

// `mcn` is the session's config name for a title stamping config properties, Stardew's
// Farm4Player otherwise.
func TestMCNFollowsTheTitle(t *testing.T) {
	defer func() { titleOptions = Options{} }()
	prp := &commonpb.MapValue{Fields: map[string]*commonpb.Value{"_BaseConfigName": gsStr("CourseMatch_20221202")}}
	titleOptions = Options{}
	if got := configName(prp); got != "Farm4Player" {
		t.Fatalf("Stardew's mcn = %q", got)
	}
	titleOptions = Options{ConfigProperties: true}
	if got := configName(prp); got != "CourseMatch_20221202" {
		t.Fatalf("mcn = %q", got)
	}
	if got := configName(nil); got != "Farm4Player" {
		t.Fatalf("mcn without a config = %q", got)
	}
}
