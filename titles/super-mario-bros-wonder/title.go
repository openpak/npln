// Package wonder is Super Mario Bros. Wonder (010015100B514000) on the shared NPLN host.
//
// Wonder runs Stardew Valley's NPLN service set (titles/stardewset) under its own tenant, plus
// the services the measured server's Wonder code and captures show it reaching
// (docs/provenance.md): the Matchmaker (CourseMatch / WorldMapMatch pools, friend pools tied
// to a FriendMatch room), messaging, the UGC store (its course query) and the hydro datastore.
// Its sessions carry their config properties, so gamesync's `mcn` is the session's own config
// name. It is served with a leaf chained to the OpenPak CA, which the console or emulator trusts
// through its system certificate store; unverified until booted.
package wonder

import (
	"github.com/openpak/npln/titles/stardew-valley/npln"
	"github.com/openpak/npln/titles/stardewset"
)

// TitleID is the Switch application id.
const TitleID = "010015100B514000"

// DefaultTenant is Wonder's NPLN tenant: Kinnay's NintendoClients wiki, "NPLN Servers",
// lists Super Mario Bros. Wonder as ba973ec6 (read 2026-09-11), and the same table gives
// Splatoon 3 the dce9377b this project had already observed, which is the cross-check.
// Our own v1.2.1 dump carries the same t-ba973ec6 (2026-09-18).
// The console dials t-ba973ec6-lp1.lp1.t.npln.srv.nintendo.net.
const DefaultTenant = "tenants/t-ba973ec6-lp1"

// Title is this game's entry in the shared NPLN host: tenant port 21014, session 22220
// (ports.md).
var Title = stardewset.Title("super-mario-bros-wonder", TitleID, ":21014", ":21015", ":22220", DefaultTenant,
	npln.Options{ConfigProperties: true, Matchmaker: true, Messaging: true, UGC: true, Hydro: true})
