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
