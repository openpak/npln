// Package hamsterplayground is Hamster Playground (010035901701A000, tinfoil.io/Title/010035901701A000) on the shared NPLN host.
//
// Tenant t-b9dd3510-lp1 from Kinnay's NintendoClients wiki, "NPLN Servers" (PRD Wave 3); a Switch 1
// title (owner, 2026-09-18). Deployed on Stardew's service set before any boot: its first run's
// log is the list of what it needs beyond that set.
package hamsterplayground

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "010035901701A000"

// Title is this game's entry in the shared NPLN host: tenant port 21118, session 24510
// (ports.md).
var Title = stardewset.Title("hamster-playground", TitleID, ":21118", ":21119", ":24510", "tenants/t-b9dd3510-lp1")
