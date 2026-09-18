// Package humanfallflat is Human Fall Flat (01000CA004DCA000) on the shared NPLN host.
//
// Our v1.6.1 dump links NPLN 1.41.1 and Pia 6.42.0 through PiaUnity
// (Openpak/prds/switch-catalogue-prd.md). It is not on Kinnay's NPLN list and its dump
// carries no tenant — the SDK resolves it at runtime. The tenant below is from a boot on the
// Ryujinx fork (2026-09-18): it dialled t-5cbc0f31-lp1.lp1.t.npln.srv.nintendo.net, asked
// QueryGameSessions first, and also resolved g2122d301.lp1.p.srv.nintendo.net (a .p.srv host,
// unknown so far) and the nncs NAT checks. Stardew (NPLN 1.43.3, Pia 6.42.1) is the closest
// verified title.
package humanfallflat

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "01000CA004DCA000"

// Title is this game's entry in the shared NPLN host: tenant port 21124, session 22240
// (ports.md).
var Title = stardewset.Title("human-fall-flat", TitleID, ":21124", ":21125", ":22240", "tenants/t-5cbc0f31-lp1")
