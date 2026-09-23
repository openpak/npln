package npln

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonpb "github.com/openpak/npln/proto/common"
	hydropb "github.com/openpak/npln/proto/hydro/v1"
)

func TestHydro(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	h := newHydro()
	a, b := bearerCtx(1, "u-a"), bearerCtx(2, "u-b")

	c, err := h.CreateContent(a, &hydropb.CreateContentRequest{Parent: "tenants/current", Content: &hydropb.Content{Payload: []byte("hi"), User: "tenants/current/users/u-b"}})
	if err != nil {
		t.Fatal(err)
	}
	if lastSeg(c.GetUser()) != "u-a" || c.GetEtag() == "" || c.GetCreateTime() == nil {
		t.Fatalf("created %v (the owner is the caller, whatever the request says)", c)
	}
	got, err := h.GetContent(b, &hydropb.GetContentRequest{Name: c.GetName()})
	if err != nil || string(got.GetPayload()) != "hi" {
		t.Fatalf("read by another player: %v, %v", got, err)
	}
	if miss, err := h.GetContent(a, &hydropb.GetContentRequest{Name: "tenants/current/contents/nope"}); err != nil || miss.GetName() != "tenants/current/contents/nope" || miss.GetPayload() != nil {
		t.Fatalf("missing content: %v, %v", miss, err)
	}

	set := &hydropb.TransformContentRequest{Name: c.GetName(), FieldTransforms: []*hydropb.FieldTransform{
		{FieldPath: "payload", TransformType: &hydropb.FieldTransform_Set{Set: &commonpb.Value{ValueType: &commonpb.Value_BytesValue{BytesValue: []byte("new")}}}},
		{FieldPath: "attributes.3", TransformType: &hydropb.FieldTransform_Increment{Increment: gsInt(2)}},
	}}
	if _, err := h.TransformContent(b, set); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("b changed a's content: %v", err)
	}
	set.Etag = "stale"
	if _, err := h.TransformContent(a, set); status.Code(err) != codes.Aborted {
		t.Fatalf("stale etag: %v", err)
	}
	set.Etag = c.GetEtag()
	if _, err := h.TransformContent(a, set); err != nil {
		t.Fatal(err)
	}
	got, _ = h.GetContent(a, &hydropb.GetContentRequest{Name: c.GetName()})
	if string(got.GetPayload()) != "new" || got.GetAttributes()[3] != 2 || got.GetEtag() == c.GetEtag() {
		t.Fatalf("after transform: %v", got)
	}
	if _, err := h.CreateContent(a, &hydropb.CreateContentRequest{Content: &hydropb.Content{Payload: make([]byte, hydroMaxPayload+1)}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized payload: %v", err)
	}

	if _, err := h.CreateContent(b, &hydropb.CreateContentRequest{Parent: "tenants/current"}); err != nil {
		t.Fatal(err)
	}
	res, err := h.SearchContent(a, &hydropb.SearchContentRequest{User: "tenants/current/users/u-a"})
	if err != nil || len(res.GetContents()) != 1 {
		t.Fatalf("search by user: %v, %v", res, err)
	}
	if res, _ := h.SearchContent(a, &hydropb.SearchContentRequest{PageSize: 1}); len(res.GetContents()) != 1 || res.GetNextPageToken() == "" {
		t.Fatalf("paged search: %v", res)
	}

	if _, err := h.DeleteContent(b, &hydropb.DeleteContentRequest{Name: c.GetName()}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("b deleted a's content: %v", err)
	}
	if _, err := h.DeleteContent(a, &hydropb.DeleteContentRequest{Name: c.GetName()}); err != nil {
		t.Fatal(err)
	}
}
