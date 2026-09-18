// Package mhrise is Monster Hunter Rise (Sunbreak is the same title) (0100B04011742000) on the shared NPLN host.
//
// Tenant t-e1c218b5-lp1 from Kinnay's NintendoClients wiki, "NPLN Servers" (PRD Wave 3). Deployed
// 2026-09-18 on Stardew's service set with everything Dinkum taught (switch-nex/npln notes),
// before any boot: its first run's log is the list of what it needs beyond that set.
package mhrise

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "0100B04011742000"

// Title is this game's entry in the shared NPLN host: tenant port 21106, session 22280
// (ports.md).
var Title = stardewset.Title("monster-hunter-rise", TitleID, ":21106", ":21107", ":22280", "tenants/t-e1c218b5-lp1")
