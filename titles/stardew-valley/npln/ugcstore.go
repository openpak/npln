package npln

// ugcstore — nn.npln.ugcstore.v1.Ugcstore and Screening: user-generated documents, Firestore
// shaped. Observed (titles/super-mario-bros-wonder/docs/provenance.md): after a course Wonder
// runs one query, the 30 most recent `ku` documents under ke/<n>/sdv/<n>/sh/<n> by `ut`, and an
// empty answer is a single response carrying only read_time. The rest is read from the schema.
//
// Trust: a document belongs to its first writer, and only the owner may change or delete it.
// A path segment shaped like an NPLN user id ("u-" + 20 base32 letters) must be the caller's
// own, so nobody writes into another player's slot first. Size limits bound every write.
// Attachments and upload/attachment URIs are not served (Unimplemented).
//
// Storage: every document, its owner and every short alias, as one JSON file (NPLN_UGC_FILE,
// default ugcstore.json next to the JWT key's default), rewritten atomically after each change.
// ponytail: whole-file rewrite; fine for a private network's volume of courses.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/openpak/npln/proto/common"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
	"github.com/openpak/npln/redact"
)

const (
	ugcMaxDoc      = 64 << 10 // bytes of one encoded document
	ugcMaxOps      = 50       // write operations per commit
	ugcMaxPerOwner = 500      // documents one player may own
	ugcMaxPath     = 1024     // bytes of a document path
	ugcMaxResults  = 300      // documents one query returns at most
	ugcMaxReport   = 16 << 10 // bytes of one encoded report
)

var userIDShape = regexp.MustCompile(`^u-[a-z2-7]{20}$`)

type ugcEntry struct {
	owner string // user id of the first writer
	doc   *ugcpb.Document
}

type ugcAlias struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Doc   string `json:"doc"` // path relative to …/documents
	Scope string `json:"scope,omitempty"`
	Owner string `json:"owner"`
}

type ugcStore struct {
	ugcpb.UnimplementedUgcstoreServer
	file string

	mu      sync.Mutex
	docs    map[string]*ugcEntry // path relative to …/documents -> entry
	aliases map[string]*ugcAlias // code -> alias
}

func newUGCStore(file string) *ugcStore {
	u := &ugcStore{file: file, docs: map[string]*ugcEntry{}, aliases: map[string]*ugcAlias{}}
	u.load()
	return u
}

type ugcFileDoc struct {
	Owner string          `json:"owner"`
	Doc   json.RawMessage `json:"doc"`
}

type ugcFileData struct {
	Docs    []ugcFileDoc `json:"docs"`
	Aliases []*ugcAlias  `json:"aliases"`
}

func (u *ugcStore) load() {
	if u.file == "" {
		return
	}
	b, err := os.ReadFile(u.file)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var data ugcFileData
	if err == nil {
		err = json.Unmarshal(b, &data)
	}
	if err != nil {
		log.Printf("[UGC] %s unreadable (%s): starting empty", u.file, redact.Err(err))
		return
	}
	for _, fd := range data.Docs {
		d := &ugcpb.Document{}
		if protojson.Unmarshal(fd.Doc, d) == nil {
			u.docs[d.GetName()] = &ugcEntry{owner: fd.Owner, doc: d}
		}
	}
	for _, a := range data.Aliases {
		u.aliases[a.Code] = a
	}
	log.Printf("[UGC] loaded %d document(s), %d alias(es) from %s", len(u.docs), len(u.aliases), u.file)
}

// save rewrites the store file. Caller holds u.mu.
func (u *ugcStore) save() {
	if u.file == "" {
		return
	}
	data := ugcFileData{Docs: make([]ugcFileDoc, 0, len(u.docs))}
	for _, e := range u.docs {
		b, err := protojson.Marshal(e.doc)
		if err != nil {
			continue
		}
		data.Docs = append(data.Docs, ugcFileDoc{Owner: e.owner, Doc: b})
	}
	for _, a := range u.aliases {
		data.Aliases = append(data.Aliases, a)
	}
	b, err := json.Marshal(data)
	if err == nil {
		tmp := u.file + ".tmp"
		if err = os.WriteFile(tmp, b, 0o600); err == nil {
			err = os.Rename(tmp, u.file)
		}
	}
	if err != nil {
		log.Printf("[UGC] saving %s: %s (changes live in memory only)", u.file, redact.Err(err))
	}
}

