package npln

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/openpak/npln/internal/connectivity"
	"github.com/openpak/npln/internal/routing"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
)

// useTable serves t as the account core's routing table and points routes at it; nil t is
// federation off (OPENPAK_ROUTING_URL unset).
func useTable(tb testing.TB, t *routing.Table) {
	tb.Helper()
	old := routes
	tb.Cleanup(func() { routes = old })
	if t == nil {
		routes = &routing.Client{}
		return
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != routing.PathTable || r.Header.Get(routing.KeyHeader) != "k" {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(t)
	}))
	tb.Cleanup(srv.Close)
	routes = &routing.Client{BaseURL: srv.URL, Key: "k", TTL: time.Hour}
	if err := routes.Refresh(context.Background()); err != nil {
		tb.Fatal(err)
	}
}

func setPlayer(tb testing.TB, uid, country string, tester bool) {
	tb.Helper()
	playerByUID.Store(uid, player{country: country, tester: tester})
	tb.Cleanup(func() { playerByUID.Delete(uid) })
}

var testNodes = []routing.Node{
	{ID: "br-gru-1", Region: "sa-east", Services: map[string]string{"turn": "198.51.100.1:3478", "stun": "198.51.100.1:3479"}},
	{ID: "us-nyc-1", Region: "us-east", Services: map[string]string{"stun": "198.51.100.2:3478"}},
	{ID: "jp-tyo-1", Region: "ap-northeast", Services: map[string]string{"stun": "198.51.100.3:3478"}},
	{ID: "au-syd-t", Region: "oc", Test: true, Services: map[string]string{"turn": "198.51.100.4:3478", "stun": "198.51.100.4:3478"}},
}

// hostAndJoin has host create a session and joiner join it, both with bearers.
func hostAndJoin(t *testing.T, g *sessionServer, host, joiner string) string {
	t.Helper()
	if _, err := g.CreateGameSessionCreationTicket(bearerCtx(1, host), &mmpb.CreateGameSessionCreationTicketRequest{}); err != nil {
		t.Fatal(err)
	}
	var gsName string
	for gsid, mem := range g.members {
		if len(mem) > 0 && lastSeg(mem[0].GetUser()) == host {
			gsName = Tenant + "/gameSessions/" + gsid
		}
	}
	if _, err := g.JoinGameSession(bearerCtx(2, joiner), &mmpb.JoinGameSessionRequest{Name: gsName}); err != nil {
		t.Fatal(err)
	}
	return gsName
}

