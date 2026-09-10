// Package npln is the shared NPLN host: one image for every NPLN title, one title per
// container, chosen by NPLN_TITLE.
//
// ponytail: the host owns TLS, the listener, /health and the TURN-secret check; each title
// still brings its own gRPC service set (titles/<game>/npln). Sharing the service
// implementations is the next step once a title beyond these two needs them.
package npln

import (
	"net"

	"google.golang.org/grpc/credentials"
)

// Title is one game the shared binary can host.
type Title struct {
	Name         string // the NPLN_TITLE value; also the health endpoint's "service"
	Listen       string // default NPLN_LISTEN (the tenant port from ports.md)
	HealthListen string // default HEALTH_LISTEN
	// Run builds the title's gRPC server over creds and serves it on lis until it fails.
	Run func(creds credentials.TransportCredentials, lis net.Listener) error
}
