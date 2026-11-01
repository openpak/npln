package splatoon3

// api — a read-only JSON view of the schedule the NPLN toyohr.Schedule service serves, for the
// shared SplatNet site (and anything else that wants the rotation without speaking gRPC).
//
//	GET /api/v1/schedule.json[?from=RFC3339][&to=RFC3339]
//
// Served on the plain health listener (HEALTH_LISTEN, 21013 in the container), internal network
// only; nothing here is secret and nothing is writable. The answer comes from the same loaded
// rotation the game is served from, through the same derivations (ranked rules per window, the
// co-op stage), so the site and the console always agree. Interface documented in
// docs/schedule-api.md.

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/openpak/npln/titles/splatoon-3/npln"
	"github.com/openpak/npln/titles/splatoon-3/rotation"
)

// current is the rotation being served; run() stores into it and the renewal loop swaps it.
var current atomic.Pointer[rotation.File]

func routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/schedule.json", serveSchedule)
	mux.HandleFunc("GET /api/v1/fests.json", serveFests)
}

var ruleNames = map[int32]string{0: "turf_war", 1: "splat_zones", 2: "tower_control", 3: "rainmaker", 4: "clam_blitz"}

type apiMode struct {
	Kind     string  `json:"kind"` // regular | bankara_series | bankara_open | x | league
	Rule     int32   `json:"rule"`
	RuleName string  `json:"rule_name"`
	Stages   []int32 `json:"stages"`
}

type apiSlot struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Modes []apiMode `json:"modes"`
}

type apiCoop struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	ShiftID    string    `json:"shift_id"`
	Stage      int32     `json:"stage"`
	Boss       string    `json:"boss"`
	KumaWeapon int32     `json:"kuma_weapon"`
	Weapons    []int64   `json:"main_weapons"` // -1 = random
}

type apiSchedule struct {
	Game        string      `json:"game"`
	GeneratedAt time.Time   `json:"generated_at"`
	SetID       string      `json:"set_id"`
	ValidUntil  time.Time   `json:"valid_until"`
	Vs          []apiSlot   `json:"vs"`
	Coop        []apiCoop   `json:"coop"`
	Season      []apiWindow `json:"season"`
}

type apiWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func mode(kind string, rule int32, stages []int32) apiMode {
	return apiMode{Kind: kind, Rule: rule, RuleName: ruleNames[rule], Stages: stages}
}

func serveSchedule(w http.ResponseWriter, r *http.Request) {
	rot := current.Load()
	if rot == nil {
		http.Error(w, `{"error":"no rotation loaded"}`, http.StatusServiceUnavailable)
		return
	}
	now := time.Now().UTC()
	from, to := now.Add(-2*time.Hour), now.Add(48*time.Hour) // the running slot, and two days ahead
	for k, dst := range map[string]*time.Time{"from": &from, "to": &to} {
		if v := r.URL.Query().Get(k); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				http.Error(w, `{"error":"`+k+` must be RFC3339"}`, http.StatusBadRequest)
				return
			}
			*dst = t.UTC()
		}
	}
	in := func(s, e time.Time) bool { return s.Before(to) && e.After(from) }

	out := apiSchedule{Game: "s3", GeneratedAt: now, SetID: npln.SetID(rot), ValidUntil: rot.VsEnd(),
		Vs: []apiSlot{}, Coop: []apiCoop{}, Season: []apiWindow{}}
	for _, v := range rot.Vs {
		if !in(v.Start, v.End) {
			continue
		}
		rr := rotation.RankedRules(v.Start)
		s := apiSlot{Start: v.Start.UTC(), End: v.End.UTC(), Modes: []apiMode{mode("regular", 0, v.Regular)}}
		for j, b := range v.Bankara {
			kind, rule := "bankara_open", b.Rule
			if j < 2 {
				rule = rr[j]
			}
			if j == 0 {
				kind = "bankara_series"
			}
			s.Modes = append(s.Modes, mode(kind, rule, b.Stages))
		}
		s.Modes = append(s.Modes, mode("x", rr[2], v.X.Stages), mode("league", rr[3], v.League.Stages))
		out.Vs = append(out.Vs, s)
	}
	for _, c := range rot.Coop {
		if in(c.Start, c.End) {
			out.Coop = append(out.Coop, apiCoop{Start: c.Start.UTC(), End: c.End.UTC(), ShiftID: c.Start.UTC().Format("20060102150405"),
				Stage: npln.CoopStage(c.Start), Boss: "SakelienGiant", KumaWeapon: -1, Weapons: []int64{-1, -1, -1, -1}})
		}
	}
	for _, s := range rot.Season {
		if in(s.Start, s.End) {
			out.Season = append(out.Season, apiWindow{s.Start.UTC(), s.End.UTC()})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(out)
}

// serveFests lists the defined Splatfests, their state now and the entries per team.
func serveFests(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=30")
	_ = json.NewEncoder(w).Encode(map[string]any{"game": "s3", "fests": npln.Fests.View(time.Now().UTC())})
}
