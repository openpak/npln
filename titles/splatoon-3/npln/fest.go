package npln

// fest — nn.npln.toyohr.v1.FestService. A Splatfest exists only when the operator defines one in
// the fest file (festfile.go, NPLN_FEST_FILE, default fests.json next to the rotation); with no
// file, or no fest inside its window, the service answers what the game reads between Splatfests
// (an empty schedule, FAILED_PRECONDITION on entry, NOT_FOUND on every Get).
//
// Served from the definition: SelectFestSchedule, CreateFestEntry (the team choice, stored once
// per player and fest) and GetFestEntry. NOT served, on purpose: GetFestDecryptionKey and
// GetFestResult stay NOT_FOUND. Which key opens which pack, and how a result is tallied from
// referee data, cannot be answered without a console (docs/splatfest.md).
//
// Resource-name shapes follow what the game parses (docs/provenance.md): fests/{f}/entries/{e},
// festSchedules/{s}. The entry id is the caller's user id; GetFestEntry ignores the id the client
// sends and answers for the caller, since the id format the client builds is unobserved.

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
)

type fest struct {
	toyohrpb.UnimplementedFestServiceServer
}

func (f *fest) SelectFestSchedule(ctx context.Context, req *toyohrpb.SelectFestScheduleRequest) (*toyohrpb.SelectFestScheduleResponse, error) {
	d := Fests.active(time.Now(), req.GetRegion())
	if d == nil {
		return &toyohrpb.SelectFestScheduleResponse{}, nil
	}
	log.Printf("[Fest] SelectFestSchedule region=%q -> fest %s", req.GetRegion(), d.ID)
	return &toyohrpb.SelectFestScheduleResponse{Schedule: d.schedule(tenantFromCtx(ctx), req.GetTarget())}, nil
}

func (f *fest) CreateFestEntry(ctx context.Context, req *toyohrpb.CreateFestEntryRequest) (*toyohrpb.FestEntry, error) {
	id := lastSeg(req.GetParent())
	d := Fests.byID(id)
	if d == nil || d.state(time.Now()) == festClosed {
		log.Printf("[Fest] CreateFestEntry fest=%q not running -> FAILED_PRECONDITION", short(id))
		return nil, status.Error(codes.FailedPrecondition, "no Splatfest is running")
	}
	uid := callerUID(ctx)
	if uid == "" {
		return nil, status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	in := req.GetFestEntry()
	team := lastSeg(in.GetFestTeam())
	if !d.hasTeam(team) {
		log.Printf("[Fest] CreateFestEntry fest=%s unknown team %q -> INVALID_ARGUMENT", id, short(in.GetFestTeam()))
		return nil, status.Error(codes.InvalidArgument, "unknown team")
	}
	e, err := Fests.enter(id, uid, in.GetFestTeam(), in.GetFestRegion())
	if err != nil {
		log.Printf("[Fest] CreateFestEntry store: %v", err)
		return nil, status.Error(codes.Internal, "entry not stored")
	}
	log.Printf("[Fest] CreateFestEntry fest=%s team=%s (entry kept)", id, short(e.Team))
	return entryMsg(tenantFromCtx(ctx), id, uid, e), nil
}

// getFestEntry answers the caller's own entry, or NOT_FOUND after a headers frame.
func getFestEntry(_ any, ss grpc.ServerStream) error {
	req := &toyohrpb.GetFestEntryRequest{}
	if err := ss.RecvMsg(req); err != nil {
		return err
	}
	ctx := ss.Context()
	fid := festOfEntryName(req.GetName())
	if uid := callerUID(ctx); uid != "" {
		if e, ok := Fests.entry(fid, uid); ok {
			return ss.SendMsg(entryMsg(tenantFromCtx(ctx), fid, uid, e))
		}
	}
	if err := sendEmptyHeaders(ss); err != nil {
		return err
	}
	return notFound("FestEntry", req.GetName())
}

// absent serves a Get on a resource OpenPak never has: NOT_FOUND after the headers, Nintendo
// shaped. newReq makes the request message the method takes.
func absent[T interface {
	proto.Message
	GetName() string
}](kind string, newReq func() T) rawHandler {
	return func(_ any, ss grpc.ServerStream) error {
		req := newReq()
		if err := ss.RecvMsg(req); err != nil {
			return err
		}
		if err := sendEmptyHeaders(ss); err != nil {
			return err
		}
		return notFound(kind, req.GetName())
	}
}

func festRawHandlers() map[string]rawHandler {
	return map[string]rawHandler{
		"GetFestEntry":         getFestEntry,
		"GetFestResult":        absent("FestResult", func() *toyohrpb.GetFestResultRequest { return &toyohrpb.GetFestResultRequest{} }),
		"GetFestDecryptionKey": absent("FestDecryptionKey", func() *toyohrpb.GetFestDecryptionKeyRequest { return &toyohrpb.GetFestDecryptionKeyRequest{} }),
	}
}
