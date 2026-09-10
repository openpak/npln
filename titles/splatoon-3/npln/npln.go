// Package npln is Splatoon 3's NPLN control plane: nn.npln.* over gRPC/HTTP2/TLS, the services
// the Switch client talks to before any gameplay traffic exists. Independently written from
// public protocol documentation and our own observations (see docs/design.md for provenance).
//
// Served today: auth, friends and presence, the toyohr schedule service, the two matchmaking
// services (Matchmaker + GameSessionService) and the gamesync mailbox. Every other method is logged
// by unknownService and answered UNIMPLEMENTED — that log line is the next work item, and it is how
// we learn Splatoon 3's call order without guessing it.
package npln

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	authpb "github.com/openpak/npln/proto/auth/v1"
	friendspb "github.com/openpak/npln/proto/friends/v1"
	gspb "github.com/openpak/npln/proto/gamesync/v1"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"

	"github.com/openpak/npln/titles/splatoon-3/rotation"
)

// Tenant is Splatoon 3's NPLN tenant, and the value of the access token's npln.tid claim. The
// host t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net is the name the retail title resolves at boot.
// Confirmed — see docs/provenance.md.
const Tenant = "tenants/t-dce9377b-lp1"

// AppID goes in the access token's npln.app_id claim, and is Splatoon 3's title id. Confirmed:
// the claim carries the title id for this tenant, not an unrelated NPLN application id.
const AppID = "0100c2500fc20000"

const tokenTTL = 8 * time.Hour

// kid names our signing key. Arbitrary, but stable: the client echoes it, it must not change
// under a running session.
const kid = "e4a1b9c7-2f60-4d35-8b12-splatoon30001"

var (
	adapterURL = envOr("NX_INTERNAL_URL", "http://127.0.0.1:20070") // nx-baas game/internal API (ports.md)
	httpc      = &http.Client{Timeout: 5 * time.Second}
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func mdGet(ctx context.Context, k string) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(k); len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// tenantFromCtx: resource names must live under the CALLER's tenant, so echo what it sent.
func tenantFromCtx(ctx context.Context) string {
	if t := mdGet(ctx, "npln-tenant-id"); t != "" {
		return "tenants/" + t
	}
	return Tenant
}

// resolveTenant turns whatever the client put in a request's tenant field into a real tenant
// name. Splatoon 3 sends the alias "tenants/current" rather than naming itself, so passing the
// field through verbatim would mint a token claiming tid "current" — which is not this tenant and
// not any tenant. Anything empty or aliased resolves to ours; a real name is honoured so the
// resource names we hand back stay under the caller's own tenant.
func resolveTenant(t string) string {
	if t == "" || t == "tenants/current" || t == "current" {
		return Tenant
	}
	return t
}

func short(s string) string {
	if len(s) > 24 {
		return s[:24] + "…"
	}
	return s
}

// ---- identity: who is calling ----
//
// The client's BAAS id_token is minted by nx-baas (the OpenPak Switch adapter). Its "nnex" claim
// is a token only that adapter can sign; we hand the claim back and it returns the player's
// Switch projection. The signing key never lives here, and a projection exists only for an
// active, verified OpenPak account, so there is no second verification gate.

// Account is the Switch adapter's view of a player (its /internal/switch/identity reply).
type Account struct {
	PID        uint64   `json:"pid"`
	BaasUserID string   `json:"baas_user_id"`
	Nickname   string   `json:"nickname"`
	Friends    []Friend `json:"friends"`
}

type Friend struct {
	PID        uint64 `json:"pid"`
	BaasUserID string `json:"baas_user_id"`
	Nickname   string `json:"nickname"`
}

var userB32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// userID is the stable NPLN user id ("u-" + 20 base32 chars) derived from the BAAS user id.
func userID(baas string) string {
	sum := sha256.Sum256([]byte("npln-user:" + baas))
	return "u-" + userB32.EncodeToString(sum[:12])
}

func (a *Account) UserID() string { return userID(a.BaasUserID) }
func (f Friend) UserID() string   { return userID(f.BaasUserID) }

// adapterIdentity asks nx-baas for a player: by nnex claim (it proves the signature) or by pid
// (already proven here from our own bearer).
func adapterIdentity(q map[string]any) (*Account, error) {
	body, _ := json.Marshal(q)
	req, err := http.NewRequest(http.MethodPost, adapterURL+"/internal/switch/identity", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", os.Getenv("NX_INTERNAL_KEY"))
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("switch identity: %s", resp.Status)
	}
	var a Account
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return nil, err
	}
	if a.PID == 0 || a.BaasUserID == "" {
		return nil, fmt.Errorf("switch identity: incomplete reply")
	}
	return &a, nil
}