// docName splits "tenants/<t>/documents[/<path>]" into the tenant prefix as the caller spelled
// it and the path under documents. The tenant must be "current" or the one served.
func docName(ctx context.Context, name string) (prefix, path string, err error) {
	name = strings.Trim(name, "/")
	i := strings.Index(name+"/", "/documents/")
	if i < 0 {
		return "", "", status.Errorf(codes.InvalidArgument, "%q is not under documents", name)
	}
	prefix = name[:i]
	if prefix != "tenants/current" && prefix != tenantFromCtx(ctx) {
		return "", "", status.Errorf(codes.NotFound, "tenant %q is not served here", prefix)
	}
	path = strings.TrimPrefix(name[i:], "/documents")
	path = strings.TrimPrefix(path, "/")
	if len(path) > ugcMaxPath {
		return "", "", status.Error(codes.InvalidArgument, "path too long")
	}
	for _, s := range strings.Split(path, "/") {
		if path != "" && (s == "" || s == "." || s == "..") {
			return "", "", status.Errorf(codes.InvalidArgument, "malformed path %q", path)
		}
	}
	return prefix, path, nil
}

// docPath is docName for a document: an even, non-zero number of segments.
func docPath(ctx context.Context, name string) (prefix, path string, err error) {
	prefix, path, err = docName(ctx, name)
	if err == nil && (path == "" || strings.Count(path, "/")%2 == 0) {
		err = status.Errorf(codes.InvalidArgument, "%q names a collection, not a document", name)
	}
	return prefix, path, err
}

func withName(d *ugcpb.Document, prefix, path string) *ugcpb.Document {
	out := proto.Clone(d).(*ugcpb.Document)
	out.Name = prefix + "/documents/" + path
	return out
}

// mayWrite: the path's user-id segments are the caller's, and an existing document is theirs.
func mayWrite(path, me string, e *ugcEntry) error {
	for _, s := range strings.Split(path, "/") {
		if userIDShape.MatchString(s) && s != me {
			return status.Errorf(codes.PermissionDenied, "%s belongs to another player", path)
		}
	}
	if e != nil && e.owner != me {
		return status.Errorf(codes.PermissionDenied, "%s belongs to another player", path)
	}
	return nil
}

func preconditionOK(p *ugcpb.Precondition, e *ugcEntry) error {
	switch c := p.GetConditionType().(type) {
	case *ugcpb.Precondition_Exists:
		if c.Exists != (e != nil) {
			return status.Error(codes.FailedPrecondition, "document existence precondition failed")
		}
	case *ugcpb.Precondition_UpdateTime:
		if e == nil || !proto.Equal(e.doc.GetUpdateTime(), c.UpdateTime) {
			return status.Error(codes.FailedPrecondition, "document update_time precondition failed")
		}
	}
	return nil
}

func applyTransform(fields *commonpb.MapValue, path string, ft *ugcpb.FieldTransform) *commonpb.Value {
	cur := lookupField(fields, path)
	var v *commonpb.Value
	switch x := ft.GetTransformType().(type) {
	case *ugcpb.FieldTransform_SetServerValue:
		v = &commonpb.Value{ValueType: &commonpb.Value_TimestampValue{TimestampValue: timestamppb.Now()}}
	case *ugcpb.FieldTransform_Increment:
		v = addValues(cur, x.Increment)
	case *ugcpb.FieldTransform_BufferedIncrement:
		v = addValues(cur, x.BufferedIncrement)
	case *ugcpb.FieldTransform_Maximum:
		v = x.Maximum
		if typeRank(cur) == 2 && compareValues(cur, v) > 0 {
			v = cur
		}
	case *ugcpb.FieldTransform_Minimum:
		v = x.Minimum
		if typeRank(cur) == 2 && compareValues(cur, v) < 0 {
			v = cur
		}
	default:
		return cur
	}
	setField(fields, path, v)
	return v
}

// addValues is Firestore's increment: integers stay integers, anything else becomes a double; a
// missing or non-numeric field counts as zero.
func addValues(cur, by *commonpb.Value) *commonpb.Value {
	ci, cInt := cur.GetValueType().(*commonpb.Value_IntegerValue)
	bi, bInt := by.GetValueType().(*commonpb.Value_IntegerValue)
	if (cInt || typeRank(cur) != 2) && bInt {
		base := int64(0)
		if cInt {
			base = ci.IntegerValue
		}
		return &commonpb.Value{ValueType: &commonpb.Value_IntegerValue{IntegerValue: base + bi.IntegerValue}}
	}
	base := 0.0
	if typeRank(cur) == 2 {
		base = number(cur)
	}
	return &commonpb.Value{ValueType: &commonpb.Value_DoubleValue{DoubleValue: base + number(by)}}
}

