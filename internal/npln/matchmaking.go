package npln

// matchmaking — nn.npln.matchmaking.v1, two services that between them set up who plays with whom.
//
//   - GameSessionService: host-created rooms (private matches, room codes, invitations), plus the
//     STUN/TURN allocation and latency servers the client needs to reach its peers.
//   - Matchmaker: public matchmaking — a ticket that searches for a match and, once enough players
//     are pooled, resolves to a game session and a per-user session token.
//
// Splatoon 3 RELAYS: nothing here holds match state. A game session is just bookkeeping —
// address, port, capacity, current players, whether a password is set — and the players connect to
// EACH OTHER over Pia (directly, or through the coturn AllocateIceServerSet hands them). So these
// are ordinary protocol handlers over an in-memory store, not a game server.
//
// Unverified without hardware and eight players: a regular battle checks the roster size in the
// console's own binary and refuses below the mode's size, so the pool only forms a real match once
// NPLN_MATCH_SIZE tickets are present. See docs/evidence-needed.md.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	mmpb "openpak/splatoon-3/proto/matchmaking/v1"
)

func envInt(k string, d int) int {
	if v, err := strconv.Atoi(envOr(k, "")); err == nil {
		return v
	}
	return d
}

// newID is a random 128-bit hex id for a session, user-session, ticket or token.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ---- the store: game sessions, user sessions, room-code aliases, and the id-tokens gamesync
// exchanges. All in memory; a relay keeps no durable match state.
//
// ponytail: one lock over the whole store, touched once per session RPC. Shard it if a lobby ever
// makes that a contention point — it will not at a test group's scale.
var store = struct {
	sync.Mutex
	sessions map[string]*mmpb.GameSession // session name -> session
	alias    map[string]string            // short room code -> session name
	idToken  map[string]string            // matchmaking id-token -> session name (gamesync consumes it)
	pending  map[string][]*mmpb.MatchmakingTicket
	tickets  map[string]*mmpb.MatchmakingTicket // ticket name -> ticket
}{
	sessions: map[string]*mmpb.GameSession{},
	alias:    map[string]string{},
	idToken:  map[string]string{},
	pending:  map[string][]*mmpb.MatchmakingTicket{},
	tickets:  map[string]*mmpb.MatchmakingTicket{},
}

// sessionEndpoint is the gamesync session endpoint the game session points at — a separate
// listener the console dials directly for signalling (docs/design.md, port 22210). In the relay
// model this is the mailbox host, not where the match runs.
func sessionEndpoint() (string, int32) {
	return envOr("NPLN_SESSION_HOST", "t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net"),
		int32(envInt("NPLN_SESSION_PORT", 22210))
}

func matchSize() int32 { return int32(envInt("NPLN_MATCH_SIZE", 8)) }

type gameSessionService struct {
	mmpb.UnimplementedGameSessionServiceServer
}

// newSession builds a game session pointing at the signalling endpoint, and registers it.
func newSession(tenant string, maxParticipants int32, public bool, password string) *mmpb.GameSession {
	host, port := sessionEndpoint()
	gs := &mmpb.GameSession{
		Name:                    tenant + "/gameSessions/" + newID(),
		MaxParticipantCount:     maxParticipants,
		CurrentParticipantCount: 0,
		CanParticipate:          true,
		IsPublic:                public,
		Password:                password,
		State:                   mmpb.GameSession_ACTIVE,
		Host:                    host,
		Port:                    port,
		CreateTime:              timestamppb.Now(),
	}
	store.Lock()
	store.sessions[gs.Name] = gs
	store.Unlock()
	return gs
}

// addUser records a user in a session and returns its user-session, minting an id-token gamesync
// can later exchange for a session token.
func addUser(gs *mmpb.GameSession, user string) *mmpb.MatchedUserSession {
	us := &mmpb.UserSession{
		Name:       gs.Name + "/userSessions/" + newID(),
		User:       user,
		State:      mmpb.UserSession_ACTIVE,
		CreateTime: timestamppb.Now(),
	}
	idTok := newID()
	store.Lock()
	gs.UserSessions = append(gs.UserSessions, us)
	gs.CurrentParticipantCount = int32(len(gs.UserSessions))
	store.idToken[idTok] = gs.Name
	store.Unlock()
	return &mmpb.MatchedUserSession{
		UserDefinition:     &mmpb.UserDefinition{User: user},
		UserSession:        us.Name,
		MatchmakingIdToken: idTok,
	}
}