func ice(t *testing.T, g *sessionServer, pid uint64, uid string) (stun, turn string) {
	t.Helper()
	set, err := g.AllocateIceServerSet(bearerCtx(pid, uid), &mmpb.AllocateIceServerSetRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return set.GetStunServer().GetHost(), set.GetTurnServers()[0].GetHost()
}

// Every member of a session gets the host's region node, whatever their own country.
func TestIceFollowsSessionHost(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NPLN_STUN_HOST", "stun.home.example")
	t.Setenv("NPLN_TURN_SECRET", "s")
	useTable(t, routing.NewTable(testNodes))

	t.Run("BR host, DE joiner: BR node", func(t *testing.T) {
		g := newSessionServer()
		setPlayer(t, "u-br", "BR", false)
		setPlayer(t, "u-de", "DE", false)
		hostAndJoin(t, g, "u-br", "u-de")
		for i, uid := range []string{"u-br", "u-de"} {
			if s, tu := ice(t, g, uint64(i+1), uid); s != "198.51.100.1" || tu != "198.51.100.1" {
				t.Fatalf("%s: stun=%s turn=%s, want BR node", uid, s, tu)
			}
		}
	})
	t.Run("DE host, BR joiner: home", func(t *testing.T) {
		g := newSessionServer()
		setPlayer(t, "u-de2", "DE", false)
		setPlayer(t, "u-br2", "BR", false)
		hostAndJoin(t, g, "u-de2", "u-br2")
		if s, tu := ice(t, g, 2, "u-br2"); s != "stun.home.example" || tu != "stun.home.example" {
			t.Fatalf("joiner got its own region: stun=%s turn=%s", s, tu)
		}
	})
	t.Run("host region without a TURN node: home", func(t *testing.T) {
		g := newSessionServer()
		setPlayer(t, "u-jp", "JP", false) // jp-tyo-1 runs STUN only
		setPlayer(t, "u-br3", "BR", false)
		hostAndJoin(t, g, "u-jp", "u-br3")
		for i, uid := range []string{"u-jp", "u-br3"} {
			if s, tu := ice(t, g, uint64(i+1), uid); s != "stun.home.example" || tu != "stun.home.example" {
				t.Fatalf("%s: stun=%s turn=%s, want home", uid, s, tu)
			}
		}
	})
	t.Run("test node only through an allow-listed host", func(t *testing.T) {
		g := newSessionServer()
		setPlayer(t, "u-au", "AU", false)
		setPlayer(t, "u-nz", "NZ", false)
		hostAndJoin(t, g, "u-au", "u-nz")
		if s, _ := ice(t, g, 1, "u-au"); s != "stun.home.example" {
			t.Fatalf("non-tester host reached the test node: %s", s)
		}
		g = newSessionServer()
		setPlayer(t, "u-au-t", "AU", true)
		setPlayer(t, "u-de-j", "DE", false)
		hostAndJoin(t, g, "u-au-t", "u-de-j")
		for i, uid := range []string{"u-au-t", "u-de-j"} {
			if s, tu := ice(t, g, uint64(i+1), uid); s != "198.51.100.4" || tu != "198.51.100.4" {
				t.Fatalf("%s in a tester's session: stun=%s turn=%s, want the test node", uid, s, tu)
			}
		}
	})
	t.Run("no session: the caller's own region", func(t *testing.T) {
		g := newSessionServer()
		setPlayer(t, "u-br4", "BR", false)
		if s, _ := ice(t, g, 1, "u-br4"); s != "198.51.100.1" {
			t.Fatalf("lone BR caller: %s", s)
		}
	})
}

// The connectivity service is told the host's country, not the caller's.
func TestConnectivityGetsHostCountry(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	useTable(t, nil)
	var countries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/participants":
			json.NewEncoder(w).Encode(map[string]string{"id": "p"})
		case "/v1/ice":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			countries = append(countries, req["country"])
			json.NewEncoder(w).Encode(connectivity.ICE{STUN: "203.0.113.10:3478", TURN: "203.0.113.10:3478", Username: "u", Password: "p", ExpiresAt: time.Now().Add(30 * time.Minute)})
		}
	}))
	defer server.Close()
	configureConnectivity(t, server.URL)
	g := newSessionServer()
	setPlayer(t, "u-br5", "BR", false)
	setPlayer(t, "u-de5", "DE", false)
	hostAndJoin(t, g, "u-br5", "u-de5")
	ice(t, g, 1, "u-br5")
	ice(t, g, 2, "u-de5")
	if len(countries) != 2 || countries[0] != "BR" || countries[1] != "BR" {
		t.Fatalf("connectivity countries %v, want [BR BR]", countries)
	}
}

func splatoonLike(tenant string) []*mmpb.LatencyMeasurementServer {
	var out []*mmpb.LatencyMeasurementServer
	for _, r := range []string{"europe-central2", "europe-west2", "northamerica-northeast1", "us-west2", "asia-northeast1", "asia-southeast1", "australia-southeast1"} {
		out = append(out, &mmpb.LatencyMeasurementServer{Name: tenant + "/latencyMeasurementServers/" + r, Region: r})
	}
	return out
}

