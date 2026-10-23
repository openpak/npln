package npln

// matchmaker — nn.npln.matchmaking.v1.Matchmaker: public matching by ticket, as Super Mario Bros.
// Wonder uses it (facts: titles/super-mario-bros-wonder/docs/provenance.md). A ticket names a
// config and carries a MatchingKey attribute; tickets agreeing on config, key and friend room
// land in one shared session until it is full. A friend pool is such a session tied to a base
// room (a FriendMatch room made through GameSessionService) by FriendGameSessionId: only that
// room's members may enter it, and it is never listed as a room of its own.
//
// Pool sessions live in the sessionServer's own maps, so gamesync, JoinGameSession and leaving
// treat them like any room; g.pools marks them (a pool has no host and closes when empty).
//
// ponytail: in-memory like the rest of the session state; a restart empties every pool.

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/openpak/npln/proto/common"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
	"github.com/openpak/npln/redact"
)

// placeDelay is the pause between the SEARCHING and PLACING updates of a ticket and the result.
var placeDelay = 300 * time.Millisecond

type mmTicket struct {
	t     *mmpb.MatchmakingTicket // as created: SEARCHING, the caller's definition only
	owner string                  // user id
	gsid  string                  // the session it was placed in, once tracked
}

type matchmaker struct {
	mmpb.UnimplementedMatchmakerServer
	rooms *sessionServer

	mu      sync.Mutex
	tickets map[string]*mmTicket // ticket id -> ticket
}

func newMatchmaker(rooms *sessionServer) *matchmaker {
	return &matchmaker{rooms: rooms, tickets: map[string]*mmTicket{}}
}

func strAttr(ud *mmpb.UserDefinition, k string) string {
	return ud.GetAttributes().GetFields()[k].GetStringValue()
}

// onlyCaller: a ticket carries exactly one definition, and it is the caller's.
func onlyCaller(uds []*mmpb.UserDefinition, uid string) error {
	if len(uds) != 1 {
		return status.Error(codes.InvalidArgument, "a ticket carries exactly one user definition")
	}
	if u := lastSeg(uds[0].GetUser()); u != "" && u != "current" && u != uid {
		return status.Error(codes.PermissionDenied, "a ticket may only name its caller")
	}
	return nil
}

func (m *matchmaker) CreateMatchmakingTicket(ctx context.Context, req *mmpb.CreateMatchmakingTicketRequest) (*mmpb.MatchmakingTicket, error) {
	diag("CreateMatchmakingTicket", req)
	user, err := callerUser(ctx)
	if err != nil {
		return nil, err
	}
	in := req.GetMatchmakingTicket()
	if err := onlyCaller(in.GetUserDefinitions(), lastSeg(user)); err != nil {
		return nil, err
	}
	tn := tenantFromCtx(ctx)
	ud := userDef(user, in.GetUserDefinitions()[0])
	cfg := lastSeg(in.GetMatchmakingConfig())
	if cfg == "" {
		return nil, status.Error(codes.InvalidArgument, "a ticket names a matchmaking config")
	}
	if err := m.friendPoolAllowed(cfg, strAttr(ud, "FriendGameSessionId"), user); err != nil {
		return nil, err
	}
	t := &mmpb.MatchmakingTicket{
		Name:              tn + "/matchmakingTickets/" + uuid4(),
		MatchmakingConfig: tn + "/matchmakingConfigs/" + cfg,
		UserDefinitions:   []*mmpb.UserDefinition{ud},
		State:             mmpb.MatchmakingTicket_SEARCHING,
	}
	m.mu.Lock()
	m.tickets[lastSeg(t.Name)] = &mmTicket{t: t, owner: lastSeg(user)}
	m.mu.Unlock()
	log.Printf("[MM] Matchmaker ticket %s config=%s key=%s by %s", redact.Seg(t.Name), redact.Tag(cfg), redact.Tag(strAttr(ud, "MatchingKey")), redact.Seg(user))
	return proto.Clone(t).(*mmpb.MatchmakingTicket), nil
}

