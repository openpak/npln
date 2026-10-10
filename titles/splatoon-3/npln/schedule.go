package npln

// schedule — nn.npln.toyohr.v1.Schedule, served from the rotation file.
//
// The rotation is data, not code: internal/rotation loads and validates it, cmd/genrotation
// writes one. Nothing recorded from the real service lives in this repository.
//
// The rule that shapes everything here: all four schedule kinds are read by the game as ONE set.
// A stale set is rejected with a visible error and the lobby says stage information is
// unavailable; an inconsistent set — kinds whose windows disagree about the present — aborts the
// game outright. So every response comes from the same loaded file, and the file was validated to
// be mutually consistent before the server started.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
	"github.com/openpak/npln/titles/splatoon-3/rotation"
)

type scheduleServer struct {
	toyohrpb.UnimplementedScheduleServer
	cur *atomic.Pointer[rotation.File] // swapped when the rotation is renewed; read once per call
}

// etag identifies a response so the client can skip one it already holds.
func etag(m proto.Message) string {
	b, err := proto.Marshal(m)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// window is the span a request asks about: from its current_time (or ours, if it sent none) for
// the duration it asked for.
func window(ts *timestamppb.Timestamp, d time.Duration) (time.Time, time.Time) {
	from := time.Now().UTC()
	if ts != nil && ts.IsValid() {
		from = ts.AsTime()
	}
	if d <= 0 {
		d = 24 * time.Hour
	}
	return from, from.Add(d)
}

func overlaps(ws, we, s, e time.Time) bool { return s.Before(we) && e.After(ws) }

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

// targetName is the schedule target a request names ("tenants/current/targets/<id>"), under the
// real tenant. The game parses every vs, coop and league schedule name and the vs params name as
// "tenants/{tenant}/targets/{target}/<collection>/{id}" (seasons are "tenants/{tenant}/
// seasonSchedules/{id}", no target) and aborts with 2162-0001 on a name that does not parse.
func targetName(target string) string {
	id := lastSeg(target)
	if id == "" {
		id = "default"
	}
	return Tenant + "/targets/" + id
}

// setID names the rotation a response came from. It is the same for every kind in one file,
// which is exactly the property the game needs them to share, and changes when it is renewed.
func setID(rot *rotation.File) string {
	return fmt.Sprintf("openpak-%d", rot.Vs[0].Start.Unix())
}

func (s *scheduleServer) SelectVsSchedules(_ context.Context, req *toyohrpb.SelectVsSchedulesRequest) (*toyohrpb.SelectVsSchedulesResponse, error) {
	rot := s.cur.Load()
	from, to := window(req.GetCurrentTime(), req.GetSelectDuration().AsDuration())
	out := &toyohrpb.SelectVsSchedulesResponse{}
	for i, w := range rot.Vs {
		if !overlaps(from, to, w.Start, w.End) {
			continue
		}
		sc := &toyohrpb.VsSchedule{
			Name:            fmt.Sprintf("%s/vsSchedules/%d", targetName(req.GetTarget()), i),
			StartTime:       ts(w.Start),
			EndTime:         ts(w.End),
			RegularSettings: &toyohrpb.RegularSettings{Stages: w.Regular},
			XSettings:       &toyohrpb.XSettings{Rule: w.X.Rule, Stages: w.X.Stages},
			LeagueSettings:  &toyohrpb.LeagueSettings{Rule: w.League.Rule, Stages: w.League.Stages},
			ScheduleSetId:   setID(rot),
		}
		for _, b := range w.Bankara {
			sc.BankaraSettings = append(sc.BankaraSettings, &toyohrpb.BankaraSettings{Rule: b.Rule, Stages: b.Stages})
		}
		out.Schedules = append(out.Schedules, sc)
	}
	out.Etag = etag(out)
	log.Printf("[Schedule] SelectVsSchedules %s..%s -> %d", from.Format(time.RFC3339), to.Format(time.RFC3339), len(out.Schedules))
	return out, nil
}

func (s *scheduleServer) SelectCoopSchedules(_ context.Context, req *toyohrpb.SelectCoopSchedulesRequest) (*toyohrpb.SelectCoopSchedulesResponse, error) {
	rot := s.cur.Load()
	from, to := window(req.GetCurrentTime(), 0)
	out := &toyohrpb.SelectCoopSchedulesResponse{}
	for i, w := range rot.Coop {
		if !overlaps(from, to, w.Start, w.End) {
			continue
		}
		out.Schedules = append(out.Schedules, &toyohrpb.CoopSchedule{
			Name:          fmt.Sprintf("%s/coopSchedules/%d", targetName(req.GetTarget()), i),
			ScheduleSetId: setID(rot),
			StartTime:     ts(w.Start),
			EndTime:       ts(w.End),
			ShiftId:       fmt.Sprintf("shift-%d", i),
			Timestamp:     ts(w.Start),
			Normal: &toyohrpb.CoopSchedule_Normal{
				Stage: w.Stage, Boss: w.Boss, MainWeapons: w.MainWeapons, KumaWeapon: w.KumaWeapon,
			},
		})
	}
	out.Etag = etag(out)
	log.Printf("[Schedule] SelectCoopSchedules -> %d", len(out.Schedules))
	return out, nil
}

func (s *scheduleServer) SelectSeasonSchedules(_ context.Context, req *toyohrpb.SelectSeasonSchedulesRequest) (*toyohrpb.SelectSeasonSchedulesResponse, error) {
	rot := s.cur.Load()
	from, to := window(req.GetCurrentTime(), 0)
	out := &toyohrpb.SelectSeasonSchedulesResponse{}
	for i, w := range rot.Season {
		if !overlaps(from, to, w.Start, w.End) {
			continue
		}
		out.Schedules = append(out.Schedules, &toyohrpb.SeasonSchedule{
			Name:          fmt.Sprintf("%s/seasonSchedules/%d", Tenant, i),
			StartTime:     ts(w.Start),
			EndTime:       ts(w.End),
			ScheduleSetId: setID(rot),
		})
	}
	out.Etag = etag(out)
	log.Printf("[Schedule] SelectSeasonSchedules -> %d", len(out.Schedules))
	return out, nil
}

func (s *scheduleServer) SelectLeagueSchedules(_ context.Context, req *toyohrpb.SelectLeagueSchedulesRequest) (*toyohrpb.SelectLeagueSchedulesResponse, error) {
	rot := s.cur.Load()
	from, to := window(req.GetCurrentTime(), 0)
	out := &toyohrpb.SelectLeagueSchedulesResponse{}
	for i, w := range rot.League {
		if !overlaps(from, to, w.Start, w.End) {
			continue
		}
		sc := &toyohrpb.LeagueSchedule{
			Name:          fmt.Sprintf("%s/leagueSchedules/%d", targetName(req.GetTarget()), i),
			Rule:          w.Rule,
			Stages:        w.Stages,
			ScheduleSetId: setID(rot),
			Timestamp:     ts(w.Start),
			StartTime:     ts(w.Start),
			EndTime:       ts(w.End),
		}
		for j, sl := range w.Slots {
			sc.Slots = append(sc.Slots, &toyohrpb.LeagueSchedule_Slot{
				Name:      fmt.Sprintf("%s/leagueSchedules/%d/slots/%d", targetName(req.GetTarget()), i, j),
				StartTime: ts(sl.Start),
				EndTime:   ts(sl.End),
			})
		}
		out.Schedules = append(out.Schedules, sc)
	}
	log.Printf("[Schedule] SelectLeagueSchedules -> %d", len(out.Schedules))
	return out, nil
}

// SelectVsParams carries timestamps of its own and must sit on the same time base as the
// schedules. Serving it from a different instant than the rotation leaves the game unable to tie
// the current rotation to its parameters.
func (s *scheduleServer) SelectVsParams(_ context.Context, req *toyohrpb.SelectVsParamsRequest) (*toyohrpb.SelectVsParamsResponse, error) {
	rot := s.cur.Load()
	from, _ := window(req.GetCurrentTime(), 0)
	cur := from
	for _, w := range rot.Vs {
		if !from.Before(w.Start) && from.Before(w.End) {
			cur = w.Start
			break
		}
	}
	out := &toyohrpb.SelectVsParamsResponse{
		Params: &toyohrpb.VsParams{
			Name:        targetName(req.GetTarget()) + "/vsParams/current",
			Timestamp_1: ts(cur),
			Timestamp_2: ts(cur),
			ParamsSetId: setID(rot),
		},
	}
	out.Etag = etag(out)
	return out, nil
}
