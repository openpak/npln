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

// One check: an nnex proof is delegated to the account server's /internal/pid-by-nex-token
// (with our X-Internal-Key), a rejected one fails, and the access/refresh tokens we issue read
// back the same PID.
func TestIdentityRoundTrip(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NEXTENDO_INTERNAL_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Token string }
		json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/internal/pid-by-nex-token" || r.Header.Get("X-Internal-Key") != "k" || in.Token != "nx2.a.b" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"pid":1800000005}`)
	}))
	defer srv.Close()
	oldURL := accountURL
	accountURL = srv.URL
	defer func() { accountURL = oldURL }()

	idToken := "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"sub":"deadbeef","nnex":"nx2.a.b"}`)) + ".sig"
	if pid, ok := pidFromNnex(idToken); !ok || pid != 1800000005 {
		t.Fatalf("nnex not proven: pid=%d ok=%v", pid, ok)
	}
	if _, ok := pidFromNexToken("nx2.a.c"); ok {
		t.Fatal("rejected nnex accepted")
	}

	tok := newToken(1800000005, Tenant+"/users/u-abc", Tenant)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "bearer "+tok.AccessToken))
	if pid, ok := callerPID(ctx); !ok || pid != 1800000005 {
		t.Fatalf("bearer did not resolve: pid=%d ok=%v", pid, ok)
	}
	if pid, ok := pidFromRefresh(tok.RefreshToken); !ok || pid != 1800000005 {
		t.Fatalf("refresh did not resolve: pid=%d ok=%v", pid, ok)
	}
}
