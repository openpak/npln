package npln

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

// One check: an nnex proof minted with the shared secret resolves to its PID, a tampered one does
// not, and the access token we issue reads back the same PID through the bearer path.
func TestIdentityRoundTrip(t *testing.T) {
	secret = []byte("test-secret")
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")

	raw := fmt.Sprintf("1800000005.stardewhost.%d", time.Now().Add(time.Hour).Unix())
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("nex:" + raw))
	nx2 := "nx2." + b64u([]byte(raw)) + "." + b64u(mac.Sum(nil))
	idToken := "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"sub":"deadbeef","nnex":"`+nx2+`"}`)) + ".sig"

	if pid, ok := pidFromNnex(idToken); !ok || pid != 1800000005 {
		t.Fatalf("nnex not proven: pid=%d ok=%v", pid, ok)
	}
	if _, ok := pidFromNexToken(nx2[:len(nx2)-2] + "xx"); ok {
		t.Fatal("tampered nnex accepted")
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
