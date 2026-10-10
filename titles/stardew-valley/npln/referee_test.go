package npln

import (
	"testing"

	commonpb "github.com/openpak/npln/proto/common"
)

// A start request gets a status-0 verdict under @@RefereeResult/<first segment> carrying the
// request's match context; the end report (a sub-document) gets status 1 on the same document,
// keeping the start's context and adding the report's results. Unknown fields are not echoed.
func TestRefereeVerdict(t *testing.T) {
	titleOptions = Options{Referee: true}
	defer func() { titleOptions = Options{} }()
	g := newGamesync(newSessionServer())
	g.sess["uss1"] = &gsSession{uid: "u-a", gsid: "room", rank: 1}
	g.sess["uss2"] = &gsSession{uid: "u-b", gsid: "room", rank: 2}
	f := func(kv map[string]*commonpb.Value) *commonpb.MapValue { return &commonpb.MapValue{Fields: kv} }

	g.mu.Lock()
	g.publishVerdict("room", "p/docs/@@RefereeRequest/123_room", f(map[string]*commonpb.Value{"stage": gsInt(5), "battle_id": gsStr("x")}))
	g.mu.Unlock()
	v := g.store["room|p/docs/@@RefereeResult/123_room"].GetFields()
	if v["status"].GetIntegerValue() != 0 || v["stage"].GetIntegerValue() != 5 || v["request_id"].GetStringValue() != "123_room" || v["battle_id"] != nil {
		t.Fatalf("start verdict: %v", v)
	}
	if n := len(v["teams"].GetMapValue().GetFields()["team1"].GetArrayValue().GetValues()); n != 1 {
		t.Fatalf("teams: %v", v["teams"])
	}

	g.mu.Lock()
	g.publishVerdict("room", "p/docs/@@RefereeReport/123_room/User/uss1", f(map[string]*commonpb.Value{"team_result": gsStr("r")}))
	g.mu.Unlock()
	v = g.store["room|p/docs/@@RefereeResult/123_room"].GetFields()
	if v["status"].GetIntegerValue() != 1 || v["stage"].GetIntegerValue() != 5 || v["team_result"].GetStringValue() != "r" {
		t.Fatalf("end verdict: %v", v)
	}
	if g.store["room|p/docs/@@RefereeResult/123_room/User/uss1"] != nil {
		t.Fatal("verdict published one level too deep")
	}
}
