package splatoon3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openpak/npln/titles/splatoon-3/rotation"
)

func TestScheduleJSON(t *testing.T) {
	now := time.Now().UTC()
	current.Store(rotation.Generate(now, 72, 24, 1))
	defer current.Store(nil)
	rec := httptest.NewRecorder()
	serveSchedule(rec, httptest.NewRequest("GET", "/api/v1/schedule.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var s apiSchedule
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Game != "s3" || len(s.Vs) == 0 || len(s.Coop) == 0 || len(s.Vs[0].Modes) != 5 {
		t.Fatalf("unexpected body: %+v", s)
	}
	for _, m := range s.Vs[0].Modes[1:] {
		if m.Rule < 1 || m.Rule > 4 || len(m.Stages) != 2 {
			t.Fatalf("ranked mode %+v", m)
		}
	}
	bad := httptest.NewRecorder()
	serveSchedule(bad, httptest.NewRequest("GET", "/api/v1/schedule.json?from=x", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad from: %d", bad.Code)
	}
}
