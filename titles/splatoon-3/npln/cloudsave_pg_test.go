package npln

import (
	"os"
	"testing"

	"github.com/openpak/npln/internal/blob"
	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
)

// Needs a scratch database: NPLN_TEST_DSN=postgres://... go test ./titles/splatoon-3/npln
func TestSaveStorePostgresImportsAndSurvivesReopen(t *testing.T) {
	dsn := os.Getenv("NPLN_TEST_DSN")
	if dsn == "" {
		t.Skip("NPLN_TEST_DSN not set")
	}
	dir := t.TempDir()
	old := &saveStore{dir: dir}
	if err := old.store("u-old", &toyohrpb.SaveRecord{}); err != nil { // an existing file save
		t.Fatal(err)
	}
	db, err := blob.Open(t.Context(), dsn, "t-splatoon")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &saveStore{dir: dir, db: db}
	rec, err := a.load("u-old") // imports the file into Postgres
	if err != nil || rec == nil {
		t.Fatalf("import: %v %v", rec, err)
	}
	if err := a.store("u-new", &toyohrpb.SaveRecord{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir + "/u-new.pb"); err == nil {
		t.Fatal("new save went to a file")
	}
	os.Remove(dir + "/u-old.pb") // prove the read now comes from Postgres
	b := &saveStore{dir: dir, db: db}
	for _, uid := range []string{"u-old", "u-new"} {
		if r, err := b.load(uid); err != nil || r == nil {
			t.Fatalf("%s after reopen: %v %v", uid, r, err)
		}
	}
}
