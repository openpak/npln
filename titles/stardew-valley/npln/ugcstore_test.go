package npln

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	commonpb "github.com/openpak/npln/proto/common"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
)

const courseParent = "tenants/current/documents/ke/1/sdv/3/sh/2937190396"

// Wonder's captured course query (docs/provenance.md of Wonder).
func courseQuery() *ugcpb.RunQueryRequest {
	return &ugcpb.RunQueryRequest{Parent: courseParent, QueryType: &ugcpb.RunQueryRequest_StructuredQuery{StructuredQuery: &ugcpb.StructuredQuery{
		From:    []*ugcpb.StructuredQuery_CollectionSelector{{CollectionId: "ku"}},
		OrderBy: []*ugcpb.StructuredQuery_Order{{Field: &ugcpb.StructuredQuery_FieldReference{FieldPath: "`ut`"}, Direction: ugcpb.StructuredQuery_Order_DESCENDING}},
		Limit:   wrapperspb.Int32(30),
	}}}
}

func putDoc(name string, fields map[string]*commonpb.Value) *ugcpb.CommitDocumentsRequest {
	return &ugcpb.CommitDocumentsRequest{WriteOperations: []*ugcpb.WriteOperation{{OperationType: &ugcpb.WriteOperation_UpdateDocument{
		UpdateDocument: &ugcpb.UpdateDocumentRequest{Document: &ugcpb.Document{Name: name, Fields: &commonpb.MapValue{Fields: fields}}}}}}}
}

func TestUGCCourseQuery(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	file := t.TempDir() + "/ugc.json"
	u := newUGCStore(file)
	a, b := userID("1111111111111111"), userID("2222222222222222")

	// Nothing stored: the answer is a single response with read_time alone.
	s := newFakeStream[ugcpb.RunQueryResponse](bearerCtx(1, a))
	if err := u.RunQuery(courseQuery(), s); err != nil {
		t.Fatal(err)
	}
	if got := s.got(); len(got) != 1 || got[0].GetDocument() != nil || got[0].GetReadTime() == nil {
		t.Fatalf("empty answer = %v", got)
	}

	for i, who := range []string{a, b} {
		pid := uint64(i + 1)
		if _, err := u.CommitDocuments(bearerCtx(pid, who), putDoc(courseParent+"/ku/"+who, map[string]*commonpb.Value{"ut": gsInt(int64(100 + i)), "t": gsInt(int64(i))})); err != nil {
			t.Fatalf("%s writes its own record: %v", who, err)
		}
	}
	// Another course, a nested collection and a different collection stay out of the answer.
	if _, err := u.CommitDocuments(bearerCtx(1, a), putDoc("tenants/current/documents/ke/1/sdv/3/sh/1/ku/"+a, map[string]*commonpb.Value{"ut": gsInt(999)})); err != nil {
		t.Fatal(err)
	}
	if _, err := u.CommitDocuments(bearerCtx(1, a), putDoc(courseParent+"/other/x", map[string]*commonpb.Value{"ut": gsInt(999)})); err != nil {
		t.Fatal(err)
	}

	// Reload from the file: the store survives a restart.
	u = newUGCStore(file)
	s = newFakeStream[ugcpb.RunQueryResponse](bearerCtx(1, a))
	if err := u.RunQuery(courseQuery(), s); err != nil {
		t.Fatal(err)
	}
	got := s.got()
	if len(got) != 2 || lastSeg(got[0].GetDocument().GetName()) != b || lastSeg(got[1].GetDocument().GetName()) != a {
		t.Fatalf("query answer = %v", got)
	}
	if got[0].GetDocument().GetName() != courseParent+"/ku/"+b {
		t.Fatalf("name %q not spelled as the caller asked", got[0].GetDocument().GetName())
	}

	// Filters and limits.
	q := courseQuery()
	q.GetStructuredQuery().Where = &ugcpb.StructuredQuery_Filter{FilterType: &ugcpb.StructuredQuery_Filter_FieldFilter{FieldFilter: &ugcpb.StructuredQuery_FieldFilter{
		Field: &ugcpb.StructuredQuery_FieldReference{FieldPath: "t"}, Op: ugcpb.StructuredQuery_FieldFilter_EQUAL, Value: gsInt(0)}}}
	q.GetStructuredQuery().Limit = wrapperspb.Int32(1)
	s = newFakeStream[ugcpb.RunQueryResponse](bearerCtx(1, a))
	if err := u.RunQuery(q, s); err != nil {
		t.Fatal(err)
	}
	if got := s.got(); len(got) != 1 || lastSeg(got[0].GetDocument().GetName()) != a {
		t.Fatalf("filtered answer = %v", got)
	}
}

