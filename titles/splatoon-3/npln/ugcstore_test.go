package npln

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	commonpb "github.com/openpak/npln/proto/common"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
)

// The store a player's records live in: a write to one's own path lands and reads back, a masked
// update touches only its fields, an increment adds, and another player's path is refused.
func TestUgcCommit(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	u := newUGCStore(t.TempDir() + "/ugc.pb")
	me := bearerCtx(1, "u-me")
	name := "tenants/current/documents/services/Hammer/users/u-me/UserAttribute/Data"
	up := func(fields map[string]*commonpb.Value, mask ...string) error {
		op := &ugcpb.UpdateDocumentRequest{Document: &ugcpb.Document{Name: name, Fields: &commonpb.MapValue{Fields: fields}}}
		if len(mask) > 0 {
			op.UpdateMask = &fieldmaskpb.FieldMask{Paths: mask}
		}
		_, err := u.CommitDocuments(me, &ugcpb.CommitDocumentsRequest{WriteOperations: []*ugcpb.WriteOperation{{OperationType: &ugcpb.WriteOperation_UpdateDocument{UpdateDocument: op}}}})
		return err
	}
	if err := up(map[string]*commonpb.Value{"a": iv(1), "b": iv(2)}); err != nil {
		t.Fatal(err)
	}
	if err := up(map[string]*commonpb.Value{"a": iv(9), "b": iv(7)}, "b"); err != nil {
		t.Fatal(err)
	}
	inc := &ugcpb.WriteOperation{OperationType: &ugcpb.WriteOperation_TransformDocument{TransformDocument: &ugcpb.TransformDocumentRequest{
		Name: name, FieldTransforms: []*ugcpb.FieldTransform{{FieldPath: "a", TransformType: &ugcpb.FieldTransform_Increment{Increment: iv(5)}}}}}}
	if _, err := u.CommitDocuments(me, &ugcpb.CommitDocumentsRequest{WriteOperations: []*ugcpb.WriteOperation{inc}}); err != nil {
		t.Fatal(err)
	}
	f := u.docs["services/Hammer/users/u-me/UserAttribute/Data"].GetFields().GetFields()
	if f["a"].GetIntegerValue() != 6 || f["b"].GetIntegerValue() != 7 {
		t.Fatalf("fields: %v", f)
	}
	other := bearerCtx(2, "u-other")
	if err := func() error {
		_, err := u.CommitDocuments(other, &ugcpb.CommitDocumentsRequest{WriteOperations: []*ugcpb.WriteOperation{inc}})
		return err
	}(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("other player's write: %v", err)
	}
	if again := newUGCStore(u.file); len(again.docs) != 1 {
		t.Fatalf("store not persisted: %d", len(again.docs))
	}
}
