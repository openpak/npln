package npln

// gamerecord — nn.npln.toyohr.v1.GameRecord/InitializeAttributes: a new player's coop or VS
// attribute document. The lobby reads services/GameRecord/users/<id>/{Coop,Vs}UserAttribute/Data;
// a missing one makes the game ask for its initial attributes here, then read the document again.
// The game parses only `attributes` and every key in it is optional (docs/provenance.md), so a new
// player starts at zero; the same map is written into the ugcstore so the read that follows finds it.

import (
	"context"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/openpak/npln/proto/common"
	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
)

type gameRecord struct {
	toyohrpb.UnimplementedGameRecordServer
	ugc *ugcStore
}

// seasonID is the season the server serves (SelectSeasonSchedules field 2); a VS document on
// another season makes the game ask for a new season.
const seasonID = 1

func (g *gameRecord) InitializeAttributes(ctx context.Context, req *toyohrpb.InitializeAttributesRequest) (*toyohrpb.InitializeAttributesResponse, error) {
	uid := callerUID(ctx)
	if uid == "" {
		return nil, status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	kind, fields := "VsUserAttribute", map[string]*commonpb.Value{
		"season_id": iv(seasonID), "udemae": iv(0), "udemae_point": iv(0), "best_udemae": iv(0),
		"is_udemae_reset": {ValueType: &commonpb.Value_BooleanValue{BooleanValue: false}},
	}
	if req.GetInitialCoop() != nil || req.GetCoopShift() != nil || req.GetCoopGrade() != nil {
		kind, fields = "CoopUserAttribute", map[string]*commonpb.Value{
			"shift_id": sv(req.GetCoopShift().GetShiftId()), "limited_shift_id": sv(""),
			"mm_disable_limit": iv(0), "total_grade_point": iv(int64(req.GetCoopGrade().GetTotalGradePoint())), "smell": iv(0),
		}
	}
	path := "services/GameRecord/users/" + uid + "/" + kind + "/Data"
	attrs := &commonpb.MapValue{Fields: fields}
	g.ugc.put(path, attrs)
	log.Printf("[GameRecord] InitializeAttributes %s -> new player's %s", req.GetInitializeType(), kind)
	return &toyohrpb.InitializeAttributesResponse{Attributes: attrs, Document: Tenant + ugcDocsPart + path}, nil
}

// put stores a document the server itself creates (not a player's commit).
func (u *ugcStore) put(path string, fields *commonpb.MapValue) {
	now := timestamppb.Now()
	u.mu.Lock()
	defer u.mu.Unlock()
	d := &ugcpb.Document{Name: path, Fields: fields, CreateTime: now, UpdateTime: now}
	if cur := u.docs[path]; cur != nil {
		d.CreateTime = cur.GetCreateTime()
	}
	u.docs[path] = d
	u.save()
}
