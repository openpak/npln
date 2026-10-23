// Package gogotown is Go-Go Town! (010095001B12A000) on the shared NPLN host.
//
// The 2026-10-12 multiplayer sweep found Auth, Friends, Gamesync, Maintenance
// and GameSession service strings, but no literal tenant. Stardew's service
// set supports the observed two-player flow: on 2026-10-12 a Switch hosted
// and Harbor joined, and the owner confirmed both played together. Room
// config details and further scenarios remain unverified. A Switch boot on
// 2026-10-12 authenticated application version 00060000 and immediately
// dialled t-05cbf648-lp1.lp1.t.npln.srv.nintendo.net. In particular, the
// Maintenance marker does not prove that the client calls that service.
package gogotown

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "010095001B12A000"

// DefaultTenant is from the 2026-10-12 Switch online attempt.
const DefaultTenant = "tenants/t-05cbf648-lp1"

// Title serves the captured tenant; NPLN_TENANT can override a later capture.
// Tenant port 21126, health 21127 and session 24550 are allocated in the port plan (https://docs.openpak.org/contributors/ports/).
var Title = stardewset.Title("go-go-town", TitleID, ":21126", ":21127", ":24550", DefaultTenant)
