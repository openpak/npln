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
