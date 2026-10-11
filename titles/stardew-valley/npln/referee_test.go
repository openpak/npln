package npln

import (
	"context"
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
	if v["status"].GetIntegerValue() != -1 || v["stage"].GetIntegerValue() != 5 || v["request_id"].GetStringValue() != "123_room" || v["battle_id"] != nil {
		t.Fatalf("start verdict: %v", v)
	}
	if n := len(v["teams"].GetMapValue().GetFields()["team1"].GetArrayValue().GetValues()); n != 1 {
		t.Fatalf("teams: %v", v["teams"])
	}

	g.mu.Lock()
	g.publishVerdict("room", "p/docs/@@Profile/123_room/User/uss1", f(nil))
	g.mu.Unlock()
	if v = g.store["room|p/docs/@@RefereeResult/123_room"].GetFields(); v["status"].GetIntegerValue() != 0 {
		t.Fatalf("kickoff: %v", v["status"])
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
	// A re-push reads the verdict back for the watching stream's room, although its last segment
	// looks like a session id.
	if f := g.fields("p/docs/@@RefereeResult/1791000000_0e3c1b2a-aaaa-bbbb-cccc-0123456789ab", "uss1"); f.GetFields()["request_id"] != nil {
		t.Fatalf("unexpected: %v", f)
	}
	g.mu.Lock()
	g.publishVerdict("room", "p/docs/@@RefereeRequest/1791000000_0e3c1b2a-aaaa-bbbb-cccc-0123456789ab", f(nil))
	g.mu.Unlock()
	if f := g.fields("p/docs/@@RefereeResult/1791000000_0e3c1b2a-aaaa-bbbb-cccc-0123456789ab", "uss1"); f.GetFields()["request_id"] == nil {
		t.Fatalf("re-push lost the verdict: %v", f)
	}
}

// The latency server is the STUN server unless NPLN_LATENCY_HOST names another: never loopback
// when the STUN host is configured.
func TestLatencyServerIsStun(t *testing.T) {
	t.Setenv("NPLN_STUN_HOST", "203.0.113.7")
	t.Setenv("NPLN_LATENCY_HOST", "")
	r, err := newSessionServer().ListLatencyMeasurementServers(context.Background(), nil)
	if err != nil || len(r.GetLatencyMeasurementServers()) != 1 {
		t.Fatal(r, err)
	}
	if s := r.GetLatencyMeasurementServers()[0]; s.GetHost() != "203.0.113.7" || s.GetPort() != 3478 {
		t.Fatalf("latency server %s:%d", s.GetHost(), s.GetPort())
	}
}
