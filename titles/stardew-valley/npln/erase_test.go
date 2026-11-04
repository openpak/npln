package npln

import (
	"path/filepath"
	"testing"


	"github.com/openpak/npln/internal/counted"
	hydropb "github.com/openpak/npln/proto/hydro/v1"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
)


func TestEraseUsers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NPLN_STATE_DIR", dir)
	t.Setenv("NPLN_DATABASE_URL", "")
	a, b := userID("aaaaaaaaaaaa0001"), userID("bbbbbbbbbbbb0002")

	rememberPlayerIdentity(a, "aaaaaaaaaaaa0001", 1)
	rememberPlayerIdentity(b, "bbbbbbbbbbbb0002", 2)
	rememberAppVersion(a, "00010002")
	goOnline(a)
	goOnline(b)

	rooms := newSessionServer()
	rooms.members["g1"] = []*mmpb.UserSession{{User: "tenants/t/users/" + a}, {User: "tenants/t/users/" + b}}
	trackRooms(rooms)

	msg := trackedMessaging(rooms)
	msg.boxes[a] = &mailbox{items: []*queued{{from: b}}}
	msg.boxes[b] = &mailbox{items: []*queued{{from: a}, {from: b}}}

	mm := trackedMatchmaker(rooms)
	mm.tickets["t1"] = &mmTicket{owner: a}
	mm.tickets["t2"] = &mmTicket{owner: b}

	h := trackedHydro(newHydro())
	h.contents["c1"] = &hydropb.Content{Name: "c1", User: "tenants/t/users/" + a}
	h.contents["c2"] = &hydropb.Content{Name: "c2", User: "tenants/t/users/" + b}

	ug := trackedUGC(newUGCStore(filepath.Join(dir, "ugc.json")))
	ug.docs["p1"] = &ugcEntry{owner: a, doc: &ugcpb.Document{Name: "p1"}}
	ug.docs["p2"] = &ugcEntry{owner: b, doc: &ugcpb.Document{Name: "p2"}}
	ug.aliases["AAA"] = &ugcAlias{Code: "AAA", Doc: "p1", Owner: a}
	ug.aliases["BBB"] = &ugcAlias{Code: "BBB", Doc: "p2", Owner: b}

	st := openCounted()
	_ = st.Mark("m", "g1/"+a)
	_ = st.Mark("m", "g1/"+b)
	st.Close()

	n, err := eraseUsers(t.Context(), []string{a})
	if err != nil || n == 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if baasOf(b) != "bbbbbbbbbbbb0002" || baasOf(a) != "" {
		t.Fatal("player identity map")
	}
	if appVersionOf(a) != "" {
		t.Fatal("app version")
	}
	if s, _ := stateOf(a); s == 1 {
		t.Fatal("A still online")
	}
	if len(rooms.members["g1"]) != 1 {
		t.Fatal("member not removed")
	}
	if msg.boxes[a] != nil || len(msg.boxes[b].items) != 1 || msg.boxes[b].items[0].from != b {
		t.Fatal("mailboxes")
	}
	if len(mm.tickets) != 1 || mm.tickets["t2"] == nil {
		t.Fatal("tickets")
	}
	if len(h.contents) != 1 || h.contents["c2"] == nil {
		t.Fatal("hydro")
	}
	if len(ug.docs) != 1 || ug.docs["p2"] == nil || len(ug.aliases) != 1 || ug.aliases["BBB"] == nil {
		t.Fatal("ugc")
	}
	cs, _ := counted.Open(t.Context(), "", dir)
	got, _ := cs.Load("m")
	if len(got) != 1 || !got["g1/"+b] {
		t.Fatalf("counted: %v", got)
	}
	if n, err := eraseUsers(t.Context(), []string{a}); err != nil || n != 0 {
		t.Fatalf("second run n=%d err=%v", n, err)
	}
}