// friendPoolAllowed: a Friend* config needs the base room, active, with the caller already in
// it (holding a room id is not membership); any other config may not name one.
func (m *matchmaker) friendPoolAllowed(cfg, room, user string) error {
	if !strings.HasPrefix(cfg, "Friend") {
		if room != "" {
			return status.Error(codes.InvalidArgument, "FriendGameSessionId needs a Friend config")
		}
		return nil
	}
	if room == "" {
		return status.Error(codes.InvalidArgument, "FriendGameSessionId required")
	}
	g := m.rooms
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.sessions[lastSeg(room)]
	if s == nil || s.GetState() != mmpb.GameSession_ACTIVE {
		return status.Error(codes.NotFound, "friend room is no longer active")
	}
	if !hasUID(g.members[lastSeg(room)], lastSeg(user)) {
		return status.Error(codes.PermissionDenied, "join the friend room before its matchmaking pool")
	}
	return nil
}

func (m *matchmaker) owned(name, uid string) *mmTicket {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.tickets[lastSeg(name)]; t != nil && t.owner == uid {
		return t
	}
	return nil
}

func poolKey(t *mmpb.MatchmakingTicket) string {
	ud := t.GetUserDefinitions()[0]
	return lastSeg(t.GetMatchmakingConfig()) + "\x00" + strAttr(ud, "MatchingKey") + "\x00" + lastSeg(strAttr(ud, "FriendGameSessionId"))
}

