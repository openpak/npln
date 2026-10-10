package npln

import (
	"context"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
	"github.com/openpak/npln/titles/splatoon-3/rotation"
)

// Every schedule name follows the grammar the game parses (and aborts on, 2162-0001, when it
// does not): vs, coop and league schedules and the vs params live under the request's target,
// seasons directly under the tenant.
func TestScheduleNamesParse(t *testing.T) {
	now := time.Now().UTC()
	var cur atomic.Pointer[rotation.File]
	cur.Store(rotation.Generate(now, 48, rotation.DefaultStages, 1))
	s := &scheduleServer{cur: &cur}
	ctx, at := context.Background(), timestamppb.New(now)
	const target = "tenants/current/targets/default"
	seg := `[^/]+`
	under := func(coll string) *regexp.Regexp {
		return regexp.MustCompile(`^tenants/` + seg + `/targets/` + seg + `/` + coll + `/` + seg + `$`)
	}
	check := func(re *regexp.Regexp, name string) {
		t.Helper()
		if !re.MatchString(name) {
			t.Errorf("%q does not parse as %s", name, re)
		}
	}
	vs, _ := s.SelectVsSchedules(ctx, &toyohrpb.SelectVsSchedulesRequest{Target: target, CurrentTime: at})
	coop, _ := s.SelectCoopSchedules(ctx, &toyohrpb.SelectCoopSchedulesRequest{Target: target, CurrentTime: at})
	league, _ := s.SelectLeagueSchedules(ctx, &toyohrpb.SelectLeagueSchedulesRequest{Target: target, CurrentTime: at})
	season, _ := s.SelectSeasonSchedules(ctx, &toyohrpb.SelectSeasonSchedulesRequest{Tenant: "tenants/current", Target: "default", CurrentTime: at})
	params, _ := s.SelectVsParams(ctx, &toyohrpb.SelectVsParamsRequest{Target: target, CurrentTime: at})
	if len(vs.GetSchedules()) == 0 || len(coop.GetSchedules()) == 0 || len(season.GetSchedules()) == 0 {
		t.Fatalf("empty rotation: vs %d coop %d season %d", len(vs.GetSchedules()), len(coop.GetSchedules()), len(season.GetSchedules()))
	}
	for _, x := range vs.GetSchedules() {
		check(under("vsSchedules"), x.GetName())
	}
	for _, x := range coop.GetSchedules() {
		check(under("coopSchedules"), x.GetName())
	}
	for _, x := range league.GetSchedules() {
		check(under("leagueSchedules"), x.GetName())
	}
	for _, x := range season.GetSchedules() {
		check(regexp.MustCompile(`^tenants/`+seg+`/seasonSchedules/`+seg+`$`), x.GetName())
	}
	check(under("vsParams"), params.GetParams().GetName())
}
