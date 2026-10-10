package rpclog

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	authpb "github.com/openpak/npln/proto/auth/v1"
)

func TestBodyHidesTokensAndShowsUnknownFields(t *testing.T) {
	t.Setenv("NPLN_TRACE_BODIES", "1")
	const jwt = "eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"
	req := &authpb.IssueTokenRequest{
		User:            "tenants/t-35b7d576-lp1/users/u-x",
		ExternalIdToken: &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: jwt}},
	}
	req.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 9, protowire.VarintType), 1))
	got := Body(req)

	if strings.Contains(got, jwt) || strings.Contains(got, "eyJ") {
		t.Fatalf("token value leaked: %s", got)
	}
	for _, want := range []string{"u-x", "external_id_token.nsa_id_token=<41 bytes, jwt, sha256 ", "unknown=4801"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	// malformed unknown bytes (a tag with no value) must not panic
	bad := &authpb.IssueTokenRequest{}
	bad.ProtoReflect().SetUnknown(protowire.AppendTag(nil, 9, protowire.VarintType))
	if b := Body(bad); !strings.Contains(b, "unknown=48") {
		t.Fatalf("malformed message: %s", b)
	}
	// the original message must be untouched: handlers still need the token
	if req.GetExternalIdToken().GetNsaIdToken() != jwt {
		t.Fatal("Body modified the request")
	}
}

// Without NPLN_TRACE_BODIES a body is its outline: no name, id, token or unknown byte.
func TestBodyIsAnOutlineByDefault(t *testing.T) {
	const user = "tenants/t-35b7d576-lp1/users/u-sentinel-9981"
	const jwt = "eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"
	req := &authpb.IssueTokenRequest{
		User:            user,
		ExternalIdToken: &authpb.ExternalIdToken{Token: &authpb.ExternalIdToken_NsaIdToken{NsaIdToken: jwt}},
	}
	req.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 9, protowire.VarintType), 1))
	got := Body(req)
	for _, bad := range []string{user, "sentinel", "9981", "eyJ", jwt, "4801"} {
		if strings.Contains(got, bad) {
			t.Fatalf("outline carries %q: %s", bad, got)
		}
	}
	for _, want := range []string{"user=<44 chars>", "nsa_id_token=<41 chars>", "unknown=<2 bytes>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("outline lacks %q: %s", want, got)
		}
	}
	if got := rawOutline(protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), "private")); got != "fields=1:len7" {
		t.Fatalf("rawOutline = %q", got)
	}
}
