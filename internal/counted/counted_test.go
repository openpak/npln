package counted

import "testing"

func TestFileStoreSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	a, _ := Open(t.Context(), "", dir)
	if err := a.Mark("m1", "g/u1"); err != nil {
		t.Fatal(err)
	}
	_ = a.Mark("m2", "g/u2")
	b, _ := Open(t.Context(), "", dir)
	got, err := b.Load("m1")
	if err != nil || !got["g/u1"] || got["g/u2"] || len(got) != 1 {
		t.Fatalf("reopened store: %v %v", got, err)
	}
}

func TestFileForget(t *testing.T) {
	dir := t.TempDir()
	a, _ := Open(t.Context(), "", dir)
	_ = a.Mark("m1", "g1/u-a")
	_ = a.Mark("m1", "g1/u-ab")
	_ = a.Mark("m2", "g2/u-a")
	n, err := a.Forget([]string{"u-a"})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, _ := a.Forget([]string{"u-a"}); n != 0 {
		t.Fatalf("second run %d", n)
	}
	got, _ := a.Load("m1")
	if !got["g1/u-ab"] || len(got) != 1 {
		t.Fatalf("survivors %v", got)
	}
}