// place seats the ticket's player in its pool: the session it already holds a seat in, else the
// first one with room, else a new one. Returns the session id and the caller's user session.
func (m *matchmaker) place(ctx context.Context, t *mmpb.MatchmakingTicket) (string, string) {
	g := m.rooms
	ud := t.GetUserDefinitions()[0]
	key := poolKey(t)
	mine := callerVersion(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	var gsid string
	for id, k := range g.pools {
		if k != key || g.sessions[id].GetState() != mmpb.GameSession_ACTIVE || !g.versionFits(id, mine) {
			continue
		}
		for _, us := range g.members[id] {
			if lastSeg(us.GetUser()) == lastSeg(ud.GetUser()) {
				return id, us.GetName() // a repeated ticket keeps its seat
			}
		}
		if gsid == "" && int32(len(g.members[id])) < g.sessions[id].GetMaxParticipantCount() {
			gsid = id
		}
	}
	if gsid == "" {
		gsid = m.newPool(ctx, t, key)
	}
	us := g.sessions[gsid].GetName() + "/userSessions/" + uuid4()
	g.addMember(gsid, &mmpb.UserSession{Name: us, User: ud.GetUser(), State: mmpb.UserSession_ACTIVE, Team: ud.GetTeam(), Attributes: ud.GetAttributes(), LatencyData: ud.GetLatencyData(), CreateTime: timestamppb.Now()})
	return gsid, us
}

// newPool opens a pool session. Caller holds rooms.mu.
func (m *matchmaker) newPool(ctx context.Context, t *mmpb.MatchmakingTicket, key string) string {
	cfg := lastSeg(t.GetMatchmakingConfig())
	ud := t.GetUserDefinitions()[0]
	gsName := tenantFromCtx(ctx) + "/gameSessions/" + uuid4()
	props := map[string]*commonpb.Value{"_BaseConfigName": gsStr(cfg), "_AliasSuffix": gsStr("")}
	if k := strAttr(ud, "MatchingKey"); k != "" {
		props["MatchingKey"] = gsStr(k)
	}
	room := lastSeg(strAttr(ud, "FriendGameSessionId"))
	if room != "" {
		props["FriendGameSessionId"] = gsStr(room)
	}
	capacity := capacityFromConfig(cfg)
	if capacity == 0 {
		capacity = 4
	}
	s := &mmpb.GameSession{
		Name:                gsName,
		MaxParticipantCount: capacity,
		CanParticipate:      true,
		IsPublic:            room == "",
		State:               mmpb.GameSession_ACTIVE,
		Host:                envOr("NPLN_RELAY_HOST", "127.0.0.1"),
		Port:                envInt("NPLN_RELAY_PORT", 21010),
		CreateTime:          timestamppb.Now(),
		Properties:          &commonpb.MapValue{Fields: props},
	}
	gsid := lastSeg(gsName)
	m.rooms.sessions[gsid] = s
	m.rooms.pools[gsid] = key
	m.rooms.versions[gsid] = callerVersion(ctx)
	m.rooms.hosts[gsid] = hostOf(lastSeg(ud.GetUser())) // a pool's host is the player who opened it
	log.Printf("[MM] pool session %s opened: config=%s capacity=%d friend room=%s", redact.Tag(gsid), redact.Tag(cfg), capacity, redact.Tag(room))
	return gsid
}

// wantsToken: include_id_token_users names the caller, by resource name, by id or as "current".
func wantsToken(include []string, uid string) bool {
	for _, u := range include {
		if id := lastSeg(u); id == uid || id == "current" {
			return true
		}
	}
	return false
}

func (m *matchmaker) TrackMatchmakingTicket(req *mmpb.TrackMatchmakingTicketRequest, stream grpc.ServerStreamingServer[mmpb.MatchmakingTicket]) error {
	ctx := stream.Context()
	user, err := callerUser(ctx)
	if err != nil {
		return err
	}
	tk := m.owned(req.GetName(), lastSeg(user))
	if tk == nil {
		return status.Error(codes.PermissionDenied, "not the caller's ticket")
	}
	m.mu.Lock()
	gsid := tk.gsid
	m.mu.Unlock()
	if gsid == "" || m.rooms.sessionOf(gsid) == nil {
		gsid, _ = m.place(ctx, tk.t)
		m.mu.Lock()
		tk.gsid = gsid
		m.mu.Unlock()
	}
	for _, st := range []mmpb.MatchmakingTicket_State{mmpb.MatchmakingTicket_SEARCHING, mmpb.MatchmakingTicket_PLACING} {
		phase := proto.Clone(tk.t).(*mmpb.MatchmakingTicket)
		phase.State = st
		if err := stream.Send(phase); err != nil {
			return err
		}
		time.Sleep(placeDelay)
	}
	done := proto.Clone(tk.t).(*mmpb.MatchmakingTicket)
	done.State = mmpb.MatchmakingTicket_SUCCEEDED
	g := m.rooms
	g.mu.Lock()
	if g.sessions[gsid] == nil {
		g.mu.Unlock()
		done.State = mmpb.MatchmakingTicket_FAILED
		return stream.Send(done)
	}
	done.GameSession = g.withMembers(gsid)
	members := append([]*mmpb.UserSession(nil), g.members[gsid]...)
	g.mu.Unlock()
	for _, us := range members {
		ud := &mmpb.UserDefinition{User: us.GetUser(), Attributes: us.GetAttributes(), LatencyData: us.GetLatencyData(), Team: us.GetTeam()}
		ms := &mmpb.MatchedUserSession{UserDefinition: ud, UserSession: us.GetName()}
		if lastSeg(us.GetUser()) == lastSeg(user) && wantsToken(req.GetIncludeIdTokenUsers(), lastSeg(user)) {
			ms = matched(ctx, ud, done.GameSession.GetName(), us.GetName())
		}
		done.MatchedUserSessions = append(done.MatchedUserSessions, ms)
	}
	log.Printf("[MM] Matchmaker ticket %s -> SUCCEEDED session=%s (%d/%d)", redact.Seg(tk.t.Name), redact.Tag(gsid), len(members), done.GameSession.GetMaxParticipantCount())
	return stream.Send(done)
}

func hasUID(list []*mmpb.UserSession, uid string) bool {
	for _, us := range list {
		if lastSeg(us.GetUser()) == uid {
			return true
		}
	}
	return false
}

func (g *sessionServer) sessionOf(gsid string) *mmpb.GameSession {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sessions[gsid]
}

// CancelMatchmakingTicket forgets the ticket. A seat already taken is left to gamesync: the
// player's session ends when its KeepUserSession stream does, as for any room.
func (m *matchmaker) CancelMatchmakingTicket(ctx context.Context, req *mmpb.CancelMatchmakingTicketRequest) (*emptypb.Empty, error) {
	user, err := callerUser(ctx)
	if err != nil {
		return nil, err
	}
	if m.owned(req.GetName(), lastSeg(user)) == nil {
		return nil, status.Error(codes.PermissionDenied, "not the caller's ticket")
	}
	m.mu.Lock()
	delete(m.tickets, lastSeg(req.GetName()))
	m.mu.Unlock()
	return &emptypb.Empty{}, nil
}

// CreateAcceptance records the caller's answer. No ticket here ever asks for one (none goes
// REQUIRING_ACCEPTANCE), so this only echoes it, for the caller alone.
func (m *matchmaker) CreateAcceptance(ctx context.Context, req *mmpb.CreateAcceptanceRequest) (*mmpb.Acceptance, error) {
	diag("CreateAcceptance", req)
	user, err := callerUser(ctx)
	if err != nil {
		return nil, err
	}
	return &mmpb.Acceptance{
		Name:     strings.TrimRight(req.GetParent(), "/") + "/acceptances/" + uuid4(),
		Users:    []string{user},
		Accepted: req.GetAcceptance().GetAccepted(),
	}, nil
}
