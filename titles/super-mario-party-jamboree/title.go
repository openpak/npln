// Package jamboree is Super Mario Party Jamboree (0100965017338000) on the shared NPLN host.
//
// Tenant t-adf89f68-lp1 from Kinnay's NintendoClients wiki, "NPLN Servers" (PRD Wave 3). Deployed
// 2026-09-18 on Stardew's service set with everything Dinkum taught (switch-nex/npln notes),
// before any boot: its first run's log is the list of what it needs beyond that set.
package jamboree

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "0100965017338000"

// Title is this game's entry in the shared NPLN host: tenant port 21100, session 22250
// (ports.md).
var Title = stardewset.Title("super-mario-party-jamboree", TitleID, ":21100", ":21101", ":22250", "tenants/t-adf89f68-lp1")
