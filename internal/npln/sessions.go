package npln

// sessions — nn.npln.matchmaking.v1.GameSessionService: the farm lobby. A host creates a game
// session (creation ticket -> tracked to SUCCEEDED), a joiner finds it (QueryGameSessions —
// Stardew's first title-specific RPC, confirmed 2026-09-01) and joins it. Every request the
// game sends is logged in full (prototext) while the Stardew flow is still being measured:
// each new field the game populates is the next thing to honour. In-memory only.
//
// ponytail: single process, one mutex, no persistence — a restart empties every farm lobby.

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/NextendoNetwork/stardew-nextendo/proto/common"
	mmpb "github.com/NextendoNetwork/stardew-nextendo/proto/matchmaking/v1"
)

type sessionServer struct {
	mmpb.UnimplementedGameSessionServiceServer

	mu       sync.Mutex
	tickets  map[string]*mmpb.GameSessionCreationTicket // ticket id -> SUCCEEDED ticket
	sessions map[string]*mmpb.GameSession               // session id -> session (no user_sessions)
	members  map[string][]*mmpb.UserSession             // session id -> participants
	aliases  map[string]string                          // short code -> session name
}

func newSessionServer() *sessionServer {
	return &sessionServer{
		tickets:  map[string]*mmpb.GameSessionCreationTicket{},
		sessions: map[string]*mmpb.GameSession{},
		members:  map[string][]*mmpb.UserSession{},
		aliases:  map[string]string{},
	}
}

func diag(what string, m proto.Message) { log.Printf("[MM][DIAG] %s =\n%s", what, prototext.Format(m)) }

// concreteUser resolves "tenants/current/users/current" to the caller's real resource name; the
// client identifies its own session by the concrete id and bounces when it only sees "current".
func concreteUser(ctx context.Context, fallback string) string {
	if uid := uidFromCtx(ctx); uid != "" {
		return tenantFromCtx(ctx) + "/users/" + uid
	}
	return fallback
}

func userDef(ctx context.Context, in *mmpb.UserDefinition) *mmpb.UserDefinition {
	ud := &mmpb.UserDefinition{User: concreteUser(ctx, in.GetUser())}
	ud.Attributes, ud.LatencyData, ud.Team = in.GetAttributes(), in.GetLatencyData(), in.GetTeam()
	return ud
}

