package npln

// ugcstore — nn.npln.ugcstore.v1.Ugcstore for Splatoon 3: the per-player documents the game keeps
// on the server (services/GameRecord/users/<id>/…, services/Hammer/users/<id>/…, locker and plaza
// posts), Firestore shaped. Facts this follows (docs/provenance.md):
//
//   - A missing document is NOT_FOUND with Nintendo's NError detail, after a headers frame. An OK
//     answer without a message becomes a default document with an empty name, which the game's
//     name parser ("tenants/{t}/documents/{path}") aborts on; a named empty document reads as a
//     document that exists, which the game refuses differently.
//   - Queries with nothing to return answer one response carrying only read_time.
//
// Trust: a path segment after "users/" must be the caller's own user id to write it, so nobody
// writes another player's records. Every document is readable (plaza lockers are public).
// Storage: one file of length-prefixed protobuf documents (NPLN_UGC_FILE, default ugcstore.pb next
// to the rotation), rewritten atomically after each commit. Nothing in a document is logged.

import (
	"context"
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/openpak/npln/proto/common"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
	"github.com/openpak/npln/redact"
)

const (
	ugcMaxDoc   = 256 << 10 // bytes of one encoded document (a save-sized record fits)
	ugcMaxOps   = 100       // write operations per commit
	ugcMaxDocs  = 200000    // documents in the whole store
	ugcMaxPath  = 1024      // bytes of a document path
	ugcDocsPart = "/documents/"
)

type ugcStore struct {
	ugcpb.UnimplementedUgcstoreServer
	file string

	mu   sync.Mutex
	docs map[string]*ugcpb.Document // path relative to …/documents/ -> document (Name holds that path)
}

func ugcFile() string {
	if f := os.Getenv("NPLN_UGC_FILE"); f != "" {
		return f
	}
	return filepath.Join(filepath.Dir(envOr("NPLN_ROTATION", "rotation.json")), "ugcstore.pb")
}

func newUGCStore(file string) *ugcStore {
	u := &ugcStore{file: file, docs: map[string]*ugcpb.Document{}}
	b, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("[Ugc] load: %s", redact.Err(err))
	}
	for len(b) > 0 {
		n, k := binary.Uvarint(b)
		if k <= 0 || uint64(len(b)-k) < n {
			log.Printf("[Ugc] load: truncated store, kept %d documents", len(u.docs))
			break
		}
		var d ugcpb.Document
		if proto.Unmarshal(b[k:k+int(n)], &d) == nil && d.GetName() != "" {
			u.docs[d.GetName()] = &d
		}
		b = b[k+int(n):]
	}
	return u
}

// save rewrites the store. Caller holds the lock.
func (u *ugcStore) save() {
	var out []byte
	for _, d := range u.docs {
		b, err := proto.Marshal(d)
		if err != nil {
			continue
		}
		out = binary.AppendUvarint(out, uint64(len(b)))
		out = append(out, b...)
	}
	if err := os.MkdirAll(filepath.Dir(u.file), 0o700); err != nil {
		log.Printf("[Ugc] save: %s", redact.Err(err))
		return
	}
	tmp := u.file + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err == nil {
		err = os.Rename(tmp, u.file)
		if err != nil {
			log.Printf("[Ugc] save: %s", redact.Err(err))
		}
	} else {
		log.Printf("[Ugc] save: %s", redact.Err(err))
	}
}

// splitDoc splits "tenants/<t>/documents/<path>" into its prefix (echoed back in names) and path.
func splitDoc(name string) (prefix, path string, err error) {
	i := strings.Index(name, ugcDocsPart)
	if !strings.HasPrefix(name, "tenants/") || i < 0 {
		return "", "", status.Error(codes.InvalidArgument, "not a document name")
	}
	path = name[i+len(ugcDocsPart):]
	if path == "" || len(path) > ugcMaxPath || strings.Contains(path, "//") {
		return "", "", status.Error(codes.InvalidArgument, "bad document path")
	}
	return name[:i+len(ugcDocsPart)], path, nil
}

