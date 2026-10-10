package npln

// referee — the match verdict Splatoon 3 waits for, published on gamesync. Facts from the reference
// server's notes on real Nintendo matches (titles/splatoon-3/docs/provenance.md):
//
//   - When every player is ready, each console writes "…/@@RefereeRequest/<time>_<game session>"
//     and watches the "@@RefereeResult" collection. With no verdict the "players are getting ready"
//     countdown ends in a communication error. At the end of a match each writes
//     "…/@@RefereeReport/<time>_<game session>/User/<user session>" and waits for a verdict again.
//   - The verdict is ONE document, "…/@@RefereeResult/<time>_<game session>" (the first segment
//     only: the game watches direct members of the collection), whose `request_id` is that
//     "<time>_<game session>"; `status` is 0 at the start and 1 when the match is decided.
//   - It carries the request's own match context (stage, mode, rule, …) and, at the end, the
//     report's `team_result` and `personal_result`; `teams` maps team1/team2/team3 to user ids.
//
// Only titles with Options.Referee publish one; Stardew's titles never write these documents.
//
// ponytail: the end verdict passes the reporter's results through and keeps no history
// (VsResults, catalog points, Splatfest tallies); a private battle needs neither.

import (
	"log"
	"sort"
	"strings"

	commonpb "github.com/openpak/npln/proto/common"
	"github.com/openpak/npln/redact"
)

// refereeFields is the versus verdict's field inventory (a Turf War verdict as Nintendo sends it).
var refereeFields = map[string]bool{
	"bankara_open_schedule_id": true, "color_pattern": true, "est_mmr_team1": true,
	"est_mmr_team2": true, "est_mmr_team3": true, "event_parameter": true, "fest_id": true,
	"fest_region": true, "fest_resource_id": true, "game_mode": true, "game_rule": true,
	"league_part_id": true, "mm_region": true, "mode": true, "parent_npln_user_id": true,
	"personal_result": true, "request_id": true, "season_id": true, "stage": true, "status": true,
	"team_result": true, "teams": true, "tournament_id": true, "trace_id": true, "users": true,
}

// refereeSeason is the season id a verdict carries when the request names none.
var refereeSeason int64 = 1

// refereeVerdict returns the verdict document for a written request or report, or "" when name is
// neither. Caller holds g.mu.
func (g *gamesyncServer) refereeVerdict(gsid, name string, written *commonpb.MapValue) (string, *commonpb.MapValue) {
	end := false
	i := strings.Index(name, "@@RefereeRequest/")
	if i < 0 {
		if i = strings.Index(name, "@@RefereeReport/"); i < 0 {
			return "", nil
		}
		end = true
	}
	rest := name[i+strings.IndexByte(name[i:], '/')+1:]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	if rest == "" {
		return "", nil
	}
	resultName := name[:i] + "@@RefereeResult/" + rest

	out := map[string]*commonpb.Value{}
	if prev := g.store[gsid+"|"+resultName]; prev != nil { // the end verdict keeps the start's context
		for k, v := range prev.GetFields() {
			out[k] = v
		}
	}
	for k, v := range written.GetFields() {
		if refereeFields[k] {
			out[k] = v
		}
	}
	setDefault := func(k string, v *commonpb.Value) {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	out["request_id"] = gsStr(rest)
	out["trace_id"] = gsStr(strings.ReplaceAll(gsid, "-", ""))
	setDefault("season_id", gsInt(refereeSeason))
	setDefault("fest_id", gsStr(""))
	setDefault("league_part_id", gsStr(""))
	setDefault("tournament_id", gsStr(""))
	if _, ok := out["teams"]; !ok {
		out["teams"] = g.refereeTeams(gsid)
	}
	if end {
		out["status"] = gsInt(1)
	} else {
		out["status"] = gsInt(0)
	}
	return resultName, &commonpb.MapValue{Fields: out}
}

// refereeTeams splits the room's players alternately into team1 and team2 by arrival, when the
// request names no teams. Caller holds g.mu.
func (g *gamesyncServer) refereeTeams(gsid string) *commonpb.Value {
	var ss []*gsSession
	for _, s := range g.sess {
		if s.gsid == gsid && s.uid != "" {
			ss = append(ss, s)
		}
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].rank < ss[j].rank })
	teams := map[string][]*commonpb.Value{"team1": nil, "team2": nil, "team3": nil}
	for i, s := range ss {
		t := "team1"
		if i%2 == 1 {
			t = "team2"
		}
		teams[t] = append(teams[t], gsStr(s.uid))
	}
	m := map[string]*commonpb.Value{}
	for t, v := range teams {
		m[t] = &commonpb.Value{ValueType: &commonpb.Value_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: v}}}
	}
	return &commonpb.Value{ValueType: &commonpb.Value_MapValue{MapValue: &commonpb.MapValue{Fields: m}}}
}

// publishVerdict stores and delivers the verdict for a written document, if it is a referee
// request or report. Caller holds g.mu.
func (g *gamesyncServer) publishVerdict(gsid, name string, written *commonpb.MapValue) {
	if !titleOptions.Referee || gsid == "" {
		return
	}
	resultName, v := g.refereeVerdict(gsid, name, written)
	if v == nil {
		return
	}
	g.store[gsid+"|"+resultName] = v
	g.deliver(gsid, resultName, "UPDATED", v)
	log.Printf("[GS] referee verdict room=%s status=%d fields=%d", redact.Tag(gsid), v.GetFields()["status"].GetIntegerValue(), len(v.GetFields()))
}
