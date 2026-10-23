package npln

// federation — which region node a session's STUN and TURN come from.
//
// Matchmaking and the gamesync session documents stay here: they are control plane, low
// volume, and share this process's room state. The game itself is Pia peer-to-peer, and when
// NAT defeats that it goes through TURN, the one hop worth putting near the players. Every
// member of a session gets the TURN of the session HOST's region node: joiners go to the
// room's node, never their own region's (federation-worldwide-plan.md, "NPLN + Pia").
//
// The decision is the shared resolver's (internal/routing), fed by the account core's routing
// table (OPENPAK_ROUTING_URL / OPENPAK_ROUTING_KEY). A player's country comes from the
// adapter's identity reply at auth; their test-node allow-list status from the core. With
// OPENPAK_ROUTING_URL unset, or no node in the host's region chain, everyone gets this
// deployment's own STUN/TURN, exactly as before federation.

import (
	"context"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openpak/npln/internal/routing"
	"github.com/openpak/npln/redact"
)

// player is what routing knows about an authenticated player.
type player struct {
	baas    string // BAAS user id: the core's "switch" subject
	country string // ISO 3166-1 alpha-2, "" = unknown
	tester  bool   // on the test-node allow-list
}

// roomHost is the routing input recorded on a session when it is created: the host's.
type roomHost struct {
	uid     string
	country string
	tester  bool
}

var (
	routes      *routing.Client
	playerByUID sync.Map // uid -> player; filled at auth, read at session creation
	routesOnce  sync.Once
)

// startRouting reads the routing env once and warms the table. Unset URL = federation off.
func startRouting() {
	routesOnce.Do(func() {
		routes = &routing.Client{BaseURL: os.Getenv("OPENPAK_ROUTING_URL"), Key: os.Getenv("OPENPAK_ROUTING_KEY")}
		if routes.BaseURL == "" {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := routes.Refresh(ctx); err != nil {
				log.Printf("[Federation] routing table: %s", redact.Err(err))
			}
		}()
	})
}

// RememberPlayer records an authenticated player's country (from the adapter identity) and,
// when federation is on, asks the core in the background whether they are a tester. Titles with
// their own auth (Splatoon 3) call it too, so their sessions route like everyone else's.
func RememberPlayer(baas, country string) {
	if baas == "" {
		return
	}
	uid := userID(baas)
	p := player{baas: baas, country: strings.ToUpper(strings.TrimSpace(country))}
	if old, ok := playerByUID.Load(uid); ok {
		p.tester = old.(player).tester // keep what the core said until it answers again
	}
	playerByUID.Store(uid, p)
	if routes == nil || routes.BaseURL == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a := routes.Account(ctx, routing.AccountRef{Namespace: "switch", Subject: baas})
		q := player{baas: baas, country: p.country, tester: a.Tester}
		if q.country == "" {
			q.country = a.Country
		}
		playerByUID.Store(uid, q)
	}()
}

// playerOf is what is known about uid; the zero player (home) when nothing is.
func playerOf(uid string) player {
	v, _ := playerByUID.Load(uid)
	p, _ := v.(player)
	return p
}

// hostOf is the routing input to record for a session uid creates.
func hostOf(uid string) roomHost {
	p := playerOf(uid)
	return roomHost{uid: uid, country: p.country, tester: p.tester}
}

// routingTable is the current table, nil (all home) when federation is off.
func routingTable() *routing.Table {
	if routes == nil {
		return nil
	}
	return routes.Table()
}

// hostPort splits "host:port"; a missing or bad port yields def.
func hostPort(s string, def int32) (string, int32) {
	h, p, err := net.SplitHostPort(s)
	if err != nil {
		return s, def
	}
	if n, err := strconv.Atoi(p); err == nil {
		return h, int32(n)
	}
	return h, def
}

// iceFor is the STUN and TURN for a member of session gsid hosted by h: the host's region
// node's when one runs TURN (its STUN too, else the TURN address answers STUN), else this
// deployment's.
func iceFor(t *routing.Table, gsid string, h roomHost) (stunHost string, stunPort int32, turnHost string, turnPort int32) {
	stunHost, stunPort = envOr("NPLN_STUN_HOST", "127.0.0.1"), envInt("NPLN_STUN_PORT", 3478)
	turnHost, turnPort = envOr("NPLN_TURN_HOST", ""), envInt("NPLN_TURN_PORT", 3478)
	r := t.Resolve(routing.Request{Stack: routing.StackNPLN, Country: h.country, Tester: h.tester, Service: "turn", Key: gsid})
	if !r.IsHome() && r.Address != "" {
		turnHost, turnPort = hostPort(r.Address, 3478)
		if s := r.Services["stun"]; s != "" {
			stunHost, stunPort = hostPort(s, 3478)
		} else {
			stunHost, stunPort = turnHost, turnPort
		}
	}
	if turnHost == "" {
		turnHost, turnPort = stunHost, stunPort
	}
	return
}

// latencyNode is the STUN address of a node in the region a GCP region name maps to, or ""
// when that region has none this caller may use (or it is home's own region).
func latencyNode(t *routing.Table, gcpRegion string, tester bool) string {
	if t == nil {
		return ""
	}
	home := t.Home
	if home == "" {
		home = routing.HomeRegion
	}
	for _, code := range t.RegionsForToken(routing.StackGCP, gcpRegion) {
		if code == home {
			continue
		}
		if nodes := t.Eligible(code, "stun", tester); len(nodes) > 0 {
			return nodes[0].Services["stun"]
		}
	}
	return ""
}
