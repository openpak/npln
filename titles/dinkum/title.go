// Package dinkum is Dinkum (0100A5A020D5E000) on the shared NPLN host.
//
// Our v1.0.7.13 dump links NPLN 1.43.3 with the WebRTC/QUIC/raft P2P add-ons
// (p2p.npln.nintendo.net) and a Unity Mirror transport over NPLN, and no Pia
// (Openpak/prds/switch-catalogue-prd.md). Dinkum is not on Kinnay's NPLN list and its dump
// carries no tenant — the SDK resolves it at runtime — so NPLN_TENANT is required until a
// boot shows the host name the console dials. Stardew's service set is the starting point;
// the P2P add-ons are likely to need more.
package dinkum

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "0100A5A020D5E000"

// Title is this game's entry in the shared NPLN host: tenant port 21122, session 22230
// (ports.md).
var Title = stardewset.Title("dinkum", ":21122", ":21123", ":22230", "")
