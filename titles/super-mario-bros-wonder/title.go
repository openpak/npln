// Package wonder is Super Mario Bros. Wonder (010015100B514000) on the shared NPLN host.
//
// Wonder is an NPLN title. Nothing of its traffic has been observed by OpenPak yet, so this
// title reuses Stardew Valley's verified NPLN service set (titles/stardewset) under Wonder's
// own tenant. What is known and what is not is in docs/ and the catalog; the one fact that
// still gates a console is outside this server: a client-side patch for the pinned
// certificate and two peer name checks that v1.2.1 carries.
package wonder

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "010015100B514000"

// DefaultTenant is Wonder's NPLN tenant: Kinnay's NintendoClients wiki, "NPLN Servers",
// lists Super Mario Bros. Wonder as ba973ec6 (read 2026-09-11), and the same table gives
// Splatoon 3 the dce9377b this project had already observed, which is the cross-check.
// Our own v1.2.1 dump carries the same t-ba973ec6 (2026-09-18).
// The console dials t-ba973ec6-lp1.lp1.t.npln.srv.nintendo.net.
const DefaultTenant = "tenants/t-ba973ec6-lp1"

// Title is this game's entry in the shared NPLN host: tenant port 21014, session 22220
// (ports.md).
var Title = stardewset.Title("super-mario-bros-wonder", TitleID, ":21014", ":21015", ":22220", DefaultTenant)
