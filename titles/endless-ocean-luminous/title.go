// Package endlessocean is Endless Ocean Luminous (010067B017588000) on the shared NPLN host.
//
// Tenant t-a71f7ea5-lp1 from Kinnay's NintendoClients wiki, "NPLN Servers" (PRD Wave 3). Deployed
// 2026-09-18 on Stardew's service set with everything Dinkum taught (switch-nex/npln notes),
// before any boot: its first run's log is the list of what it needs beyond that set.
package endlessocean

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "010067B017588000"

// Title is this game's entry in the shared NPLN host: tenant port 21112, session 24500
// (ports.md).
var Title = stardewset.Title("endless-ocean-luminous", TitleID, ":21112", ":21113", ":24500", "tenants/t-a71f7ea5-lp1")