// mayWrite: a path naming a user ("…/users/<id>/…") belongs to that user.
func mayWrite(uid, path string) bool {
	seg := strings.Split(path, "/")
	for i := 0; i+1 < len(seg); i++ {
		if seg[i] == "users" && strings.HasPrefix(seg[i+1], "u-") && seg[i+1] != uid {
			return false
		}
	}
	return true
}

func named(d *ugcpb.Document, prefix, path string) *ugcpb.Document {
	c := proto.Clone(d).(*ugcpb.Document)
	c.Name = prefix + path
	return c
}

// docKind is a document path for the log, with user ids replaced.
func docKind(path string) string {
	seg := strings.Split(path, "/")
	out := []string{}
	for i, s := range seg {
		if i > 0 && seg[i-1] == "users" {
			s = "<user>"
		}
		out = append(out, s)
	}
	k := strings.Join(out, "/")
	if len(k) > 120 {
		k = k[:120]
	}
	return k
}

// getDocument is raw (rawunary.go): a stored document is one message, a missing one NOT_FOUND.
func (u *ugcStore) getDocument(_ any, ss grpc.ServerStream) error {
	var req ugcpb.GetDocumentRequest
	if err := ss.RecvMsg(&req); err != nil {
		return err
	}
	if callerUID(ss.Context()) == "" {
		return status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	prefix, path, err := splitDoc(req.GetName())
	if err != nil {
		return err
	}
	u.mu.Lock()
	d := u.docs[path]
	u.mu.Unlock()
	if d == nil {
		log.Printf("[Ugc] GetDocument %s -> NOT_FOUND", docKind(path))
		if err := sendEmptyHeaders(ss); err != nil {
			return err
		}
		return notFound("Document", path)
	}
	log.Printf("[Ugc] GetDocument %s -> found", docKind(path))
	return ss.SendMsg(named(d, prefix, path))
}

func (u *ugcStore) BulkGetDocuments(req *ugcpb.BulkGetDocumentsRequest, stream grpc.ServerStreamingServer[ugcpb.BulkGetDocumentsResponse]) error {
	if callerUID(stream.Context()) == "" {
		return status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	now := timestamppb.Now()
	for _, n := range req.GetNames() {
		prefix, path, err := splitDoc(n)
		if err != nil {
			return err
		}
		u.mu.Lock()
		d := u.docs[path]
		u.mu.Unlock()
		r := &ugcpb.BulkGetDocumentsResponse{ReadTime: now}
		if d != nil {
			r.Result = &ugcpb.BulkGetDocumentsResponse_Found{Found: named(d, prefix, path)}
		} else {
			r.Result = &ugcpb.BulkGetDocumentsResponse_Missing{Missing: n}
		}
		if err := stream.Send(r); err != nil {
			return err
		}
	}
	return nil
}

// RunQuery: ponytail — no query is evaluated yet; every query answers "nothing", one response
// carrying only read_time, which is how an empty result looks on the wire.
func (u *ugcStore) RunQuery(req *ugcpb.RunQueryRequest, stream grpc.ServerStreamingServer[ugcpb.RunQueryResponse]) error {
	log.Printf("[Ugc] RunQuery -> empty")
	return stream.Send(&ugcpb.RunQueryResponse{ReadTime: timestamppb.Now()})
}

func (u *ugcStore) CommitDocuments(ctx context.Context, req *ugcpb.CommitDocumentsRequest) (*ugcpb.CommitDocumentsResponse, error) {
	uid := callerUID(ctx)
	if uid == "" {
		return nil, status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	if len(req.GetWriteOperations()) > ugcMaxOps {
		return nil, status.Error(codes.InvalidArgument, "too many write operations")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	// Apply to a copy first: a commit is all or nothing.
	work := map[string]*ugcpb.Document{}
	get := func(p string) *ugcpb.Document {
		if d, ok := work[p]; ok {
			return d
		}
		return u.docs[p]
	}
	now := timestamppb.New(time.Now().UTC())
	resp := &ugcpb.CommitDocumentsResponse{CommitTime: now}
	var kinds []string
	for _, op := range req.GetWriteOperations() {
		res := &ugcpb.WriteResult{UpdateTime: now}
		switch o := op.GetOperationType().(type) {
		case *ugcpb.WriteOperation_UpdateDocument:
			_, path, err := splitDoc(o.UpdateDocument.GetDocument().GetName())
			if err != nil {
				return nil, err
			}
			if !mayWrite(uid, path) {
				return nil, status.Error(codes.PermissionDenied, "not your document")
			}
			cur := get(path)
			if err := precondition(o.UpdateDocument.GetCurrentDocument(), cur); err != nil {
				return nil, err
			}
			nd := &ugcpb.Document{Name: path, CreateTime: now, Fields: &commonpb.MapValue{Fields: map[string]*commonpb.Value{}}}
			if cur != nil {
				nd = proto.Clone(cur).(*ugcpb.Document)
			}
			src := o.UpdateDocument.GetDocument().GetFields().GetFields()
			if mask := o.UpdateDocument.GetUpdateMask().GetPaths(); len(mask) > 0 {
				for _, p := range mask {
					setPath(nd.Fields, p, lookupPath(src, p))
				}
			} else {
				nd.Fields = &commonpb.MapValue{Fields: map[string]*commonpb.Value{}}
				for k, v := range src {
					nd.Fields.Fields[k] = v
				}
			}
			nd.UpdateTime = now
			if proto.Size(nd) > ugcMaxDoc {
				return nil, status.Error(codes.InvalidArgument, "document too large")
			}
			work[path] = nd
			kinds = append(kinds, "update "+docKind(path))
		case *ugcpb.WriteOperation_DeleteDocument:
			_, path, err := splitDoc(o.DeleteDocument.GetName())
			if err != nil {
				return nil, err
			}
			if !mayWrite(uid, path) {
				return nil, status.Error(codes.PermissionDenied, "not your document")
			}
			if err := precondition(o.DeleteDocument.GetCurrentDocument(), get(path)); err != nil {
				return nil, err
			}
			work[path] = nil
			kinds = append(kinds, "delete "+docKind(path))
		case *ugcpb.WriteOperation_TransformDocument:
			_, path, err := splitDoc(o.TransformDocument.GetName())
			if err != nil {
				return nil, err
			}
			if !mayWrite(uid, path) {
				return nil, status.Error(codes.PermissionDenied, "not your document")
			}
			cur := get(path)
			nd := &ugcpb.Document{Name: path, CreateTime: now, Fields: &commonpb.MapValue{Fields: map[string]*commonpb.Value{}}}
			if cur != nil {
				nd = proto.Clone(cur).(*ugcpb.Document)
			}
			for _, t := range o.TransformDocument.GetFieldTransforms() {
				res.TransformResults = append(res.TransformResults, transform(nd.Fields, t, now))
			}
			nd.UpdateTime = now
			work[path] = nd
			kinds = append(kinds, "transform "+docKind(path))
		default:
			return nil, status.Error(codes.Unimplemented, "attachments are not served")
		}
		resp.WriteResults = append(resp.WriteResults, res)
	}
	for p, d := range work {
		if d == nil {
			delete(u.docs, p)
		} else {
			u.docs[p] = d
		}
	}
	if len(u.docs) > ugcMaxDocs {
		return nil, status.Error(codes.ResourceExhausted, "store full")
	}
	u.save()
	log.Printf("[Ugc] CommitDocuments %s", strings.Join(kinds, ", "))
	return resp, nil
}

func precondition(p *ugcpb.Precondition, cur *ugcpb.Document) error {
	switch c := p.GetConditionType().(type) {
	case *ugcpb.Precondition_Exists:
		if c.Exists != (cur != nil) {
			if c.Exists {
				return notFound("Document", "precondition")
			}
			return status.Error(codes.AlreadyExists, "document already exists")
		}
	case *ugcpb.Precondition_UpdateTime:
		if cur == nil || !cur.GetUpdateTime().AsTime().Equal(c.UpdateTime.AsTime()) {
			return status.Error(codes.FailedPrecondition, "document changed")
		}
	}
	return nil
}

func lookupPath(m map[string]*commonpb.Value, p string) *commonpb.Value {
	parts := strings.Split(p, ".")
	for i, k := range parts {
		v, ok := m[k]
		if !ok {
			return nil
		}
		if i == len(parts)-1 {
			return v
		}
		m = v.GetMapValue().GetFields()
	}
	return nil
}

// setPath sets (or, for nil, removes) a dotted field path, creating maps on the way.
func setPath(m *commonpb.MapValue, p string, v *commonpb.Value) {
	parts := strings.Split(p, ".")
	for _, k := range parts[:len(parts)-1] {
		if m.Fields == nil {
			m.Fields = map[string]*commonpb.Value{}
		}
		next := m.Fields[k].GetMapValue()
		if next == nil {
			if v == nil {
				return
			}
			next = &commonpb.MapValue{Fields: map[string]*commonpb.Value{}}
			m.Fields[k] = &commonpb.Value{ValueType: &commonpb.Value_MapValue{MapValue: next}}
		}
		m = next
	}
	last := parts[len(parts)-1]
	if v == nil {
		delete(m.Fields, last)
		return
	}
	if m.Fields == nil {
		m.Fields = map[string]*commonpb.Value{}
	}
	m.Fields[last] = v
}

func num(v *commonpb.Value) (float64, bool, bool) { // value, isInt, ok
	switch x := v.GetValueType().(type) {
	case *commonpb.Value_IntegerValue:
		return float64(x.IntegerValue), true, true
	case *commonpb.Value_DoubleValue:
		return x.DoubleValue, false, true
	case *commonpb.Value_FloatValue:
		return float64(x.FloatValue), false, true
	}
	return 0, false, false
}

// transform applies one field transform and returns the field's new value.
func transform(m *commonpb.MapValue, t *ugcpb.FieldTransform, now *timestamppb.Timestamp) *commonpb.Value {
	cur := lookupPath(m.GetFields(), t.GetFieldPath())
	var out *commonpb.Value
	switch x := t.GetTransformType().(type) {
	case *ugcpb.FieldTransform_SetServerValue:
		out = &commonpb.Value{ValueType: &commonpb.Value_TimestampValue{TimestampValue: now}}
	case *ugcpb.FieldTransform_Increment, *ugcpb.FieldTransform_BufferedIncrement:
		by := t.GetIncrement()
		if by == nil {
			by = t.GetBufferedIncrement()
		}
		a, ai, _ := num(cur)
		b, bi, _ := num(by)
		if ai && bi || cur == nil && bi {
			out = iv(int64(a) + int64(b))
		} else {
			out = &commonpb.Value{ValueType: &commonpb.Value_DoubleValue{DoubleValue: a + b}}
		}
	case *ugcpb.FieldTransform_Maximum:
		out = x.Maximum
		if a, _, ok := num(cur); ok {
			if b, _, _ := num(x.Maximum); a >= b {
				out = cur
			}
		}
	case *ugcpb.FieldTransform_Minimum:
		out = x.Minimum
		if a, _, ok := num(cur); ok {
			if b, _, _ := num(x.Minimum); a <= b {
				out = cur
			}
		}
	}
	if out != nil {
		setPath(m, t.GetFieldPath(), out)
	}
	return out
}