func (u *ugcStore) CommitDocuments(ctx context.Context, req *ugcpb.CommitDocumentsRequest) (*ugcpb.CommitDocumentsResponse, error) {
	user, err := callerUser(ctx)
	if err != nil {
		return nil, err
	}
	me := lastSeg(user)
	ops := req.GetWriteOperations()
	if len(ops) > ugcMaxOps {
		return nil, status.Errorf(codes.InvalidArgument, "at most %d writes per commit", ugcMaxOps)
	}
	now := timestamppb.Now()
	u.mu.Lock()
	defer u.mu.Unlock()
	staged := map[string]*ugcEntry{} // path -> new entry, nil = deleted
	get := func(p string) *ugcEntry {
		if e, ok := staged[p]; ok {
			return e
		}
		return u.docs[p]
	}
	results := make([]*ugcpb.WriteResult, 0, len(ops))
	for _, op := range ops {
		var name string
		switch x := op.GetOperationType().(type) {
		case *ugcpb.WriteOperation_UpdateDocument:
			name = x.UpdateDocument.GetDocument().GetName()
		case *ugcpb.WriteOperation_DeleteDocument:
			name = x.DeleteDocument.GetName()
		case *ugcpb.WriteOperation_TransformDocument:
			name = x.TransformDocument.GetName()
		default:
			return nil, status.Error(codes.Unimplemented, "attachments are not served")
		}
		_, path, err := docPath(ctx, name)
		if err != nil {
			return nil, err
		}
		old := get(path)
		if err := mayWrite(path, me, old); err != nil {
			return nil, err
		}
		next := &ugcEntry{owner: me, doc: &ugcpb.Document{Name: path, Fields: &commonpb.MapValue{}, CreateTime: now}}
		if old != nil {
			next.doc = proto.Clone(old.doc).(*ugcpb.Document)
			if next.doc.Fields == nil {
				next.doc.Fields = &commonpb.MapValue{}
			}
		}
		next.doc.UpdateTime = now
		res := &ugcpb.WriteResult{UpdateTime: now}
		switch x := op.GetOperationType().(type) {
		case *ugcpb.WriteOperation_UpdateDocument:
			if err := preconditionOK(x.UpdateDocument.GetCurrentDocument(), old); err != nil {
				return nil, err
			}
			in := x.UpdateDocument.GetDocument().GetFields()
			if mask := x.UpdateDocument.GetUpdateMask(); len(mask.GetPaths()) > 0 {
				for _, p := range mask.GetPaths() {
					v := lookupField(in, p)
					if v != nil {
						v = proto.Clone(v).(*commonpb.Value)
					}
					setField(next.doc.Fields, p, v)
				}
			} else {
				next.doc.Fields = proto.Clone(in).(*commonpb.MapValue)
				if next.doc.Fields == nil {
					next.doc.Fields = &commonpb.MapValue{}
				}
			}
		case *ugcpb.WriteOperation_DeleteDocument:
			if err := preconditionOK(x.DeleteDocument.GetCurrentDocument(), old); err != nil {
				return nil, err
			}
			staged[path] = nil
			results = append(results, &ugcpb.WriteResult{UpdateTime: now})
			continue
		case *ugcpb.WriteOperation_TransformDocument:
			for _, ft := range x.TransformDocument.GetFieldTransforms() {
				res.TransformResults = append(res.TransformResults, applyTransform(next.doc.Fields, ft.GetFieldPath(), ft))
			}
			for _, bt := range x.TransformDocument.GetBufferedFieldTransforms() {
				ft := &ugcpb.FieldTransform{TransformType: &ugcpb.FieldTransform_Increment{Increment: bt.GetIncrement()}}
				res.TransformResults = append(res.TransformResults, applyTransform(next.doc.Fields, bt.GetFieldPath(), ft))
			}
		}
		if proto.Size(next.doc) > ugcMaxDoc {
			return nil, status.Errorf(codes.InvalidArgument, "%s is over %d bytes", path, ugcMaxDoc)
		}
		if old != nil {
			next.owner = old.owner
		}
		staged[path] = next
		results = append(results, res)
	}
	// a player's document count is checked once, on the commit's outcome
	owned := 0
	for p, e := range u.docs {
		if _, changed := staged[p]; !changed && e.owner == me {
			owned++
		}
	}
	for _, e := range staged {
		if e != nil && e.owner == me {
			owned++
		}
	}
	if owned > ugcMaxPerOwner {
		return nil, status.Errorf(codes.ResourceExhausted, "at most %d documents per player", ugcMaxPerOwner)
	}
	for p, e := range staged {
		if e == nil {
			delete(u.docs, p)
		} else {
			u.docs[p] = e
		}
	}
	u.save()
	log.Printf("[UGC] commit by %s: %d write(s)", redact.Tag(me), len(ops))
	return &ugcpb.CommitDocumentsResponse{WriteResults: results, CommitTime: now}, nil
}

