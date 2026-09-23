// Package peacewalker is METAL GEAR SOLID: Peace Walker, Master Collection version
// (0100C6F01C4F8000), on the shared NPLN host.
//
// It runs Stardew Valley's service set under its own tenant. What the title needs beyond that
// set (docs/provenance.md): a TURN server in the ICE set (the set already has one), lobby
// capacity read from its Mmc_<mode><n> config names, a join answer naming only the joiner, and
// FriendSearch queries restricted to friends — all in the shared session code. Title-specific:
// the config properties Nintendo stamps on every session (and with them the session's own
// config name as gamesync's `mcn`), and the Matchmaker, messaging, UGC store and hydro
// datastore, which the measured server registers for this title (docs/provenance.md). It is
// served with a leaf chained to the OpenPak CA, which the console or emulator trusts through
// its system certificate store; unverified until booted.
package peacewalker

import (
	"github.com/openpak/npln/titles/stardew-valley/npln"
	"github.com/openpak/npln/titles/stardewset"
)

// TitleID is the Switch application id.
const TitleID = "0100C6F01C4F8000"

// DefaultTenant is Peace Walker's NPLN tenant (docs/provenance.md). The console dials
// t-4f53195c-lp1.lp1.t.npln.srv.nintendo.net.
const DefaultTenant = "tenants/t-4f53195c-lp1"

// Title is this game's entry in the shared NPLN host: tenant port 21114, session 24530
// (ports.md).
var Title = stardewset.Title("metal-gear-solid-peace-walker", TitleID, ":21114", ":21115", ":24530", DefaultTenant,
	npln.Options{ConfigProperties: true, Matchmaker: true, Messaging: true, UGC: true, Hydro: true})
