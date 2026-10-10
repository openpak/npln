# Changelog — npln

Generated from git history on 2026-09-15. `git log` stays the source
of truth; this file is the readable summary.

## v0.7.9 — 2026-10-10

- Splatoon 3 match referee: a verdict document answers each `@@RefereeRequest` (status 0) and
  `@@RefereeReport` (status 1) on gamesync (`Options.Referee`, Splatoon 3 only). Private battle
  rooms hold 10. Both untested on consoles yet.

## v0.7.8 — 2026-10-10

- Splatoon 3 enters the lobby on a Harbor console and the Stages tab lists the rotation. Co-op
  shifts carry a monthly reward (the lobby refused a shift without one), and
  GameRecord/InitializeAttributes gives a new player zeroed coop and VS attributes.
- Not yet: Private Battle is locked for a new player ("Try some Regular Battles first!").

## v0.7.7 — 2026-10-10

- Splatoon 3 boots online and reaches the Square on a Harbor console: the toyohr boot services
  (cloud save, lobby messaging, user screening, locker, canola, maintenance), ugcstore and
  FestService, with Nintendo-shaped NOT_FOUND (NError detail) for every absent resource.
- Splatoon 3 presence KeepAlive heartbeats on a timer and never answers acks (it was a loop of
  ~2,000 heartbeats a minute).
- Splatoon 3 schedules the lobby accepts: ranked rules 1-4, no Challenge event, co-op shifts and
  season with ids from the game's own tables. The lobby still raises a communication error after
  its "maps and modes updated" notice; the cause is being traced.
- Splatoon 3 rooms, matchmaking and gamesync are now Stardew Valley's service set
  (`RegisterSessions`); untested for Splatoon 3.

## v0.7.6 — 2026-10-10

- Logs carry no personal data, for any title: no account, user, session or pid id, nickname,
  token, ticket, name, friend code or client address. An id is logged as a short per-process tag
  (`~` and six hex digits, a keyed hash), so lines about one player still match up within a run.
  Error text drops the addresses and URLs that net and url errors spell out.
