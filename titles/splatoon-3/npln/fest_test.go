package npln

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
)

func TestFestFlow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NPLN_JWT_KEY", filepath.Join(dir, "k.key"))
	t.Setenv("NPLN_ROTATION", filepath.Join(dir, "rotation.json"))
	Fests = &festStore{}
	f := &fest{}
	now := time.Now().UTC()
	tok := mintAccessToken(0x1234, Tenant+"/users/u-abc", Tenant)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+tok))

	// No file: empty schedule, entry refused.
	r, _ := f.SelectFestSchedule(ctx, &toyohrpb.SelectFestScheduleRequest{Region: "us"})
	if r.GetSchedule() != nil {
		t.Fatal("schedule with no fest file")
	}
	req := &toyohrpb.CreateFestEntryRequest{Parent: Tenant + "/fests/F1", FestEntry: &toyohrpb.FestEntry{FestTeam: "alpha", FestRegion: "us"}}
	if _, err := f.CreateFestEntry(ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("want FAILED_PRECONDITION, got %v", err)
	}

	def := map[string]any{"fests": []festDef{
		{ID: "F1", Teams: []string{"alpha", "bravo"}, GameData: "res", GameDataRevision: "rev", Regions: []string{"us"},
			Open: now.Add(-3 * time.Hour), Start: now.Add(-2 * time.Hour), Mid: now.Add(time.Hour), End: now.Add(2 * time.Hour), Close: now.Add(3 * time.Hour)},
		{ID: "BAD", Open: now, Start: now, Mid: now, End: now, Close: now},
	}}
	b, _ := json.Marshal(def)
	os.WriteFile(filepath.Join(dir, "fests.json"), b, 0o644)

	r, _ = f.SelectFestSchedule(ctx, &toyohrpb.SelectFestScheduleRequest{Region: "us"})
	s := r.GetSchedule()
	if s == nil || s.GetFest() != Tenant+"/fests/F1" || s.GetFestGameData() != "res" || s.GetTimetable().GetMidTime() == nil {
		t.Fatalf("schedule: %v", s)
	}
	if r, _ := f.SelectFestSchedule(ctx, &toyohrpb.SelectFestScheduleRequest{Region: "eu"}); r.GetSchedule() != nil {
		t.Fatal("eu got a us fest")
	}
	req.FestEntry.FestTeam = "zulu"
	if _, err := f.CreateFestEntry(ctx, req); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad team: %v", err)
	}
	req.FestEntry.FestTeam = "bravo"
	e, err := f.CreateFestEntry(ctx, req)
	if err != nil || e.GetFestTeam() != "bravo" {
		t.Fatalf("entry: %v %v", e, err)
	}
	req.FestEntry.FestTeam = "alpha" // first choice wins
	if e, _ = f.CreateFestEntry(ctx, req); e.GetFestTeam() != "bravo" {
		t.Fatal("entry changed")
	}
	// Survives a restart.
	Fests = &festStore{}
	if got, ok := Fests.entry("F1", "u-abc"); !ok || got.Team != "bravo" {
		t.Fatalf("not persisted: %v", got)
	}
	if v := Fests.View(now); len(v) != 1 || v[0].State != festLive || v[0].Entries["bravo"] != 1 {
		t.Fatalf("view: %+v", v)
	}
}
