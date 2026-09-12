// Package stardew is Stardew Valley on the shared NPLN host.
package stardew

import (
	"log"
	"net"
	"os"

	"google.golang.org/grpc/credentials"

	host "github.com/openpak/npln"
	"github.com/openpak/npln/titles/stardew-valley/npln"
)

// Title is this game's entry in the shared NPLN host (tenant port 21010, ports.md).
var Title = host.Title{
	Name:         "stardew-valley",
	Listen:       ":21010",
	HealthListen: ":21011",
	Run: func(creds credentials.TransportCredentials, lis net.Listener) error {
		srv := npln.NewServer(creds, npln.StardewTenant)
		// A host creates a session and the server hands every joiner an
		// address to dial. The console dials THAT directly -- not through
		// Traefik, which only knows SNI -- so it has to be a port of its own,
		// published on the host. Without one the session was advertised at the
		// relay default, 127.0.0.1, and no peer could ever reach the farm
		// [runtime, 2026-09-12: a session was created and tracked SUCCEEDED,
		// and the game still could not join it]. Splatoon 3 has had this since
		// it was written; Stardew never did. 22200 is its block in ports.md.
		if addr := env("NPLN_SESSION_LISTEN", ":22200"); addr != "" && addr != lis.Addr().String() {
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

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
