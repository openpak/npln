package npln

// hydro — nn.npln.hydro.v1.Datastore: small binary contents a player keeps on the server
// (titles/super-mario-bros-wonder/docs/provenance.md). Known from earlier research: created names
// are <parent>/contents/<id> with server-set times, a missing content reads back as an empty
// content carrying the asked name, and a search returns at most 50. What Wonder stores is not
// known, so payloads are opaque.
//
// Beyond that, read from the schema: a content belongs to the player who created it; others may
// read it, and change it only when it grants UPDATE. An etag, when sent, must match. Transforms
// set or increment `payload` (set only) or `attributes.<n>`. TrackContent reports, as an empty
// message, each change to a tracked content.
//
// ponytail: in-memory; a restart empties the datastore.

import (
	"context"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/openpak/npln/proto/common"
	hydropb "github.com/openpak/npln/proto/hydro/v1"
	"github.com/openpak/npln/redact"
)

const (
	hydroMaxPayload  = 64 << 10 // bytes of one content's payload
	hydroMaxAttrs    = 64       // attributes per content
	hydroMaxPerOwner = 200      // contents one player may own
	hydroMaxBatch    = 50       // requests in one batch
	hydroSearchMax   = 50       // contents one search returns
)

type hydro struct {
	hydropb.UnimplementedDatastoreServer

	mu       sync.Mutex
	contents map[string]*hydropb.Content // name -> content
	rev      map[string]uint64           // name -> revision, the etag
	watch    map[chan string]struct{}    // TrackContent streams, told each changed name
}

func newHydro() *hydro {
	return &hydro{contents: map[string]*hydropb.Content{}, rev: map[string]uint64{}, watch: map[chan string]struct{}{}}
}

// changed bumps a content's etag and tells trackers. Caller holds h.mu.
func (h *hydro) changed(name string) {
	h.rev[name]++
	if c := h.contents[name]; c != nil {
		c.Etag = strconv.FormatUint(h.rev[name], 10)
	}
	for w := range h.watch {
		select {
		case w <- name:
		default:
		}
	}
}

func (h *hydro) owned(me string) int {
	n := 0
	for _, c := range h.contents {
		if lastSeg(c.GetUser()) == me {
			n++
		}
	}
	return n
}

