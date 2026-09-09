package npln

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/metadata"

	authpb "openpak/splatoon-3/proto/auth/v1"
)

// One check over the whole non-trivial path: an id_token's nnex claim is delegated to nx-baas's
// /internal/switch/identity behind our X-Internal-Key, a claim it rejects fails closed, the NPLN
// user id derives from the BAAS id, and the access/refresh tokens the Auth service hands back
// read the same PID out again.
func TestAuthRoundTrip(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NX_INTERNAL_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Nnex string
			PID  uint64
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/internal/switch/identity" || r.Header.Get("X-Internal-Key") != "k" ||
			(in.Nnex != "nx2.a.b" && in.PID != 1800000005) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"pid":1800000005,"baas_user_id":"9198f6a6930b5fd1","nickname":"Host",`+
			`"friends":[{"pid":1800000006,"baas_user_id":"0011223344556677","nickname":"Friend"}]}`)
	}))
	defer srv.Close()
	old := adapterURL
	adapterURL = srv.URL
	defer func() { adapterURL = old }()

	idToken := "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"sub":"deadbeef","nnex":"nx2.a.b"}`)) + ".sig"
	ext := &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: idToken}}

	auth := &authServer{}
	resp, err := auth.IssuePrearrangedUserToken(context.Background(),
		&authpb.IssuePrearrangedUserTokenRequest{Tenant: Tenant, UserIndex: 0, ExternalIdToken: ext})
	if err != nil {
		t.Fatalf("IssuePrearrangedUserToken: %v", err)
	}
	uid := lastSeg(resp.GetUser().GetName())
	if len(uid) != 22 || uid[:2] != "u-" || uid != userID("9198f6a6930b5fd1") {
		t.Fatalf("bad user id %q", uid)
	}

	tok := resp.GetToken()
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "bearer "+tok.GetAccessToken()))
	if pid, ok := callerPID(ctx); !ok || pid != 1800000005 {
		t.Fatalf("bearer did not resolve: pid=%d ok=%v", pid, ok)
	}
	if pid, ok := pidFromRefresh(tok.GetRefreshToken()); !ok || pid != 1800000005 {
		t.Fatalf("refresh did not resolve: pid=%d ok=%v", pid, ok)
	}
	if _, ok := pidFromRefresh(tok.GetRefreshToken() + "x"); ok {
		t.Fatal("tampered refresh token accepted")
	}

	// Fail-closed: a claim the adapter does not recognise must not mint a token.
	bad := &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{
		NsaIdToken: "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"nnex":"nx2.a.c"}`)) + ".sig"}}
	if _, err := auth.IssueToken(context.Background(), &authpb.IssueTokenRequest{ExternalIdToken: bad}); err == nil {
		t.Fatal("unprovable identity issued a token")
	}

	if me, err := lookupAccount(1800000005); err != nil || me.BaasUserID != "9198f6a6930b5fd1" {
		t.Fatalf("pid lookup: %+v %v", me, err)
	}
}