func TestUGCWritesAreTheOwners(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	u := newUGCStore("")
	a, b := userID("1111111111111111"), userID("2222222222222222")
	shared := courseParent + "/boards/top"

	if _, err := u.CommitDocuments(bearerCtx(2, b), putDoc(courseParent+"/ku/"+a, map[string]*commonpb.Value{"ut": gsInt(1)})); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("b wrote into a's slot: %v", err)
	}
	if _, err := u.CommitDocuments(bearerCtx(1, a), putDoc(shared, map[string]*commonpb.Value{"n": gsInt(1)})); err != nil {
		t.Fatal(err)
	}
	if _, err := u.CommitDocuments(bearerCtx(2, b), putDoc(shared, map[string]*commonpb.Value{"n": gsInt(2)})); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("b overwrote a's document: %v", err)
	}
	del := &ugcpb.CommitDocumentsRequest{WriteOperations: []*ugcpb.WriteOperation{{OperationType: &ugcpb.WriteOperation_DeleteDocument{DeleteDocument: &ugcpb.DeleteDocumentRequest{Name: shared}}}}}
	if _, err := u.CommitDocuments(bearerCtx(2, b), del); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("b deleted a's document: %v", err)
	}
	big := putDoc(courseParent+"/ku/"+a, map[string]*commonpb.Value{"blob": {ValueType: &commonpb.Value_BytesValue{BytesValue: make([]byte, ugcMaxDoc)}}})
	if _, err := u.CommitDocuments(bearerCtx(1, a), big); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized document: %v", err)
	}
	attach := &ugcpb.CommitDocumentsRequest{WriteOperations: []*ugcpb.WriteOperation{{OperationType: &ugcpb.WriteOperation_SetAttachment{SetAttachment: &ugcpb.SetAttachmentRequest{Parent: shared}}}}}
	if _, err := u.CommitDocuments(bearerCtx(1, a), attach); status.Code(err) != codes.Unimplemented {
		t.Fatalf("attachment: %v", err)
	}

	// A failing write leaves the whole commit unapplied.
	mixed := putDoc(courseParent+"/ku/"+b, map[string]*commonpb.Value{"ut": gsInt(1)})
	mixed.WriteOperations = append(mixed.WriteOperations, putDoc(shared, nil).WriteOperations...)
	if _, err := u.CommitDocuments(bearerCtx(2, b), mixed); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("mixed commit: %v", err)
	}
	if _, err := u.GetDocument(bearerCtx(2, b), &ugcpb.GetDocumentRequest{Name: courseParent + "/ku/" + b}); status.Code(err) != codes.NotFound {
		t.Fatalf("half a refused commit was applied: %v", err)
	}

	// Transforms: increment and server time, by the owner.
	tr := &ugcpb.CommitDocumentsRequest{WriteOperations: []*ugcpb.WriteOperation{{OperationType: &ugcpb.WriteOperation_TransformDocument{TransformDocument: &ugcpb.TransformDocumentRequest{
		Name: shared, FieldTransforms: []*ugcpb.FieldTransform{
			{FieldPath: "n", TransformType: &ugcpb.FieldTransform_Increment{Increment: gsInt(5)}},
			{FieldPath: "`ut`", TransformType: &ugcpb.FieldTransform_SetServerValue{SetServerValue: ugcpb.ServerValueType_REQUEST_TIME}},
		}}}}}}
	res, err := u.CommitDocuments(bearerCtx(1, a), tr)
	if err != nil {
		t.Fatal(err)
	}
	if res.GetWriteResults()[0].GetTransformResults()[0].GetIntegerValue() != 6 {
		t.Fatalf("increment = %v", res.GetWriteResults()[0].GetTransformResults())
	}
	d, err := u.GetDocument(bearerCtx(2, b), &ugcpb.GetDocumentRequest{Name: shared})
	if err != nil || d.GetFields().GetFields()["ut"].GetTimestampValue() == nil {
		t.Fatalf("read back %v, %v", d, err)
	}

	// Short codes: the owner makes one, anyone resolves it.
	if _, err := u.CreateDocumentShortAlias(bearerCtx(2, b), &ugcpb.CreateDocumentShortAliasRequest{DocumentShortAlias: &ugcpb.DocumentShortAlias{Document: shared}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("b made a code for a's document: %v", err)
	}
	al, err := u.CreateDocumentShortAlias(bearerCtx(1, a), &ugcpb.CreateDocumentShortAliasRequest{Parent: "tenants/current", DocumentShortAlias: &ugcpb.DocumentShortAlias{Document: shared}})
	if err != nil {
		t.Fatal(err)
	}
	back, err := u.GetDocumentShortAlias(bearerCtx(2, b), &ugcpb.GetDocumentShortAliasRequest{Name: al.GetName()})
	if err != nil || back.GetDocument() != shared {
		t.Fatalf("code resolves to %v, %v", back, err)
	}
}

func TestCreateReport(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	s := &screening{}
	r, err := s.CreateReport(bearerCtx(1, "u-a"), &ugcpb.CreateReportRequest{Parent: "tenants/current", Report: &ugcpb.Report{ScreeningTarget: courseParent, Category: "offensive"}})
	if err != nil || r.GetName() == "" || r.GetCreateTime() == nil {
		t.Fatalf("report = %v, %v", r, err)
	}
	if _, err := s.CreateReport(bearerCtx(1, "u-a"), &ugcpb.CreateReportRequest{Report: &ugcpb.Report{Reason: string(make([]byte, ugcMaxReport))}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized report: %v", err)
	}
}
