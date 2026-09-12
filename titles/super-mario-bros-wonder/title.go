// Package wonder is Super Mario Bros. Wonder (010015100B514000) on the shared NPLN host.
//
// Wonder is an NPLN title. Nothing of its traffic has been observed by OpenPak yet, so this
// title reuses Stardew Valley's verified NPLN service set (auth, friends, game sessions,
// gamesync) under Wonder's own tenant. What is known and what is not is in docs/ and the
// catalog; the one fact that still gates a console is outside this server: a client-side
// patch for the pinned certificate and two peer name checks that v1.2.1 carries.
package wonder

import (
	"errors"
	"log"
	"net"
	"os"
	"regexp"

	"google.golang.org/grpc/credentials"

	host "github.com/openpak/npln"
	"github.com/openpak/npln/titles/stardew-valley/npln"
)

// TitleID is the Switch application id.
const TitleID = "010015100B514000"

var tenantShape = regexp.MustCompile(`^tenants/t-[0-9a-f]{8}-lp1$`)

// Title is this game's entry in the shared NPLN host (tenant port 21014, ports.md).
var Title = host.Title{
	Name:         "super-mario-bros-wonder",
	Listen:       ":21014",
	HealthListen: ":21015",
	Run: func(creds credentials.TransportCredentials, lis net.Listener) error {
		tenant, err := Tenant()
		if err != nil {
			return err
		}
		srv := npln.NewServer(creds, tenant)
		// The address a host's session is advertised at, dialled directly by
		// every joiner rather than through Traefik, which only knows SNI.
		// Without it a session is advertised at the relay default, 127.0.0.1,
		// and nobody can join. 22220 is Wonder's block in ports.md.
		if addr := env("NPLN_SESSION_LISTEN", ":22220"); addr != "" && addr != lis.Addr().String() {
			if sl, err := net.Listen("tcp", addr); err != nil {
				log.Printf("session endpoint: cannot listen on %s: %v", addr, err)
			} else {
				log.Printf("session/gamesync endpoint on %s (gRPC/TLS)", addr)
				go func() { log.Printf("session endpoint: %v", srv.Serve(sl)) }()
			}
		}
		return srv.Serve(lis)
	},
}

// DefaultTenant is Wonder's NPLN tenant: Kinnay's NintendoClients wiki, "NPLN Servers",
// lists Super Mario Bros. Wonder as ba973ec6 (read 2026-09-11), and the same table gives
// Splatoon 3 the dce9377b this project had already observed, which is the cross-check.
// The console dials t-ba973ec6-lp1.lp1.t.npln.srv.nintendo.net.
const DefaultTenant = "tenants/t-ba973ec6-lp1"

// Tenant is the tenant to serve: NPLN_TENANT when set (a capture that disagrees with the
// public list wins), else DefaultTenant. A malformed value is refused: the client rejects
// every resource name under a wrong tenant, so serving one is worse than not starting.
func Tenant() (string, error) {
	t := os.Getenv("NPLN_TENANT")
	if t == "" {
		return DefaultTenant, nil
	}
	if !tenantShape.MatchString(t) {
		return "", errors.New("NPLN_TENANT must look like tenants/t-XXXXXXXX-lp1, got " + t)
	}
	return t, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
