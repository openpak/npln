package npln

import (
	"os"
	"path/filepath"
	"testing"

	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
)

func TestEraseUsers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NPLN_JWT_KEY", filepath.Join(dir, "k.key"))
	t.Setenv("NPLN_ROTATION", filepath.Join(dir, "rotation.json"))
	a, b := "u-aaaaaaaaaaaaaaaaaaaa", "u-bbbbbbbbbbbbbbbbbbbb"

	oldSaves, oldFests := saves, Fests
	defer func() { saves, Fests = oldSaves, oldFests }()
	saves = &saveStore{dir: filepath.Join(dir, "saves")}
	Fests = &festStore{}
	for _, u := range []string{a, b} {
		if err := saves.store(u, &toyohrpb.SaveRecord{}); err != nil {
			t.Fatal(err)
		}
		if _, err := Fests.enter("F1", u, "alpha", "us"); err != nil {
			t.Fatal(err)
		}
	}
	ug := newUGCStore(filepath.Join(dir, "ugc.pb"))
	ug.docs["services/Hammer/users/"+a+"/UserAttribute/Data"] = &ugcpb.Document{Name: "x"}
	ug.docs["services/Hammer/users/"+b+"/UserAttribute/Data"] = &ugcpb.Document{Name: "y"}
	ugcStores.Lock()
	ugcStores.list = append(ugcStores.list, ug)
	ugcStores.Unlock()
	goOnline(a)
	goOnline(b)

	n, err := eraseUsers(t.Context(), []string{a})
	if err != nil || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "saves", a+".pb")); err == nil {
		t.Fatal("A's save file survived")
	}
	if _, err := os.Stat(filepath.Join(dir, "saves", b+".pb")); err != nil {
		t.Fatal("B's save file gone")
	}
	if _, ok := Fests.entry("F1", a); ok {
		t.Fatal("A's fest entry survived")
	}
	if _, ok := Fests.entry("F1", b); !ok {
		t.Fatal("B's fest entry gone")
	}
	// The entries file on disk no longer holds A.
	fresh := &festStore{}
	fresh.init()
	if _, ok := fresh.ent["F1"][a]; ok || len(fresh.ent["F1"]) != 1 {
		t.Fatalf("persisted entries: %v", fresh.ent)
	}
	if len(ug.docs) != 1 {
		t.Fatalf("docs: %d", len(ug.docs))
	}
	if st, _ := stateOf(a); st == 1 {
		t.Fatal("A still online")
	}
	if n, err := eraseUsers(t.Context(), []string{a}); err != nil || n != 0 {
		t.Fatalf("second run n=%d err=%v", n, err)
	}
}
