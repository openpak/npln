// Package stardew is Stardew Valley on the shared NPLN host.
package stardew

import (
	"net"

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
		return npln.NewServer(creds).Serve(lis)
	},
}
