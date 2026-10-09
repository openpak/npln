package npln

// Latest version only, and versions never share a session.
//
// The NSA id token a title logs in with is minted by nx-baas and carries nintendo.ai (the title)
// and nintendo.av (its installed version, %08x of the u32), the values aauth saw. aauth already
// turns an outdated version away with 2124-3001; a login that still arrives with one (a token
// cached from before an update was published) is refused here with FAILED_PRECONDITION
// (2321-3072), after asking nx-baas's /internal/title-version, the same table.
//
// Sessions keep the version of the player who made them, server-side only: nothing is added to
// what the client sees (an injected property once made Stardew drop every farm from its list).
// A player of another version is not listed a session, not placed in its pool and not let in by
// name. Only what can be read decides: an unknown version (no claim, not 8 hex digits, or a
// bearer from before a restart) meets everyone, and an nx-baas that cannot answer refuses no one.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// appVersionByUID is each user's title version as of their last id-token login.
var appVersionByUID sync.Map // user id -> %08x

// appClaims reads nintendo.ai/av out of an NSA id token's payload. nx-baas proves the token; this
// only reads it.
func appClaims(idToken string) (ai, av string) {
	seg := strings.Split(idToken, ".")
	if len(seg) < 2 {
		return "", ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(seg[1], "="))
	if err != nil {
		return "", ""
	}
	var c struct {
		Nintendo struct {
			AI string `json:"ai"`
			AV string `json:"av"`
		} `json:"nintendo"`
	}
	if json.Unmarshal(payload, &c) != nil {
		return "", ""
	}
	return c.Nintendo.AI, c.Nintendo.AV
}

func isAppVersion(av string) bool {
	if len(av) != 8 {
		return false
	}
	for _, c := range av {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// errOutdated is the refusal for a version below the latest: FAILED_PRECONDITION, 2321-3072.
func errOutdated() error {
	return status.Error(codes.FailedPrecondition, "a software update is required to play online")
}

func isOutdated(err error) bool { return status.Code(err) == codes.FailedPrecondition }

// titleOutdated asks nx-baas whether a title version is older than the latest. Any failure to
// answer is "no".
func titleOutdated(ai, av string) (bool, string) {
	if ai == "" || av == "" {
		return false, ""
	}
	q := url.Values{"title": {ai}, "version": {av}}
	req, err := http.NewRequest(http.MethodGet, adapterURL+"/internal/title-version?"+q.Encode(), nil)
	if err != nil {
		return false, ""
	}
	req.Header.Set("X-Internal-Key", os.Getenv("NX_INTERNAL_KEY"))
	resp, err := httpc.Do(req)
	if err != nil {
		log.Printf("[Auth] version check %s %s: %v (allowed)", ai, av, err)
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[Auth] version check %s %s: nx-baas %s (allowed)", ai, av, resp.Status)
		return false, ""
	}
	var v struct {
		Latest         string `json:"latest"`
		UpdateRequired bool   `json:"update_required"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return false, ""
	}
	return v.UpdateRequired, v.Latest
}

// rememberAppVersion records a proven user's version ("" forgets it).
func rememberAppVersion(uid, av string) {
	if !isAppVersion(av) {
		appVersionByUID.Delete(uid)
		return
	}
	appVersionByUID.Store(uid, strings.ToLower(av))
}

func appVersionOf(uid string) string {
	if v, ok := appVersionByUID.Load(uid); ok {
		return v.(string)
	}
	return ""
}

// callerVersion is the calling user's version, "" when unknown.
func callerVersion(ctx context.Context) string {
	if _, uid, ok := bearer(ctx); ok {
		return appVersionOf(uid)
	}
	return ""
}

// versionFits: a session takes a player of its own version, and either side unknown fits.
// Caller holds g.mu.
func (g *sessionServer) versionFits(gsid, v string) bool {
	have := g.versions[gsid]
	return have == "" || v == "" || have == v
}
