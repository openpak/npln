package rotation

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func at() time.Time { return time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC) }

// good builds a rotation that is valid at at(): every kind current, sorted, non-overlapping, and
// still defined well past the present.
func good() *File {
	s := at().Add(-time.Hour)
	return &File{
		Version: Version,
		Vs: []VsWindow{
			{Window: Window{Start: s, End: s.Add(2 * time.Hour)},
				Regular: []int32{1, 2}, Bankara: []Mode{{Rule: 1, Stages: []int32{3, 4}}}},
			{Window: Window{Start: s.Add(2 * time.Hour), End: s.Add(8 * time.Hour)},
				Regular: []int32{5, 6}, Bankara: []Mode{{Rule: 2, Stages: []int32{7, 8}}}},
		},
		Coop:   []CoopWindow{{Window: Window{Start: s, End: s.Add(24 * time.Hour)}, Boss: "boss-1"}},
		Season: []Window{{Start: s.Add(-720 * time.Hour), End: s.Add(720 * time.Hour)}},
		League: []LeagueWindow{{Window: Window{Start: s, End: s.Add(8 * time.Hour)},
			Slots: []Window{{Start: s, End: s.Add(2 * time.Hour)}}}},
	}
}

func TestValidRotationPasses(t *testing.T) {
	if err := good().Validate(at()); err != nil {
		t.Fatalf("a good rotation was rejected: %v", err)
	}
}

// The failures below are the ones that matter to a console. A stale set is rejected by the game
// with a visible error; an inconsistent set is not rejected at all, it aborts the game. So the
// loader has to catch both, and say which entry is at fault.
func TestValidatorCatches(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*File)
		want   string
	}{
		{"a kind that is not current", func(f *File) {
			// Co-op moved into the future: the other three still cover now, so nothing about this
			// file looks wrong except that the game would abort on it.
			f.Coop[0].Start = at().Add(48 * time.Hour)
			f.Coop[0].End = at().Add(72 * time.Hour)
		}, "coop"},
		{"a missing kind", func(f *File) { f.Season = nil }, "season"},
		{"overlapping windows", func(f *File) {
			f.Vs[1].Start = f.Vs[0].End.Add(-time.Minute)
		}, "overlap"},
		{"a window that ends before it starts", func(f *File) {
			f.Vs[0].End = f.Vs[0].Start.Add(-time.Hour)
		}, "before it starts"},
		{"a missing timestamp", func(f *File) { f.Coop[0].End = time.Time{} }, "missing start or end"},
		{"a rotation about to run out", func(f *File) {
			f.Vs = f.Vs[:1]
			f.Vs[0].End = at().Add(30 * time.Minute)
		}, "at least"},
		{"the wrong format version", func(f *File) { f.Version = 99 }, "version"},
		{"an empty stage list", func(f *File) { f.Vs[0].Regular = nil }, "regular stages"},
		{"an out-of-order league slot", func(f *File) {
			f.League[0].Slots = append(f.League[0].Slots, Window{Start: at().Add(-5 * time.Hour), End: at()})
		}, "slots"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := good()
			c.mutate(f)
			err := f.Validate(at())
			if err == nil {
				t.Fatal("accepted a rotation that would break the console")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("message %q does not mention %q — an operator has to be able to find the entry", err, c.want)
			}
		})
	}
}

// Load must reject on disk too, and a typo'd key must not be silently ignored: that is how a
// rotation quietly loses a kind and starts aborting consoles.
func TestLoad(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.json")
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Comments are allowed so a generated file can explain itself.
	write(ok, "// a comment\n"+jsonOf(t, good()))
	if _, err := Load(ok, at()); err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}

	bad := filepath.Join(dir, "typo.json")
	write(bad, strings.Replace(jsonOf(t, good()), `"coop"`, `"co-op"`, 1))
	if _, err := Load(bad, at()); err == nil {
		t.Fatal("a misspelled key was accepted")
	}

	if _, err := Load(filepath.Join(dir, "nope.json"), at()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file must report as missing so the caller can warn instead of dying: %v", err)
	}
}

func jsonOf(t *testing.T, f *File) string {
	t.Helper()
	b, err := marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestKeep(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rotation.json")

	// Missing: written, current, and far enough ahead not to be renewed again right away.
	f, renewed, err := Keep(p, at())
	if err != nil || !renewed {
		t.Fatalf("missing file: renewed=%v err=%v", renewed, err)
	}
	if _, err := Load(p, at()); err != nil {
		t.Fatalf("the written file does not load: %v", err)
	}
	if f.VsEnd().Sub(at()) < RefreshAhead {
		t.Fatalf("a fresh rotation already needs renewing: ends %s", f.VsEnd())
	}

	// Current: served as is.
	if _, renewed, err := Keep(p, at().Add(24*time.Hour)); err != nil || renewed {
		t.Fatalf("current file: renewed=%v err=%v", renewed, err)
	}

	// Close to running out, and long after it ran out (a restart after a stop): renewed, not refused.
	for _, until := range []time.Duration{RefreshAhead - time.Hour, -24 * time.Hour} {
		later := f.VsEnd().Add(-until)
		g, renewed, err := Keep(p, later)
		if err != nil || !renewed {
			t.Fatalf("at %s: renewed=%v err=%v", later, renewed, err)
		}
		if err := g.Validate(later); err != nil {
			t.Fatalf("renewed rotation invalid at %s: %v", later, err)
		}
		f = g
	}

	// A present, current, broken file is still refused rather than overwritten.
	os.WriteFile(p, []byte(strings.Replace(jsonOf(t, good()), `"coop"`, `"co-op"`, 1)), 0o644)
	if f, _, err := Keep(p, at()); f != nil || err == nil {
		t.Fatal("a broken file was served")
	}
}
