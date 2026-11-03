package blob

import (
	"os"
	"testing"
)

// Needs a scratch database: NPLN_TEST_DSN=postgres://... go test ./internal/blob
func TestSurvivesReopen(t *testing.T) {
	dsn := os.Getenv("NPLN_TEST_DSN")
	if dsn == "" {
		t.Skip("NPLN_TEST_DSN not set")
	}
	a, err := Open(t.Context(), dsn, "t-blob")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Put("k", "1", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if ins, _ := a.Import("k", "1", []byte("other")); ins {
		t.Fatal("import must not overwrite")
	}
	b, _ := Open(t.Context(), dsn, "t-blob")
	defer b.Close()
	got, ok, err := b.Get("k", "1")
	if err != nil || !ok || string(got) != "one" {
		t.Fatalf("reopened: %q %v %v", got, ok, err)
	}
	if _, ok, _ := b.Get("k", "2"); ok {
		t.Fatal("phantom key")
	}
}
