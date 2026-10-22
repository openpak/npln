package npln

// referee — the match verdict Splatoon 3 waits for, published on gamesync. Facts from earlier research
// notes on real Nintendo matches (titles/splatoon-3/docs/provenance.md):
//
//   - When every player is ready, each console writes "…/@@RefereeRequest/<time>_<game session>"
//     and watches the "@@RefereeResult" collection. The verdict goes -1 (accepted), then the console
//     writes "…/@@Profile/…" and the verdict goes 0 (kickoff); -1/0/1 in the wrong order are ignored,
//     and status -1 must carry the console's own game_mode/game_rule and one `users` entry per player. With no verdict the "players are getting ready"
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
	"fmt"
	"log"
	"sort"
	"strings"

	commonpb "github.com/openpak/npln/proto/common"
	"github.com/openpak/npln/redact"
)

// refereeKickoff moves the room's accepted verdicts (status -1) to kickoff (status 0) once a
// console has written its @@Profile, the step the game takes after seeing -1. Caller holds g.mu.
func (g *gamesyncServer) refereeKickoff(gsid string) {
	for k, v := range g.store {
		name := strings.TrimPrefix(k, gsid+"|")
		if name == k || !strings.Contains(name, "@@RefereeResult/") || v.GetFields()["status"].GetIntegerValue() != -1 {
			continue
		}
		f := map[string]*commonpb.Value{}
		for kk, vv := range v.GetFields() {
			f[kk] = vv
		}
		f["status"] = gsInt(0)
		nv := &commonpb.MapValue{Fields: f}
		g.store[k] = nv
		g.deliver(gsid, name, "UPDATED", nv)
		log.Printf("[GS] referee kickoff room=%s %s", redact.Tag(gsid), nameShape(name))
	}
}

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
	if _, ok := out["users"]; !ok { // one entry per player: the game compares the count
		var us []*commonpb.Value
		for _, t := range out["teams"].GetMapValue().GetFields() {
			us = append(us, t.GetArrayValue().GetValues()...)
		}
		out["users"] = &commonpb.Value{ValueType: &commonpb.Value_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: us}}}
	}
	if end {
		out["status"] = gsInt(1)
	} else {
		out["status"] = gsInt(-1) // accepted; kickoff (0) follows the consoles' @@Profile
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
	if strings.Contains(name, "@@Profile/") {
		g.refereeKickoff(gsid)
		return
	}
	resultName, v := g.refereeVerdict(gsid, name, written)
	if v == nil {
		return
	}
	log.Printf("[GS] referee request %s users=%s teams=%s", nameShape(name), valueShape(written.GetFields()["users"]), valueShape(written.GetFields()["teams"]))
	g.store[gsid+"|"+resultName] = v
	g.deliver(gsid, resultName, "UPDATED", v)
	log.Printf("[GS] referee verdict room=%s %s status=%d fields=%d", redact.Tag(gsid), nameShape(resultName), v.GetFields()["status"].GetIntegerValue(), len(v.GetFields()))
}

// nameShape is a document or collection name for the log with every id-like segment (one holding
// a digit and longer than 6 characters, e.g. a session uuid or "<time>_<session>") replaced.
func nameShape(n string) string {
	seg := strings.Split(n, "/")
	for i, x := range seg {
		if len(x) > 6 && strings.ContainsAny(x, "0123456789") {
			seg[i] = "<id>"
		}
	}
	return strings.Join(seg, "/")
}

// valueShape describes a value's structure for the log: types, keys and lengths, never contents.
func valueShape(v *commonpb.Value) string {
	switch x := v.GetValueType().(type) {
	case nil:
		return "-"
	case *commonpb.Value_StringValue:
		return fmt.Sprintf("str(%d)", len(x.StringValue))
	case *commonpb.Value_IntegerValue:
		return "int"
	case *commonpb.Value_ArrayValue:
		vs := x.ArrayValue.GetValues()
		if len(vs) == 0 {
			return "[]"
		}
		return fmt.Sprintf("[%d x %s]", len(vs), valueShape(vs[0]))
	case *commonpb.Value_MapValue:
		keys := make([]string, 0, len(x.MapValue.GetFields()))
		for k, vv := range x.MapValue.GetFields() {
			keys = append(keys, k+":"+valueShape(vv))
		}
		sort.Strings(keys)
		return "{" + strings.Join(keys, ",") + "}"
	default:
		return fmt.Sprintf("%T", x)
	}
}