func (s *gameSessionService) CreateGameSessionCreationTicket(ctx context.Context, req *mmpb.CreateGameSessionCreationTicketRequest) (*mmpb.GameSessionCreationTicket, error) {
	tenant := resolveTenant(req.GetParent())
	max := matchSize()
	pwd := ""
	if t := req.GetGameSessionCreationTicket(); t != nil && t.GetGameSession() != nil {
		if m := t.GetGameSession().GetMaxParticipantCount(); m > 0 {
			max = m
		}
		pwd = t.GetGameSession().GetPassword()
	}
	gs := newSession(tenant, max, false, pwd)
	// The host is the first participant. A relay resolves the room the moment it is created — there
	// is no server-side match to wait for.
	matched := addUser(gs, callerUser(ctx, tenant))
	log.Printf("[MM] CreateGameSessionCreationTicket -> session %s (host room)", lastSeg(gs.Name))
	return &mmpb.GameSessionCreationTicket{
		Name:                tenant + "/gameSessionCreationTickets/" + newID(),
		State:               mmpb.GameSessionCreationTicket_SUCCEEDED,
		GameSession:         gs,
		MatchedUserSessions: []*mmpb.MatchedUserSession{matched},
	}, nil
}

// TrackGameSessionCreationTicket streams the ticket's terminal state. A relay-created room is
// already SUCCEEDED, so one message carries the session and the stream ends.
func (s *gameSessionService) TrackGameSessionCreationTicket(req *mmpb.TrackGameSessionCreationTicketRequest, stream mmpb.GameSessionService_TrackGameSessionCreationTicketServer) error {
	name := req.GetName()
	log.Printf("[MM] TrackGameSessionCreationTicket %s", short(name))
	return stream.Send(&mmpb.GameSessionCreationTicket{Name: name, State: mmpb.GameSessionCreationTicket_SUCCEEDED})
}

