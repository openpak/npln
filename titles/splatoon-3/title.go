// Package splatoon3 is Splatoon 3 on the shared NPLN host.
package splatoon3

import (
	"fmt"
	"log"
	"net"
	"os"
	"sync/atomic"
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
	rot, err := keepRotation(rotPath)
	if rot == nil {
		return fmt.Errorf("%w\n\nRefusing to start: serving this rotation risks aborting the console. Delete it and the server writes a fresh one", err)
	}
	log.Printf("rotation %s: %d vs, %d coop, %d season, %d league, runs to %s",
		rotPath, len(rot.Vs), len(rot.Coop), len(rot.Season), len(rot.League), rot.VsEnd().Format(time.RFC3339))
	var cur atomic.Pointer[rotation.File]
	cur.Store(rot)
	// The rotation is anchored to real time and runs out; renew it before it does, while running.
	go func() {
		for range time.Tick(time.Hour) {
			if f, _ := keepRotation(rotPath); f != nil {
				cur.Store(f)
			}
		}
	}()
	srv := npln.NewServer(creds, &cur)
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

// keepRotation renews the rotation file when it is missing or close to running out, and returns
// what to serve. A nil rotation means the file is present but unusable, and err says why.
func keepRotation(path string) (*rotation.File, error) {
	rot, renewed, err := rotation.Keep(path, time.Now().UTC())
	switch {
	case rot == nil:
		log.Printf("rotation %s: %v", path, err)
	case renewed:
		log.Printf("rotation %s renewed: %d vs to %s (%v)", path, len(rot.Vs), rot.VsEnd().Format(time.RFC3339), err)
	}
	return rot, err
}