func (h *hydro) create(ctx context.Context, parent string, in *hydropb.Content) (*hydropb.Content, error) {
	user, err := callerUser(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.GetPayload()) > hydroMaxPayload || len(in.GetAttributes()) > hydroMaxAttrs {
		return nil, status.Errorf(codes.InvalidArgument, "content over %d bytes or %d attributes", hydroMaxPayload, hydroMaxAttrs)
	}
	c := proto.Clone(in).(*hydropb.Content)
	if c == nil {
		c = &hydropb.Content{}
	}
	now := timestamppb.Now()
	c.Name = strings.TrimRight(parent, "/") + "/contents/" + uuid4()
	c.User, c.CreateTime, c.UpdateTime = user, now, now
	if d := c.GetTtl(); d != nil {
		c.Expiration = &hydropb.Content_ExpireTime{ExpireTime: timestamppb.New(time.Now().Add(d.AsDuration()))}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.owned(lastSeg(user)) >= hydroMaxPerOwner {
		return nil, status.Errorf(codes.ResourceExhausted, "at most %d contents per player", hydroMaxPerOwner)
	}
	h.contents[c.Name] = c
	h.changed(c.Name)
	log.Printf("[Hydro] create %s by %s (%d bytes)", redact.Tag(c.Name), redact.Seg(user), len(c.GetPayload()))
	return proto.Clone(c).(*hydropb.Content), nil
}

func (h *hydro) CreateContent(ctx context.Context, req *hydropb.CreateContentRequest) (*hydropb.Content, error) {
	return h.create(ctx, req.GetParent(), req.GetContent())
}

func (h *hydro) BatchCreateContent(ctx context.Context, req *hydropb.BatchCreateContentRequest) (*hydropb.BatchCreateContentResponse, error) {
	if len(req.GetRequests()) > hydroMaxBatch {
		return nil, status.Errorf(codes.InvalidArgument, "at most %d requests", hydroMaxBatch)
	}
	out := &hydropb.BatchCreateContentResponse{}
	for _, r := range req.GetRequests() {
		parent := r.GetParent()
		if parent == "" {
			parent = req.GetParent()
		}
		c, err := h.create(ctx, parent, r.GetContent())
		if err != nil {
			return nil, err
		}
		out.Contents = append(out.Contents, c)
	}
	return out, nil
}

// live is a content that exists and has not expired. Caller holds h.mu.
func (h *hydro) live(name string) *hydropb.Content {
	c := h.contents[name]
	if c != nil && c.GetExpireTime() != nil && time.Now().After(c.GetExpireTime().AsTime()) {
		delete(h.contents, name)
		return nil
	}
	return c
}

func (h *hydro) GetContent(ctx context.Context, req *hydropb.GetContentRequest) (*hydropb.Content, error) {
	if _, err := callerUser(ctx); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.live(req.GetName()); c != nil {
		return proto.Clone(c).(*hydropb.Content), nil
	}
	now := timestamppb.Now() // the working servers answer a missing content empty, not NotFound
	return &hydropb.Content{Name: req.GetName(), CreateTime: now, UpdateTime: now}, nil
}

// mayChange: the owner, or anyone when the content grants UPDATE; a sent etag must match.
func mayChange(c *hydropb.Content, me, etag string) error {
	if c == nil {
		return status.Error(codes.NotFound, "no such content")
	}
	if lastSeg(c.GetUser()) != me {
		granted := false
		for _, p := range c.GetPermissions() {
			granted = granted || p == hydropb.Content_UPDATE
		}
		if !granted {
			return status.Error(codes.PermissionDenied, "not the caller's content")
		}
	}
	if etag != "" && etag != c.GetEtag() {
		return status.Error(codes.Aborted, "etag mismatch")
	}
	return nil
}

func (h *hydro) remove(ctx context.Context, r *hydropb.DeleteContentRequest) error {
	user, err := callerUser(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.live(r.GetName())
	if c == nil {
		return nil // already gone
	}
	if lastSeg(c.GetUser()) != lastSeg(user) { // deleting is the owner's alone, UPDATE or not
		return status.Error(codes.PermissionDenied, "not the caller's content")
	}
	if err := mayChange(c, lastSeg(user), r.GetEtag()); err != nil {
		return err
	}
	delete(h.contents, r.GetName())
	h.changed(r.GetName())
	return nil
}

func (h *hydro) DeleteContent(ctx context.Context, req *hydropb.DeleteContentRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, h.remove(ctx, req)
}

func (h *hydro) BatchDeleteContent(ctx context.Context, req *hydropb.BatchDeleteContentRequest) (*emptypb.Empty, error) {
	if len(req.GetRequests()) > hydroMaxBatch {
		return nil, status.Errorf(codes.InvalidArgument, "at most %d requests", hydroMaxBatch)
	}
	for _, r := range req.GetRequests() {
		if err := h.remove(ctx, r); err != nil {
			return nil, err
		}
	}
	return &emptypb.Empty{}, nil
}

func rpcStatus(err error) *spb.Status {
	return status.Convert(err).Proto()
}

func (h *hydro) BulkDeleteContent(ctx context.Context, req *hydropb.BulkDeleteContentRequest) (*hydropb.BulkDeleteContentResponse, error) {
	if len(req.GetRequests()) > hydroMaxBatch {
		return nil, status.Errorf(codes.InvalidArgument, "at most %d requests", hydroMaxBatch)
	}
	out := &hydropb.BulkDeleteContentResponse{}
	for _, r := range req.GetRequests() {
		out.Status = append(out.Status, rpcStatus(h.remove(ctx, r)))
	}
	return out, nil
}

// transform applies field transforms: `payload` takes a bytes set; `attributes.<n>` (or
// `attributes[<n>]`) takes an integer set or increment.
func (h *hydro) transform(ctx context.Context, r *hydropb.TransformContentRequest) error {
	user, err := callerUser(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.live(r.GetName())
	if err := mayChange(c, lastSeg(user), r.GetEtag()); err != nil {
		return err
	}
	next := proto.Clone(c).(*hydropb.Content)
	for _, ft := range r.GetFieldTransforms() {
		p := strings.Trim(ft.GetFieldPath(), "`")
		if p == "payload" {
			b, ok := ft.GetSet().GetValueType().(*commonpb.Value_BytesValue)
			if !ok {
				return status.Error(codes.InvalidArgument, "payload takes a bytes set")
			}
			next.Payload = b.BytesValue
			continue
		}
		key, ok := strings.CutPrefix(p, "attributes")
		key = strings.Trim(key, ".[]`")
		n, err := strconv.ParseInt(key, 10, 32)
		if !ok || err != nil {
			return status.Errorf(codes.InvalidArgument, "cannot transform %q", ft.GetFieldPath())
		}
		if next.Attributes == nil {
			next.Attributes = map[int32]int32{}
		}
		switch x := ft.GetTransformType().(type) {
		case *hydropb.FieldTransform_Set:
			next.Attributes[int32(n)] = int32(x.Set.GetIntegerValue())
		case *hydropb.FieldTransform_Increment:
			next.Attributes[int32(n)] += int32(x.Increment.GetIntegerValue())
		}
	}
	if len(next.GetPayload()) > hydroMaxPayload || len(next.GetAttributes()) > hydroMaxAttrs {
		return status.Errorf(codes.InvalidArgument, "content over %d bytes or %d attributes", hydroMaxPayload, hydroMaxAttrs)
	}
	next.UpdateTime = timestamppb.Now()
	h.contents[next.Name] = next
	h.changed(next.Name)
	return nil
}

func (h *hydro) TransformContent(ctx context.Context, req *hydropb.TransformContentRequest) (*hydropb.TransformContentResponse, error) {
	if err := h.transform(ctx, req); err != nil {
		return nil, err
	}
	return &hydropb.TransformContentResponse{Status: rpcStatus(nil)}, nil
}

func (h *hydro) BulkTransformContent(ctx context.Context, req *hydropb.BulkTransformContentRequest) (*hydropb.BulkTransformContentResponse, error) {
	if len(req.GetRequests()) > hydroMaxBatch {
		return nil, status.Errorf(codes.InvalidArgument, "at most %d requests", hydroMaxBatch)
	}
	out := &hydropb.BulkTransformContentResponse{}
	for _, r := range req.GetRequests() {
		out.Status = append(out.Status, rpcStatus(h.transform(ctx, r)))
	}
	return out, nil
}

// SearchContent lists live contents, the named user's only when a user is given, oldest first,
// paged by an offset token.
func (h *hydro) SearchContent(ctx context.Context, req *hydropb.SearchContentRequest) (*hydropb.SearchContentResponse, error) {
	if _, err := callerUser(ctx); err != nil {
		return nil, err
	}
	size := int(req.GetPageSize())
	if size <= 0 || size > hydroSearchMax {
		size = hydroSearchMax
	}
	from, _ := strconv.Atoi(req.GetPageToken())
	h.mu.Lock()
	var all []*hydropb.Content
	for name := range h.contents {
		c := h.live(name)
		if c == nil {
			continue
		}
		if u := lastSeg(req.GetUser()); u != "" && u != "current" && lastSeg(c.GetUser()) != u {
			continue
		}
		all = append(all, proto.Clone(c).(*hydropb.Content))
	}
	h.mu.Unlock()
	sort.Slice(all, func(i, j int) bool {
		if c := all[i].GetCreateTime().AsTime().Compare(all[j].GetCreateTime().AsTime()); c != 0 {
			return c < 0
		}
		return all[i].GetName() < all[j].GetName()
	})
	out := &hydropb.SearchContentResponse{}
	if from < 0 || from > len(all) {
		from = len(all)
	}
	end := min(from+size, len(all))
	out.Contents = all[from:end]
	if end < len(all) {
		out.NextPageToken = strconv.Itoa(end)
	}
	return out, nil
}

// TrackContent sends an empty response each time one of the named contents changes (the
// targets' keys are content names), until the caller goes away.
func (h *hydro) TrackContent(req *hydropb.TrackContentRequest, stream grpc.ServerStreamingServer[hydropb.TrackContentResponse]) error {
	ctx := stream.Context()
	if _, err := callerUser(ctx); err != nil {
		return err
	}
	w := make(chan string, 16)
	h.mu.Lock()
	h.watch[w] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.watch, w)
		h.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case name := <-w:
			if _, tracked := req.GetTargets()[name]; tracked {
				if err := stream.Send(&hydropb.TrackContentResponse{}); err != nil {
					return nil
				}
			}
		}
	}
}
