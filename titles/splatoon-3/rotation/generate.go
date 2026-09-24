package rotation

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"time"
)

// The rotation is generated, not recorded: nothing captured from the real service is in this
// repository or in what Generate writes. It is anchored to real time — the window containing
// "now" is always present — because a rotation that does not describe the present is rejected
// by the game and the lobby reports that stage information is unavailable.
//
// The versus rotation turns on a two-hour boundary. Co-op shifts and seasons are longer; the
// exact real-world cadence of each is not something this repository has established, and it does
// not have to match — what matters is that every kind is current and the windows are consistent.
const (
	vsPeriod     = 2 * time.Hour
	coopPeriod   = 24 * time.Hour
	seasonPeriod = 90 * 24 * time.Hour
)

// Defaults for a rotation the server writes for itself.
const (
	GenerateHours = 30 * 24 // how far ahead a generated rotation reaches
	DefaultStages = 24      // stage ids to rotate through
	// RefreshAhead is when the server replaces its rotation: a generated file lasts GenerateHours,
	// so it is renewed about every three weeks and never gets near MinAhead.
	RefreshAhead = 7 * 24 * time.Hour
)

// Generate builds a consistent set: every kind starts on or before `at`, and all of them are
// derived from the same anchor so they cannot disagree about the present. Output is deterministic
// for a given at and seed.
func Generate(at time.Time, hours, stages int, seed int64) *File {
	rng := rand.New(rand.NewSource(seed))
	at = at.UTC()
	f := &File{Version: Version}

	// Versus: aligned to the period boundary at or before the anchor, so "now" is always inside
	// the first window rather than in a gap before it.
	start := at.Truncate(vsPeriod)
	for i := 0; time.Duration(i)*vsPeriod < time.Duration(hours)*time.Hour; i++ {
		s := start.Add(time.Duration(i) * vsPeriod)
		f.Vs = append(f.Vs, VsWindow{
			Window:  Window{Start: s, End: s.Add(vsPeriod)},
			Regular: pick(rng, stages, 2),
			Bankara: []Mode{
				{Rule: rule(rng), Stages: pick(rng, stages, 2)},
				{Rule: rule(rng), Stages: pick(rng, stages, 2)},
			},
			X:      Mode{Rule: rule(rng), Stages: pick(rng, stages, 2)},
			League: Mode{Rule: rule(rng), Stages: pick(rng, stages, 2)},
		})
	}

	// Co-op: same treatment, longer period.
	coopStart := at.Truncate(coopPeriod)
	for i := 0; time.Duration(i)*coopPeriod < time.Duration(hours)*time.Hour+coopPeriod; i++ {
		s := coopStart.Add(time.Duration(i) * coopPeriod)
		f.Coop = append(f.Coop, CoopWindow{
			Window:      Window{Start: s, End: s.Add(coopPeriod)},
			Stage:       int32(rng.Intn(stages) + 1),
			Boss:        "boss-1",
			MainWeapons: []int64{1, 2, 3, 4},
			KumaWeapon:  1,
		})
	}

	// Season: one window wide enough that the anchor is comfortably inside it.
	seasonStart := at.Add(-seasonPeriod / 2).Truncate(24 * time.Hour)
	f.Season = []Window{{Start: seasonStart, End: seasonStart.Add(seasonPeriod)}}

	// League: one event spanning the generated range, with a slot per versus window so the game
	// has something inside it. The event must cover the anchor like every other kind.
	ev := LeagueWindow{
		Window: Window{Start: start, End: start.Add(time.Duration(hours) * time.Hour)},
		Rule:   rule(rng),
		Stages: pick(rng, stages, 2),
	}
	for _, w := range f.Vs {
		ev.Slots = append(ev.Slots, Window{Start: w.Start, End: w.End})
	}
	f.League = []LeagueWindow{ev}

	f.Sort()
	return f
}

// VsEnd is when the last versus window ends: the moment this rotation stops describing the present.
func (f *File) VsEnd() time.Time {
	if len(f.Vs) == 0 {
		return time.Time{}
	}
	return f.Vs[len(f.Vs)-1].End.UTC()
}

// Write saves a rotation with a header saying where it came from and when it runs out.
func Write(path string, f *File, at time.Time) error {
	b, err := marshal(f)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("// Generated, anchored at %s.\n"+
		"// The server renews it on its own; the last versus window ends %s.\n",
		at.UTC().Format(time.RFC3339), f.VsEnd().Format(time.RFC3339))
	return os.WriteFile(path, append([]byte(header), b...), 0o644)
}

// Keep returns the rotation to serve at `now`, renewing the file first when it is missing or
// ends within RefreshAhead — so a running server never serves a rotation that runs out, and a
// restart after a long stop does not refuse to start over an expired file. A file that is present
// and current still goes through Load, so a broken hand edit is refused as before. A renewed
// rotation that cannot be written (read-only mount) is still served; the error says so.
func Keep(path string, now time.Time) (f *File, renewed bool, err error) {
	now = now.UTC()
	cur, err := read(path)
	stale := err == nil && cur.VsEnd().Sub(now) < RefreshAhead
	if !errors.Is(err, fs.ErrNotExist) && !stale {
		f, err = Load(path, now)
		return f, false, err
	}
	f = Generate(now, GenerateHours, DefaultStages, now.Unix())
	if err := f.Validate(now); err != nil {
		return nil, false, fmt.Errorf("generated an invalid rotation (a bug in Generate): %w", err)
	}
	if err := Write(path, f, now); err != nil {
		return f, true, fmt.Errorf("renewed rotation served from memory only: %w", err)
	}
	return f, true, nil
}

// rule picks one of the five versus rules. The numbering (0 turf war, then the four ranked
// rules) is the public SplatNet 3 ordering; see docs/provenance.md for what is and is not
// verified about these ids.
func rule(rng *rand.Rand) int32 { return int32(rng.Intn(5)) }

// pick chooses n distinct stage ids in 1..stages.
func pick(rng *rand.Rand, stages, n int) []int32 {
	seen := map[int]bool{}
	out := make([]int32, 0, n)
	for len(out) < n {
		v := rng.Intn(stages) + 1
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, int32(v))
	}
	return out
}
