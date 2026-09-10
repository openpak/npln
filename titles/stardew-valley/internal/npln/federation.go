package npln

// federation — which region node a player's STUN and TURN come from.
//
// Matchmaking and the gamesync session documents stay here: they are control plane, low
// volume, and share this process's room state. The game itself is Pia peer-to-peer, and when
// NAT defeats that it goes through TURN, which is the one hop worth putting in the player's own
// country. Regions come from openpak.org's /api/v1/regions (OPENPAK_REGIONS_URL); a player's
// country from the adapter's identity reply at auth.

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Region is one approved node as /api/v1/regions lists it.
type Region struct {
	ID       string            `json:"id"`
	Country  string            `json:"country"`
	Services map[string]string `json:"services"`
}

var (
	regions      atomic.Pointer[[]Region]
	countryByUID sync.Map // uid -> "AU"; filled at auth, read at room creation
	regionsOnce  sync.Once
)

// startRegions polls the region list once a minute. Failures keep the last good list.
func startRegions() {
	regionsOnce.Do(func() {
		url := envOr("OPENPAK_REGIONS_URL", "")
		if url == "" {
			return
		}
		go func() {
			for {
				refreshRegions(url)
				time.Sleep(time.Minute)
			}
		}()
	})
}

func refreshRegions(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := httpc.Do(req)
	if err != nil {
		log.Printf("[Federation] regions: %v", err)
		return
	}
	defer resp.Body.Close()
	var out struct {
		Regions []Region `json:"regions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != 200 {
		log.Printf("[Federation] regions: HTTP %d err=%v", resp.StatusCode, err)
		return
	}
	for i := range out.Regions {
		out.Regions[i].Country = strings.ToUpper(out.Regions[i].Country)
	}
	regions.Store(&out.Regions)
}

// regionFor is the node serving a country with the named service, or nil for home.
// ponytail: first match; nearest-by-ping when someone in Perth complains about Sydney.
func regionFor(country, service string) *Region {
	rs := regions.Load()
	if rs == nil || country == "" {
		return nil
	}
	country = strings.ToUpper(country)
	for i := range *rs {
		r := &(*rs)[i]
		if r.Country == country && r.Services[service] != "" {
			return r
		}
	}
	return nil
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

// iceFor is the STUN and TURN a player uses: their own country's when a node runs them, so the
// NAT probe is a short hop, else this deployment's.
func iceFor(uid string) (stunHost string, stunPort int32, turnHost string, turnPort int32) {
	stunHost, stunPort = envOr("NPLN_STUN_HOST", "127.0.0.1"), envInt("NPLN_STUN_PORT", 3478)
	turnHost, turnPort = envOr("NPLN_TURN_HOST", ""), envInt("NPLN_TURN_PORT", 3478)
	c, _ := countryByUID.Load(uid)
	country, _ := c.(string)
	if r := regionFor(country, "turn"); r != nil {
		turnHost, turnPort = hostPort(r.Services["turn"], 3478)
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
