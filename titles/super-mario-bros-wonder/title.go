// Package wonder is Super Mario Bros. Wonder (010015100B514000) on the shared NPLN host.
//
// Wonder is an NPLN title. Nothing of its traffic has been observed by OpenPak yet, so this
// title reuses Stardew Valley's verified NPLN service set (auth, friends, game sessions,
// gamesync) under Wonder's own tenant. What is known and what is not is in docs/ and the
// catalog; the two facts that gate a console are outside this server: the NPLN tenant id
// (NPLN_TENANT, required) and a client-side patch for the pinned certificate and two peer
// name checks that v1.2.1 carries.
package wonder

import (
	"errors"
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
		return npln.NewServer(creds, tenant).Serve(lis)
	},
}

// Tenant is Wonder's NPLN tenant from NPLN_TENANT. It is not known yet: it is the
// npln-tenant-id every RPC carries, and the t-… label in the SNI the console dials
// (t-<id>-lp1.lp1.t.npln.srv.nintendo.net). Refusing to start beats serving a wrong tenant,
// which the client rejects on every resource name.
func Tenant() (string, error) {
	t := os.Getenv("NPLN_TENANT")
	switch {
	case t == "":
		return "", errors.New("NPLN_TENANT is required for super-mario-bros-wonder: the tenant id has not been captured yet (tenants/t-XXXXXXXX-lp1)")
	case !tenantShape.MatchString(t):
		return "", errors.New("NPLN_TENANT must look like tenants/t-XXXXXXXX-lp1, got " + t)
	}
	return t, nil
}