func latencyHosts(t *testing.T, ctx context.Context) []string {
	t.Helper()
	resp, err := newSessionServer().ListLatencyMeasurementServers(ctx, &mmpb.ListLatencyMeasurementServersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range resp.GetLatencyMeasurementServers() {
		out = append(out, s.GetHost())
	}
	return out
}

// Each candidate whose GCP region has a STUN node measures that node; the rest answer from home.
func TestLatencyCandidatesPerRegion(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NPLN_STUN_HOST", "stun.home.example")
	old := titleOptions
	t.Cleanup(func() { titleOptions = old })
	titleOptions = Options{LatencyServers: splatoonLike}
	useTable(t, routing.NewTable(testNodes))
	h := "stun.home.example"
	want := []string{h, h, "198.51.100.2", h, "198.51.100.3", h, h}
	if got := latencyHosts(t, context.Background()); !equal(got, want) {
		t.Fatalf("anonymous: %v, want %v", got, want)
	}
	setPlayer(t, "u-tester", "DE", true)
	want[6] = "198.51.100.4"
	if got := latencyHosts(t, bearerCtx(1, "u-tester")); !equal(got, want) {
		t.Fatalf("tester: %v, want %v", got, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Federation off, or a table with no nodes: exactly the ICE sets and latency list of before.
func TestHomeOnlyUnchanged(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NPLN_STUN_HOST", "192.0.2.10")
	t.Setenv("NPLN_STUN_PORT", "3478")
	t.Setenv("NPLN_TURN_HOST", "192.0.2.11")
	t.Setenv("NPLN_TURN_PORT", "3479")
	t.Setenv("NPLN_TURN_SECRET", "s")
	old := titleOptions
	t.Cleanup(func() { titleOptions = old })
	titleOptions = Options{LatencyServers: splatoonLike}

	type answer struct {
		ice     []*mmpb.IceServerSet
		latency *mmpb.ListLatencyMeasurementServersResponse
	}
	run := func(t *testing.T) answer {
		setPlayer(t, "u-br-h", "BR", true)
		setPlayer(t, "u-de-h", "DE", false)
		g := newSessionServer()
		hostAndJoin(t, g, "u-br-h", "u-de-h")
		var a answer
		for i, uid := range []string{"u-br-h", "u-de-h", "u-lone"} {
			set, err := g.AllocateIceServerSet(bearerCtx(uint64(i+1), uid), &mmpb.AllocateIceServerSetRequest{})
			if err != nil {
				t.Fatal(err)
			}
			set.UpdateTime = nil
			a.ice = append(a.ice, set)
		}
		var err error
		a.latency, err = g.ListLatencyMeasurementServers(bearerCtx(1, "u-br-h"), &mmpb.ListLatencyMeasurementServersRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	check := func(t *testing.T, a answer) {
		for _, set := range a.ice {
			if s, tu := set.GetStunServer(), set.GetTurnServers()[0]; s.GetHost() != "192.0.2.10" || s.GetPort() != 3478 || tu.GetHost() != "192.0.2.11" || tu.GetPort() != 3479 {
				t.Fatalf("ICE set changed: %v", set)
			}
		}
		for _, s := range a.latency.GetLatencyMeasurementServers() {
			if s.GetHost() != "192.0.2.10" || s.GetPort() != 3478 {
				t.Fatalf("latency candidate changed: %v", s)
			}
		}
	}
	useTable(t, nil)
	off := run(t)
	check(t, off)
	useTable(t, routing.NewTable(nil))
	empty := run(t)
	check(t, empty)
	for i := range off.ice {
		// The credentials embed the expiry second; equal whenever both runs fall in one second.
		off.ice[i].TurnServers[0].Username, off.ice[i].TurnServers[0].Password = "", ""
		empty.ice[i].TurnServers[0].Username, empty.ice[i].TurnServers[0].Password = "", ""
		if !proto.Equal(off.ice[i], empty.ice[i]) {
			t.Fatalf("zero nodes differs from federation off:\n%v\n%v", off.ice[i], empty.ice[i])
		}
	}
	if !proto.Equal(off.latency, empty.latency) {
		t.Fatal("latency list differs between federation off and zero nodes")
	}
}
