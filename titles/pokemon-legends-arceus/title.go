// Package arceus is Pokémon Legends: Arceus (01001F5010DFA000) on the shared NPLN host.
//
// Tenant t-e047112f-lp1 from Kinnay's NintendoClients wiki, "NPLN Servers" (PRD Wave 3). Deployed
// 2026-09-18 on Stardew's service set with everything Dinkum taught (switch-nex/npln notes),
// before any boot: its first run's log is the list of what it needs beyond that set.
package arceus

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "01001F5010DFA000"

// Title is this game's entry in the shared NPLN host: tenant port 21104, session 22270
// (ports.md).
var Title = stardewset.Title("pokemon-legends-arceus", TitleID, ":21104", ":21105", ":22270", "tenants/t-e047112f-lp1")
