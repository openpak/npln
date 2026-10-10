package npln

// fest — nn.npln.toyohr.v1.FestService. OpenPak runs no Splatfest, so the schedule selection is
// empty and every fest resource is absent (NOT_FOUND after a headers frame, rawunary.go).
//
// ponytail: an operator-defined Splatfest (schedule, entries, results, team keys) is a later
// feature; the empty answers are what the game reads between Splatfests.

import (
	"context"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
)

type fest struct {
	toyohrpb.UnimplementedFestServiceServer
}

func (f *fest) SelectFestSchedule(context.Context, *toyohrpb.SelectFestScheduleRequest) (*toyohrpb.SelectFestScheduleResponse, error) {
	return &toyohrpb.SelectFestScheduleResponse{}, nil
}

func (f *fest) CreateFestEntry(context.Context, *toyohrpb.CreateFestEntryRequest) (*toyohrpb.FestEntry, error) {
	log.Printf("[Fest] CreateFestEntry with no Splatfest running -> FAILED_PRECONDITION")
	return nil, status.Error(codes.FailedPrecondition, "no Splatfest is running")
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
		"GetFestEntry":         absent("FestEntry", func() *toyohrpb.GetFestEntryRequest { return &toyohrpb.GetFestEntryRequest{} }),
		"GetFestResult":        absent("FestResult", func() *toyohrpb.GetFestResultRequest { return &toyohrpb.GetFestResultRequest{} }),
		"GetFestDecryptionKey": absent("FestDecryptionKey", func() *toyohrpb.GetFestDecryptionKeyRequest { return &toyohrpb.GetFestDecryptionKeyRequest{} }),
	}
}
