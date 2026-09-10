// Package splatoon3 is Splatoon 3 on the shared NPLN host.
package splatoon3

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc/credentials"

	host "github.com/openpak/npln"
	"github.com/openpak/npln/titles/splatoon-3/npln"
	"github.com/openpak/npln/titles/splatoon-3/rotation"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// Title is this game's entry in the shared NPLN host (tenant port 21012, ports.md).
var Title = host.Title{
	Name:         "splatoon-3",
	Listen:       ":21012",
	HealthListen: ":21013",
	Run:          run,
}

func run(creds credentials.TransportCredentials, lis net.Listener) error {
	rotPath := env("NPLN_ROTATION", "rotation.json")
	rot, err := rotation.Load(rotPath, time.Now().UTC())
	switch {
	case err == nil:
		log.Printf("rotation %s loaded: %d vs, %d coop, %d season, %d league",
			rotPath, len(rot.Vs), len(rot.Coop), len(rot.Season), len(rot.League))
	case errors.Is(err, fs.ErrNotExist):
		log.Printf("no rotation at %s — schedules will not be served. Write one: go run ./titles/splatoon-3/cmd/genrotation -o %s", rotPath, rotPath)
	default:
		return fmt.Errorf("%w\n\nRefusing to start: serving this rotation risks aborting the console. Regenerate it: go run ./titles/splatoon-3/cmd/genrotation -o %s", err, rotPath)
	}
	srv := npln.NewServer(creds, rot)
	// The console dials the session/gamesync endpoint directly (22210), not through Traefik.
	if sessionAddr := env("NPLN_SESSION_LISTEN", ":22210"); sessionAddr != "" && sessionAddr != lis.Addr().String() {
		if sl, err := net.Listen("tcp", sessionAddr); err != nil {
			log.Printf("session endpoint: cannot listen on %s: %v", sessionAddr, err)
		} else {
			log.Printf("session/gamesync endpoint on %s (gRPC/TLS)", sessionAddr)
			go func() { log.Printf("session endpoint: %v", srv.Serve(sl)) }()
		}
	}
	return srv.Serve(lis)
}
