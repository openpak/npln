package npln

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	authpb "github.com/openpak/npln/proto/auth/v1"
)

// A re-issue without a provable token passes only for the user this same connection proved.
func TestIssueTokenTrustsOnlyItsOwnConnection(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NX_INTERNAL_KEY", "k")
	adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"pid":1800000005,"baas_user_id":"9198f6a6930b5fd1","nickname":"Host"}`)
	}))
	defer adapter.Close()
	oldURL := adapterURL
	adapterURL = adapter.URL
	defer func() { adapterURL = oldURL }()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, "", AppID)
	go srv.Serve(lis)
	defer srv.Stop()
	dial := func() authpb.AuthClient {
		c, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return authpb.NewAuthClient(c)
	}
	ctx := context.Background()
	jwt := "eyJhbGciOiJSUzI1NiJ9." + b64u([]byte(`{"nnex":"nx2.a.b"}`)) + ".sig"
	garbage := &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: "00000000000000010000000000000000"}}

	host := dial()
	first, err := host.IssuePrearrangedUserToken(ctx, &authpb.IssuePrearrangedUserTokenRequest{
		ExternalIdToken: &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: jwt}}})
	if err != nil {
		t.Fatal(err)
	}
	me := first.GetUser().GetName()

	if _, err := host.IssueToken(ctx, &authpb.IssueTokenRequest{User: me, ExternalIdToken: garbage}); err != nil {
		t.Fatalf("same connection, same user: %v", err)
	}
	if _, err := host.IssueToken(ctx, &authpb.IssueTokenRequest{User: me + "x", ExternalIdToken: garbage}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("same connection, other user: %v", err)
	}
	if _, err := dial().IssueToken(ctx, &authpb.IssueTokenRequest{User: me, ExternalIdToken: garbage}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("new connection: %v", err)
	}

	// a connection that only ever showed our access token (a console reconnecting after a
	// server restart) has proven the user too
	back := dial()
	bctx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+first.GetToken().GetAccessToken())
	if _, err := back.ValidateToken(bctx, &emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if _, err := back.IssueToken(ctx, &authpb.IssueTokenRequest{User: me, ExternalIdToken: garbage}); err != nil {
		t.Fatalf("reconnected with a bearer: %v", err)
	}
}
