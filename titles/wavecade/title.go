// Package wavecade is Wavecade (0100F7A017F4C000, tinfoil.io/Title/0100F7A017F4C000) on the shared NPLN host.
//
// Tenant t-61cb5c35-lp1 from Kinnay's NintendoClients wiki, "NPLN Servers" (PRD Wave 3); a Switch 1
// title (owner, 2026-09-18). Deployed on Stardew's service set before any boot: its first run's
// log is the list of what it needs beyond that set.
package wavecade

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "0100F7A017F4C000"

// Title is this game's entry in the shared NPLN host: tenant port 21120, session 24520
// (ports.md).
var Title = stardewset.Title("wavecade", TitleID, ":21120", ":21121", ":24520", "tenants/t-61cb5c35-lp1")
