# Changelog — npln

Generated from git history on 2026-09-15. `git log` stays the source
of truth; this file is the readable summary.

## v0.5.14 — 2026-09-18

- Six titles on the Stardew set under their public tenants, deployed before any boot:
  Super Mario Party Jamboree (21100), Pokémon Scarlet/Violet (21102, Scarlet's app_id),
  Legends: Arceus (21104), Monster Hunter Rise (21106), Bayonetta 3 (21110), Endless Ocean
  Luminous (21112). Held back: Puyo Puyo Tetris 2S (Switch 2), Hamster Playground and
  Wavecade (platform unconfirmed), Nintendo Classics (several apps, one tenant), MACHI KORO
  (no tenant).

## v0.5.13 — 2026-09-18

- Human Fall Flat's tenant is `t-5cbc0f31-lp1` (from a boot), now its default; deployed as
  `openpak-human-fall-flat`.

## v0.5.12 — 2026-09-18

- Room codes leave out Z as well: Dinkum's room-code screen refuses I, O, Z and symbols.

## v0.5.11 — 2026-09-18

- KeepAlive never answers an ack. A real console acks each heartbeat at once, and answering it
  was a heartbeat/ack storm that dropped the console's connection every few seconds, before
  it could ask for its friends' presence. Heartbeats now come from a 10 s timer.
- Room codes are 6 capital letters (no I/O), what Dinkum's room-code screen accepts; lookup
  ignores case. They were 8 lowercase hex, which could not be typed in.

## v0.5.10 — 2026-09-18

- The Stardew set serves `PresenceService/SubscribePresences` and keeps who is online and
  what they published (ported from Splatoon 3): a friend now sees a Dinkum host as hosting.
- First real console on Dinkum: TLS accepted, no certificate pinning, login and friends work.

## v0.5.9 — 2026-09-18

- Room codes are named `tenants/…/GameSessionShortAliases/<code>`, capital G. The NPLN SDK
  compares that collection name byte for byte (read from Dinkum's code) and aborted the game
  on our lowercase `gameSessionShortAliases` — the freeze right after hosting.

## v0.5.8 — 2026-09-18

- A call with a valid access token (ours) counts as proof of its user for that connection,
  like a login does. A console that reconnects after a server restart keeps its token, never
  logs in again, and was refused when it then hosted.

## v0.5.7 — 2026-09-18

- The room-code answer (`CreateGameSessionShortAlias`) names the session with the real tenant
  instead of echoing the client's `tenants/current/…`; Dinkum's NPLN SDK aborted right after
  receiving it.
- Stream messages the server sends are logged too (`[RPC<]`), not only what it receives.

## v0.5.6 — 2026-09-18

- `PresenceService/KeepAlive` opens with a heartbeat. Dinkum opens the stream and waits for
  the server; without it the presence client never connected and the game aborted as soon
  as it published its room code.

## v0.5.5 — 2026-09-18

- `IssueToken` without a provable id token is re-issued for the user the same connection
  already proved, and refused for anyone else. Dinkum sends a 32-byte non-JWT when it hosts.
- Token fields in the log also say hex/printable/binary and carry a short SHA-256
  fingerprint; still never the value.

## v0.5.4 — 2026-09-18

- Access tokens name the title they were issued for (`npln.app_id`). Every title on the
  Stardew set (Wonder, Dinkum, Human Fall Flat) was getting Stardew's id, and Dinkum refused
  to create a session (2321-5760) without making a call.

## v0.5.3 — 2026-09-18

- Every call's content is in the log by default, for every title: `[RPC>]` for each request
  and stream message, `[RPC<]` for unary answers, fields the proto does not define in hex, and
  the first message of an unimplemented call. Token fields show only length and JWT-or-not.

## v0.5.1 — 2026-09-18

- Dinkum's tenant is `t-35b7d576-lp1` (from a boot), now its default; deployed as
  `openpak-dinkum`. Login, ActivateUser and the friends stream work; TLS is not pinned.
- The Stardew set answers `PresenceService/KeepAlive`, which Dinkum opens right after
  ActivateUser (it got Unimplemented). Pings only; published presence is not kept yet.

## v0.5.0 — 2026-09-18

- Dinkum (21122, session 22230) and Human Fall Flat (21124, session 22240) registered: NPLN
  titles found in our own dumps and on no public list. Their dumps carry no tenant, so both
  refuse to start until `NPLN_TENANT` is set from a boot.
- `titles/stardewset`: Stardew's verified service set under another tenant, shared by
  Wonder, Dinkum and Human Fall Flat. Wonder's behaviour is unchanged.

## v0.3.1 — 2026-09-12



## v0.3.0 — 2026-09-11



## v0.2.0 — 2026-09-10



## v0.1.0 — 2026-09-10



## v0.1.0 — 2026-09-10

- One image for every NPLN title [0c6f8d7]
- titles/splatoon-3: import splatoon-3 with its history [017cdbd]
- titles/stardew-valley: import stardew-valley with its history [71d440c]
- npln: the shared NPLN host [0caa6ae]
- Go 1.26, the house version [04e192f]
- Go 1.26, the house version [c4e9ed6]
- Deploy from the release image; stop tracking the shared-docs symlink [33ddbc7]
- NPLN: the bearer is the identity, sessions leave the store as copies [470638a]
- NPLN: the bearer is the identity, and it expires [01ac876]
- Matchmaking, gamesync mailbox, and the container: the relay protocol work [0381ca0]
- STUN and TURN from the player's own region [4aeff7b]
- Settle relay-versus-simulation: it relays. Not the Fall Guys wall [36efcb9]
- Schedules: generated rotation, validated at load, code split from data [cbc99de]
- Friends and presence, both dynamic, with the PID/uid pairing [343b869]
- Settle the protocol facts; resolve tenants/current; add the provenance ledger [9440012]
- NPLN skeleton: auth against nx-baas, TLS on 21012, health on 21013 [8478458]
- seed: empty splatoon-3, AGPL-3.0-only, clean-room gate [8cdf788]
- docs: bring stardew-valley's docs in line with what is deployed [1326dae]
- Finish the move off Nextendo [4a808e3]
- A health port beside the gRPC one [431392d]
- Ship a container image on tag: rootless-ready Dockerfile + ghcr release workflow [cbbb24d]
- handoff: session 14 — nnex flow verified, hardening list passed, 3 players, mailbox redelivery fix [843319d]
- npln: deliver every mailbox write, even when the bytes are unchanged [7896f63]
- handoff: session 13 — production box prep, upstream PRs, nnex via account server, citron re-test [1e80132]
- npln: have the account server prove the nnex claim; drop NEXTENDO_SECRET [753bfdb]
- deploy/oci: let coturn bind relays on the local addresses [d83dc12]
- deploy: OCI/arm64 compose with patched coturn, env template, production guide, sni-router patch; handoff: production plan and mailbox-blob fix [7ac927d]
- npln: send X-Internal-Key to the account server when NEXTENDO_INTERNAL_KEY is set (off-network deployments) [9c5b193]
- gamesync: stop merging the mailbox blob into member documents (rejoin drop root cause) [39271c1]
- handoff: session 12b — flaky rejoin root-caused (write-path placeholder push) and fixed; farm closes on host leave; server keepalive; account/profile recovery notes [c91d270]
- sessions: match members by uid when dropping; npln: server keepalive so vanished consoles are dropped within ~25 s [b59864b]
- gamesync: overlay server-owned ids on write-path deliveries too; close farm / drop member when a stream ends; log member-document pushes [6380077]
- handoff: session 12 — co-op confirmed working end to end; next = hardening [471016f]
- handoff: session 12 — join confirmed connected both sides; next blocker is game-layer visibility [e706f70]
- gamesync: serve server-owned ussid/ucsid/upcsid on member documents; handoff: session 12 — upcsid=0 placeholder was the join blocker, joiner now reaches session state 9 [f4754cb]
- gamesync: push DELETED when a participant leaves; handoff: session 10 — 2318-1201 decoded (Pia 0x648e connect timeout), ghost-station bug fixed, joiner never acts on host's mailbox reply [70c3735]
- handoff: syscall trace (properly correlated with live glue state) proves the P2P UDP sockets are created and bound but never connected or sent on; correct an uncorrelated false-negative trace attempt [61b7afd]
- handoff: packet capture proves the state-7 connect task never emits a single packet; not reachability/NAT, fails before the socket layer; correct one live-read pointer chain error, flag a transient misread lesson [d30a279]
- handoff: live read exonerates the SwitchNetworkGlue state machine (async connect genuinely started and still pending, not broken); bug is below it at the vtable-dispatched network calls; next = packet capture during the stall [5c27595]
- handoff: state-7 task traced to its concrete start-connect call (f1f8); both dispatches are runtime vtables, needs one live read (status field, start tick, vtable identity) [8270d57]

- … 18 earlier commits omitted (see `git log`)
