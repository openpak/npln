package npln

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/openpak/npln/bans"
	authpb "github.com/openpak/npln/proto/auth/v1"
)

// A banned account gets no token, and a token it already holds stops working.
func TestBannedAccountIsLockedOut(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NX_INTERNAL_KEY", "k")
	var banned atomic.Bool
	adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if banned.Load() {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":"account_banned"}`)
			return
		}
		fmt.Fprint(w, `{"pid":1800000005,"baas_user_id":"9198f6a6930b5fd1","nickname":"Host"}`)
	}))
	defer adapter.Close()
	oldURL, oldGate := adapterURL, bans.Default
	adapterURL, bans.Default = adapter.URL, bans.New(adapter.URL, "k")
	defer func() { adapterURL, bans.Default = oldURL, oldGate }()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, "", AppID)
	go srv.Serve(lis)
	defer srv.Stop()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := authpb.NewAuthClient(conn)

	ctx := context.Background()
	jwt := "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"openpak":"nx2.a.b"}`)) + ".sig"
	ext := &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: jwt}}

	issued, err := client.IssueToken(ctx, &authpb.IssueTokenRequest{ExternalIdToken: ext})
	if err != nil {
		t.Fatalf("active account: %v", err)
	}
	withToken := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+issued.GetToken().GetAccessToken())
	if _, err := client.ValidateToken(withToken, &emptypb.Empty{}); err != nil {
		t.Fatalf("active account's token: %v", err)
	}

	banned.Store(true)
	// A fresh gate: the active answer above is otherwise trusted for up to 30 s.
	bans.Default = bans.New(adapter.URL, "k")
	srv.Stop()
	lis2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv2 := NewServer(nil, "", AppID)
	go srv2.Serve(lis2)
	defer srv2.Stop()
	conn2, err := grpc.NewClient(lis2.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	client = authpb.NewAuthClient(conn2)

	if _, err := client.IssueToken(ctx, &authpb.IssueTokenRequest{ExternalIdToken: ext}); !bans.IsRefused(err) {
		t.Fatalf("banned sign-in: want the banned refusal, got %v", err)
	}
	if _, err := client.ValidateToken(withToken, &emptypb.Empty{}); !bans.IsRefused(err) {
		t.Fatalf("banned account's live token: want the banned refusal, got %v", err)
	}
	if _, err := client.RefreshToken(withToken, &authpb.RefreshTokenRequest{RefreshToken: issued.GetToken().GetRefreshToken()}); !bans.IsRefused(err) {
		t.Fatalf("banned refresh: want the banned refusal, got %v", err)
	}
}
