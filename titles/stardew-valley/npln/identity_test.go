package npln

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/metadata"
)

// One check: the nnex claim is delegated to nx-baas's /internal/switch/identity (with our
// X-Internal-Key), a rejected one fails, the user id derives from the BAAS id, and the
// access/refresh tokens we issue read back the same PID.
func TestIdentityRoundTrip(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NX_INTERNAL_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Nnex string
			PID  uint64
		}
		json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/internal/switch/identity" || r.Header.Get("X-Internal-Key") != "k" || (in.Nnex != "nx2.a.b" && in.PID != 1800000005) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"pid":1800000005,"baas_user_id":"9198f6a6930b5fd1","nickname":"Host","friends":[{"pid":1800000006,"baas_user_id":"0011223344556677","nickname":"Friend"}]}`)
	}))
	defer srv.Close()
	oldURL := adapterURL
	adapterURL = srv.URL
	defer func() { adapterURL = oldURL }()

	idToken := "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"sub":"deadbeef","nnex":"nx2.a.b"}`)) + ".sig"
	acc, err := accountFromNnex(idToken)
	if err != nil || acc.PID != 1800000005 || len(acc.Friends) != 1 {
		t.Fatalf("nnex not proven: %+v %v", acc, err)
	}
	if uid := acc.UserID(); len(uid) != 22 || uid[:2] != "u-" || uid == acc.Friends[0].UserID() {
		t.Fatalf("bad user id %q", uid)
	}
	if _, err := accountFromNnex("eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"nnex":"nx2.a.c"}`)) + ".sig"); err == nil {
		t.Fatal("rejected nnex accepted")
	}
	if me, err := lookupAccount(1800000005); err != nil || me.BaasUserID != "9198f6a6930b5fd1" {
		t.Fatalf("pid lookup: %+v %v", me, err)
	}

	tok := newToken(1800000005, Tenant+"/users/"+acc.UserID(), Tenant)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "bearer "+tok.AccessToken))
	if pid, ok := callerPID(ctx); !ok || pid != 1800000005 {
		t.Fatalf("bearer did not resolve: pid=%d ok=%v", pid, ok)
	}
	if pid, ok := pidFromRefresh(tok.RefreshToken); !ok || pid != 1800000005 {
		t.Fatalf("refresh did not resolve: pid=%d ok=%v", pid, ok)
	}
}