- rpclog logs each message as its outline by default: the fields set, enum and bool values, the
  length of every string and bytes field. The full body, token shapes and unknown fields in hex
  need `NPLN_TRACE_BODIES=1` (and `NPLN_GS_DIAG=1` for gamesync's store dump); never set in
  production.

## v0.7.5 — 2026-10-09

- Latest version only. A login whose NSA id token names a version below the latest (nx-baas's
  `/internal/title-version`, the table aauth refuses with) gets FAILED_PRECONDITION, 2321-3072.
  An nx-baas that cannot answer refuses no one.
- Versions never share a session: rooms and Matchmaker pools keep their creator's version
  server-side, and a player of another version is not listed them, placed in them or let in by
  name. An unknown version meets everyone. Splatoon 3's own service copy is not covered yet.

## v0.7.4 — 2026-09-25

- Terraria is live: Ryujinx hosted, Eden joined, two accounts in one world, no UNIMPLEMENTED.
  Stardew Valley's service set covers the title whole, so it keeps no title-specific Options.

## v0.7.3 — 2026-09-25

- Terraria (0100E46006708000), tenant `t-455965db-lp1`, on Stardew Valley's service set; tenant
  port 21116, health 21117, session 24540. Found by a Ryujinx boot: pressing Host dialled the
  tenant, called `nn.npln.auth.v1.Auth/IssuePrearrangedUserToken`, got the catch-all's 404 and
  hung there. Its session layer is Pia over NPLN.

## v0.7.2 — 2026-09-24

- deps: grpc 1.83.2 (Dependabot)

## v0.7.1 — 2026-09-24

- Splatoon 3: the server keeps its rotation current itself. A missing file is written, and one
  within 7 days of running out is renewed 30 days ahead (checked hourly, swapped in while
  running), so the rotation no longer expires and a restart after it ran out no longer refuses to
  start. The schedule set id now changes with each renewal. `cmd/genrotation` stays for hand-made
  files.

## v0.7.0 — 2026-09-24

- NPLN services beyond Stardew's set, opt-in per title through new `npln.Options` fields, and
  on for Super Mario Bros. Wonder and METAL GEAR SOLID: Peace Walker (facts and sources in
  `titles/super-mario-bros-wonder/docs/provenance.md`; new `.proto` files under `proto/messaging`,
  `proto/ugcstore`, `proto/hydro`, written from the protocol descriptors):
  - `Matchmaker` (matchmaking.v1): tickets pooled by config, `MatchingKey` and friend room;
    `CourseMatch_*` pools hold 4, `WorldMapMatch_*` / `FriendWorldMapMatch_*` 12. Friend pools
    need `FriendGameSessionId` naming a live base room the caller is in, are private, and are
    never listed by `QueryGameSessions` nor joinable by name. A pool closes when its last member
    leaves. Tracking streams SEARCHING, PLACING, then SUCCEEDED with every member; only the
    caller's entry carries a token.
  - `Messaging` (messaging.v1): `RecvMessage` opens with a 95 s keep-alive and repeats every
    45 s; `SendMessage` to friends or session mates, resume tokens, `need_ack` messages kept
    until acked and the ack delivered to the sender.
  - `UGC` (ugcstore.v1 Ugcstore + Screening): documents stored in `NPLN_UGC_FILE` (default
    `ugcstore.json`); Firestore-style queries (Wonder's course query answers read_time alone
    when empty); writes only by a document's first writer, user-id path segments must be the
    caller's, size and count limits, atomic commits; short codes; reports logged. Attachments
    and upload URIs are not served.
  - `Hydro` (hydro.v1 Datastore): contents in memory, owner-only changes unless UPDATE is
    granted, etags, `payload` / `attributes.<n>` transforms, search, TrackContent.
  - The ban gate covers all of them (the server-wide interceptors).
- Shared session changes: `FriendMatch*` / `WorldMapMatch*` / `FriendWorldMapMatch*` rooms hold
  12 and `CourseMatch*` 4 when the ticket names no size; `Friend*` rooms are not public;
  friend pools are skipped by `QueryGameSessions` and refused by `JoinGameSession` to
  non-members; a re-hosting player no longer evicts pool sessions.
- `mcn` in gamesync `docs/__stg/All` and `docs/__gs/f` is the session's `_BaseConfigName` for a
  title with `ConfigProperties` (Wonder, Peace Walker); every other title keeps `Farm4Player`.
- Wonder now stamps `_BaseConfigName` / `_AliasSuffix` on its sessions (`ConfigProperties`).
- Docs: no title needs a client-side game patch. Wonder and Peace Walker are served with a leaf
  chained to the OpenPak CA, which the console/emulator trusts via its system store; unverified
  until booted.

- METAL GEAR SOLID: Peace Walker (Master Collection, `0100C6F01C4F8000`) on tenant
  `t-4f53195c-lp1`: `NPLN_TITLE=metal-gear-solid-peace-walker`, tenant 21114, health 21115,
  session 24530. Facts and sources in its `docs/provenance.md`. Shared session changes, for
  every title on the Stardew set:
  - A creation ticket with no size takes it from an `Mmc_<mode><n>` config name (`Mmc_CoOps2`
    holds 2); other names keep 4.
  - `QueryGameSessions` with the `FriendSearch` config is refused unless every named user is
    the caller or a friend (nx-baas friend list).
  - A search property sent without a value matches every room.
  - `npln.Options` (passed through `stardewset.Title`): `ConfigProperties` stamps
    `_BaseConfigName` / `_AliasSuffix` on created sessions; only Peace Walker sets it.

- Bans (website/docs/ban-lookup.md): new package `bans`. An account banned in the OpenPak
  admin is refused by every title with gRPC `PermissionDenied` "This OpenPak account has been
  banned". nx-baas answers `/internal/switch/identity` with 403 `account_banned` for it.
  - Sign-in (`IssueToken`, `IssuePrearrangedUserToken`, `IssueAnonymousUserToken`) refuses a
    banned account; Stardew's re-issue on an already-proven connection is refused too.
  - `RefreshToken` re-checks the account, and refuses when the check cannot be answered.
  - Every RPC carrying one of our tokens is checked (an access token by its pid, a gamesync
    session token by the user id recorded at token issue). An open stream, KeepUserSession
    among them, is re-checked every 30 s and ended when the account is banned. Active
    answers are cached for 30 s here and at most 30 s in nx-baas, so a ban lands within a
    minute. An nx-baas outage mid-session keeps the player.
  - No new environment variables: it uses `NX_INTERNAL_URL` and `NX_INTERNAL_KEY`.
  - Known, not from this change: `TestKeepAliveAnswersEveryPing` in `titles/stardew-valley/npln`
    already reports a data race under `-race`.

## v0.5.15 — 2026-09-18

- Hamster Playground (21118, session 24510) and Wavecade (21120, session 24520), both Switch 1,
  on their public tenants. NPLN session ports continue in 24000–24999 (open at the provider).

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
- Finish the move off Reference [4a808e3]
- A health port beside the gRPC one [431392d]
- Ship a container image on tag: rootless-ready Dockerfile + ghcr release workflow [cbbb24d]
- handoff: session 14 — openpak flow verified, hardening list passed, 3 players, mailbox redelivery fix [843319d]
- npln: deliver every mailbox write, even when the bytes are unchanged [7896f63]
- handoff: session 13 — production box prep, upstream PRs, openpak via account server, citron re-test [1e80132]
- npln: have the account server prove the openpak claim; drop OPENPAK_SECRET [753bfdb]
- deploy/oci: let coturn bind relays on the local addresses [d83dc12]
- deploy: OCI/arm64 compose with patched coturn, env template, production guide, sni-router patch; handoff: production plan and mailbox-blob fix [7ac927d]
- npln: send X-Internal-Key to the account server when OPENPAK_INTERNAL_KEY is set (off-network deployments) [9c5b193]
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
