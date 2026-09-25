// Package terraria is Terraria (0100E46006708000) on the shared NPLN host.
//
// Observed on the Ryujinx fork, 2026-09-25: pressing Host in-game dialled
// t-455965db-lp1.lp1.t.npln.srv.nintendo.net (gRPC, ALPN grpc-exp/h2) and called
// nn.npln.auth.v1.Auth/IssuePrearrangedUserToken. nx-baas had no route for the tenant and
// answered 404, and the title hung on that call until it was killed — an unrouted NPLN tenant
// freezes this game rather than failing it.
//
// Its guest threads are nn.npln.* plus Pia (BackgroundScheduler, SendThreadStream,
// ReceiveThreadStream), so the session layer is Pia over NPLN, not Dinkum's Mirror transport.
// Terraria is not on Kinnay's NPLN list; the tenant above is from that boot. It starts on
// Stardew Valley's service set under its own tenant — the first boot's UNIMPLEMENTED log is the
// list of what it needs beyond that set. The JP (010024B00F9D0000) and KR (0100E990104DC000)
// releases are untested; they are expected to share this tenant.
package terraria

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "0100E46006708000"

// DefaultTenant is Terraria's NPLN tenant, from the 2026-09-25 boot.
const DefaultTenant = "tenants/t-455965db-lp1"

// Title is this game's entry in the shared NPLN host: tenant port 21116, session 24540
// (ports.md).
var Title = stardewset.Title("terraria", TitleID, ":21116", ":21117", ":24540", DefaultTenant)