func lookupAccount(pid uint64) (*Account, error) { return adapterIdentity(map[string]any{"pid": pid}) }

// accountFromNnex pulls the nnex claim out of the id_token and has the adapter prove it. The
// id_token's own signature is not checked here on purpose: the nnex claim inside it is what the
// adapter signed, and only the adapter can verify it.
func accountFromNnex(idToken string) (*Account, error) {
	seg := strings.Split(idToken, ".")
	if len(seg) < 2 {
		return nil, fmt.Errorf("not a jwt")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(seg[1], "="))
	if err != nil {
		return nil, err
	}
	var claims struct {
		Nnex string `json:"nnex"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Nnex == "" {
		return nil, fmt.Errorf("no nnex claim")
	}
	return adapterIdentity(map[string]any{"nnex": claims.Nnex})
}

// gatedIdentity resolves the external id token to (pid, "tenants/…/users/u-…"). Fail-closed.
func gatedIdentity(ext *authpb.ExternalIdToken, tenant string) (uint64, string, error) {
	if tenant == "" {
		tenant = Tenant
	}
	acc, err := accountFromNnex(ext.GetNsaIdToken())
	if err != nil {
		log.Printf("[Auth] identity not provable (%v) -> REFUSED", err)
		return 0, "", status.Error(codes.PermissionDenied, "OpenPak account not recognised — link your OpenPak account to play online")
	}
	return acc.PID, tenant + "/users/" + acc.UserID(), nil
}

// ---- ES256 access tokens ----

var (
	keyOnce sync.Once
	signKey *ecdsa.PrivateKey
)

// signingKey is persisted (NPLN_JWT_KEY) so a restart does not invalidate every live session.
func signingKey() *ecdsa.PrivateKey {
	keyOnce.Do(func() {
		path := envOr("NPLN_JWT_KEY", "npln_jwt_es256.key")
		if b, err := os.ReadFile(path); err == nil {
			if blk, _ := pem.Decode(b); blk != nil {
				if k, err := x509.ParseECPrivateKey(blk.Bytes); err == nil {
					signKey = k
					return
				}
			}
		}
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			log.Printf("[Auth] ecdsa key: %v", err)
			return
		}
		signKey = k
		if der, err := x509.MarshalECPrivateKey(k); err == nil {
			if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
				log.Printf("[Auth] key not persisted (%v): tokens die with this process", err)
			}
		}
	})
	return signKey
}

func signJWT(header, payload map[string]any) string {
	hj, _ := json.Marshal(header)
	pj, _ := json.Marshal(payload)
	signing := b64u(hj) + "." + b64u(pj)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, signingKey(), sum[:])
	if err != nil {
		log.Printf("[Auth] sign: %v", err)
		return ""
	}
	sig := make([]byte, 64) // JWS: r||s left-padded, not DER
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64u(sig)
}

func verifyJWT(tok string) (payload []byte, ok bool) {
	p := strings.Split(tok, ".")
	if len(p) != 3 {
		return nil, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(p[2])
	if err != nil || len(sig) != 64 {
		return nil, false
	}
	sum := sha256.Sum256([]byte(p[0] + "." + p[1]))
	if !ecdsa.Verify(&signingKey().PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return nil, false
	}
	payload, err = base64.RawURLEncoding.DecodeString(p[1])
	if err != nil {
		return nil, false
	}
	// A token we signed is still only good until its exp: every token minted here carries one.
	var c struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &c) != nil || c.Exp == 0 || time.Now().Unix() >= c.Exp {
		return nil, false
	}
	return payload, true
}

func accountID(uid string) string { // "u-xyz…" -> "a-ayz…", the shape Nintendo's aid has
	if body := strings.TrimPrefix(uid, "u-"); len(body) > 1 {
		return "a-a" + body[1:]
	}
	return "a-openpak"
}

func mintAccessToken(pid uint64, userPath, tenant string) string {
	now := time.Now()
	uid := lastSeg(userPath)
	return signJWT(
		map[string]any{"alg": "ES256", "jku": "jwkSets/nplnAccessToken", "kid": kid},
		map[string]any{
			"exp": now.Add(tokenTTL).Unix(), "iat": now.Unix(), "iss": "default iss", "sub": uid,
			"npln": map[string]any{
				"aid": accountID(uid), "app_id": AppID,
				// ponytail: blanket allow. Splatoon 3's real authorization scope list is unknown
				// and the client only reads its own rights from it; narrow it once a capture
				// shows which scopes the title actually checks.
				"authorization": map[string]any{"allow": []string{"**"}, "deny": []string{}, "nso_restricted": false},
				"ext_id":        fmt.Sprintf("%016x", pid), "ext_id_type": 1,
				"tid": strings.TrimPrefix(tenant, "tenants/"),
			},
		})
}

// refreshKey MACs our refresh tokens; derived from the persisted ES256 key so restarts keep them
// valid.
func refreshKey() []byte {
	sum := sha256.Sum256(append([]byte("openpak-npln-refresh:"), signingKey().D.Bytes()...))
	return sum[:]
}

func newToken(pid uint64, userPath, tenant string) *authpb.Token {
	mac := hmac.New(sha256.New, refreshKey())
	body := fmt.Sprintf("openpak-npln-refresh.%d", pid)
	mac.Write([]byte(body))
	return &authpb.Token{
		User:         userPath,
		AccessToken:  mintAccessToken(pid, userPath, tenant),
		RefreshToken: body + "." + b64u(mac.Sum(nil)),
		Ttl:          durationpb.New(tokenTTL),
	}
}

func pidFromRefresh(tok string) (uint64, bool) {
	i := strings.LastIndexByte(tok, '.')
	if i <= 0 || !strings.HasPrefix(tok, "openpak-npln-refresh.") {
		return 0, false
	}
	mac := hmac.New(sha256.New, refreshKey())
	mac.Write([]byte(tok[:i]))
	if !hmac.Equal([]byte(b64u(mac.Sum(nil))), []byte(tok[i+1:])) {
		return 0, false
	}
	pid, err := strconv.ParseUint(strings.TrimPrefix(tok[:i], "openpak-npln-refresh."), 10, 64)
	return pid, err == nil && pid != 0
}

// bearer reads the PID and NPLN user id back from the bearer access token (signature and
// expiry verified with our key). Both travel inside the token, so no pairing table is needed.
func bearer(ctx context.Context) (pid uint64, uid string, ok bool) {
	a := strings.TrimSpace(mdGet(ctx, "authorization"))
	a = strings.TrimPrefix(strings.TrimPrefix(a, "Bearer "), "bearer ")
	payload, ok := verifyJWT(a)
	if !ok {
		return 0, "", false
	}
	var c struct {
		Sub  string `json:"sub"`
		Npln struct {
			ExtID string `json:"ext_id"`
		} `json:"npln"`
	}
	if json.Unmarshal(payload, &c) != nil {
		return 0, "", false
	}
	pid, err := strconv.ParseUint(c.Npln.ExtID, 16, 64)
	if err != nil || pid == 0 {
		return 0, "", false
	}
	return pid, c.Sub, true
}

func callerPID(ctx context.Context) (uint64, bool) {
	pid, _, ok := bearer(ctx)
	return pid, ok
}

// trustUIDMetadata restores the pre-2026-09 behaviour of naming the caller by the client-sent
// uid header. ponytail: a knob, not a default -- it exists only for a console run that shows a
// session or presence RPC arriving without a bearer, which is unmeasured for this title.
var trustUIDMetadata = os.Getenv("NPLN_TRUST_UID_METADATA") == "1"

// callerUID is the caller's NPLN user id, from the bearer token. The uid request metadata is
// client-controlled and is not an identity.
func callerUID(ctx context.Context) string {
	if _, uid, ok := bearer(ctx); ok && uid != "" {
		return uid
	}
	if uid := mdGet(ctx, "uid"); trustUIDMetadata && uid != "" {
		return uid
	}
	return ""
}

// callerUser is the caller's user resource name under the given tenant.
func callerUser(ctx context.Context, tenant string) (string, error) {
	uid := callerUID(ctx)
	if uid == "" {
		return "", status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	return tenant + "/users/" + uid, nil
}

// userPathForPID names an already-proven PID: from the bearer when it is the same player, else
// through the adapter. It is what RefreshToken uses instead of the user the request claims.
func userPathForPID(ctx context.Context, pid uint64) (string, error) {
	if p, uid, ok := bearer(ctx); ok && p == pid && uid != "" {
		return tenantFromCtx(ctx) + "/users/" + uid, nil
	}
	acc, err := lookupAccount(pid)
	if err != nil {
		return "", status.Error(codes.Unauthenticated, "identity could not be resolved")
	}
	return tenantFromCtx(ctx) + "/users/" + acc.UserID(), nil
}

// ---- gRPC plumbing ----

// Nintendo's gateway stamps npln-grpc-type on every response; the client is used to reading it.
func typeUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	_ = grpc.SetHeader(ctx, metadata.Pairs("npln-grpc-type", "Unary"))
	log.Printf("[RPC] %s tenant=%q uid=%q auth=%q", info.FullMethod, mdGet(ctx, "npln-tenant-id"),
		mdGet(ctx, "uid"), short(mdGet(ctx, "authorization")))
	resp, err := h(ctx, req)
	if err != nil {
		log.Printf("[RPC] %s -> ERROR %v", info.FullMethod, err)
	}
	return resp, err
}

func typeStream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
	kind := "ServerStreaming"
	switch {
	case info.IsClientStream && info.IsServerStream:
		kind = "BidirectionalStreaming"
	case info.IsClientStream:
		kind = "ClientStreaming"
	}
	_ = ss.SetHeader(metadata.Pairs("npln-grpc-type", kind))
	log.Printf("[RPC] %s (%s)", info.FullMethod, kind)
	err := h(srv, ss)
	if err != nil {
		log.Printf("[RPC] %s -> ERROR %v", info.FullMethod, err)
	}
	return err
}

// unknownService logs any method we do not serve yet: that log line IS the next work item, and
// for this title it is the whole point of deploying the skeleton.
func unknownService(_ any, ss grpc.ServerStream) error {
	m, _ := grpc.MethodFromServerStream(ss)
	log.Printf("[RPC] UNIMPLEMENTED %s — next handler to write", m)
	return status.Errorf(codes.Unimplemented, "method %s not implemented", m)
}

// connTracer logs connection lifetime: "h2 established" with zero RPCs is the client-side cancel
// signature (error 2321-4992), which is what the NPLN online-gate playbook tells you to look for.
type connTracer struct{ open atomic.Int64 }

func (*connTracer) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context   { return ctx }
func (*connTracer) HandleRPC(context.Context, stats.RPCStats)                         {}
func (*connTracer) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }
func (c *connTracer) HandleConn(_ context.Context, s stats.ConnStats) {
	switch s.(type) {
	case *stats.ConnBegin:
		log.Printf("[CONN] begin (h2 established) open=%d", c.open.Add(1))
	case *stats.ConnEnd:
		log.Printf("[CONN] end open=%d", c.open.Add(-1))
	}
}

// NewServer wires the services we can answer. creds==nil gives plaintext h2c (behind a
// TLS-terminating edge). rot==nil leaves the schedule service unregistered — the rest of the
// server still works, and the game will report that stage information is unavailable.
func NewServer(creds credentials.TransportCredentials, rot *rotation.File) *grpc.Server {
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(typeUnary),
		grpc.StreamInterceptor(typeStream),
		grpc.UnknownServiceHandler(unknownService),
		grpc.StatsHandler(&connTracer{}),
		// The client pings often, also without streams; grpc-go's default policy answers with
		// GOAWAY(ENHANCE_YOUR_CALM) and kills every in-flight RPC (measured for Stardew against
		// the same SDK family).
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 5 * time.Second, PermitWithoutStream: true}),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 15 * time.Second, Timeout: 10 * time.Second}),
	}
	if creds != nil {
		opts = append(opts, grpc.Creds(creds))
	}
	s := grpc.NewServer(opts...)
	authpb.RegisterAuthServer(s, &authServer{})
	// Friends and presence must both be dynamic. A static friend list stalls the game's session
	// setup, and a static presence list means two players can never see each other.
	friendspb.RegisterFriendsServer(s, &friendsServer{})
	friendspb.RegisterPresenceServiceServer(s, &presenceServer{})
	// Matchmaking and the gamesync mailbox: Splatoon 3 relays, so these are protocol bookkeeping
	// over an in-memory store, not a game server. GameSessionService serves host-created rooms
	// (room codes, invitations) and AllocateIceServerSet; Matchmaker serves public matchmaking;
	// Gamesync is the document mailbox the consoles rendezvous through.
	mmpb.RegisterGameSessionServiceServer(s, &gameSessionService{})
	mmpb.RegisterMatchmakerServer(s, &matchmaker{})
	gspb.RegisterGamesyncServer(s, &gamesync{})
	if rot != nil {
		toyohrpb.RegisterScheduleServer(s, &scheduleServer{rot: rot})
	} else {
		log.Printf("[Schedule] no rotation loaded — the lobby will report stage information unavailable")
	}
	return s
}
