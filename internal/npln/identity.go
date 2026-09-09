package npln

// identity — who is calling, and the tokens that prove it.
//
//   - The client's BAAS id_token (minted by nx-baas, the OpenPak Switch adapter) carries in its
//     "nnex" claim a token only the adapter can sign. We hand that claim back to the adapter,
//     which proves it and returns the player's Switch projection (pid, BAAS user id = NSA id)
//     plus the friends that have one. The signing key never lives here; a projection exists
//     only for an active, e-mail-verified OpenPak account, so there is no separate verified gate.
//   - We answer with an ES256 JWT in Nintendo's shape (npln.authorization allow ["**"]); the
//     client decodes it to learn its rights, and echoes it as `authorization: bearer` on every call.

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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	authpb "openpak/stardew-valley/proto/auth/v1"
)

const tokenTTL = 8 * time.Hour // Nintendo's exp-iat = 28800s

var (
	adapterURL = envOr("NX_INTERNAL_URL", "http://127.0.0.1:20070") // nx-baas game/internal API (ports.md)
	httpc      = &http.Client{Timeout: 5 * time.Second}
)

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// Account is the Switch adapter's view of a player (its /internal/switch/identity reply).
type Account struct {
	PID        uint64   `json:"pid"`
	BaasUserID string   `json:"baas_user_id"`
	Nickname   string   `json:"nickname"`
	Friends    []Friend `json:"friends"`
	Country    string   `json:"country"` // where the player lives; picks the region node for their rooms
}

type Friend struct {
	PID        uint64 `json:"pid"`
	BaasUserID string `json:"baas_user_id"`
	Nickname   string `json:"nickname"`
}

var userB32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// userID is the stable NPLN user id ("u-" + 20 base32 chars, the shape the client has been
// measured to accept) derived from the adapter's BAAS user id.
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

// accountFromNnex pulls the nnex claim out of the id_token and has the adapter prove it.
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
	if acc.Country != "" {
		countryByUID.Store(acc.UserID(), strings.ToUpper(acc.Country))
	}
	return acc.PID, tenant + "/users/" + acc.UserID(), nil
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

// refreshKey MACs our refresh tokens; derived from the persisted ES256 key so restarts keep them valid.
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
