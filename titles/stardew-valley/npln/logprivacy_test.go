package npln

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	authpb "github.com/openpak/npln/proto/auth/v1"
)

// lockedLog collects the log output of a test; the server logs from its own goroutines.
type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// A login and a token exchange, with sentinel ids, names, tokens and a bearer, must leave none of
// them in the log: the lines carry tags only.
func TestLogsCarryNoPersonalData(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NX_INTERNAL_KEY", "k")
	const (
		baasID   = "9198f6a6930b5fd1"
		nickname = "Sentinelnick"
		pid      = "1800000777"
		bearer   = "bearer-sentinel-5521"
		uidValue = "uid-sentinel-7731"
		garbage  = "00000000000000010000000000000000"
	)
	adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"pid":%s,"baas_user_id":%q,"nickname":%q}`, pid, baasID, nickname)
	}))
	defer adapter.Close()
	oldURL := adapterURL
	adapterURL = adapter.URL
	defer func() { adapterURL = oldURL }()

	var out lockedLog
	prev := log.Writer()
	log.SetOutput(&out)
	defer log.SetOutput(prev)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, "", AppID)
	go srv.Serve(lis)
	defer srv.Stop()
	c, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	auth := authpb.NewAuthClient(c)

	jwt := "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"openpak":"nx2.a.b","sub":"`+baasID+`"}`)) + ".sig"
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+bearer, "uid", uidValue)
	first, err := auth.IssuePrearrangedUserToken(ctx, &authpb.IssuePrearrangedUserTokenRequest{
		ExternalIdToken: &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: jwt}}})
	if err != nil {
		t.Fatal(err)
	}
	me := first.GetUser().GetName()
	auth.IssueToken(ctx, &authpb.IssueTokenRequest{User: me, ExternalIdToken: &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: garbage}}})

	logged := out.String()
	if !strings.Contains(logged, "[Auth] IssuePrearrangedUserToken pid=~") {
		t.Fatalf("login line missing or untagged:\n%s", logged)
	}
	for _, secret := range []string{baasID, nickname, pid, bearer, uidValue, garbage, jwt, me} {
		if strings.Contains(logged, secret) {
			t.Errorf("log contains %q:\n%s", secret, logged)
		}
	}
}
