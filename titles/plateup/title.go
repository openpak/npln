// Package plateup hosts PlateUp! on the shared NPLN server.
//
// The Switch 1.5.2 build (content version 1572864) selects NPLN for
// matchmaking/Gamesync and QUIC peer transport; Photon is disabled for
// platform type 6 (Switch). See docs/online-flow.md for the measured call
// paths. The tenant was captured from Switch TLS SNI on 2026-10-12.
package plateup

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "01003F901A83E000"

// DefaultTenant was observed immediately after this application's aauth call.
const DefaultTenant = "tenants/t-a8f1d737-lp1"

// Title starts the initial Auth/Friends/Presence/GameSession/Gamesync service
// set. This is registration, not verified retail compatibility: QUIC peer
// setup and LeaderboardService still require client acceptance testing.
// Ports are reserved in the workspace docs/ports.md.
var Title = stardewset.Title("plateup", TitleID, ":21128", ":21129", ":24560", DefaultTenant)
