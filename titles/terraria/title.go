// Package terraria is Terraria (0100E46006708000) on the shared NPLN host.
//
// Observed on the Ryujinx fork, 2026-09-25: pressing Host in-game dialled
// t-455965db-lp1.lp1.t.npln.srv.nintendo.net (gRPC, ALPN grpc-exp/h2) and called
// nn.npln.auth.v1.Auth/IssuePrearrangedUserToken. nx-baas had no route for the tenant and
// answered 404, and the title hung on that call until it was killed — an unrouted NPLN tenant
// freezes this game rather than failing it.
//
// Its guest threads are nn.npln.* plus Pia (BackgroundScheduler, SendThreadStream,
// ReceiveThreadStream), so the session layer is Pia over NPLN, not Dinkum's Mirror transport.
// Terraria is not on Kinnay's NPLN list; the tenant above is from that boot.
//
// Verified the same day: Ryujinx hosted and Eden joined, two accounts in one world.
// CreateGameSessionCreationTicket -> Track -> Gamesync IssueToken -> KeepUserSession ->
// AllocateIceServerSet -> GetDocument/WriteDocuments, and JoinGameSession for the joiner.
// Stardew Valley's service set covers it whole: not one UNIMPLEMENTED, so it needs no Options.
// The Pia relay host it resolves (g2122d301.lp1.p.srv.nintendo.net) is never dialled — ICE
// through our own TURN is enough. The JP (010024B00F9D0000) and KR (0100E990104DC000) releases
// are untested; they are expected to share this tenant.
//
// Friends' worlds in the Join list (read from the 1.4.5.6 IL2CPP build, confirmed on hardware
// 2026-10-11): the list comes from nn.npln.friends.v1.PresenceService only, never from
// nn::friends presence. Each player publishes two attributes, type="Terraria" and
// session=base64(MPSession_Switch.WriteGameProperties); a friend's world is listed when the
// presence is ONLINE, type is "Terraria" and session is non-empty and parses. A world hosted
// with the Host screen's "Invite Only" toggle on (GUIMultiplayerHost.InviteOnly, label key
// Mobile.OpenInviteOnlyTitle, off at each game launch) carries InviteOnly=1 in that blob and
// Switch_FriendUserPresence.UpdateSessions drops it, so it never shows in a friend's Join
// list; invitations still work. "A Switch's world is missing from the list but invites work"
// is that toggle, not the server.
package terraria

import "github.com/openpak/npln/titles/stardewset"

// TitleID is the Switch application id.
const TitleID = "0100E46006708000"

// DefaultTenant is Terraria's NPLN tenant, from the 2026-09-25 boot.
const DefaultTenant = "tenants/t-455965db-lp1"

// Title is this game's entry in the shared NPLN host: tenant port 21116, session 24540
// (ports.md).
var Title = stardewset.Title("terraria", TitleID, ":21116", ":21117", ":24540", DefaultTenant)
