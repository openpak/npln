// Package rotation is the stage/mode rotation the server serves, and the file it comes from.
//
// Code and data are separate on purpose. The rotation is DATA, read from a file at run time;
// this package defines that file, loads it, and — most importantly — refuses to load one that
// would hurt the console. `cmd/genrotation` writes a valid file anchored to real time, so a
// fresh clone can serve a working rotation with nothing captured in it. An operator holding
// their own recorded rotation points the server at that instead.
//
// Why the validation is strict: a STALE rotation is rejected by the game with a visible error
// and the lobby reports that stage information is unavailable, but an INCONSISTENT one — types
// whose windows do not share a time base — is not rejected at all. It aborts the game
// (`2162-0001`). A server that hands the console a set it cannot use is worse than a server that
// refuses to start, so every problem below is a load-time failure with the offending entry named.
package rotation

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Version is the file format this build understands.
const Version = 1

// MinAhead is how far past "now" a rotation must still be defined to be worth serving. A file
// that expires within the hour would let a session start and then run out mid-play.
const MinAhead = 2 * time.Hour

// Window is a span of time. Every schedule kind embeds one, and the validator's rules are mostly
// about these.
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Mode is one mode's settings inside a VS window: which rule is played, on which stages.
type Mode struct {
	Rule   int32   `json:"rule"`
	Stages []int32 `json:"stages"`
}

// VsWindow is one turn of the versus rotation: the four ladders that run concurrently.
type VsWindow struct {
	Window
	Regular []int32 `json:"regular_stages"`
	Bankara []Mode  `json:"bankara"`
	X       Mode    `json:"x"`
	League  Mode    `json:"league"`
}

// CoopWindow is one co-op shift.
type CoopWindow struct {
	Window
	Stage       int32   `json:"stage"`
	Boss        string  `json:"boss"`
	MainWeapons []int64 `json:"main_weapons"`
	KumaWeapon  int32   `json:"kuma_weapon"`
}

// LeagueWindow is a league event and the slots it runs in.
type LeagueWindow struct {
	Window
	Rule   int32    `json:"rule"`
	Stages []int32  `json:"stages"`
	Slots  []Window `json:"slots"`
}

// File is the rotation on disk.
type File struct {
	Version int            `json:"version"`
	Vs      []VsWindow     `json:"vs"`
	Coop    []CoopWindow   `json:"coop"`
	Season  []Window       `json:"season"`
	League  []LeagueWindow `json:"league"`
}

// Load reads and validates a rotation file. It never returns a partially valid rotation: either
// the console can be served from it, or this is an error the operator has to see.
func Load(path string, at time.Time) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rotation: %w", err)
	}
	var f File
	dec := json.NewDecoder(newTrimReader(b))
	dec.DisallowUnknownFields() // a typo'd key silently doing nothing is how a rotation goes stale
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("rotation %s: %w", path, err)
	}
	if err := f.Validate(at); err != nil {
		return nil, fmt.Errorf("rotation %s: %w", path, err)
	}
	return &f, nil
}

// Validate reports the first reason this rotation must not be served at time `at`.
func (f *File) Validate(at time.Time) error {
	if f.Version != Version {
		return fmt.Errorf("version %d, want %d", f.Version, Version)
	}

	vs := make([]Window, len(f.Vs))
	for i, w := range f.Vs {
		vs[i] = w.Window
		if len(w.Regular) == 0 {
			return fmt.Errorf("vs[%d]: no regular stages", i)
		}
		if len(w.Bankara) == 0 {
			return fmt.Errorf("vs[%d]: no bankara modes", i)
		}
	}
	coop := make([]Window, len(f.Coop))
	for i, w := range f.Coop {
		coop[i] = w.Window
		if w.Boss == "" {
			return fmt.Errorf("coop[%d]: no boss", i)
		}
	}
	league := make([]Window, len(f.League))
	for i, w := range f.League {
		league[i] = w.Window
		if err := checkWindows(fmt.Sprintf("league[%d].slots", i), w.Slots); err != nil {
			return err
		}
	}

	for _, set := range []struct {
		name string
		ws   []Window
	}{{"vs", vs}, {"coop", coop}, {"season", f.Season}, {"league", league}} {
		if len(set.ws) == 0 {
			return fmt.Errorf("%s: empty. Every schedule kind must be present — the game reads them as one set", set.name)
		}
		if err := checkWindows(set.name, set.ws); err != nil {
			return err
		}
		// The shared time base. All four kinds must describe the same present, or the console
		// aborts rather than reporting an error.
		if !covers(set.ws, at) {
			return fmt.Errorf("%s: nothing covers %s. Every schedule kind must be current at the same instant; regenerate the whole file rather than one kind",
				set.name, at.UTC().Format(time.RFC3339))
		}
	}

	// Serving a rotation that expires immediately starts a session that cannot finish.
	if end := lastEnd(vs); end.Sub(at) < MinAhead {
		return fmt.Errorf("vs: ends %s, only %s past %s. Regenerate: a rotation must stay defined at least %s ahead",
			end.UTC().Format(time.RFC3339), end.Sub(at).Round(time.Minute), at.UTC().Format(time.RFC3339), MinAhead)
	}
	return nil
}

// checkWindows enforces what makes a list of spans a schedule: each one ordered, and the list
// sorted with no overlap. Overlapping windows are the inconsistency that aborts the game.
func checkWindows(name string, ws []Window) error {
	for i, w := range ws {
		if w.Start.IsZero() || w.End.IsZero() {
			return fmt.Errorf("%s[%d]: missing start or end", name, i)
		}
		if !w.End.After(w.Start) {
			return fmt.Errorf("%s[%d]: ends %s at or before it starts %s",
				name, i, w.End.UTC().Format(time.RFC3339), w.Start.UTC().Format(time.RFC3339))
		}
		if i > 0 && w.Start.Before(ws[i-1].End) {
			return fmt.Errorf("%s[%d]: starts %s, before %s[%d] ends %s — windows must be sorted and must not overlap",
				name, i, w.Start.UTC().Format(time.RFC3339), name, i-1, ws[i-1].End.UTC().Format(time.RFC3339))
		}
	}
	return nil
}

func covers(ws []Window, at time.Time) bool {
	for _, w := range ws {
		if !at.Before(w.Start) && at.Before(w.End) {
			return true
		}
	}
	return false
}

func lastEnd(ws []Window) time.Time {
	var last time.Time
	for _, w := range ws {
		if w.End.After(last) {
			last = w.End
		}
	}
	return last
}

// Sort orders every list by start time, so a hand-edited file does not have to be.
func (f *File) Sort() {
	sort.SliceStable(f.Vs, func(i, j int) bool { return f.Vs[i].Start.Before(f.Vs[j].Start) })
	sort.SliceStable(f.Coop, func(i, j int) bool { return f.Coop[i].Start.Before(f.Coop[j].Start) })
	sort.SliceStable(f.Season, func(i, j int) bool { return f.Season[i].Start.Before(f.Season[j].Start) })
	sort.SliceStable(f.League, func(i, j int) bool { return f.League[i].Start.Before(f.League[j].Start) })
}
