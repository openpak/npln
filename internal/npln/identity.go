package npln

// identity — who is calling, and the tokens that prove it.
//
//   - The emulator's BAAS id_token carries, in its "nnex" claim, the nx2 token nextendo-account
//     signed with the shared NEXTENDO_SECRET ("nx2.<b64(pid.username.expiry)>.<b64(hmac)>").
//     That HMAC is the proof; the id_token's own RS256 signature is not checked here.
//   - The account server's /internal/npln-friends gate then requires a verified account.
//   - We answer with an ES256 JWT in Nintendo's shape (npln.authorization allow ["**"]); the
//     client decodes it to learn its rights, and echoes it as `authorization: bearer` on every call.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	authpb "github.com/NextendoNetwork/stardew-nextendo/proto/auth/v1"
)

const tokenTTL = 8 * time.Hour // Nintendo's exp-iat = 28800s

var (
	secret     = []byte(os.Getenv("NEXTENDO_SECRET"))
	accountURL = envOr("NEXTENDO_ACCOUNT_URL", "http://account:8080")
	httpc      = &http.Client{Timeout: 5 * time.Second}
)

func allowUnverified() bool { return os.Getenv("NPLN_ALLOW_UNVERIFIED") == "1" }

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// pidFromNnex verifies the nx2 token found in the id_token's nnex claim and returns the proven PID.
func pidFromNnex(idToken string) (uint64, bool) {
	seg := strings.Split(idToken, ".")
	if len(seg) < 2 {
		return 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(seg[1], "="))
	if err != nil {
		return 0, false
	}
	var claims struct {
		Nnex string `json:"nnex"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Nnex == "" {
		return 0, false
	}
	return pidFromNexToken(claims.Nnex)
}

func pidFromNexToken(s string) (uint64, bool) {
	if len(secret) == 0 || !strings.HasPrefix(s, "nx2.") {
		return 0, false
	}
	parts := strings.Split(s[4:], ".")
	if len(parts) != 2 {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("nex:" + string(raw)))
	if !hmac.Equal([]byte(b64u(mac.Sum(nil))), []byte(parts[1])) {
		return 0, false
	}
	f := strings.SplitN(string(raw), ".", 3) // pid.username.expiry
	if len(f) != 3 {
		return 0, false
	}
	pid, err := strconv.ParseUint(f[0], 10, 64)
	exp, eerr := strconv.ParseInt(f[2], 10, 64)
	if err != nil || pid == 0 || eerr != nil || time.Now().Unix() > exp {
		return 0, false
	}
	return pid, true
}

// Account is the account server's view of a player (its /internal/npln-friends reply).
type Account struct {
	PID        uint64   `json:"pid"`
	UserID     string   `json:"user_id"`
	AccountHex string   `json:"account_hex"`
	Verified   bool     `json:"verified"`
	Friends    []Friend `json:"friends"`
}

type Friend struct {
	PID        uint64 `json:"pid"`
	UserID     string `json:"user_id"`
	AccountHex string `json:"account_hex"`
	Name       string `json:"name"`
}

func lookupAccount(pid uint64) (*Account, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/internal/npln-friends?pid=%d", accountURL, pid), nil)
	if err != nil {
		return nil, err
	}
	if k := os.Getenv("NEXTENDO_INTERNAL_KEY"); k != "" { // the account server's off-network caller key
		req.Header.Set("X-Internal-Key", k)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("npln-friends pid=%d: %s", pid, resp.Status)
	}
	var a Account
	return &a, json.NewDecoder(resp.Body).Decode(&a)
}

// gatedIdentity resolves the external id token to (pid, "tenants/…/users/u-…"). Fail-closed.
func gatedIdentity(ext *authpb.ExternalIdToken, tenant string) (uint64, string, error) {
	if tenant == "" {
		tenant = Tenant
	}
	pid, ok := pidFromNnex(ext.GetNsaIdToken())
	if !ok {
		log.Printf("[Auth] identity not provable (no valid nnex claim) -> REFUSED")
		return 0, "", status.Error(codes.PermissionDenied, "Nextendo account not recognised — sign in with your Nextendo account to play online")
	}
	acc, err := lookupAccount(pid)
	if err != nil {
		log.Printf("[Auth] nnex proves pid=%d but account server failed: %v -> REFUSED", pid, err)
		return 0, "", status.Error(codes.PermissionDenied, "Nextendo account not reachable")
	}
	if !acc.Verified && !allowUnverified() {
		return 0, "", status.Error(codes.PermissionDenied, "Nextendo account not verified — verify your e-mail to play online")
	}
	return pid, tenant + "/users/" + acc.UserID, nil
}

// ---- ES256 access token ----

var (
	keyOnce sync.Once
	signKey *ecdsa.PrivateKey
)

const kid = "b7e9c1a2-5d3f-4c8e-9a10-stardew00001"

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
	k := signingKey()
	hj, _ := json.Marshal(header)
	pj, _ := json.Marshal(payload)
	signing := b64u(hj) + "." + b64u(pj)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, k, sum[:])
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
	return payload, err == nil
}

func accountID(uid string) string { // "u-xyz…" -> "a-ayz…", the shape Nintendo's aid has
	if body := strings.TrimPrefix(uid, "u-"); len(body) > 1 {
		return "a-a" + body[1:]
	}
	return "a-nextendo"
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
				"authorization": map[string]any{"allow": []string{"**"}, "deny": []string{}, "nso_restricted": false},
				"ext_id":        fmt.Sprintf("%016x", pid), "ext_id_type": 1,
				"tid": strings.TrimPrefix(tenant, "tenants/"),
			},
		})
}

// mintSessionToken is the `gss` JWT a MatchedUserSession carries; the client's Pia/NPLN layer
// parses it to build Gamesync/IssueToken (shape measured on a real capture by the reference server).
func mintSessionToken(uid, tenant, gsName, userSess, team, attrJSON, ltcyJSON string) string {
	now := time.Now()
	return signJWT(
		map[string]any{"alg": "ES256", "kid": kid},
		map[string]any{
			"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "iss": "gss", "sub": uid,
			"gamesync": map[string]any{
				"attr": attrJSON, "gsid": lastSeg(gsName), "ltcy": ltcyJSON, "team": team,
				"tid": strings.TrimPrefix(tenant, "tenants/"), "typ": 1, "uid": uid, "usid": lastSeg(userSess),
			},
		})
}

func newToken(pid uint64, userPath, tenant string) *authpb.Token {
	mac := hmac.New(sha256.New, secret)
	body := fmt.Sprintf("nextendo-npln-refresh.%d", pid)
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
	if i <= 0 || !strings.HasPrefix(tok, "nextendo-npln-refresh.") {
		return 0, false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(tok[:i]))
	if !hmac.Equal([]byte(b64u(mac.Sum(nil))), []byte(tok[i+1:])) {
		return 0, false
	}
	pid, err := strconv.ParseUint(strings.TrimPrefix(tok[:i], "nextendo-npln-refresh."), 10, 64)
	return pid, err == nil && pid != 0
}

// callerPID reads the PID back from the bearer access token (signature verified with our key).
func callerPID(ctx context.Context) (uint64, bool) {
	a := strings.TrimSpace(mdGet(ctx, "authorization"))
	a = strings.TrimPrefix(strings.TrimPrefix(a, "Bearer "), "bearer ")
	payload, ok := verifyJWT(a)
	if !ok {
		return 0, false
	}
	var c struct {
		Npln struct {
			ExtID string `json:"ext_id"`
		} `json:"npln"`
	}
	if json.Unmarshal(payload, &c) != nil {
		return 0, false
	}
	pid, err := strconv.ParseUint(c.Npln.ExtID, 16, 64)
	return pid, err == nil && pid != 0
}