// masked keeps only a read mask's fields.
func masked(d *ugcpb.Document, m *fieldmaskpb.FieldMask) *ugcpb.Document {
	if len(m.GetPaths()) == 0 {
		return d
	}
	p := &ugcpb.StructuredQuery_Projection{}
	for _, f := range m.GetPaths() {
		p.Fields = append(p.Fields, &ugcpb.StructuredQuery_FieldReference{FieldPath: f})
	}
	return project(d, p)
}

func (u *ugcStore) GetDocument(ctx context.Context, req *ugcpb.GetDocumentRequest) (*ugcpb.Document, error) {
	if _, err := callerUser(ctx); err != nil {
		return nil, err
	}
	prefix, path, err := docPath(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	e := u.docs[path]
	u.mu.Unlock()
	if e == nil {
		return nil, status.Errorf(codes.NotFound, "document %q not found", req.GetName())
	}
	return masked(withName(e.doc, prefix, path), req.GetReadMask()), nil
}

func (u *ugcStore) BulkGetDocuments(req *ugcpb.BulkGetDocumentsRequest, stream grpc.ServerStreamingServer[ugcpb.BulkGetDocumentsResponse]) error {
	ctx := stream.Context()
	if _, err := callerUser(ctx); err != nil {
		return err
	}
	if len(req.GetNames()) > ugcMaxResults {
		return status.Errorf(codes.InvalidArgument, "at most %d names", ugcMaxResults)
	}
	now := timestamppb.Now()
	for _, n := range req.GetNames() {
		prefix, path, err := docPath(ctx, n)
		if err != nil {
			return err
		}
		u.mu.Lock()
		e := u.docs[path]
		u.mu.Unlock()
		r := &ugcpb.BulkGetDocumentsResponse{ReadTime: now, Result: &ugcpb.BulkGetDocumentsResponse_Missing{Missing: n}}
		if e != nil {
			r.Result = &ugcpb.BulkGetDocumentsResponse_Found{Found: masked(withName(e.doc, prefix, path), req.GetReadMask())}
		}
		if err := stream.Send(r); err != nil {
			return err
		}
	}
	return nil
}

func (u *ugcStore) RunQuery(req *ugcpb.RunQueryRequest, stream grpc.ServerStreamingServer[ugcpb.RunQueryResponse]) error {
	ctx := stream.Context()
	if _, err := callerUser(ctx); err != nil {
		return err
	}
	prefix, parent, err := docName(ctx, req.GetParent())
	if err != nil {
		return err
	}
	q := req.GetStructuredQuery()
	if q == nil {
		return status.Error(codes.InvalidArgument, "a structured query is required")
	}
	u.mu.Lock()
	all := make([]qdoc, 0, len(u.docs))
	for p, e := range u.docs {
		all = append(all, qdoc{path: p, fields: e.doc.GetFields(), doc: withName(e.doc, prefix, p)})
	}
	u.mu.Unlock()
	docs, skipped := runQuery(q, parent, all, ugcMaxResults)
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	now := timestamppb.Now()
	log.Printf("[UGC] RunQuery parent=%s -> %d document(s)", redact.Tag(parent), len(docs))
	if len(docs) == 0 { // Firestore's empty answer: read_time alone (observed shape)
		return stream.Send(&ugcpb.RunQueryResponse{ReadTime: now, SkippedResults: skipped})
	}
	for i, d := range docs {
		r := &ugcpb.RunQueryResponse{Document: d, ReadTime: now}
		if i == 0 {
			r.SkippedResults = skipped
		}
		if err := stream.Send(r); err != nil {
			return err
		}
	}
	return nil
}

func (u *ugcStore) CreateDocumentShortAlias(ctx context.Context, req *ugcpb.CreateDocumentShortAliasRequest) (*ugcpb.DocumentShortAlias, error) {
	user, err := callerUser(ctx)
	if err != nil {
		return nil, err
	}
	me := lastSeg(user)
	prefix, path, err := docPath(ctx, req.GetDocumentShortAlias().GetDocument())
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	e := u.docs[path]
	if e == nil {
		return nil, status.Error(codes.NotFound, "no such document")
	}
	if e.owner != me {
		return nil, status.Error(codes.PermissionDenied, "only the document's owner makes its code")
	}
	for _, a := range u.aliases { // one code per document
		if a.Doc == path {
			return aliasOut(a, prefix), nil
		}
	}
	code := roomCode()
	for u.aliases[code] != nil {
		code = roomCode()
	}
	parent := strings.TrimRight(req.GetParent(), "/")
	if parent == "" {
		parent = prefix
	}
	a := &ugcAlias{Code: code, Name: parent + "/documentShortAliases/" + code, Doc: path, Scope: req.GetDocumentShortAlias().GetScope(), Owner: me}
	u.aliases[code] = a
	u.save()
	return aliasOut(a, prefix), nil
}

func aliasOut(a *ugcAlias, prefix string) *ugcpb.DocumentShortAlias {
	return &ugcpb.DocumentShortAlias{Name: a.Name, Document: prefix + "/documents/" + a.Doc, Scope: a.Scope}
}

func (u *ugcStore) alias(ctx context.Context, name string) (*ugcpb.DocumentShortAlias, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	a := u.aliases[strings.ToUpper(lastSeg(name))]
	if a == nil || u.docs[a.Doc] == nil {
		return nil, false
	}
	prefix := "tenants/current"
	if strings.HasPrefix(name, tenantFromCtx(ctx)+"/") {
		prefix = tenantFromCtx(ctx)
	}
	return aliasOut(a, prefix), true
}

func (u *ugcStore) GetDocumentShortAlias(ctx context.Context, req *ugcpb.GetDocumentShortAliasRequest) (*ugcpb.DocumentShortAlias, error) {
	if _, err := callerUser(ctx); err != nil {
		return nil, err
	}
	if a, ok := u.alias(ctx, req.GetName()); ok {
		return a, nil
	}
	return nil, status.Errorf(codes.NotFound, "alias %q not found", req.GetName())
}

func (u *ugcStore) BulkGetDocumentShortAliases(ctx context.Context, req *ugcpb.BulkGetDocumentShortAliasesRequest) (*ugcpb.BulkGetDocumentShortAliasesResponse, error) {
	if _, err := callerUser(ctx); err != nil {
		return nil, err
	}
	if len(req.GetRequests()) > ugcMaxResults {
		return nil, status.Errorf(codes.InvalidArgument, "at most %d aliases", ugcMaxResults)
	}
	out := &ugcpb.BulkGetDocumentShortAliasesResponse{}
	for _, r := range req.GetRequests() {
		res := &ugcpb.BulkGetDocumentShortAliasesResult{Result: &ugcpb.BulkGetDocumentShortAliasesResult_Missing{Missing: r.GetName()}}
		if a, ok := u.alias(ctx, r.GetName()); ok {
			res.Result = &ugcpb.BulkGetDocumentShortAliasesResult_Found{Found: a}
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

// screening — nn.npln.ugcstore.v1.Screening: a player reporting another's content. Reports are
// logged (the moderator's queue is the log) and kept in memory; upload URIs are not served.
type screening struct {
	ugcpb.UnimplementedScreeningServer

	mu      sync.Mutex
	reports []*ugcpb.Report
}

const maxReportsKept = 1000

func (s *screening) CreateReport(ctx context.Context, req *ugcpb.CreateReportRequest) (*ugcpb.Report, error) {
	user, err := callerUser(ctx)
	if err != nil {
		return nil, err
	}
	if proto.Size(req.GetReport()) > ugcMaxReport {
		return nil, status.Errorf(codes.InvalidArgument, "report over %d bytes", ugcMaxReport)
	}
	r := proto.Clone(req.GetReport()).(*ugcpb.Report)
	if r == nil {
		r = &ugcpb.Report{}
	}
	parent := strings.TrimRight(req.GetParent(), "/")
	if parent == "" {
		parent = tenantFromCtx(ctx)
	}
	r.Name = parent + "/reports/" + uuid4()
	r.CreateTime = timestamppb.New(time.Now())
	s.mu.Lock()
	s.reports = append(s.reports, r)
	if len(s.reports) > maxReportsKept {
		s.reports = s.reports[len(s.reports)-maxReportsKept:]
	}
	s.mu.Unlock()
	log.Printf("[UGC] REPORT by %s: target=%s type=%s category=%s reason=%d chars", redact.Seg(user), redact.Tag(r.GetScreeningTarget()), redact.Tag(r.GetScreeningTargetType()), redact.Tag(r.GetCategory()), len(r.GetReason()))
	return r, nil
}
