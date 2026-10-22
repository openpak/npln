// Package corekeeper is Core Keeper (010070201D1BA000) on the shared NPLN host.
//
// The Switch 1.3.0.3 client has an NPLN session manager and QUIC peer transport,
// alongside a secondary PlayFab Party implementation. See docs/provenance.md
// for the measured static paths and the runtime checks still required.
package corekeeper

import (
	"github.com/openpak/npln/titles/stardew-valley/npln"
	"github.com/openpak/npln/titles/stardewset"
)

// TitleID is the Switch application id.
const TitleID = "010070201D1BA000"

// DefaultTenant was observed in Harbor's Start Online Game flow on 2026-10-12.
const DefaultTenant = "tenants/t-b0546eeb-lp1"

// Title serves tenant 21130, health 21131 and session/Gamesync 24570. Preserve
// the game's config names in session properties instead of Stardew's Farm4Player.
var Title = stardewset.Title("core-keeper", TitleID, ":21130", ":21131", ":24570", DefaultTenant,
	npln.Options{ConfigProperties: true})
