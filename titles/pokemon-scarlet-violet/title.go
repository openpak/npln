// Package scarletviolet is Pokémon Scarlet and Violet (0100A3D008C5C000) on the shared NPLN host.
//
// Tenant t-50e39f8f-lp1 from Kinnay's NintendoClients wiki, "NPLN Servers" (PRD Wave 3). Deployed
// 2026-09-18 on Stardew's service set with everything Dinkum taught (switch-nex/npln notes),
// before any boot: its first run's log is the list of what it needs beyond that set.
// ponytail: one tenant for two games, one app_id in the token (Scarlet's). Violet
// (01008F6008C5E000) may be refused when it hosts, as Dinkum was on a wrong app_id; take the
// app id from the caller's id token (nintendo.ai) when a Violet boot shows it.
package scarletviolet

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "0100A3D008C5C000"

// Title is this game's entry in the shared NPLN host: tenant port 21102, session 22260
// (ports.md).
var Title = stardewset.Title("pokemon-scarlet-violet", TitleID, ":21102", ":21103", ":22260", "tenants/t-50e39f8f-lp1")
