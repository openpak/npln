// Package bayonetta3 is Bayonetta 3 (01004A4010FEA000) on the shared NPLN host.
//
// Tenant t-2c06a4d3-lp1 from Kinnay's NintendoClients wiki, "NPLN Servers" (PRD Wave 3). Deployed
// 2026-09-18 on Stardew's service set with everything Dinkum taught (switch-nex/npln notes),
// before any boot: its first run's log is the list of what it needs beyond that set.
package bayonetta3

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "01004A4010FEA000"

// Title is this game's entry in the shared NPLN host: tenant port 21110, session 22290
// (ports.md).
var Title = stardewset.Title("bayonetta-3", TitleID, ":21110", ":21111", ":22290", "tenants/t-2c06a4d3-lp1")