// attrJSON / ltcyJSON render a UserDefinition into the gss token's typed-value strings.
func attrJSON(attrs *commonpb.MapValue) string {
	m := map[string]any{}
	for k, v := range attrs.GetFields() {
		switch x := v.GetValueType().(type) {
		case *commonpb.Value_StringValue:
			m[k] = map[string]any{"type": "string", "value": x.StringValue}
		case *commonpb.Value_IntegerValue:
			m[k] = map[string]any{"type": "integer", "value": x.IntegerValue}
		case *commonpb.Value_DoubleValue:
			m[k] = map[string]any{"type": "double", "value": x.DoubleValue}
		case *commonpb.Value_BooleanValue:
			m[k] = map[string]any{"type": "boolean", "value": x.BooleanValue}
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func ltcyJSON(ld *mmpb.LatencyData) string {
	lat := map[string]any{}
	for region, d := range ld.GetLatencies() {
		lat[region] = map[string]any{"nanos": d.GetSeconds()*1_000_000_000 + int64(d.GetNanos())}
	}
	b, _ := json.Marshal(map[string]any{"latencies": lat})
	return string(b)
}

func matched(ctx context.Context, ud *mmpb.UserDefinition, gsName, userSess string) *mmpb.MatchedUserSession {
	return &mmpb.MatchedUserSession{
		UserDefinition:     ud,
		UserSession:        userSess,
		MatchmakingIdToken: mintSessionToken(lastSeg(ud.GetUser()), tenantFromCtx(ctx), gsName, userSess, ud.GetTeam(), attrJSON(ud.GetAttributes()), ltcyJSON(ud.GetLatencyData())),
	}
}

func (g *sessionServer) addMember(gsid string, us *mmpb.UserSession) (count int32) {
	list := g.members[gsid]
	for i, m := range list {
		if m.GetUser() == us.GetUser() { // same player rejoining: replace, never double-count
			list[i] = us
			g.members[gsid] = list
			return int32(len(list))
		}
	}
	g.members[gsid] = append(list, us)
	return int32(len(list))
}

// withMembers renders a session for the wire: user_sessions (field 12) is filled from the live
// participant list; the stored copy stays bare.
func (g *sessionServer) withMembers(gsid string) *mmpb.GameSession {
	s := proto.Clone(g.sessions[gsid]).(*mmpb.GameSession)
	s.UserSessions = append([]*mmpb.UserSession(nil), g.members[gsid]...)
	s.CurrentParticipantCount = int32(len(s.UserSessions))
	return s
}

// ---- hosting ----

func (g *sessionServer) CreateGameSessionCreationTicket(ctx context.Context, req *mmpb.CreateGameSessionCreationTicketRequest) (*mmpb.GameSessionCreationTicket, error) {
	diag("CreateGameSessionCreationTicket", req)
	tn := tenantFromCtx(ctx)
	in := req.GetGameSessionCreationTicket()
	ticketName := tn + "/gameSessionCreationTickets/" + uuid4()
	gsName := tn + "/gameSessions/" + uuid4()

	var hostUD *mmpb.UserDefinition
	if uds := in.GetUserDefinitions(); len(uds) > 0 {
		hostUD = userDef(ctx, uds[0])
	} else {
		hostUD = userDef(ctx, nil)
	}

	room := &mmpb.GameSession{
		Name:                    gsName,
		MaxParticipantCount:     4, // Stardew farms hold up to 4 players
		CurrentParticipantCount: 1,
		CanParticipate:          true,
		State:                   mmpb.GameSession_ACTIVE,
		CreateTime:              timestamppb.Now(),
		// host:port is REQUIRED (empty -> 2321-4608 at once): the session transport the client dials next.
		Host: envOr("NPLN_RELAY_HOST", "127.0.0.1"),
		Port: envInt("NPLN_RELAY_PORT", 18501),
	}
	room.IsPublic = true // a farm is listed to friends even when the host sends no flag (measured: the create carries none)
	if rg := in.GetGameSession(); rg != nil {
		room.Password = rg.GetPassword()
		if rg.GetIsPublic() {
			room.IsPublic = true
		}
		if rg.GetMaxParticipantCount() > 0 {
			room.MaxParticipantCount = rg.GetMaxParticipantCount()
		}
	}
	// Keep the host's session properties EXACTLY as sent (only _Pia_SystemData). Measured via the
	// client's own join filter (main FUN_07bb3d30 + criteria matcher at 0x76ecee4): the joiner
	// compares each session's config id against its search criteria (bit1). Injecting Nintendo/S3's
	// _BaseConfigName/_AliasSuffix gave the farm a config id that fails Stardew's match, so every
	// returned farm was dropped from the list. The host advertises only _Pia_SystemData; mirror that.
	room.Properties = in.GetGameSession().GetProperties()
	cfg := in.GetMatchmakingConfig()
	if cfg != "" {
		cfg = tn + "/matchmakingConfigs/" + lastSeg(cfg) // echoed with the concrete tenant, as Nintendo does
	}
	userSess := gsName + "/userSessions/" + uuid4()
	t := &mmpb.GameSessionCreationTicket{
		Name:                ticketName,
		MatchmakingConfig:   cfg,
		UserDefinitions:     []*mmpb.UserDefinition{hostUD},
		State:               mmpb.GameSessionCreationTicket_SUCCEEDED,
		GameSession:         room,
		MatchedUserSessions: []*mmpb.MatchedUserSession{matched(ctx, hostUD, gsName, userSess)},
	}

	g.mu.Lock()
	// A host re-loading its farm creates a fresh session each time; drop its PRIOR sessions so the
	// store holds one farm per host instead of accumulating stale duplicates (which the joiner's
	// QueryGameSessions then returns as N copies). The host is member rank 1 (first added).
	hostUser := hostUD.GetUser()
	for gsid, mem := range g.members {
		if len(mem) > 0 && mem[0].GetUser() == hostUser {
			delete(g.sessions, gsid)
			delete(g.members, gsid)
			log.Printf("[MM] evict stale farm session=%s (host %s re-hosted)", gsid, lastSeg(hostUser))
		}
	}
	g.tickets[lastSeg(ticketName)] = t
	g.sessions[lastSeg(gsName)] = room
	g.addMember(lastSeg(gsName), &mmpb.UserSession{Name: userSess, User: hostUD.GetUser(), State: mmpb.UserSession_ACTIVE, Attributes: hostUD.GetAttributes(), CreateTime: timestamppb.Now()})
	g.mu.Unlock()
	log.Printf("[MM] Create host=%s ticket=%s session=%s @ %s:%d", lastSeg(hostUD.GetUser()), lastSeg(ticketName), lastSeg(gsName), room.Host, room.Port)

	// Returned PENDING; Track streams PENDING -> SUCCEEDED so the host's state machine sees the transition.
	pending := &mmpb.GameSessionCreationTicket{Name: ticketName, MatchmakingConfig: t.MatchmakingConfig, UserDefinitions: t.UserDefinitions, State: mmpb.GameSessionCreationTicket_PENDING}
	if p := in.GetGameSession().GetProperties(); p != nil {
		pending.GameSession = &mmpb.GameSession{Properties: p} // Nintendo's PENDING ticket already carries the requested properties
	}
	return pending, nil
}

func (g *sessionServer) TrackGameSessionCreationTicket(req *mmpb.TrackGameSessionCreationTicketRequest, stream grpc.ServerStreamingServer[mmpb.GameSessionCreationTicket]) error {
	g.mu.Lock()
	t := g.tickets[lastSeg(req.GetName())]
	g.mu.Unlock()
	if t == nil {
		return stream.Send(&mmpb.GameSessionCreationTicket{Name: req.GetName(), State: mmpb.GameSessionCreationTicket_FAILED})
	}
	if err := stream.Send(&mmpb.GameSessionCreationTicket{Name: t.Name, MatchmakingConfig: t.MatchmakingConfig, UserDefinitions: t.UserDefinitions, State: mmpb.GameSessionCreationTicket_PENDING}); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	log.Printf("[MM] Track %s -> SUCCEEDED session=%s (stream closes)", lastSeg(t.Name), lastSeg(t.GetGameSession().GetName()))
	return stream.Send(t) // the client treats the ticket as complete only when the stream closes OK
}

func (g *sessionServer) CancelGameSessionCreationTicket(ctx context.Context, req *mmpb.CancelGameSessionCreationTicketRequest) (*emptypb.Empty, error) {
	g.mu.Lock()
	if t := g.tickets[lastSeg(req.GetName())]; t != nil {
		gsid := lastSeg(t.GetGameSession().GetName())
		delete(g.tickets, lastSeg(req.GetName()))
		delete(g.sessions, gsid)
		delete(g.members, gsid)
	}
	g.mu.Unlock()
	log.Printf("[MM] Cancel ticket %s", lastSeg(req.GetName()))
	return &emptypb.Empty{}, nil
}

// ---- finding and joining ----

func (g *sessionServer) GetGameSession(ctx context.Context, req *mmpb.GetGameSessionRequest) (*mmpb.GameSession, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sessions[g.realID(req.GetName())] == nil {
		return nil, status.Errorf(codes.NotFound, "game session %q not found", req.GetName())
	}
	return g.withMembers(g.realID(req.GetName())), nil
}

func (g *sessionServer) BatchGetGameSessions(ctx context.Context, req *mmpb.BatchGetGameSessionsRequest) (*mmpb.BatchGetGameSessionsResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := &mmpb.BatchGetGameSessionsResponse{}
	for _, n := range req.GetNames() {
		if g.sessions[g.realID(n)] != nil {
			out.GameSessions = append(out.GameSessions, g.withMembers(g.realID(n)))
		}
	}
	return out, nil
}

// QueryGameSessions lists joinable farms. Filters honoured: users (sessions any of them is in —
// "friends' farms"), properties (every requested field must match), min_vacancy_count.
// ponytail: linear scan; fine for a private network.
func (g *sessionServer) QueryGameSessions(ctx context.Context, req *mmpb.QueryGameSessionsRequest) (*mmpb.QueryGameSessionsResponse, error) {
	diag("QueryGameSessions", req)
	g.mu.Lock()
	defer g.mu.Unlock()
	out := &mmpb.QueryGameSessionsResponse{}
	for id, s := range g.sessions {
		if s.GetState() != mmpb.GameSession_ACTIVE {
			continue
		}
		full := g.withMembers(id)
		if req.GetMinVacancyCount() > 0 && full.MaxParticipantCount-full.CurrentParticipantCount < req.GetMinVacancyCount() {
			continue
		}
		if !propsMatch(full.GetProperties(), req.GetProperties()) || !hasAnyUser(full, req.GetUsers()) {
			continue
		}
		if os.Getenv("NPLN_QUERY_VARIANTS") == "1" { // measurement: which shape does the client list?
			out.GameSessions = append(out.GameSessions, g.queryVariants(full)...)
			continue
		}
		out.GameSessions = append(out.GameSessions, full)
	}
	log.Printf("[MM] QueryGameSessions users=%d props=%d -> %d session(s)", len(req.GetUsers()), len(req.GetProperties().GetFields()), len(out.GameSessions))
	if len(out.GameSessions) > 0 {
		diag("QueryGameSessions response", out)
	}
	return out, nil
}

// queryVariants (round 3): v1 host = LAN address (cap 4); v2 name under tenants/current (cap 5);
// v3 is_public=false (cap 6); v4 unchanged but capacity 8 (cap 8). Round 2 (user path form,
// no user_sessions, no properties) changed nothing: none listed.
func (g *sessionServer) queryVariants(full *mmpb.GameSession) []*mmpb.GameSession {
	// Round 8: find the proto field feeding the matcher's session+0x30 (bit4 "current") and +0x40
	// (bit3 odd byte). Every variant zeroes blob[0x16] (bit4 needs current>blob[0x16]); each sets a
	// different field high/odd; cap 9 = kitchen sink (all set) which should list AND be joinable.
	zero16 := func(v *mmpb.GameSession) {
		f := v.GetProperties().GetFields()["_Pia_SystemData"]
		if f == nil {
			return
		}
		b := append([]byte(nil), f.GetBytesValue()...)
		if len(b) > 0x16 {
			b[0x15], b[0x16] = 0, 0
		}
		v.Properties = proto.Clone(v.Properties).(*commonpb.MapValue)
		v.Properties.Fields["_Pia_SystemData"] = &commonpb.Value{ValueType: &commonpb.Value_BytesValue{BytesValue: b}}
	}
	kitchen := func(v *mmpb.GameSession) {
		v.CurrentParticipantCount = 3
		v.CanParticipate = true
		v.IsPublic = true
		v.State = mmpb.GameSession_ACTIVE
		v.Host = envOr("NPLN_RELAY_HOST", "127.0.0.1")
		v.Port = 18501
	}
	cases := []struct {
		cap int32
		fn  func(v *mmpb.GameSession)
	}{
		{2, func(v *mmpb.GameSession) { v.CurrentParticipantCount = 3 }},
		{3, func(v *mmpb.GameSession) { v.Port = 99 }},
		{4, func(v *mmpb.GameSession) { v.State = mmpb.GameSession_ACTIVE }},
		{5, func(v *mmpb.GameSession) { v.CanParticipate = true }},
		{6, func(v *mmpb.GameSession) { v.IsPublic = true }},
		{7, func(v *mmpb.GameSession) { v.Password = ""; v.Host = "127.0.0.1" }},
		{8, func(v *mmpb.GameSession) { v.CurrentParticipantCount = 3; v.CanParticipate = true; v.IsPublic = true }},
		{9, kitchen},
	}
	base := full.GetName()[:strings.LastIndex(full.GetName(), "/")+1]
	var out []*mmpb.GameSession
	for _, c := range cases {
		v := proto.Clone(full).(*mmpb.GameSession)
		id := uuid4()
		g.aliases["v:"+id] = lastSeg(full.GetName())
		v.Name = base + id
		v.MaxParticipantCount = c.cap
		c.fn(v)
		zero16(v)
		out = append(out, v)
	}
	return out
}

// realID maps a variant name back to the stored farm id. Caller holds g.mu.
func (g *sessionServer) realID(name string) string {
	if real := g.aliases["v:"+lastSeg(name)]; real != "" {
		return real
	}
	return lastSeg(name)
}

func propsMatch(have, want *commonpb.MapValue) bool {
	for k, v := range want.GetFields() {
		if !proto.Equal(have.GetFields()[k], v) {
			return false
		}
	}
	return true
}

func hasAnyUser(s *mmpb.GameSession, users []string) bool {
	if len(users) == 0 {
		return true
	}
	for _, us := range s.GetUserSessions() {
		for _, u := range users {
			if lastSeg(us.GetUser()) == lastSeg(u) {
				return true
			}
		}
	}
	return false
}

func (g *sessionServer) JoinGameSession(ctx context.Context, req *mmpb.JoinGameSessionRequest) (*mmpb.JoinGameSessionResponse, error) {
	diag("JoinGameSession", req)
	g.mu.Lock()
	defer g.mu.Unlock()
	gsid := g.realID(req.GetName())
	s := g.sessions[gsid]
	if s == nil {
		return nil, status.Errorf(codes.NotFound, "game session %q not found", req.GetName())
	}
	if s.GetPassword() != "" && req.GetPassword() != s.GetPassword() {
		return nil, status.Error(codes.PermissionDenied, "wrong password")
	}
	var ud *mmpb.UserDefinition
	if uds := req.GetUserDefinitions(); len(uds) > 0 {
		ud = userDef(ctx, uds[0])
	} else {
		ud = userDef(ctx, nil)
	}
	if int32(len(g.members[gsid])) >= s.GetMaxParticipantCount() && !memberOf(g.members[gsid], ud.GetUser()) {
		return nil, status.Error(codes.ResourceExhausted, "game session is full")
	}
	userSess := s.GetName() + "/userSessions/" + uuid4()
	n := g.addMember(gsid, &mmpb.UserSession{Name: userSess, User: ud.GetUser(), State: mmpb.UserSession_ACTIVE, Attributes: ud.GetAttributes(), CreateTime: timestamppb.Now()})
	log.Printf("[MM] Join %s <- %s (%d/%d)", gsid, lastSeg(ud.GetUser()), n, s.GetMaxParticipantCount())
	return &mmpb.JoinGameSessionResponse{
		GameSession:         g.withMembers(gsid),
		MatchedUserSessions: []*mmpb.MatchedUserSession{matched(ctx, ud, s.GetName(), userSess)},
	}, nil
}

func memberOf(list []*mmpb.UserSession, user string) bool {
	for _, m := range list {
		if m.GetUser() == user {
			return true
		}
	}
	return false
}

// SyncGameSession lets the host push its current session (properties, capacity, public flag).
func (g *sessionServer) SyncGameSession(ctx context.Context, req *mmpb.SyncGameSessionRequest) (*mmpb.SyncGameSessionResponse, error) {
	if !req.GetKeepAliveOnly() {
		diag("SyncGameSession", req)
	}
	gsid := lastSeg(req.GetGameSession().GetName())
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.sessions[gsid]
	if s == nil {
		return nil, status.Errorf(codes.NotFound, "game session %q not found", req.GetGameSession().GetName())
	}
	if in := req.GetGameSession(); !req.GetKeepAliveOnly() && in != nil {
		if in.Properties != nil {
			s.Properties = in.Properties
		}
		if in.GetMaxParticipantCount() > 0 {
			s.MaxParticipantCount = in.GetMaxParticipantCount()
		}
		s.IsPublic, s.Password, s.CanParticipate = in.GetIsPublic(), in.GetPassword(), in.GetCanParticipate()
		if in.GetState() != mmpb.GameSession_STATE_UNSPECIFIED {
			s.State = in.GetState()
		}
	}
	return &mmpb.SyncGameSessionResponse{UserSessions: append([]*mmpb.UserSession(nil), g.members[gsid]...)}, nil
}

func (g *sessionServer) ListUserSessions(ctx context.Context, req *mmpb.ListUserSessionsRequest) (*mmpb.ListUserSessionsResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return &mmpb.ListUserSessionsResponse{UserSessions: append([]*mmpb.UserSession(nil), g.members[lastSeg(req.GetParent())]...)}, nil
}

func (g *sessionServer) GetUserSession(ctx context.Context, req *mmpb.GetUserSessionRequest) (*mmpb.UserSession, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, list := range g.members {
		for _, us := range list {
			if us.GetName() == req.GetName() {
				return us, nil
			}
		}
	}
	return nil, status.Errorf(codes.NotFound, "user session %q not found", req.GetName())
}

// ---- invites by code ----

func (g *sessionServer) CreateGameSessionShortAlias(ctx context.Context, req *mmpb.CreateGameSessionShortAliasRequest) (*mmpb.GameSessionShortAlias, error) {
	code := uuid4()[:8]
	g.mu.Lock()
	g.aliases[code] = req.GetGameSessionShortAlias().GetGameSession()
	g.mu.Unlock()
	return &mmpb.GameSessionShortAlias{Name: tenantFromCtx(ctx) + "/gameSessionShortAliases/" + code, GameSession: req.GetGameSessionShortAlias().GetGameSession(), ExpireTime: timestamppb.New(time.Now().Add(time.Hour))}, nil
}

func (g *sessionServer) GetGameSessionShortAlias(ctx context.Context, req *mmpb.GetGameSessionShortAliasRequest) (*mmpb.GameSessionShortAlias, error) {
	g.mu.Lock()
	gs := g.aliases[lastSeg(req.GetName())]
	g.mu.Unlock()
	if gs == "" {
		return nil, status.Errorf(codes.NotFound, "alias %q not found", req.GetName())
	}
	return &mmpb.GameSessionShortAlias{Name: req.GetName(), GameSession: gs, ExpireTime: timestamppb.New(time.Now().Add(time.Hour))}, nil
}

// ---- connectivity ----

func (g *sessionServer) AllocateIceServerSet(ctx context.Context, req *mmpb.AllocateIceServerSetRequest) (*mmpb.IceServerSet, error) {
	diag("AllocateIceServerSet", req)
	set := &mmpb.IceServerSet{
		Name:                tenantFromCtx(ctx) + "/iceServerSets/" + uuid4(),
		StunServer:          &mmpb.StunServer{Host: envOr("NPLN_STUN_HOST", "127.0.0.1"), Port: envInt("NPLN_STUN_PORT", 3478), Protocol: mmpb.StunServer_UDP},
		Ttl:                 durationpb.New(time.Hour),
		ClientCacheDuration: durationpb.New(10 * time.Minute),
		UpdateTime:          timestamppb.Now(),
	}
	if h := envOr("NPLN_TURN_HOST", ""); h != "" {
		set.TurnServers = []*mmpb.TurnServer{{Host: h, Port: envInt("NPLN_TURN_PORT", 3478), Protocol: mmpb.TurnServer_UDP, Username: envOr("NPLN_TURN_USER", ""), Password: envOr("NPLN_TURN_PASSWORD", "")}}
	}
	return set, nil
}

func (g *sessionServer) ListLatencyMeasurementServers(ctx context.Context, req *mmpb.ListLatencyMeasurementServersRequest) (*mmpb.ListLatencyMeasurementServersResponse, error) {
	host := envOr("NPLN_LATENCY_HOST", "127.0.0.1")
	return &mmpb.ListLatencyMeasurementServersResponse{LatencyMeasurementServers: []*mmpb.LatencyMeasurementServer{
		{Name: tenantFromCtx(ctx) + "/latencyMeasurementServers/eu-01-udp", Region: "europe-west2", Host: host, Port: envInt("NPLN_LATENCY_PORT", 18504), Protocol: mmpb.LatencyMeasurementServer_UDP},
	}}, nil
}
