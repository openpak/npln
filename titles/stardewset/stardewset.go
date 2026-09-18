// Package stardewset hosts a title on Stardew Valley's verified NPLN service set (auth,
// friends, game sessions, gamesync) under the title's own tenant. It is how a title that
// OpenPak has not yet observed gets on the air: its first boot's UNIMPLEMENTED log is
// the list of what it needs beyond Stardew.
package stardewset

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

var tenantShape = regexp.MustCompile(`^tenants/t-[0-9a-f]{8}-lp1$`)

// Title builds a title's host entry. session is the gamesync/session address every
// joiner dials directly (not through Traefik, which only knows SNI); without it a
// session is advertised at the relay default, 127.0.0.1, and nobody can join.
// defaultTenant may be empty when no source gives the tenant yet: the title then
// refuses to start until NPLN_TENANT is set.
func Title(name, listen, healthListen, session, defaultTenant string) host.Title {
	return host.Title{
		Name:         name,
		Listen:       listen,
		HealthListen: healthListen,
		Run: func(creds credentials.TransportCredentials, lis net.Listener) error {
			tenant, err := Tenant(defaultTenant)
			if err != nil {
				return err
			}
			srv := npln.NewServer(creds, tenant)
			if addr := env("NPLN_SESSION_LISTEN", session); addr != "" && addr != lis.Addr().String() {
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
}

// Tenant is the tenant to serve: NPLN_TENANT when set (a capture that disagrees with a
// public list wins), else def. A malformed value is refused: the client rejects every
// resource name under a wrong tenant, so serving one is worse than not starting.
func Tenant(def string) (string, error) {
	t := os.Getenv("NPLN_TENANT")
	if t == "" {
		if def == "" {
			return "", errors.New("no known tenant for this title: set NPLN_TENANT=tenants/t-XXXXXXXX-lp1 from the host name its console dials (t-XXXXXXXX-lp1.lp1.t.npln.srv.nintendo.net)")
		}
		return def, nil
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