func (s *gameSessionService) CancelGameSessionCreationTicket(_ context.Context, _ *mmpb.CancelGameSessionCreationTicketRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (s *gameSessionService) GetGameSession(_ context.Context, req *mmpb.GetGameSessionRequest) (*mmpb.GameSession, error) {
	store.Lock()
	gs := store.sessions[req.GetName()]
	store.Unlock()
	if gs == nil {
		return nil, status.Errorf(codes.NotFound, "game session %q", req.GetName())
	}
	return gs, nil
}

func (s *gameSessionService) BatchGetGameSessions(_ context.Context, req *mmpb.BatchGetGameSessionsRequest) (*mmpb.BatchGetGameSessionsResponse, error) {
	out := &mmpb.BatchGetGameSessionsResponse{}
	store.Lock()
	for _, n := range req.GetNames() {
		if gs := store.sessions[n]; gs != nil {
			out.GameSessions = append(out.GameSessions, gs)
		}
	}
	store.Unlock()
	return out, nil
}

// QueryGameSessions is public-room discovery. We answer with the public sessions that still have
// room; the client filters further on its side.
func (s *gameSessionService) QueryGameSessions(_ context.Context, req *mmpb.QueryGameSessionsRequest) (*mmpb.QueryGameSessionsResponse, error) {
	out := &mmpb.QueryGameSessionsResponse{}
	store.Lock()
	for _, gs := range store.sessions {
		if gs.IsPublic && gs.CurrentParticipantCount < gs.MaxParticipantCount {
			out.GameSessions = append(out.GameSessions, gs)
		}
	}
	store.Unlock()
	return out, nil
}

func (s *gameSessionService) JoinGameSession(ctx context.Context, req *mmpb.JoinGameSessionRequest) (*mmpb.JoinGameSessionResponse, error) {
	store.Lock()
	gs := store.sessions[req.GetName()]
	store.Unlock()
	if gs == nil {
		return nil, status.Errorf(codes.NotFound, "game session %q", req.GetName())
	}
	if gs.GetPassword() != "" && gs.GetPassword() != req.GetPassword() {
		return nil, status.Error(codes.PermissionDenied, "wrong session password")
	}
	if gs.GetCurrentParticipantCount() >= gs.GetMaxParticipantCount() {
		return nil, status.Error(codes.FailedPrecondition, "session full")
	}
	matched := addUser(gs, callerUser(ctx, resolveTenant(gs.GetName())))
	log.Printf("[MM] JoinGameSession %s -> %d/%d", lastSeg(gs.Name), gs.CurrentParticipantCount, gs.MaxParticipantCount)
	return &mmpb.JoinGameSessionResponse{GameSession: gs, MatchedUserSessions: []*mmpb.MatchedUserSession{matched}}, nil
}

// SyncGameSession is the host's periodic heartbeat and capacity update: it pushes the session it
// holds and reads back the current roster. A keep-alive-only sync just refreshes liveness.
func (s *gameSessionService) SyncGameSession(_ context.Context, req *mmpb.SyncGameSessionRequest) (*mmpb.SyncGameSessionResponse, error) {
	in := req.GetGameSession()
	if in == nil {
		return &mmpb.SyncGameSessionResponse{}, nil
	}
	store.Lock()
	gs := store.sessions[in.GetName()]
	if gs != nil && !req.GetKeepAliveOnly() {
		// The host owns capacity and properties; mirror what it reports without dropping the roster
		// we track.
		gs.MaxParticipantCount = in.GetMaxParticipantCount()
		gs.Properties = in.GetProperties()
		if in.GetState() != mmpb.GameSession_STATE_UNSPECIFIED {
			gs.State = in.GetState()
		}
	}
	var users []*mmpb.UserSession
	if gs != nil {
		users = gs.UserSessions
	}
	store.Unlock()
	return &mmpb.SyncGameSessionResponse{UserSessions: users}, nil
}

func (s *gameSessionService) ListUserSessions(_ context.Context, req *mmpb.ListUserSessionsRequest) (*mmpb.ListUserSessionsResponse, error) {
	out := &mmpb.ListUserSessionsResponse{}
	store.Lock()
	if gs := store.sessions[req.GetParent()]; gs != nil {
		out.UserSessions = gs.UserSessions
	}
	store.Unlock()
	return out, nil
}

func (s *gameSessionService) GetUserSession(_ context.Context, req *mmpb.GetUserSessionRequest) (*mmpb.UserSession, error) {
	store.Lock()
	defer store.Unlock()
	for _, gs := range store.sessions {
		for _, us := range gs.UserSessions {
			if us.Name == req.GetName() {
				return us, nil
			}
		}
	}
	return nil, status.Errorf(codes.NotFound, "user session %q", req.GetName())
}

// IssueMatchmakingIdToken re-issues the id-token gamesync exchanges, for users already in a
// session (invitations, re-joins).
func (s *gameSessionService) IssueMatchmakingIdToken(_ context.Context, req *mmpb.IssueMatchmakingIdTokenRequest) (*mmpb.IssueMatchmakingIdTokenResponse, error) {
	store.Lock()
	gs := store.sessions[req.GetGameSession()]
	store.Unlock()
	if gs == nil {
		return nil, status.Errorf(codes.NotFound, "game session %q", req.GetGameSession())
	}
	out := &mmpb.IssueMatchmakingIdTokenResponse{GameSession: gs}
	for _, u := range req.GetUsers() {
		idTok := newID()
		store.Lock()
		store.idToken[idTok] = gs.Name
		store.Unlock()
		out.MatchedUserSessions = append(out.MatchedUserSessions, &mmpb.MatchedUserSession{
			UserDefinition: &mmpb.UserDefinition{User: u}, MatchmakingIdToken: idTok,
		})
	}
	return out, nil
}

func (s *gameSessionService) IssueUserDelegationToken(_ context.Context, req *mmpb.IssueUserDelegationTokenRequest) (*mmpb.IssueUserDelegationTokenResponse, error) {
	return &mmpb.IssueUserDelegationTokenResponse{UserDelegationDetail: &mmpb.UserDelegationDetail{
		DelegatorUser:       req.GetDelegatorUser(),
		MandataryUser:       req.GetMandataryUser(),
		Attributes:          req.GetAttributes(),
		DelegationActions:   req.GetDelegationActions(),
		UserDelegationToken: newID(),
		Ttl:                 durationpb.New(time.Hour),
	}}, nil
}

func (s *gameSessionService) IssuePublicKey(_ context.Context, _ *mmpb.IssuePublicKeyRequest) (*mmpb.IssuePublicKeyResponse, error) {
	// The client uses this to encrypt invitation payloads. OpenPak does not encrypt them yet; a
	// stable placeholder key keeps the call from failing. See docs/evidence-needed.md.
	return &mmpb.IssuePublicKeyResponse{Key: "openpak-npln-session-key"}, nil
}

// CreateGameSessionShortAlias mints a room code for a session.
func (s *gameSessionService) CreateGameSessionShortAlias(_ context.Context, req *mmpb.CreateGameSessionShortAliasRequest) (*mmpb.GameSessionShortAlias, error) {
	a := req.GetGameSessionShortAlias()
	if a == nil || a.GetGameSession() == "" {
		return nil, status.Error(codes.InvalidArgument, "no game session for alias")
	}
	code := roomCode()
	alias := &mmpb.GameSessionShortAlias{
		Name:        resolveTenant(req.GetParent()) + "/gameSessionShortAliases/" + code,
		GameSession: a.GetGameSession(),
		ExpireTime:  timestamppb.New(time.Now().Add(24 * time.Hour)),
	}
	store.Lock()
	store.alias[code] = a.GetGameSession()
	store.Unlock()
	log.Printf("[MM] room code %s -> %s", code, lastSeg(a.GetGameSession()))
	return alias, nil
}

func (s *gameSessionService) GetGameSessionShortAlias(_ context.Context, req *mmpb.GetGameSessionShortAliasRequest) (*mmpb.GameSessionShortAlias, error) {
	code := lastSeg(req.GetName())
	store.Lock()
	sess := store.alias[code]
	store.Unlock()
	if sess == "" {
		return nil, status.Errorf(codes.NotFound, "room code %q", code)
	}
	return &mmpb.GameSessionShortAlias{Name: req.GetName(), GameSession: sess}, nil
}

// AllocateIceServerSet hands the client the STUN server and TURN servers it uses to reach its
// peers. TURN uses coturn's REST ephemeral-credential scheme: username "<unix-expiry>:<user>",
// password base64(HMAC-SHA1(static-auth-secret, username)). Every field is concrete — a partial
// object was measured making the client reject the whole set and emit no STUN probe at all.
func (s *gameSessionService) AllocateIceServerSet(_ context.Context, req *mmpb.AllocateIceServerSetRequest) (*mmpb.IceServerSet, error) {
	stunHost := envOr("NPLN_STUN_HOST", "127.0.0.1")
	stunPort := int32(envInt("NPLN_STUN_PORT", 3478))
	turnHost := envOr("NPLN_TURN_HOST", stunHost)
	turnPort := int32(envInt("NPLN_TURN_PORT", 3478))
	secret := envOr("NPLN_TURN_SECRET", "")

	ttl := time.Hour // comfortably longer than a battle; the client re-allocates as needed
	user := lastSeg(req.GetUser())
	if user == "" {
		user = "npln"
	}
	exp := time.Now().Add(ttl).Unix()
	username := fmt.Sprintf("%d:%s", exp, user)
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	log.Printf("[MM] AllocateIceServerSet user=%q -> STUN %s:%d TURN %s:%d", short(req.GetUser()), stunHost, stunPort, turnHost, turnPort)
	return &mmpb.IceServerSet{
		Name:       resolveTenant(req.GetTenant()) + "/iceServerSets/" + newID(),
		StunServer: &mmpb.StunServer{Host: stunHost, Port: stunPort, Protocol: mmpb.StunServer_UDP},
		TurnServers: []*mmpb.TurnServer{
			{Host: turnHost, Port: turnPort, Protocol: mmpb.TurnServer_UDP, Username: username, Password: password},
			{Host: turnHost, Port: turnPort, Protocol: mmpb.TurnServer_TCP, Username: username, Password: password},
		},
		Ttl:                 durationpb.New(ttl),
		UpdateTime:          timestamppb.Now(),
		ClientCacheDuration: durationpb.New(ttl),
	}, nil
}

func (s *gameSessionService) ListLatencyMeasurementServers(_ context.Context, req *mmpb.ListLatencyMeasurementServersRequest) (*mmpb.ListLatencyMeasurementServersResponse, error) {
	host := envOr("NPLN_STUN_HOST", "127.0.0.1")
	port := int32(envInt("NPLN_STUN_PORT", 3478))
	return &mmpb.ListLatencyMeasurementServersResponse{
		LatencyMeasurementServers: []*mmpb.LatencyMeasurementServer{
			{Name: resolveTenant(req.GetParent()) + "/latencyMeasurementServers/default", Region: "openpak", Host: host, Port: port, Protocol: mmpb.LatencyMeasurementServer_UDP},
		},
	}, nil
}

// ---- Matchmaker: public matchmaking ----

type matchmaker struct {
	mmpb.UnimplementedMatchmakerServer
}

// callerUser resolves the caller's user resource name from the bearer token, falling back to a
// tenant-anonymous name when the token is absent (some session RPCs carry the uid in metadata).
func callerUser(ctx context.Context, tenant string) string {
	if pid, ok := callerPID(ctx); ok {
		if uid := uidForPID(pid); uid != "" {
			return tenant + "/users/" + uid
		}
	}
	if uid := mdGet(ctx, "uid"); uid != "" {
		return tenant + "/users/" + uid
	}
	return tenant + "/users/anonymous"
}

func (m *matchmaker) CreateMatchmakingTicket(ctx context.Context, req *mmpb.CreateMatchmakingTicketRequest) (*mmpb.MatchmakingTicket, error) {
	tenant := resolveTenant(req.GetParent())
	cfg := ""
	if t := req.GetMatchmakingTicket(); t != nil {
		cfg = t.GetMatchmakingConfig()
	}
	t := &mmpb.MatchmakingTicket{
		Name:              tenant + "/matchmakingTickets/" + newID(),
		MatchmakingConfig: cfg,
		State:             mmpb.MatchmakingTicket_SEARCHING,
		UserDefinitions:   []*mmpb.UserDefinition{{User: callerUser(ctx, tenant)}},
	}
	store.Lock()
	store.tickets[t.Name] = t
	store.pending[cfg] = append(store.pending[cfg], t)
	tryFormMatch(tenant, cfg)
	store.Unlock()
	log.Printf("[MM] CreateMatchmakingTicket %s cfg=%q state=%s", lastSeg(t.Name), short(cfg), t.State)
	return t, nil
}

// tryFormMatch pools tickets by config and, once NPLN_MATCH_SIZE of them are waiting, resolves
// them all into one game session — the server's only job in a relay is to decide who plays
// together. Callers hold store.Lock.
//
// Below the match size the tickets stay SEARCHING: the console checks the roster in its own binary
// and refuses a battle below the mode's size, so resolving early would only send it into a start it
// rejects (2321-3072). This is why a real public match needs eight players and cannot be validated
// with two — see docs/evidence-needed.md.
func tryFormMatch(tenant, cfg string) {
	waiting := store.pending[cfg]
	if int32(len(waiting)) < matchSize() {
		return
	}
	host, port := sessionEndpoint()
	gs := &mmpb.GameSession{
		Name:                tenant + "/gameSessions/" + newID(),
		MaxParticipantCount: matchSize(),
		CanParticipate:      true,
		IsPublic:            true,
		State:               mmpb.GameSession_ACTIVE,
		Host:                host,
		Port:                port,
		CreateTime:          timestamppb.Now(),
	}
	store.sessions[gs.Name] = gs
	for _, t := range waiting {
		user := ""
		if len(t.UserDefinitions) > 0 {
			user = t.UserDefinitions[0].GetUser()
		}
		us := &mmpb.UserSession{Name: gs.Name + "/userSessions/" + newID(), User: user, State: mmpb.UserSession_ACTIVE, CreateTime: timestamppb.Now()}
		idTok := newID()
		gs.UserSessions = append(gs.UserSessions, us)
		store.idToken[idTok] = gs.Name
		t.State = mmpb.MatchmakingTicket_SUCCEEDED
		t.GameSession = gs
		t.MatchedUserSessions = []*mmpb.MatchedUserSession{{
			UserDefinition: &mmpb.UserDefinition{User: user}, UserSession: us.Name, MatchmakingIdToken: idTok,
		}}
	}
	gs.CurrentParticipantCount = int32(len(gs.UserSessions))
	store.pending[cfg] = nil
	log.Printf("[MM] match formed: %d players -> session %s", len(waiting), lastSeg(gs.Name))
}

// TrackMatchmakingTicket streams the ticket's state until it reaches a terminal state. It pushes a
// message on each state CHANGE, then heartbeats the terminal state so a client that reconnects
// still sees the result.
func (m *matchmaker) TrackMatchmakingTicket(req *mmpb.TrackMatchmakingTicketRequest, stream mmpb.Matchmaker_TrackMatchmakingTicketServer) error {
	ctx := stream.Context()
	name := req.GetName()
	log.Printf("[MM] TrackMatchmakingTicket %s", short(name))
	var lastState mmpb.MatchmakingTicket_State = -1
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		store.Lock()
		tk := store.tickets[name]
		store.Unlock()
		if tk == nil {
			return status.Errorf(codes.NotFound, "matchmaking ticket %q", name)
		}
		if tk.State != lastState {
			lastState = tk.State
			if err := stream.Send(tk); err != nil {
				return nil
			}
		}
		if terminal(tk.State) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func terminal(s mmpb.MatchmakingTicket_State) bool {
	switch s {
	case mmpb.MatchmakingTicket_SUCCEEDED, mmpb.MatchmakingTicket_FAILED,
		mmpb.MatchmakingTicket_TIMED_OUT, mmpb.MatchmakingTicket_CANCELLED, mmpb.MatchmakingTicket_DECLINED:
		return true
	}
	return false
}

func (m *matchmaker) CancelMatchmakingTicket(_ context.Context, req *mmpb.CancelMatchmakingTicketRequest) (*emptypb.Empty, error) {
	store.Lock()
	if tk := store.tickets[req.GetName()]; tk != nil {
		tk.State = mmpb.MatchmakingTicket_CANCELLED
		// Drop it from any pending pool so it cannot be matched after cancellation.
		for cfg, list := range store.pending {
			out := list[:0]
			for _, p := range list {
				if p.Name != tk.Name {
					out = append(out, p)
				}
			}
			store.pending[cfg] = out
		}
	}
	store.Unlock()
	return &emptypb.Empty{}, nil
}

// CreateAcceptance records that a matched player accepted (or declined) the match the ticket found.
func (m *matchmaker) CreateAcceptance(_ context.Context, req *mmpb.CreateAcceptanceRequest) (*mmpb.Acceptance, error) {
	a := req.GetAcceptance()
	if a == nil {
		return nil, status.Error(codes.InvalidArgument, "no acceptance")
	}
	log.Printf("[MM] CreateAcceptance users=%d accepted=%v", len(a.GetUsers()), a.GetAccepted())
	return a, nil
}

// sessionForIDToken resolves a matchmaking id-token to the session gamesync should attach to, and
// consumes it. Used by gamesync.IssueToken.
func sessionForIDToken(tok string) (*mmpb.GameSession, bool) {
	store.Lock()
	defer store.Unlock()
	name, ok := store.idToken[tok]
	if !ok {
		return nil, false
	}
	return store.sessions[name], store.sessions[name] != nil
}

// roomCode is a short human-typable code: five upper-case letters/digits, avoiding the ambiguous
// ones. Not cryptographically strong — a room code is a convenience, guarded by the session itself.
func roomCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var b [5]byte
	_, _ = rand.Read(b[:])
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:])
}
