# stardew-nextendo Handoff

## Project Objective

Build an independently written, open-source compatibility service for the network-facing behavior needed by Stardew Valley multiplayer on Nintendo Switch. This is clean-room interoperability work only; proprietary code, assets, binaries, credentials, keys, certificates, raw captures, and player-sensitive data must remain outside this repository.

## Current Status

- 2026-09-02 (session 4, LATEST — the real P2P gate is Pia's **nplnd** service): after fixing
  coturn (patched image: ALLOCATE without REQUESTED-TRANSPORT → UDP; TURN allocations now succeed
  for both consoles), the server race (copy-on-write), the duplicate-push feedback loop (pushes are
  now change-only + immediate per write; see gamesync.go watcher.push/deliver), and moving every
  address to the LAN IP (NAT check/STUN/TURN/relay on 10.87.0.2 via NEXTENDO_NAT_IP + coturn
  --relay-ip), the join STILL fails 2318-1201 after ~25 s: the mailbox signaling is orderly (host
  207-B roster with station ids 1+2, 78-B encrypted NAT-traversal messages both ways, a final 94-B
  host message), the host probes the joiner's transport port (93-B every 500 ms) but the joiner
  never sends a probe and never reads its socket. Mid-join Pia job dump (tools/piajobs.py +
  joinprobe.sh): the joiner is parked in `NplnBackgroundProcessJob::WaitConnectNetwork` /
  `JoinSessionJob::WaitConnectNetwork` (glue 7, session state 0), i.e. BEFORE mesh startup.
  KEY OBSERVATION: every session (host and joiner) sends 292/308/324/356/372/388-byte UDP
  datagrams to **127.0.0.1:34343** — port 34343 (0x8627) is hardcoded in ELF fn 0x75dd5e4 and
  127.0.0.1 is where the emulator resolves `g2122d301.lp1.p.srv.nintendo.net` (the host the early
  handoff called "resolved, never dialed"). That is Pia's `nn::pia::nplnd` plugin
  (`NplndLoginJob::WaitLogin`, `NplndFacade`, `AttachMeshJob`, `NplndPlayerInfo`, `IceServerConfigGetter`)
  logging in to Nintendo's nplnd P2P service, which NOBODY in the family has implemented (the
  reference S3 server never got P2P either). The join's "connect network" waits on it. NEXT: capture
  the :34343 payloads (`tcpdump -X udp port 34343` armed → scratch/nplnd-34343.log), RE the nplnd
  login/attach-mesh protocol (start at fn 0x75dd5e4 and the NplndLoginJob), and build a minimal
  nplnd UDP service in this repo; route `g2122d301.lp1.p.srv.nintendo.net` to it.

- 2026-09-02 (session 4, LATE — JOIN PATH: signaling works, P2P blocked by coturn): with the farm
  listed, the joiner's click sends **JoinGameSession** (first ever), opens gamesync, and both sides
  signal through `docs/__pgn/All/__stu/<uss>` used as a per-station MAILBOX: peers WRITE into the
  owner's doc (fields suid/susid/sussid/suscid = SENDER, `pl` = message bytes) and the owner watches
  its own doc. Observed: joiner→host 9-byte `01 12 …` probes (every ~0.5 s) + 78-byte `32 ab 98 64
  90 22 …` messages; host→joiner one long `01 11 00 b0 …` message (host station id + candidates)
  then 78-byte replies. Our server relays these (stored write + wake push). Two server bugs found
  live: (1) mergeFields mutated stored maps in place → "concurrent map iteration and map write"
  panics mid-join (container restarted twice; caused 2321-5251 on the joiner / 2318-1500 on the
  host) — fixed copy-on-write; (2) `withStation` now relays a participant's `pl` into its `__pus`
  member doc (harmless; the real channel is the mailbox). UDP: the host probes the joiner's port
  (93-byte packets, every 500 ms, via 127.0.0.1 and 10.87.0.2 — nncs reports both consoles'
  mapped address as 127.0.0.1); the joiner never answers and its socket shows unread Recv-Q.
  ROOT CAUSE of the P2P stall: both consoles authenticate to coturn fine (our HMAC creds accepted)
  but every TURN **ALLOCATE gets 400 "Transport field missed or wrong"** — Pia sends the ALLOCATE
  WITHOUT a REQUESTED-TRANSPORT attribute (coturn `if (!transport)`), retries every 500 ms, and Pia
  gives up with **2318-1202**. FIX IN PROGRESS: nextendo-local `stun/Dockerfile` builds coturn from
  source with a one-line patch (missing REQUESTED-TRANSPORT → UDP); compose `stun` now builds it.
  Tools: header-only UDP capture `sudo tcpdump -i any -nn -l udp …` (needs sudo; CAP_NET_RAW) and
  `readsess.py joiner` (base cached per pid → instant) read the joiner in the ~20 s connecting
  window (glue 7 = JoinSessionAsync pending, session state 2).

- 2026-09-02 (session 4, RESULT — LOBBY-DATA GATE FOUND AND OPENED): the host never published lobby
  data because Pia silently rejected our `AllocateIceServerSet` answer (STUN only, non-empty ttl):
  with a rejected ICE set Pia never starts the mesh transport — no STUN probe, no UDP socket, the
  glue stays in state 6 (`CreateSessionAsync` pending forever), Pia Session state 0, all station
  fields 0 — and the app-data flush (gated on local station == host station) never runs. Live read
  BEFORE the fix: glue 6, session 0, stations 0. FIX: `AllocateIceServerSet` now returns the shape
  the reference server measured on Nintendo (STUN + TURN with `<exp>:<user>` / base64 HMAC-SHA1
  credentials, `ttl` present-but-empty, client cache 90 s) and nextendo-local's coturn runs
  STUN+TURN with `--use-auth-secret` (`NPLN_TURN_SECRET`, `.env` + compose; backups
  `.env.bak-turn`, `compose.yml.bak-turn`). Live read AFTER: glue 9, session 1, local station ==
  host station (0x6b49d203 idx 18), and the host writes `docs/__gs/m {ip, prp._Pia_SystemData}`
  three times at hosting (mirrored into the farm's GameSession by the new `mirrorProps`). Next:
  the joiner's Refresh must now list the farm (blob carries `key\nvalue\n` lobby text); then the
  JOIN path (JoinGameSession → gamesync → Pia mesh join via STUN/TURN on loopback) is the next
  milestone.

- 2026-09-02 (session 4, IN PROGRESS — lobby-data channel narrowed): static RE only (no emulator
  freezes). Findings so far, all from the binary + logs:
  (1) **Presence RULED OUT**: Ryujinx stub logging is on and Stardew makes ZERO `nn::friends` IPC
  calls (no GetFriendList/UpdateUserPresence in either emulator log), so lobby data does not ride
  system presence AppField even though Ryujinx-Nextendo relays it.
  (2) **No NPLN RPC can update a session**: the binary embeds no UpdateGameSession/SyncGameSession
  method path (full path list checked). Pia's post-create property updates are gamesync
  **WriteDocuments** carrying a `prp` map: the create/update jobs (`NplnBackgroundProcessJob::
  WaitCreateNetwork` 0x7ab2880, `WaitUpdateHostPlayerName` 0x7ab6980, `WaitRegistSessionProperty`
  0x7ab0910, plus 0x7aa9130) build the field path `["prp","_Pia_SystemData"]` (constant helper
  0x7ad6bbc returns `"prp"`). This matches the reference server's note that Nintendo republishes
  session properties in `docs/__gs/m.prp`. Our host never sent such a write (only `__pus`).
  (3) **The joiner can only see `_Pia_SystemData`**: the search consumer (`WaitSearchNetwork`)
  looks up exactly that key in the returned session's properties, memcpy's it (0x5c..0x200 bytes)
  into the session-info object at +0x1e8 (len +0xb0) and pushes counts + name-derived id; no other
  property survives. So Stardew's `SessionInfo` (class `StardewValley.SDKs.Switch.Internal.
  SessionInfo`, kept as `Dictionary<uint,SessionInfo>` keyed by Pia session id) must be fed from
  application data INSIDE the blob. The host's 92-byte blob has none → empty list.
  (4) Game-side class map: `StardewValley.SDKs.Switch.{SwitchSDKHelper,SwitchSDKNetHelper,
  SwitchNetServer,SwitchNetClient,Internal.SessionInfo}`; native glue class `SwitchNetwork`. The
  binary is Sickhead **Brute** (C#→C++), so C# calls the glue directly; reflection tables only lead
  to lazy static getters (dead end for locating bodies).
  (5) **Blob layout (Pia NetSystemPropertyData, big-endian)**: [0..1] u16 header length (0x5c),
  [2] u8 (0x16), [3..4] u16, [5..0x14] 16-byte id, [0x15],[0x16] count bytes, [0x17..0x5c) 69-byte
  host PlayerInfo (nickname). Application data is APPENDED after the header: accessor 0x75ed41c
  returns `total_len − BE16(blob[0])` = app-data size (0 for our host). Max blob 0x200.
  (6) **Lobby-data channel FOUND (game side)**: C# `flush` at 0x1ab8b80 serializes the lobby
  Dictionary<string,string> as `key\nvalue\n…` text → bytes → glue trampoline 0x1abaf40 →
  `SwitchNetwork` update at 0x1ac57f8, which calls Pia UpdateNetworkProperty (0x760b26c →
  `ChangeStateJob::WaitUpdateSessionPropertyAsync`) → NPLN job rewrites `_Pia_SystemData` with app
  data and WriteDocuments it as `prp._Pia_SystemData` (0x7aa9130). The per-tick C# update
  (0x1abae90) only flushes when the glue state (+0x68) == 9; state 9 is set in the glue state
  machine (0x1ac2350) when Pia's create-session async completes OK.
  (7) **THE GATE**: 0x1ac57f8 silently returns unless the Pia Session singleton (ELF ptr 0xe5fc7f8;
  guest 0x16B027F8) has local station (+0xe0 u64 id, +0xe8 u16 index) non-zero AND equal to the
  host station (+0xf0/+0xf8) — an is-host check (station event handler 0x1ac66b0 confirms the
  field roles). The host emulator has NO UDP socket after hosting (only the 2 NAT-check sockets at
  startup) and coturn saw nothing, so Pia's transport/mesh never started and the local station is
  presumably still 0 → lobby data never published → empty join list. Per-user station doc is
  `docs/__pgn/All/__stu/<uss>` (keys suid/susid/sussid/suscid/pl; reader 0x7b57718), which we
  synthesize like the reference server.
  (8) More glue facts: the glue singleton pointer is a static at ELF 0xe582088 (guest 0x16A88088;
  object 0x270 bytes: +0x68 state, +0x6d is-host, +0x6f update-pending, +0x70 backend mode). The
  glue's create (0x1ac5070, state 4→7) calls Pia `Session::CreateSessionAsync` (0x760b160) with a
  setting that carries NO application data — so the 92-byte create-time blob is expected, and lobby
  data can only arrive via the later UpdateNetworkProperty flush. That NPLN job (0x7ab5f30) writes
  `prp._Pia_SystemData` plus one bool field (key at ELF 0x985d086) and has no other wait; we never
  saw it, so the flush never ran. Max application data is 0x1a4 bytes (header getter 0x7703328
  returns 0x5c, max-app getter 0x7703330 returns 0x1a4). Lobby key literals present in the binary:
  farmName, farmType, date, farmhands, newFarmhands, protocolVersion, privacy, hostName, serverName,
  gameVersion, password, players, Cabins.
  LIVE-TEST TOOL (read-only, no freeze): `/mnt/media/nextendo-research/scratch/tools/readsess.py
  <host-pid>` (run with sudo) finds the guest→host base and prints the glue state and the Session
  local/host station pairs while the host is hosting. NOTE: the host emulator process that was
  running at session start had already exited the game (log: game-exit at 00:11:19), so no live
  read was possible this session; also its whole-run log shows only the 2 NAT-check UDP sockets —
  no Pia transport socket was ever created while hosting.
  NEXT: (a) when the user hosts again: run readsess.py on the host pid and watch the server log for
  a WriteDocuments carrying `prp` (that is the lobby-data flush); (b) find what assigns the local station under NPLN
  (the `__stu` reader / mesh creation) and what our server must return so the host becomes station
  host (then it will publish `prp._Pia_SystemData`, which we must store AND mirror into the
  session's properties for QueryGameSessions); (c) LAZY FALLBACK if (b) is deep: append the
  `key\nvalue\n` app data server-side to the blob we return (keys farmName/protocolVersion/privacy…
  from the lobby dictionary; exact key set still to read from the C# side).

- 2026-09-02 (session 3, END STATE): with the consolidated setup fully working — verified shared
  accounts, correct routing, host hosting ONE clean farm (dedup fix live), joiner querying and getting
  exactly 1 session — the joiner's Co-op → Join list is STILL EMPTY. Socket-level proof: after the
  query the joiner opens NO connection (no 18501, no STUN/UDP, nothing to the host addr) — it builds
  the list SYNCHRONOUSLY from the query response. The response's only app data is the host's 92-byte
  `_Pia_SystemData` blob, which carries just Pia flags + the host nickname ("OutboundHost"), NO
  farmName / protocolVersion / privacy. So the joiner has no lobby data → no `CoopMenu.FriendFarmSlot`.
  THE ONE REMAINING BLOCKER: the host does not advertise Stardew lobby data anywhere the joiner reads.
  NEXT: determine why a real host publishes no lobby data on our stack — decode the `_Pia_SystemData`
  blob's application-data region (the ~52 trailing zero bytes may be where farmName/protocolVersion/
  privacy belong, unpopulated under emulation), or find whether Stardew sets lobby data via a Pia
  session-property call we don't capture. protocolVersion value = `1.6.15`; farm name observed = "Test".
  FIXED THIS SESSION: (1) "new farm every load" — the store now evicts a host's prior sessions on
  Create (dedup by host; `internal/npln/sessions.go`), so QueryGameSessions returns one farm per host,
  not N stale duplicates. (2) Ryujinx profile consolidation (below). HOST-QUIT FREEZE diagnosed: on
  exit-to-title the host cleanly closes its gamesync KeepUserSession stream (the only server signal —
  NO save RPC / cloud-save call), then the GAME UI hangs while the emulator idles (PTC saves + a BSD
  poll loop, a thread ~92% CPU, no exception). It is a game/emulator online-session-teardown hang, not
  a server-handleable save — nothing to implement server-side. `save://Test_<id>` in the guest log is
  the farm's normal save at CREATION, not an exit signal.

- 2026-09-02 (session 3, RYUJINX PROFILE CONSOLIDATION): the family moved from a Ryujinx data dir per
  game to **two shared profiles for all games** — host `~/ryujinx-instances/host` + joiner
  `~/ryujinx-instances/joiner` — signed into the family-wide shared accounts **`OutboundHost`
  (1800000003)** / **`OutboundJoiner` (1800000004)** (per `shared-docs/conventions.md`, set by the
  citron consolidation; names historical, treated as generic host/joiner). New family launchers
  `shared-docs/scripts/launch-ryujinx-{host,joiner}.sh` (engine `launch-ryujinx.sh <host|joiner>`);
  `scripts/launch-ryujinx.sh` is now a thin wrapper (`--joiner`, `--menu`) that bakes in Stardew's NSP
  + route via `NEXTENDO_ROUTE`. The shared profile's `nextendo_routes.env` accumulates one line per
  game (launchers ensure, never overwrite). Account migration DONE (2026-09-02): `OutboundHost`
  (1800000003, u-s5jwdwpyopejkzvxcsjq) and `OutboundJoiner` (1800000004, u-rxroqs444xrkmhpny2na) are
  mutual friends AND now VERIFIED (verified via `GET /api/verify?token=…`, an HMAC token bound to
  id+email under `NEXTENDO_SECRET`; recipe in `shared-docs/ryujinx-isolation.md`), so Stardew's auth
  gate passes for them. Remaining profile TODO (emulators closed): seed the two profile dirs and sign
  them into these accounts. Old dated entries below still name the
  per-game dirs (`~/ryujinx-instances/stardew`, `stardew-join`) and the old accounts — historical.
  Docs updated: `shared-docs/ryujinx-isolation.md`, `emulator-launch-protocol.md`, `conventions.md`
  (citron agent's), `README.md`, `docs/local-stack.md`.

- 2026-09-02 (session 3, MECHANISM): the join-list entry is `CoopMenu.FriendFarmSlot` built from
  `CoopMenu.FriendFarmData`, filled via `RequestFriendLobbyData` → `CoopMenu.LobbyUpdateCallback`. The
  lobby data is a SERIALIZED object (there is a generated `ServerPrivacySerializer`; the field-descriptor
  table at ELF 0x72bb0b4 lists the serialized fields) with fields `farmName`, `hostName`, `serverName`,
  `protocolVersion`/`gameVersion`, `privacy` (enum `Public`/`FriendsOnly`/`InviteOnly`), `Cabins`.
  CRUCIAL server-log finding: on our stack the HOST never publishes ANY of this — its only session
  property is `_Pia_SystemData` (92 mostly-zero bytes, no farmName/version), it makes NO SyncGameSession
  / property-update call, its gamesync WriteDocuments are only participant docs (`__us`,`__pus`,`__stu`
  with fields pgn,pusa,ucsid,uid,upcsid,ussid), and there are ZERO UNIMPLEMENTED calls. So the joiner
  has no lobby data to build an entry from → empty list. TWO hypotheses for where the lobby data is
  meant to flow: (A) it is exchanged P2P — `RequestFriendLobbyData` connects to the friend's Pia
  session and the host sends serialized lobby data over it; under one-box emulation the Pia connection
  can't form, so the joiner (which makes NO RPC after the query) never gets it → the join list and the
  P2P/relay milestone are the SAME gate; (B) the host only advertises lobby data once its farm is
  actually joinable in-game (a built cabin / `enableFarmhandCreation` / `serverPrivacy` != InviteOnly),
  which may not be set. NEXT (needs the emulator, currently in use for Outbound): verify the host farm
  is in-game joinable (cabins/privacy) BEFORE more protocol work; then determine the lobby-data channel
  by decompiling `RequestFriendLobbyData`/`GetLobbyData` (AOT C#; key literals are code-referenced —
  protocolVersion@0x8387218←fn0x72aff9c, privacy@0x8461ffa←fns 0x72be798/0x72c3144/0x72daf48/0x7360590,
  serverName@0x9319100←fn0x72bb0b4, hostName@0x88d0aee, farmName@0x8383ad0←fn0x7065ad0). If it turns
  out lobby data rides GameSession properties, the fix is server-side (advertise the keys); if P2P, the
  relay/mesh must work first. Clean decompiles in ~/ghidra-projects/out3/.

- 2026-09-02 (session 3, ROOT CAUSE narrowed to Stardew lobby-data): drove the joiner to the co-op
  **Join** tab (screenshot: two tabs Join/Host, an empty list box, a Refresh button — a browse-based
  list) and confirmed it stays empty while the server returns valid farms (loopback host + duplicate
  count already ruled out; 1 clean farm also empty). Mined the game binary's UTF-16 C# symbols (ASCII
  `strings` misses them; use `strings -e l`). The join list is built from Stardew **lobby data**, not
  the NPLN GameSession alone: symbols `RequestFriendLobbyData`, `GetLobbyData`, `GetLobbyOwnerName`,
  `GetLobbyFromInviteCode`, `AddLobbyUpdateListener`, `canOfferInvite`/`offerInvite`, plus
  `CheckProtocolSupport` / `CompareGameVersions` / `get_ProtocolVersion` / `protocolVersion` and a
  `ServerPrivacy` enum (`Public`/`FriendsOnly`/`InviteOnly`). To render a friend's farm the joiner
  needs that friend's lobby data — farmName, protocolVersion, serverPrivacy, player counts — which our
  `QueryGameSessions` response does NOT carry (the host's Create advertised only `_Pia_SystemData`, and
  our echoed blob is 92 mostly-zero bytes). The joiner's trace stops right after the query (no gamesync,
  no GetDocument), so it is NOT fetching lobby data over the session either. So joining needs the
  lobby-data layer, a level ABOVE the NPLN session/filter the project was stuck on. NEXT: find where
  Stardew expects the lobby data — is it a GameSession property beyond `_Pia_SystemData`, inside the
  `_Pia_SystemData` blob, or fetched by the joiner (RequestFriendLobbyData) via gamesync docs / P2P?
  Start from `RequestFriendLobbyData`/`GetLobbyData` in the binary. Candidate quick test: also verify
  the HOST farm is actually joinable in-game (cabins built / `enableFarmhandCreation` / `serverPrivacy`
  not InviteOnly) — `BuildStartingCabins`, `availableFarmhands`, `CAN_BUILD_CABIN` gate joinability
  host-side regardless of the network.

- 2026-09-02 (session 3, loopback RULED OUT): set the advertised `host` to a DISTINCT address
  (`NPLN_RELAY_HOST=127.0.0.2` in nextendo-local `.env`, rebuilt only the `stardew` container; listener
  is `*:18501` so it still reaches the server; `.env.bak-hosttest` backup left). Host re-hosted (Create
  session=3eaeb752 @ 127.0.0.2:18501, full flow OK), joiner queried and got exactly 1 clean farm
  (`host="127.0.0.2"`), and the join list was STILL empty with no follow-up. So "game hides self/
  loopback-hosted farms" is FALSE. Also confirmed: friend linkage works (the joiner's QueryGameSessions
  `users=[hostUID]` matched the farm, so the joiner knows the host as a friend). The game's own stdout
  (Ryujinx `ServiceLm` Guest Log, ProgramName StardewValley) logs boot noise + `[Nextendo][notif] 1
  friend(s), 1 in a game` but NOTHING about rejecting the session. Blocker is squarely in the game's
  (or Pia's post-matcher) session-list handling. NEXT unchanged: bisect empty-vs-nonempty SDK browse
  result (memory capture), then RE the game's join-list consumer if non-empty.

- 2026-09-02 (session 3, live trace): **The game RECEIVES the farms and sends NO follow-up.** Passive
  server-log watch of a real join attempt: `IssuePrearrangedUserToken×2 → ActivateUser →
  SubscribeFriendUsers → QueryGameSessions (4 sessions returned) → (nothing)`. No `JoinGameSession`,
  `GetGameSession`, or gamesync follow. So the returned farms never become selectable — the blocker is
  after the query, in the browse-result → join-list path. All farms advertise `host="127.0.0.1"` (the
  joiner's OWN address on this one-box test) — a prime suspect for the game treating them as self /
  unreachable. NEXT: one capture to bisect — read whether Pia's browse handed the game an EMPTY list
  (post-matcher Pia drop) or a NON-EMPTY one (game-UI drop); and try a non-loopback `host` server-side.

- 2026-09-02 (session 3, runtime): **THE SEARCH FILTER IS NOT THE BLOCKER — our farm PASSES it.**
  Ran the freeze-capture (`capsession.sh`) against a live joiner searching in Co-op. Read OUR session's
  Pia session-info object directly: `obj+0xa0 = blob[0x16] = 1`, `obj+0xa2 = struct[0x30] = 4`
  (= MaxParticipantCount — resolves the field id: struct[0x30] is MAX, not current), `obj+0xa6 =
  struct[0x40] = 1`. Hand-evaluating the matcher (Farm8, bits 3+4): bit4 vacancy `struct[0x30](4) >
  blob[0x16](1)` and `4 != 1` → PASS; bit3 `struct[0x40](1) & 1` odd → PASS. So `WaitSearchNetwork`
  KEEPS the farm. The join list is STILL empty, so the blocker is DOWNSTREAM of the search filter, not
  the filter. Server returned 4 well-formed sessions (stale duplicates the in-memory store accumulated
  across the host's re-hosting; not variants) — all 4 kept by the client. Presence is still never
  called, so it is not a presence gate the client asks for. NEXT: the post-filter path —
  `session::BrowseSessionJob::CompleteProcess` / the result list Pia hands the game, OR the game's own
  C++ join-list logic, OR an unverified criteria bit (read the criteria object at mgr+0x1130 to confirm
  only bits 3+4 are set). Joiner emulator crashed ~3m48s in on a PTC cache save race (not OOM; a stale
  second process on the join dir held `1.6.15.13-default.info`) — cleaned, relaunch is safe. Capture
  tooling proven end-to-end: `/mnt/media/nextendo-research/scratch/capsession.sh <joinpid>` freezes
  the joiner 120ms after its query and reads the session-info object off the guest RAM.

- 2026-09-02 (session 3): **CONTRADICTION RESOLVED — the decompiled join filter is Pia's WAN session
  search, not the gRPC QueryGameSessions display path.** Re-derived true function boundaries from the
  binary's `.eh_frame` unwind tables (the prior analysis used manually-created Ghidra functions with
  bad stack bounds), re-decompiled cleanly, and identified the function by its OWN diagnostic string +
  vtable: `FUN_07bb3d30` (Ghidra) = ELF-VA 0x7ab3d40 = **`nn::pia::npln::NplnBackgroundProcessJob::
  WaitSearchNetwork`** (sibling `WaitSearchNetworkBySessionId` at 0x7ab4900). It filters PIA session
  objects (0x98-byte structs) against a Pia `NetSessionSearchCriteria`, reading mostly the
  `_Pia_SystemData` blob + a couple of Pia-struct fields — NOT the protobuf GameSession numeric
  fields. THAT is why ~40 server-side protobuf variants all failed: the join filter never reads
  max/current/is_public off the wire. Live-confirmed the host blob advertises `blob[0x16]=1`
  (`_Pia_SystemData = 00 5c 16 00…(zeros)…01 01 00 00 00 0b 01 "stardewhost"`). Bit-4 of the matcher
  is a vacancy test: KEEP needs `struct[0x30] > blob[0x16]` (i.e. > 1); bit-3 needs `struct[0x40]` odd.
  Suspected root cause: a solo host (blob[0x16]=1) reads as "no vacancy" and is dropped. ONE UNKNOWN
  LEFT: whether Pia-struct[0x30] is max (keep) or current (drop), settled by a single runtime freeze-
  read of OUR session while the joiner searches (needs the user to drive the Join menu). Corrected
  addressing note: **Ghidra addr = ELF-VA + 0x100000; the prior "module VA" values were Ghidra addrs,
  so the real ELF functions sit 0x100000 BELOW them** (e.g. matcher 0x76ecee4→ELF 0x75ecee4). See
  Experiment 2026-09-02 (session 3).

- 2026-09-02: **Own NPLN server (`cmd/npln`) implements auth + friends + game-sessions + gamesync and drives Stardew through authentication and FARM HOSTING end to end** on the local podman stack (Ryujinx client). JOINING is the single open blocker: the joiner's `QueryGameSessions` returns the friend's farm, the client receives it complete (verified in guest memory), but filters it out before the Join list. The client filter was decompiled to exact logic, but a contradiction (our sessions *should* list per the decompile, yet ~40 variants failed) points to the analyzed function likely being a sibling of the true display path. See Experiment 2026-09-02.

- 2026-08-31: The workspace was found completely empty and was not a Git repository.
- 2026-08-31: A minimal Go research scaffold and protocol-neutral TCP/UDP observer were implemented and exercised with synthetic traffic.
- 2026-08-31: An empty Git repository was initialized after the scaffold was created; no commit was made.
- 2026-08-31: Stardew's startup reaches NNCS NAT checks and the NPLN tenant over redirected networking. DNS, TCP, SNI, TLS server-flight behavior, and the certificate-trust failure were measured.
- 2026-08-31: A clean-room, exact-build Citron compatibility patch crossed the TLS trust boundary. Stardew now completes the client handshake and sends its first encrypted application record to Nextendo, then immediately closes. The first HTTP/2/gRPC method remains unknown.
- 2026-08-31: A controlled loopback TLS-termination probe proved the client's first encrypted record is a pre-HEADERS cancel flight (preface/SETTINGS/ACK/RST/WINDOW_UPDATE), not an HTTP/2 request. The client cancels its first RPC before transmission regardless of server behavior; the blocker is client-local, upstream of the wire protocol.
- 2026-09-01: The local Nextendo stack is complete on podman (`nextendo-local`: account 8099, npln 18500, baas-jwks 18448, nncs UDP 10025/10125, website/dashboard). Per-title launch wrappers (`scripts/launch-citron.sh`, `scripts/launch-ryujinx.sh`) pin every host to it and isolate the emulator profile; runbook in `docs/local-stack.md`. Next: sign the emulator into the local account and rerun the online attempt (identity-consistent test).
- 2026-09-01: FIRST AUTHENTICATED STARDEW SESSION on the local podman stack (Ryujinx + local account `stardewhost`): `Auth/IssuePrearrangedUserToken` PASSED, then `Friends/ActivateUser`, `Friends/SubscribeFriendUsers`, then `GameSessionService/QueryGameSessions` → server UNIMPLEMENTED → client 2321-4224. The Stardew-specific RPC surface starts at `QueryGameSessions`; that is the next handler to build. Under citron the same identity still cancels pre-HEADERS (2321-4992): emulator gap, not protocol.
- 2026-09-01: Own NPLN server written (`cmd/npln`), deployed as `stardew` container on 18501, tenant route switched to it. Auth/Friends/GameSessionService implemented per reference shapes; every session RPC dumps its request (DIAG) so hosting/joining can be built from what Stardew actually sends.
- 2026-09-01 (evening): HOSTING WORKS on our server. Host flow measured end to end: `QueryGameSessions` (search config `Farm8Player_GameSessionSearchConfigs`, view BASIC, min_vacancy 1, page_size 1) → `CreateGameSessionCreationTicket` (matchmaking config `Farm4Player`, one `_Pia_SystemData` bytes property carrying the host name) → Track → session transport dialed with SNI `gamesync.npln.nintendo.net` (cert must cover it; that SAN was the gate) → `Gamesync/IssueToken` → `KeepUserSession` watching `docs/__us/<uss>`, `docs/__pgn/All/__stu/<uss>`, `docs/__stg/All` and collection `docs/__pgn/All/__pus` → host writes its `__pus` member (pgn,pusa,ucsid,uid,upcsid,ussid) → `AllocateIceServerSet` → `GetDocument docs/__gs/f` → the farm loads. Joining: second account `stardewjoin` (pid 1800000007) + second Ryujinx data dir `~/ryujinx-instances/stardew-join`; STUN-only coturn added to the stack (`stun` profile, 127.0.0.1:3478) for ICE.
- 2026-09-01 (late): JOIN LIST STAYS EMPTY. The joiner (`stardewjoin`) queries `QueryGameSessions` with `users=[host]` (page_size 20, view BASIC, `Farm8Player_GameSessionSearchConfigs`) and receives the host's ACTIVE session, yet lists nothing and sends no further RPC. Twelve response variants (user path form, no user_sessions, no properties, LAN host address, name under tenants/current, is_public false, capacity 4..8, friend relationship flags, Nintendo's `_BaseConfigName`/`_AliasSuffix` system properties, config echoed with concrete tenant) all produced an empty list — the gate is client-side and not in those fields. Next: decompile the client's `_Pia_SystemData` / QueryGameSessions consumers (Ghidra project `~/ghidra-projects/sdfull`, script `~/ghidra-projects/decomp_query.py`, output `~/ghidra-projects/out/`).
- 2026-09-01 (night): JOIN FILTER LOCALISED by decompiling the exact build (Ghidra project `~/ghidra-projects/sdfull`). The joiner's QueryGameSessions result consumer is main `FUN_07bb3d30`; it deserialises each session's `_Pia_SystemData` bytes property (accepts length 0x5c..0x200; the host's blob is exactly 0x5c=92 bytes, so it passes) and then filters with a criteria matcher (`0x76ecee4`). For the QueryGameSessions path the criteria (built in `0x1bc23b8` from `Farm8Player_GameSessionSearchConfigs`) sets **bit3** and **bit4** (setters `0x76edbfc`/`0x76edc0c`), NOT bit1/bit2. The matcher's bit3/bit4 read the session via VIRTUAL methods (`vtbl+0x30 &1`; `session+0xa2` vs `vtbl+0x20`/`vtbl+0x38`, plus `param_3` from `*(*(mgr+0x80)+0x510)`, default 1) on a session-info object built from the BLOB, not from the wire GameSession. Twenty server-side variants (config name in every form, max_participant 2..9, current 0/1, is_public, user_sessions present/absent, properties present/absent) ALL produced an empty join list — because the deciding values come from the host's opaque `_Pia_SystemData`, which we echo unchanged and cannot vary from the server. Presence is never called (0 Presence RPCs), so the list is not presence-driven. NEXT (heavier): runtime memory inspection of the joiner at the filter moment — read the criteria object (`mgr+0x1130`) and one session-info object (`uStack_658`) to capture the real bit3/bit4 comparison values; or decode the Pia `_Pia_SystemData` layout (92-byte struct: byte[1]=0x5c length, byte[2]=0x16, host name "tobagin" at ~offset 0x1a) to learn which field the host must advertise as "joinable/has-vacancy". Decompiled functions saved under `~/ghidra-projects/out/`.
- 2026-09-01 (late night): join filter DECOMPILED precisely but NOT yet defeated. In main `FUN_07bb3d30` the returned sessions are copied into 0x98-byte structs (converter `fn_07bb79a0`) and matched by `fn_076ecee4` against criteria built in `0x1bc23b8` from `Farm8Player_GameSessionSearchConfigs` (criteria bits 3 and 4 set; bit1/bit2 not). Session-info getters: vtbl+0x10=`*(u64)obj+0x98` (config), +0x20=`*(u16)obj+0xa0`, +0x30=`*(u8)obj+0xa6`, +0x38=`*(u16)obj+0xa4`; matcher also reads `*(u16)obj+0xa2` directly. Object fields are filled: obj+0xa0/0xa4 = blob byte 0x16 (`bStack_45a`), obj+0xa2 = converter-struct[0x30] (wire), obj+0xa6 = converter-struct[0x40] byte (wire). So the two live checks are **bit4 KEEP: wire[0x30] > blob[0x16]** and **bit3 KEEP: wire[0x40] low byte odd**, plus a pre-gate `wire[0x38]!=0`. BUT: ~36 server-side variants — zeroing blob[0x16], sweeping max/current/port/state/flags/host, every config-name form — ALL still list nothing, including a kitchen-sink variant. So either `FUN_07bb3d30` is NOT the QueryGameSessions display path, or the SDK GameSession offsets (0x30/0x38/0x40) map to different proto fields than assumed (need the QueryGameSessionsResponse→GameSession parser's field→offset map). Runtime confirmation is BLOCKED: Ryujinx runs non-dumpable (`/proc/<pid>/{maps,mem}` root-owned), ptrace_scope=0 but no passwordless sudo; citron is dumpable but citron can't get Stardew online (2321-4992). Decompiled fns under `~/ghidra-projects/out/` (vm_10/20/30/38 = the getters, parse, FUN_07bb3d30 site fns). NEXT: (a) decompile the QueryGameSessionsResponse/GameSession protobuf parser to get the true SDK field offsets, or (b) enable Ryujinx guest GDB stub (config `GdbStubPort=55555`) to read guest RAM at the filter, or run the joiner under a dumpable wrapper / with sudo memory read.
- No joining, peer connectivity, or gameplay traffic is confirmed yet.

## Architecture

Since 2026-09-01 the main component is `cmd/npln` + `internal/npln`: Stardew's own NPLN gRPC/TLS server (Auth with nnex-proof identity and ES256 access tokens, Friends from the Nextendo graph, GameSessionService with an in-memory farm-session store that logs every request in full while the flow is measured). Generated NPLN protobuf bindings live in `proto/` (from the family's reference server, notice preserved). Deployed as the `stardew` container (18501) of `nextendo-local`; the reference `splatoon-3` server stays the model for gamesync and P2P.

The earlier research component is a dependency-free Go process with independent TCP and UDP listeners and a shared structured-event sink. Socket handling is separate from event serialization. It reads a bounded amount of traffic, logs byte counts but not contents or remote addresses, and sends no guessed response. Service modules will be introduced only when observations justify them.

## Environment

- Workspace: `/home/tobagin/REPOS/stardew-nextendo`
- Session date: 2026-08-31
- Nearby Nextendo-related repositories exist and are being reviewed only for independently written, license-compatible infrastructure concepts. No code has been copied.
- Local stack (2026-09-01): `~/REPOS/nextendo-local` on podman — see `docs/local-stack.md` for ports, identity chain, launch and verification. Emulator profiles (2026-09-02: consolidated to two SHARED host/joiner profiles for all games — see the Current Status consolidation note): Ryujinx `~/ryujinx-instances/{host,joiner}` (`--root-data-dir`, via `shared-docs/scripts/launch-ryujinx-{host,joiner}.sh`); citron `~/.local/share/nextendo-citron/{host,joiner}`. (Earlier this project used per-game dirs `~/ryujinx-instances/stardew` + `stardew-join`.)

## Discoveries

### Repository baseline

- **CONFIRMED:** The project directory initially contained no files or directories.
- **CONFIRMED:** The project directory initially had no Git metadata.
- **CONFIRMED:** Neighboring workspaces include several Nextendo-related projects; their relevance and licensing have not yet been assessed.
- **CONFIRMED:** `../fallguys-nextendo` is an MIT-licensed, independently described clean-room Go project using a metadata-only HTTP observer as its first milestone.
- **CONFIRMED:** `../outbound-nextendo` documents a different game's Photon-specific behavior. It explicitly warns against assuming Nintendo NEX; none of its Photon findings are evidence about Stardew Valley.
- **CONFIRMED:** No Stardew-specific infrastructure or observation was found in the empty project directory.

## Protocol Notes

- **CONFIRMED:** Stardew uses the Nintendo NPLN control plane over TLS on TCP 443 and negotiates HTTP/2 when independently probed.
- **CONFIRMED:** NNCS NAT-check traffic uses UDP ports 33334 and 10025 with 16-byte datagrams in the observed startup flow.
- **CONFIRMED:** Stardew's embedded NPLN client negotiates TLS 1.2 with `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256` and ALPN `h2`, sends client SETTINGS including a Nintendo-custom ephemeral id `0xfe03`, ACKs server SETTINGS, and then cancels its first RPC before transmitting HEADERS — regardless of server SETTINGS content or application responses (see Experiment 2026-08-31-7). The application protocol is expected to be HTTP/2/gRPC from public NPLN documentation, but no Stardew HEADERS/service/method has yet been sent to any server.
- **CONFIRMED:** No account/BAAS/token host resolution occurs in the observed flow; the NPLN tenant is contacted directly after NNCS NAT checks.

## Hostname / Service Inventory

- `nncs1-lp1.n.n.srv.nintendo.net` — **CONFIRMED**, NNCS/NAT-related lookup.
- `nncs2-lp1.n.n.srv.nintendo.net` — **CONFIRMED**, NNCS/NAT-related lookup.
- `g2122d301.lp1.p.srv.nintendo.net` — **CONFIRMED**, game/P2P-monitoring identifier; exact role remains unknown.
- `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net` — **CONFIRMED**, Stardew NPLN tenant endpoint over TCP 443/TLS.

## Endpoint Inventory

Confirmed on the wire (Ryujinx, local stack, 2026-09-01), in order after the NNCS NAT checks:

1. `/nn.npln.auth.v1.Auth/IssuePrearrangedUserToken` — **CONFIRMED**, tenant `t-9f607adf-lp1`, sent on two parallel connections; accepted once the `nnex` claim resolves to a verified local account.
2. `/nn.npln.friends.v1.Friends/ActivateUser` — **CONFIRMED**, bearer token from step 1.
3. `/nn.npln.friends.v1.Friends/SubscribeFriendUsers` — **CONFIRMED** (0 friends, 4-byte message).
4. `/nn.npln.matchmaking.v1.GameSessionService/QueryGameSessions` — **CONFIRMED** and IMPLEMENTED. Joiner sends it with `users=[friend]`, view BASIC, `Farm8Player_GameSessionSearchConfigs`, min_vacancy 1, page_size 20.
5. Hosting flow **CONFIRMED end to end** on our server: `CreateGameSessionCreationTicket` (matchmaking config `Farm4Player`, one `_Pia_SystemData` bytes property) → `TrackGameSessionCreationTicket` (PENDING→SUCCEEDED) → session transport dialed with SNI `gamesync.npln.nintendo.net` → `Gamesync/IssueToken` → `KeepUserSession` (watches `docs/__us/<uss>`, `docs/__pgn/All/__stu/<uss>`, `docs/__stg/All`, collection `docs/__pgn/All/__pus`) + `WriteDocuments` → `AllocateIceServerSet` → `GetDocument docs/__gs/f` → farm loads and stays hosted.
6. Joining: the query returns the farm, but the client filters it out before display (see the join-filter finding in Current status). OPEN.

## Authentication Flow — CONFIRMED

Ryujinx (the working client; citron still cancels pre-HEADERS 2321-4992 — emulator gap). Order:
1. NNCS NAT checks (UDP 10025/10125), answered by the local `nncs` container.
2. Two parallel TLS/h2 connections to the tenant host `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net`
   (routed to our `stardew` NPLN server on 18501). Each sends
   `Auth/IssuePrearrangedUserToken` with an `ExternalIdToken` whose `nsa_id_token` field is the
   emulator's BAAS id_token. That id_token carries an `nnex` claim = the `nx2.` token
   nextendo-account HMAC-signed (`pid.username.expiry`) with the shared `NEXTENDO_SECRET`.
3. Our server verifies the `nnex` HMAC → PID, calls the account server
   `/internal/npln-friends?pid=` → requires the account be verified, and mints an ES256 access
   token (Nintendo shape: `npln.authorization.allow=["**"]`, `ext_id=<pid hex>`, `tid`). The client
   echoes it as `authorization: bearer` on every later call.

Identity gate (measured): not signed in → 2321-4992 (pre-auth cancel); signed in to a DIFFERENT
account deployment (production) → 2321-5760 (UNAUTHENTICATED, token unprovable); signed in to the
SAME local account the NPLN server validates against → PASS. `stardewhost` (pid 1800000005) is the
verified local test account.

## Startup Flow — CONFIRMED

Boot resolves `nncs1/nncs2` (NAT), `g2122d301.lp1.p.srv.nintendo.net` (resolved, never dialed), and
the NPLN tenant. With both build-scoped patches applied (X509 chain + certificate-acceptance flag),
TLS completes and the client reaches the gRPC layer. Ryujinx holds the first NPLN resolution until
the JIT burst calms (`MaybeDelayNplnInit`). The BAAS `jku` fetch
(`e0d67c50…baas.nintendo.com/1.0.0/certificates`) is served by the local `baas-jwks` (same signing
key the emulator uses).

## Multiplayer Menu Flow — CONFIRMED

Opening Co-op re-runs auth (two `IssuePrearrangedUserToken`), then `Friends/ActivateUser`
("tenants/current/users/current"), `Friends/SubscribeFriendUsers` (held open, keep-alive), then
`GameSessionService/QueryGameSessions`. The joiner's query carries
`users=[<friend uid>]`, view BASIC, `game_session_search_config =
tenants/current/gameSessionSearchConfigs/Farm8Player_GameSessionSearchConfigs`, `min_vacancy_count=1`,
`page_size=20`. The game re-authenticates each time the menu is opened. Presence
(`nn.npln.friends.v1.PresenceService`) is NEVER called (0 RPCs) — the join list is not NPLN-presence
driven.

## Farm Hosting Flow — CONFIRMED end to end

`QueryGameSessions` (0 results) → `CreateGameSessionCreationTicket` (request carries only
`matchmaking_config = tenants/current/matchmakingConfigs/Farm4Player`, one `UserDefinition` with the
caller, and `game_session.properties{_Pia_SystemData: <92-byte blob>}`; NO other fields, NO is_public,
NO counts) → server returns the ticket PENDING then `TrackGameSessionCreationTicket` streams
PENDING→SUCCEEDED with the full GameSession (host/port, our synthesized fields) → the client dials the
session transport with SNI `gamesync.npln.nintendo.net` (our npln cert must cover that SAN — this was
a real gate) → `Gamesync/IssueToken` (exchanges the matchmaking id-token for a gss token) →
`KeepUserSession` bidi stream watching `docs/__us/<uss>`, `docs/__pgn/All/__stu/<uss>`,
`docs/__stg/All`, and collection `docs/__pgn/All/__pus`; the host `WriteDocuments` its `__pus` member
(fields pgn,pusa,ucsid,uid,upcsid,ussid) → `AllocateIceServerSet` (we return STUN 127.0.0.1:3478) →
`GetDocument docs/__gs/f` → the farm loads and stays hosted. The `_Pia_SystemData` blob is the host's
Pia session descriptor (92 bytes: byte[1]=0x5c total length, byte[2]=0x16, bytes[0x15..0x16]=01 01,
then a length-prefixed account name).

## Farm Discovery Flow — PARTIAL

The joiner's `QueryGameSessions(users=[friend])` reaches our server and we return the friend's farm
(server-side user filter matches). The client receives a complete, correct
QueryGameSessionsResponse (verified by reading the joiner's guest memory — the wire bytes are intact:
name, max=4, current=1, can_participate=1, is_public=1, state=ACTIVE, host=127.0.0.1, port=18501,
`_Pia_SystemData` blob, one user_session for the host). BUT the client filters the farm out before
display — see Farm Join Flow and Experiment 2026-09-02.

## Farm Join Flow — OPEN (single remaining blocker)

The returned farm never appears in the joiner's Join list. Decompiled the client's filter to the
exact logic (Experiment 2026-09-02) but a CONTRADICTION remains: the decompiled checks, worked
through by hand, say our sessions should list, yet ~40 server-side variants (every wire field, config
name, blob-byte mutation, and a kitchen-sink) all produced an empty list. Most likely the analyzed
function (`FUN_07bb3d30`) is a sibling of the true display path, not the path itself. Next step is to
CONFIRM the actual QueryGameSessions response-callback before any more server changes. Also unruled-
out: the Join UI may gate on the friend showing as "playing Stardew" via account presence.

## Invitation Flow

Unknown.

## Peer Connectivity — not yet reached

Not reachable until joining works. Hosting allocates an ICE server set (STUN, and TURN if
configured) and the session's `game_session.host:port` points at our relay endpoint (127.0.0.1:18501
by default). The actual Pia peer connection uses the `_Pia_SystemData` descriptor + ICE; standing up
a real relay/STUN/TURN and address rewriting is the milestone after joining. A STUN-only coturn is in
the stack (`stun` profile, 127.0.0.1:3478).

## NAT Traversal

Unknown. Standard STUN/TURN behavior must not be assumed.

## Session Lifecycle — PARTIAL

Host: Create → Track(SUCCEEDED) → gamesync IssueToken → KeepUserSession (held) + document writes.
On quit the host tries to SAVE the farm (cloud save) which is unimplemented → the host emulator
freezes on exit. The server keeps the session in its in-memory map until the ticket is cancelled;
every `stardew` container redeploy drops all in-memory farms (host must re-host after a redeploy).

## Disconnect / Reconnect Behavior

Unknown.

## Experiments

### Experiment 2026-09-02 (session 3): Contradiction resolved — the filter is Pia WAN search, not the gRPC path

Objective: resolve the Experiment-2026-09-02 contradiction (our sessions should list per the decompile,
yet ~40 server variants failed) by confirming the true identity of `FUN_07bb3d30` before any more
server changes.

Method (GUI-free static analysis; external ELF only, no proprietary bytes in repo). Built a small
offline toolchain over the reconstructed `main.elf` (scratch: session scratchpad, not committed):
- `ehfuncs.py` — parses the ELF's `.eh_frame_hdr`/`.eh_frame` to get AUTHORITATIVE function ranges
  (the prior analysis manually created Ghidra functions with bad stack analysis — the source of the
  wrong offsets), plus a numpy BL-scan for exact callers/callees.
- `xref.py` — ADRP+ADD/LDR string-xref scan and per-function referenced-string listing.
- `disas.py` — capstone disassembly with BL targets annotated by eh_frame fn + diagnostic string.
- `gdec.py` — pyghidra decompile that first DELETES the wrongly-bounded overlapping functions, then
  creates the function at the true eh_frame entry and decompiles (outputs in `~/ghidra-projects/out3/`).

Results (all CONFIRMED from the binary + a live server-log blob read):

1. **Addressing bug in the prior notes.** Ghidra imports the PIE at base 0x100000, so `getAddress(0x…)`
   in the old scripts was a GHIDRA address = ELF-VA + 0x100000. The real ELF functions are therefore
   0x100000 BELOW the "module VA" values used before (matcher 0x76ecee4 → ELF 0x75ecee4, filter
   0x7bb3d30 → ELF 0x7ab3d40, converter 0x7bb79a0 → ELF 0x7ab79a0−… etc.). The old decompiles landed
   in the right functions ONLY because Ghidra's base absorbed the offset; the manually-created function
   boundaries (not Ghidra-base) were what corrupted the stack analysis.

2. **`FUN_07bb3d30` is `NplnBackgroundProcessJob::WaitSearchNetwork`.** Its vtable slot lives at
   ELF 0xba5e268 in the `nn::pia::npln::NplnBackgroundProcessJob` vtable group; the function embeds the
   diagnostic string `"NplnBackgroundProcessJob::WaitSearchNetwork"` via its search-starter
   (0x7ab3870). Its sibling (0x7ab4900) is `WaitSearchNetworkBySessionId`. Neither reaches the gRPC
   `QueryGameSessions` client stub (0x7bd2710, whose wrapper 0x7bd26c4 sets state=2) within 4 call
   hops — the gRPC RPC is issued asynchronously by Pia's NplnService/dispatcher, and the response is
   converted into Pia session objects that THIS function then filters. So it IS on the display path,
   but as Pia's session-search consumer, and it filters on Pia-session + blob fields, not wire fields.

3. **Why the 40 variants failed.** The matcher (clean decompile, ELF 0x75ecee4) for the Farm8 criteria
   (bits 3+4 set) reads: bit3 `vm_30(obj)&1` (= Pia-struct[0x40] byte, must be ODD); bit4 DROP if
   `obj+0xa2 == vm_20(obj)` OR `obj+0xa2 < param_3 + vm_38(obj)`, where obj+0xa2 = (u16)Pia-struct[0x30],
   vm_20 = vm_38 = blob[0x16], param_3 defaults to 1. I.e. KEEP needs `struct[0x30] > blob[0x16]` and
   `struct[0x40]` odd. These come from the PIA session object (converter `fn_7ab79a0` copies a 0x98
   Pia struct: name@0, sub-msg-ptr@0x20, two int32@0x30, two int32@0x38, bools@0x40, string@0x48,
   longs@0x68/0x70, user-session vector@0x78) and the `_Pia_SystemData` blob — NOT the protobuf
   GameSession fields the server was varying.

4. **Live blob confirmed.** Host session 74844b5b's `_Pia_SystemData` property in the server log:
   `00 5c 16 00 …(18 zeros)… 01 01 00 00 00 0b 01 "stardewhost" …`. So blob[1]=0x5c(len 92),
   blob[2]=0x16, blob[0x15]=1, blob[0x16]=1, blob[0x1a]=0x0b(name len), then the account name. Bit-4
   thus needs `struct[0x30] > 1`.

Interpretation / suspected root cause: bit-4 is a "has-vacancy" gate. If Pia-struct[0x30] is the
CURRENT participant count, a solo host (1) fails `1 > blob[0x16]=1` and the farm is dropped as full;
if it is MAX (4), it keeps. (Round 8's blob[0x16]=0 attempt was on the WRONG assumption that these
were protobuf fields — it edited the wire, which this filter ignores.)

Decision rule / NEXT (unchanged in spirit, now correctly targeted): do ONE runtime freeze-read of
OUR session's Pia struct[0x30]/[0x38]/[0x40] while the joiner is mid-search, to fix whether
struct[0x30] is max or current and whether an earlier gate fires. That is the single measurement that
turns this into a one-shot fix. It needs the user to open Co-op → Join (the GUI can't be driven by
the agent). Candidate fixes to try once the field is known, in order of laziness: (a) if struct[0x30]
is current-count sourced from the blob, rewrite `blob[0x16]` (or the count byte) server-side so the
solo host advertises vacancy; (b) if it is a protobuf field after all, set it; (c) inject a second
phantom user_session so the count reads > 1. Do NOT run blind server variants — the filter ignores
the wire.

Artifacts: `~/ghidra-projects/out3/` (clean eh_frame-bounded decompiles: filterA/filterB, matcher,
converter, the two search starters, criteria builder, the Pia browse-job chain, the gRPC stub). The
offline toolchain lives in the session scratchpad; no proprietary bytes in the repo.

### Experiment 2026-09-02: Full NPLN server built; join filter decompiled to a contradiction

Objective: with the identity-consistent local stack working (auth passes), implement the whole
online flow in this repo and drive two Stardew instances to host + join + play.

Method and results (all sanitized; raw binaries/dumps stayed external):

1. **Own NPLN server written** — `cmd/npln` + `internal/npln` in this repo:
   - `internal/npln/server.go` — gRPC/TLS server, `npln-grpc-type` header on every reply, keepalive
     enforcement relaxed (grpc-go default sends GOAWAY(ENHANCE_YOUR_CALM) and kills in-flight RPCs),
     an UnknownServiceHandler that logs `UNIMPLEMENTED <method>` (the next thing to build), and a
     conn tracer.
   - `internal/npln/identity.go` — `nnex` HMAC verification → PID, account lookup + verified gate,
     ES256 access/session token minting in Nintendo's claim shape, `callerPID` from the bearer token.
   - `internal/npln/auth.go` — Auth service (IssuePrearrangedUserToken, IssueToken,
     IssueAnonymousUserToken, RefreshToken, ValidateToken).
   - `internal/npln/friends.go` — Friends service (ActivateUser, SubscribeFriendUsers streamed from
     the Nextendo account graph, keep-alive held open).
   - `internal/npln/sessions.go` — GameSessionService (Create/Track/Cancel, Get/BatchGet/Query,
     Join, Sync, ListUserSessions, short aliases, AllocateIceServerSet, ListLatencyMeasurementServers)
     with an in-memory farm store; logs every request in full (`prototext`) while the flow is measured.
   - `internal/npln/gamesync.go` — Gamesync session transport (IssueToken, KeepUserSession bidi
     document-watch stream, WriteDocuments/Commit/GetDocument/ListDocuments/QueryCollectionIds,
     room documents `docs/__gs/{f,m,r,n,ck}` and per-user `__us/__pus/__stu/__stg` in the measured
     Firestore-style MapValue schema).
   - `proto/` holds the generated NPLN protobuf bindings, copied from the family `splatoon-3` server
     (PolyForm, notice preserved in `proto/NOTICE.md`), import path rewritten. NOTE: rewrite ONLY the
     Go import lines, NEVER the embedded rawDesc bytes (doing so corrupts the descriptors — panic on
     init).

2. **Deployed as the `stardew` container** (tcp 18501) of `nextendo-local`; the emulator route table
   points the Stardew tenant at it, keeping the reference `splatoon-3` server on 18500 for S3.
   Three account-server bundle fixes were required for the npln→account internal call to pass its
   guard (see the local-stack doc / Session Log): `NEXTENDO_DATA_DIR=/data`,
   `data/account/internal_net.conf`=`10.89.1.0/24 10.89.1.100` + a static account IP, and NO
   `NEXTENDO_INTERNAL_KEY` on the account service (the npln server sends no X-Internal-Key header).

3. **CONFIRMED working end to end**: authentication (nnex-proof identity), friends, and FARM HOSTING
   including the full gamesync session transport (see the Flow sections). The npln cert had to cover
   SAN `gamesync.npln.nintendo.net` (a real gate; reissued the cert with that SAN + the tenant).
   Two local accounts made friends: `stardewhost` (pid 1800000005), `stardewjoin` (pid 1800000007).
   A STUN-only coturn added (`stun` profile).

4. **JOIN BLOCKER — the returned farm never lists.** The joiner queries with `users=[host]` and gets
   the farm, but the client drops it before display. Ruled out by ~40 server variants (every
   GameSession wire field; every `_BaseConfigName` form; blob-byte mutations; a kitchen-sink): none
   list. Ruled out presence (never called). Confirmed by reading the joiner's guest memory that the
   response is received complete and correct.

5. **Decompiled the client filter** (Ghidra project `~/ghidra-projects/sdfull`; base convention:
   function addrs are module-VA `getAddress(0x…)`, string/data addrs are +0x100000; guest VA =
   0x8506000 + (Ghidra_addr − 0x100000)). The QueryGameSessions result filter is main `FUN_07bb3d30`;
   it copies each session (`fn_07bb79a0`) into a 0x98-byte struct and matches it (`fn_076ecee4`)
   against criteria built from `Farm8Player_GameSessionSearchConfigs`. Struct layout (from the
   converter + helper `cpy_078795c4`, whose 0x20 field is a POINTER to a 0x30-byte sub-message, not a
   string): 0x00 name(std::string) | 0x20 msg-ptr | 0x30 two int32 | 0x38 two int32 | 0x40 bools |
   0x48 std::string | 0x68/0x70 longs | 0x78 vector(user_sessions). The blob is deserialized to
   obj+0x1e8 (length at +0xb0). Session-info getters: vm_10=obj+0x98, vm_20=obj+0xa0, vm_30=obj+0xa6,
   vm_38=obj+0xa4; matcher also reads obj+0xa2 directly. KEEP-prep sets obj+0xa0/0xa4 = blob[0x16],
   obj+0xa2 = (u16)struct[0x30], obj+0xa6 = struct[0x40]. For the Farm8 query the criteria has bits 3
   and 4 set (setters `fn_76edbfc`/`fn_76edc0c`), not 1/2. Live checks:
     - outer gate: `fn_76ed01c(crit)!=1 || struct[0x40]!=0` (crit bit3=1 ⇒ struct[0x40] must be ≠0);
     - pre-matcher vacancy gate: compares blob[0x16] (`bStack_45a`) against `*(mgr+0x1a48)` (GAME-side);
     - `struct[0x38]!=0 || *(mgr+0x1a4c)!=0`;
     - matcher bit3: `struct[0x40] & 1` must be ODD;
     - matcher bit4: KEEP needs `(u16)struct[0x30] > blob[0x16]` (drop if == or < 1+blob[0x16]).
   Remaining field ambiguity: struct[0x30] = max OR current; struct[0x40] = can_participate OR
   is_public. `vm_68` (called first in the matcher) reads blob header bytes (`ed4c0`=blob[2],
   `ed4e4`=BE(blob[3]), `vm_50`=BE(blob[0..1])=length) but its return is IGNORED — no side effect on
   the decision.

6. **THE CONTRADICTION.** Round 8 set `blob[0x16]=0` + current=3 + max=4 + can_participate=true +
   is_public=true. That passes bit4 (struct[0x30] > 0) and bit3 (struct[0x40] odd) under EVERY
   interpretation of the two ambiguous fields, and passes the pre-matcher gate (blob[0x16]=0 ⇒ never
   drops). It STILL did not list. Therefore an anchor assumption is wrong — most likely
   `FUN_07bb3d30` is a sibling routine, not the true QueryGameSessions display path.

Decision rule / NEXT: do NOT run more server variants or refine `FUN_07bb3d30`. First CONFIRM the
real display path — trace the QueryGameSessions gRPC unary response callback that fills the session
vector at `(mgr+0x2800)` — and separately verify whether the Join UI is gated on account presence
("friend playing Stardew"). Then one clean runtime read of the confirmed struct settles the field
ids, and the fix is a one-shot.

Runtime capture method (works, sanitized): passwordless sudo enabled
(`/etc/sudoers.d/<user>-nopasswd`); Ryujinx runs non-dumpable so read its memory as root via
`/proc/<pid>/mem`. Guest RAM is the largest `/dev/shm/Ryujinx-*` shm set (~10 GB); the 512 GB set is
the reserved HostMappedUnsafe address-space mirror (skip it). Ryujinx maps the guest AS at MULTIPLE
host mirror bases — one confirmed base: host `0x7e53ddd616fc` = guest `0x11D616FC` (Farm8 string) ⇒
`host_base 0x7E53CC000000`; pointer-following across mirrors is the only hard part. To catch the
transient parsed response, freeze the joiner ~120 ms after its query (Monitor on the server log +
`sleep 0.12` + `sudo kill -STOP`), scan, then `kill -CONT`. SCRATCH MUST LIVE ON
`/mnt/media/nextendo-research/scratch/` — writing multi-GB dumps to the 15 GB `/tmp` tmpfs hits
EDQUOT and kills the shell tool for the whole session (per the playbook §5). The GDB stub reads guest
memory READ-ONLY but CRASHES the emulator on breakpoints — never set breakpoints.

Tooling: `~/ghidra-projects/out/` and `out2/` hold the decompiled functions (filter, matcher,
converter, getters, blob readers). `/mnt/media/nextendo-research/scratch/scan.sh` and `dump.sh` are
the memory tools.

Artifacts: no proprietary bytes in the repo; the Ghidra project, ELF, dumps, and scratch are all
external. Server code and sanitized conclusions only.


### Experiment 2026-09-01-B: Identity-consistent local run — auth PASSES, Stardew's first own RPC observed

Hypothesis: with the emulator signed into the SAME account deployment the local NPLN server validates against, `IssuePrearrangedUserToken` passes and the next RPCs become visible.

Method: full local podman stack (`docs/local-stack.md`); Ryujinx via `scripts/launch-ryujinx.sh --menu`, signed in to the local account server as `stardewhost` (pid 1800000005), then Stardew 0.20.0 loaded (build E7F845…, both built-in patches applied), Co-op → Online. citron via `scripts/launch-citron.sh`, same account, same attempt.

Observation:

- **CONFIRMED (Ryujinx):** NAT checks answered by the local `nncs`; tenant TLS to the local `npln`; `IssuePrearrangedUserToken` received on two connections, `nnex prouve pid=1800000005`. First attempts were DENIED because the npln→account `/internal/npln-friends` call was refused (account internal guard: rule file not read since `NEXTENDO_DATA_DIR` was unset; then the handler's own `X-Internal-Key` demand, which the npln server never sends) → game 2321-5760 (UNAUTHENTICATED). After fixing the bundle (see local-stack doc) the auth PASSED, `Friends/ActivateUser` and `SubscribeFriendUsers` succeeded, and `GameSessionService/QueryGameSessions` arrived → UNIMPLEMENTED → **2321-4224** in the game.
- **HIGH:** 2321-4224 is the client-side mapping of gRPC UNIMPLEMENTED (server-attributed; the converter table has no entry read for it yet).
- **CONFIRMED (citron):** identical stack and account, patches applied (both logged), NAT checks answered locally, three tenant connections with h2 established and ZERO RPCs → 2321-4992. The pre-HEADERS cancel now follows the EMULATOR, not the identity or the server. Candidates: Ryujinx's gRPC connect repairs (`NEXTENDO_GRPC_CONNECT_SYNC`, lost-address substitution) and its NPLN-init hold, which citron lacks (`NEXTENDO_NPLN_DELAY_MS` exists but was not set).

Decision: Ryujinx is the Stardew test client until citron's gRPC connect path is repaired. Next: log the sanitized request fields of `QueryGameSessions` on the reference server (or the probe), then implement the minimal response in this repository's own service — the first Stardew handler.

Artifacts: sanitized server-log conclusions only; tokens never recorded.

### Experiment 2026-09-01 (cont.): FIRST STARDOW RPCs OBSERVED — identity deployment confirmed as the gate

Correction to the attribution above: the **2321-5760** result was **Ryujinx + Stardew + production Nextendo account + local reference server** (not citron). Ryujinx results: **without** Nextendo login → 2321-4992; **with** Nextendo login → 2321-5760.

Breakthrough (server log, local reference server):

```
CONN begin (h2 established)
RPC begin method=/nn.npln.auth.v1.Auth/IssuePrearrangedUserToken
RPC InHeader remote=127.0.0.1:50566
[NPLN RPC] /nn.npln.auth.v1.Auth/IssuePrearrangedUserToken tenant="t-9f607adf-lp1" uid="" auth=""
[NPLN Auth] jeton NSA non prouvable — le client subsdk (HMAC) est requis en prod -> REFUS
IssuePrearrangedUserToken DENIED ext=nsa:eyJ… : PermissionDenied
  = "Nextendo account not recognised — sign in with your Nextendo account to play online"
```

- **CONFIRMED:** first Stardew NPLN method observed on the wire: `nn.npln.auth.v1.Auth/IssuePrearrangedUserToken`, tenant `t-9f607adf-lp1` — matches the method surface embedded in the binary (experiment 8).
- **CONFIRMED:** the full client chain works end-to-end under Ryujinx once the patches actually apply: patches → TLS → h2 → auth metadata → RPC delivered. The citron-side equivalent remains to be re-tested (citron patches were always applying; its local failures are now attributed to the identity deployment mismatch, same as Ryujinx).
- **CONFIRMED:** the error code maps to the account state: no Nextendo login → 2321-4992 (UNAVAILABLE, pre-auth failure path); Nextendo login → RPC sent → server denial → 2321-5760 (UNAUTHENTICATED).
- **CONFIRMED:** the remaining rejection is identity proof: the `nsa:` NsaIdToken presented by the emulator (signed into the PRODUCTION account server) is not provable against the LOCAL reference server, which requires production-mode HMAC-proof NSA tokens and trusts `NEXTENDO_ACCOUNT_URL=127.0.0.1:8099` (the local account server).

Next steps:

1. **Identity-consistent local test**: sign the emulator into the account server the NPLN server trusts (local: custom-server override / route the account hostname to `127.0.0.1:8099`; Ryujinx currently signs into production). Expect `IssuePrearrangedUserToken` to PASS and the flow to advance to the next RPC (friends/matchmaking/gamesync — the full Stardew RPC surface finally observable).
2. **Production provisioning** (end goal): register Stardew's tenant `t-9f607adf-lp1` and accept its NSA tokens on the Nextendo server, then test Stardew + production end-to-end.
3. Both paths converge on the same deliverable: the Stardew RPC catalog (every method the game calls, in order) — the input for the Stardew server handlers.

### Experiment 2026-08-31-11 / 2026-09-01: The identity deployment is the gate (UNAUTHENTICATED breakthrough)

Hypothesis (user-driven): the NPLN backend validates the client's identity against its OWN account deployment. Stardew was only ever tested with an identity the server does not recognize (locally-fabricated / local-account tokens), which — not TLS, not pinning — is why every server rejected it.

Method and observations:

1. **Patch-application bug found and fixed (Ryujinx).** `ModLoader.ApplyNsoPatches` only invoked the built-in patch tables inside the `ModsInterdits` (Splatoon 3 mods-ban) branch, so Stardew's patches never applied under Ryujinx; the earlier Ryujinx local test was invalid (client died at TLS, server saw nothing). Fixed: built-in tables now apply for every title (build-ID keyed, no-op otherwise). citron was never affected (its `nso.cpp` patch is unconditional for the title).
2. **Citron S3 correction.** citron + S3 + production **always worked**; the single morning failure (2321-4992) was session-specific (region selection Europe vs the supported Americas, and/or account state). The "citron NPLN gap" theory is retracted; the playbooks were corrected.
3. **The account deployment changes the outcome (Stardew, citron, production NPLN, both patches):**
   - local account (`stardewhost`, local `nextendo-account`): **2321-4992** (UNAVAILABLE — cancelled pre-authentication), identically against the local reference server AND production.
   - production account (signed in via production `nextendo-account` after the stored local token was rejected at startup): **2321-5760** — grpc status 16 = **UNAUTHENTICATED** per the same conversion table.

Conclusion:

- **CONFIRMED:** the identity deployment is a real gate. With a recognized identity, Stardew's flow reaches authentication and gets a credential verdict instead of the pre-authentication cancel. The earlier "client-local, environment-independent" interpretation is refined: the failure followed the *identity*, not the server or the emulator.
- **OPEN:** where the UNAUTHENTICATED verdict is produced (production auth rejecting the fabricated token/tenant, or a client-side check against the production identity), and what production needs to accept Stardew: tenant `t-9f607adf-lp1` provisioning and/or per-title BAAS client id (the fabricated `aud` is still S3's `ed9e2f05d286f7b8` in both emulators).
- **NEXT:** (1) read the production NPLN server logs for the rejected call — method, status, `rawContext` (the splatoon-3 server spells the failing method out); (2) provision Stardew's tenant on the Nextendo server (or point the local stack at a fully matching account+NPLN pair) and retest; (3) if production auth still rejects, align the fabricated token's `aud` with Stardew's registered BAAS client id (value TBD from the game/tenant config).

Artifacts: no proprietary material; production log excerpts stay on the operator's side and enter this document only as sanitized conclusions.

### Experiment 2026-08-31-10: Pin-bypass differential, socket-level teardown analysis, and the identity-mismatch hypothesis

Hypothesis:

If the certificate-acceptance flag was the last client-local gate, forcing it (plus the X509 chain patch) lets the first auth RPC reach the server. If the abort persists, the transport teardown is a symptom and the failure lies in the SDK's login orchestration — prime suspect: a local identity cross-check between the fabricated BAAS id_token and the ACC account identity.

Method:

1. Rebuilt citron with both build-scoped patches (X509 0x79B4C10 + flag-read 0x782F5D0); ran Stardew against the local full reference NPLN server on the tapped port with the linked local account.
2. At the failure dialog, scanned guest rw memory for the result constant, grpc failure strings, and runtime materialized SDK diagnostics.
3. Correlated citron's socket-level DIAG logs (send/recv/shutdown per fd) with the server's [stats] CONN lines.

Observation:

- **CONFIRMED (decisive negative):** both patches applied (logged at load); the server logged three "h2 established" connections with ZERO RPCs — identical abort to pre-flag-patch runs. The OpenSSL verification layer is eliminated as the gate.
- **CONFIRMED (socket level):** per connection: ClientHello(213B) → server flight (1412+189+44B reads) → client 126B handshake flight → client 133B ApplicationData → `Shutdown(fd, how=2)` called BY THE GUEST 82-85ms after connect, ~1-3ms after the send. The 133B flight already contains the h2 RST_STREAM(stream=1, REFUSED_STREAM) — the call was cancelled BEFORE the transport finished coming up, and the SDK then shut the socket down itself. The transport (TLS/h2) is a victim, not the cause.
- **CONFIRMED (result mapping):** the grpc-status→nn::Result table at VA 0x98CA1AC (module 321): OK→2000-0, UNKNOWN→2321-384, FAILED_PRECONDITION→2321-3072, INTERNAL→2321-4608, UNAVAILABLE→2321-4992, UNAUTHENTICATED→2321-5760. The observed 2321-4992 is a client-side gRPC UNAVAILABLE — consistent with the SDK shutting down the whole channel, failing the queued auth call.
- **CONFIRMED (runtime diagnostics):** the SDK's auth diagnostic " (Auth RPC result)" (static rodata 0x9885c a6; leading space — appended after a dynamic status message) was materialized 18x in guest RAM at the dialog, with the dynamic part ending "…losed" — matching the static strings "Transport closed" (0x9864930) / "Socket closed" (0x9852b5e). The "(Auth RPC result, original status code: " template never ran (no server status existed). The auth resource "tenants/t-9f607adf-lp1/users/current" was in the heap — the auth layer was mid-request-preparation.
- **CONFIRMED (gateway lead):** Stardew resolves `g2122d301.lp1.p.srv.nintendo.net` at startup and menu time but NEVER dials it (no ConnectImpl line). The splatoon-3 server repository documents the equivalent pre-gRPC identity chain for S3: `gw.hac.lp1.vermillion.srv.nintendo.net` (device init + accounts/config with online_license) and `val.hac.lp1.penne.srv.nintendo.net` (login tickets) — same `*.lp1.p*srv.nintendo.net` platform family. Why Stardew's SDK skips the gateway dial is unresolved.
- **CONFIRMED (identity mismatch candidate):** citron's fabricated BAAS id_token uses `sub = RandomHex(0x10)` (random) while `IManagerForApplication::GetAccountId` returns the linked account's uid (1800000005). An env-gated override `NEXTENDO_BAAS_SUB` already exists in the citron worktree (added for the Fall Guys EOS work). If Stardew's auth stack cross-checks token sub vs account identity locally (S3's demonstrably does not, since S3 works with the random sub), the auth call would be aborted pre-send with exactly the observed signature.

Next steps (in order):

1. **Env-only differential:** relaunch Stardew with `NEXTENDO_BAAS_SUB=1800000005` (token sub = linked uid). If the auth RPC fires (server CONN lines with RPCs / IssueToken), the identity cross-check was the gate. Variants if needed: `u-1800000005`, zero-padded/hex forms.
2. If sub-alignment alone fails: try aligning `aud` per-title (find Stardew's expected BAAS client id — not present as a plaintext 16-hex string in main; may be in game data or verified structurally).
3. If identity alignment fails entirely: the healthy-title differential (run Splatoon 3 under the same tap and observe whether it dials its vermillion/penne gateway pre-gRPC and sends HEADERS immediately) — the handoff's previously prescribed experiment, now doubly motivated. Requires asking per the Launch Protocol when other titles run.
4. Optional deeper static: the SDK's diagnostic-table handlers near the "(Auth RPC result)" table-init sites (0x74c1c dc/0x73f4320/0x711f678) reveal the failure-classification logic.

Artifacts: no proprietary material; all analysis external; sanitized conclusions only.

### Experiment 2026-08-31-9: Static identification of the certificate-acceptance gate and build-scoped pin-bypass patch

Hypothesis:

The deterministic pre-HEADERS cancel (2321-4992) is the NPLN SDK's own certificate-acceptance check, the same mechanism the Splatoon 3 patch bypasses (`LDRB W10,[X21,#0x38]` at S3-main 0x157B20 → `MOV W10,#1`). Stardew's main embeds the identical SDK code; locating it offline yields the Stardew patch offset.

Method (offline analysis; completed Ghidra project + raw disassembly):

1. Full Ghidra auto-analysis of main.elf completed and SAVED on disk (project `/home/tobagin/ghidra-projects/sdfull`, log `/home/tobagin/ghidra_sdfull2.log`, ~7.7 GB). Address mapping resolved empirically: the reconstructed ELF is PIE with 0-based VAs; Ghidra imports it at image base 0x100000 (Ghidra address = VA + 0x100000; the earlier session's "-0x160" note was a misreading). In the flat NSO, text file offset = VA + 0x100 (NSO header retained); the analysis-time "ELF VA" and "program_image offset" of the existing X509 patch are the same number.
2. Searched the text segment for the exact S3 idiom `LDRB W10,[X21,#0x38]` (`3940E2AA`): 2 hits, exactly one in the SDK/TLS region (VA 0x782F5D0) and one in low rtld/libc code. No `STRB [xN,#0x38]` exists anywhere in text — the flag is never set; it is a build-disabled option, permanently 0.
3. Disassembled the containing function (VA 0x782f2e0, called from 0x7822f3c/0x7825410; not auto-discovered by Ghidra, boundaries found via BL-target scan): it is the SDK's SSL-context setup. It creates an SSL_CTX, calls `SSL_CTX_set_verify` (identified as the tiny setter at VA 0x78e5310 storing mode+callback at ctx+352/+400, mode=1 = SSL_VERIFY_PEER) with `callback = flag ? always_ok_stub(0x782fac0: mov w0,#1; ret) : real_check_stub(0x782fad0)`. The real-check stub is the classic OpenSSL verify callback `int cb(int preverify_ok, X509_STORE_CTX*)`: preverify_ok → 1; otherwise returns 1 only when `X509_STORE_CTX_get_error(ctx)` ∈ {9,10,11} (cert-not-yet-valid/expired/notBefore-field — console clock-skew tolerances), else 0. So flag=0 (always, in this build) installs a callback that rejects any chain-erroring certificate other than expiry-class.
4. Identified the error-code machinery: the grpc-status→nn::Result converter near VA 0x77702c0-0x7770460 contains a 17-entry u32 table at VA 0x98CA1AC mapping gRPC status codes to NPLN results with module 321: OK→2000-0, UNKNOWN→2321-384, FAILED_PRECONDITION→2321-3072, INTERNAL→2321-4608, UNAVAILABLE→**2321-4992**, UNAUTHENTICATED→2321-5760, etc. The observed dialog code 2321-4992 is therefore a client-side gRPC UNAVAILABLE — the call failed without a server response (channel/subchannel security setup), matching the probe's cancelled-stream signature and the splatoon-3 server repository's independent notes on the same code.
5. Derived the patch: VA 0x782F5D0, original `AA E2 40 39` (`ldrb w10,[x21,#0x38]`) → `2A 00 80 52` (`mov w10,#1`), exactly mirroring kCertificateBypass semantics. Added as a second guarded, build-scoped entry (title 0100E65002BB8000, module main, build E7F845093E8CBC68DACF011CCB620D6667B5A20B, exact original-byte fingerprint) in the external citron loader patch block alongside the X509 patch; citron rebuilt successfully.

Observation:

- **CONFIRMED (static):** flag read at 0x782F5D0 is the unique S3-analogue in the binary; the never-set flag (no `strb` anywhere) proves the always-accept path is dead code in this build, exactly as in S3.
- **CONFIRMED (static):** 2321-4992 = gRPC UNAVAILABLE via the converter table — the cancel is produced client-side before/without a server response.
- **PENDING RUNTIME:** retest with both patches (X509 + flag bypass) through the loopback probe; expected outcome is the first client HEADERS at the server. Not yet run at handoff time (another title's citron instance was on the machine; Launch Protocol requires asking first).
- **OPEN LEADS if the flag bypass does not clear the gate:** (a) the S3 server work documented a pre-gRPC REST "vermillion/penne" device-identity step (devices/initialize, vermillion-device-id, accounts/config with `online_license.is_available`) whose failure also surfaces as 2321-4992 with zero gRPC HEADERS; Stardew resolves `g2122d301.lp1.p.srv.nintendo.net` (the `*.lp1.p.srv.nintendo.net` family) but never connects to it — worth tapping separately; (b) grpc TSI post-handshake peer checks beyond OpenSSL verification (peer property/fingerprint compares) can be decompiled next (the setup function's second setter call at VA 0x78e4dd0 stores an unrelated parser helper, not a pin callback).

Decision rule:

- HEADERS observed at the probe ⇒ the certificate-acceptance flag was the last client-local gate; proceed to RPC cataloging (which services/methods Stardew requests first) and start implementing responses.
- Identical pre-HEADERS cancel ⇒ the gate is further upstream (vermillion device identity or TSI peer checks); follow the open leads above, one variable per run.

Artifacts: patch metadata and offsets recorded here are independently derived interoperability conclusions; original-byte fingerprints live only in the external GPL citron worktree. No proprietary bytes entered this repository.

### Experiment 2026-08-31-8: Client-side gate isolation (account link, local account, id_token claims)

Hypothesis:

The universal pre-HEADERS cancel (both titles, any server) is caused by a client-side credential gate rather than transport behavior; a linked Nextendo account and/or correct per-game BAAS id_token claims will clear it.

Method:

1. Production control: fresh citron without the tap envs; Splatoon 3 lobby attempt against production Nextendo.
2. Created a dedicated local account (`stardewhost`, pid 1800000005) on the local nextendo-account (127.0.0.1:8099) via `/api/register`, verified via the dev-mode logged link. Credentials stored outside any repository under `~/.local/share/stardew-nextendo-research/`. (`stardewjoin` pending the server's registration rate limit.)
3. Relaunched the Stardew citron instance with `NEXTENDO_API=http://127.0.0.1:8099` (citron's `BaseUrl()` accepts loopback for the account API) plus the tap envs; user signed in via **NexTendo → Sign in** (OAuth code emitted for pid 1800000005, confirmed in the account server log).
4. Offline, authorized local analysis of the already-exported main module (external artifacts under `~/.local/share/citron/research/stardew-0.20.0/`): strings/JSON scan of the reconstructed ELF.

Observation:

- **CONFIRMED:** Splatoon 3 fails against production (2321-4992) from a freshly first-booted, unlinked profile — the earlier "S3 works" assumption did not hold for this environment at that time.
- **CONFIRMED:** After linking (`stardewhost`), citron serves `GetAccountId` (linked Network ID) and issues a signed BAAS id_token (1405 bytes) via `EnsureIdTokenCacheAsync`/`LoadIdTokenCache` immediately before the attempt — and Stardew STILL cancels pre-HEADERS (RST REFUSED_STREAM, identical signature, 12:56:15). Account link alone does not clear the gate.
- **CONFIRMED (offline):** Stardew's main module embeds the complete standard NPLN method surface — `nn.npln.auth.v1.Auth` (IssueToken, IssuePrearrangedUserToken, RefreshAnonymousUserToken, …), `nn.npln.friends.v1.Friends`, `nn.npln.gamesync.v1.Gamesync`, `nn.npln.matchmaking.v1.{Matchmaker,GameSessionService}` — plus the tenant hostname format `%s.%s%s.t.npln.srv.nintendo.net:443` and a `_npln:/npln_config.json` config-file reference (file lives in game data, not in main).
- **CONFIRMED (offline/cross-repo):** citron and Ryujinx-Nextendo both hardcode the fabricated BAAS id_token `aud` to `ed9e2f05d286f7b8` (with iss `e0d67c509fb203858ebcb2fe3f88c2aa.baas.nintendo.com`, random `sub`, game-agnostic claims). No emulator presents a per-game audience. Ryujinx-Nextendo independently marks Stardew `online-broken`.
- **CONFIRMED (probe, SNI):** Stardew offers ALPN `[grpc-exp, h2]` (grpc-exp first), same as Splatoon 3.

Hypothesis (current):

Stardew's NPLN SDK validates the fabricated id_token locally before its first auth RPC and rejects it on game-specific claims — most plausibly `aud` (BAAS client id), which is hardcoded to another title's value. The expected value likely lives in the game's `npln_config.json` (game data, not main). This would explain: identical pre-HEADERS aborts for both titles pre-link, Stardew failing post-link, S3-works/Stardew-broken across both emulators, and Ryujinx-Nextendo's `online-broken` marking.

- **CONFIRMED ( conclusive negative):** Against the FULL reference NPLN server (splatoon-3 implementation: complete `nn.npln.*` services, emulator-token-accepting auth, tenant-covering cert) run locally on the tapped port with the linked local account, Stardew completes TLS+h2 ("h2 established — reached the gRPC layer") and then closes with **ZERO RPCs received** — identical pre-HEADERS cancel, 2321-4992. Combined with runs A–C: the gate is definitively inside Stardew's NPLN SDK initialization, before any request exists. Server-side completeness is irrelevant.
- **CONFIRMED (offline):** The cloned nextendo-nncs reference server implements ONLY the UDP NAT-check responder (Pia NatDetectionJob, 16-byte probes, NAT classification) — it delivers no environment/config data. The `_npln:/npln_config.json` file is not in the base RomFS dump (Content/ assets only) and not persisted in emulated NAND; it is an in-memory SDK mount.
- **CONFIRMED (offline):** Stardew main embeds its own Pia/NPLN login state machine diagnostics (`ChangeStateJob::WaitInitializeNetworkSdk`, `ChangeStateJob::Login`, `LoginJob::FailureProcess`, `(Auth RPC result)`, `Failed to get NsaIdToken cache. Need call EnsureNetworkServiceAccountAvailableAsync()`) — a local wrapper around the SDK whose failure branch produces the observed behavior. ADRP+ADD xref anchors located for these strings (code refs at ~0x760cf24, 0x776bea0, 0x7788354, 0x7610994/0x7610d38 in main build E7F845…).
- **CONFIRMED (memory):** Guest-RAM inspection (authorized) found the citron-fabricated id_token DECODED in the SDK heap (`"aud":"ed9e2f05d286f7b8"` with iss/iat/exp/jku/jti/di claims) — the token is delivered AND parsed; no validation-error text nearby. `_npln:/npln_config.json` does not exist in guest RAM, the base RomFS dump (Content/ assets only), or emulated NAND — the mount is never materialized at runtime.
- **CONFIRMED (citron HLE):** `IManagerForApplication::EnsureIdTokenCacheAsync` is a stub returning success + async interface; `LoadIdTokenCache` serves the real token; `EnsureNetworkServiceAccountAvailableAsync` is not implemented in citron's ACC tables. The id_token `aud` remains hardcoded (`ed9e2f05d286f7b8`) with a random `sub` and a `jku` pointing at the real Nintendo BAAS certificates URL.
- **CONFIRMED (log):** The complete ACC IPC sequence around the abort is clean (CheckAvailability, GetAccountId ×3, EnsureIdTokenCacheAsync, async HasDone/GetResult, LoadIdTokenCache with 1405-byte token, GetProfile/GetBase, GetUserExistence, GetBaasAccountManagerForApplication, InitializeApplicationInfoV2) — no unknown commands, no error results. Account-side IPC is formally satisfied.
- **CONFIRMED (offline, static):** With corrected LOAD-segment mappings (LOAD0 covers VAs < 0x7c46000; an earlier mapping bug produced garbage windows), located the wrapper's diagnostic formatter: code at 0x776b438/0x776be20/0x776bf28 formats `(ResultNetworkServiceAccountUnavailable)` when handed an nn::Result with module field 0x7c (124 = Account) and description in [200, 269]. All three refs live in one ~34KB function at 0x776377c that also references the NsaIdToken-failure string; the Pia login state machine (ChangeStateJob/LoginJob refs) lives in a ~110KB function at 0x75f1e04. `_npln:/npln_config.json` turned out to be grpc-core service-config plumbing (a channel target string), not a title config — not a differentiator vs S3.
- **CONFIRMED (experiment):** Name-mimicry ruled out: serving a certificate whose CN is EXACTLY the tenant hostname (vs wildcard) produced the identical pre-HEADERS abort (2321-4992, zero RPCs at the full local server). Stardew's pin is hash/DER-based, not a name compare — a build-scoped binary patch (S3-style) is required.
- **BREAKTHROUGH (root cause identified, HIGH confidence):** The citron Nextendo fork contains `src/core/loader/nextendo_s3_patches.cpp` (mirrored from Ryujinx-Nextendo's `NextendoS3Patches.cs`) whose own documentation states: without the integrated patch, "the game's certificate PINNING remains active" and "NPLN fails with **2321-4992** after a successful TLS handshake." This is EXACTLY the Stardew symptom — and the S3 patch is a single 4-byte IPS entry: at S3-main offset 0x00157B20, `LDRB W10,[X21,#0x38]` → `MOV W10,#1` (Ryujinx notes: "64-byte-unique signature in 100 MB"). I.e., the NPLN SDK's post-handshake pin check reads a stored "certificate accepted" flag; the patch forces it true. Diagnosis: **Stardew's NPLN SDK performs the same style of local certificate-pin check; our X.509_verify_cert chain-validation patch satisfied chain validation but not the pin, so the SDK aborts with 2321-4992 before any RPC — on every server, regardless of account state.** S3 works on emulators solely because its pin check is patched.
- **CONFIRMED (offline):** Stardew's pin is not name-based: no "Nintendo CA"/"Nintendo Class 2"/"nintendo-private-server" strings exist in main (0 hits), and the tenant hostname is assembled at runtime (`%s.%s%s.t.npln.srv.nintendo.net:443`) — so serving mimicry certs cannot pass it; a build-scoped binary patch (or equivalent) is required, exactly like S3's.
- **CONFIRMED (offline):** All four direct BL callers of Stardew's X509_verify_cert (0x78df090, 0x78df67c, 0x78fdf14, 0x79b724c) are OpenSSL-internal (surrounding code raises ERR_LIB_SSL=20 errors with statem file/line args) — the pin check consumes the handshake result elsewhere (S3-style stored flag), not by calling X509_verify_cert directly.
- **TOOLING:** devkitA64 `aarch64-none-elf-objdump -D -b binary -m aarch64 --adjust-vma=...` works for raw windows; capstone 5.0.7 + pyghidra available (persistent Ghidra 12.1.3 at `~/.local/opt/ghidra_12.1.3_PUBLIC`; headless analysis command in `/home/tobagin/ghidra_sdfull.log`). radare2/Ghidra-from-pip unavailable.
- **TOOLING (mapping, settled 2026-08-31):** reconstructed main.elf is PIE with 0-based VAs; Ghidra imports at base 0x100000 (Ghidra addr = VA + 0x100000). Flat NSO keeps its 0x100-byte header: text file offset = VA + 0x100; rodata file offset = VA + 0x300 in the observed region — verify segment deltas from the NSO segment table, do not assume. Ghidra full analysis left the SDK/login coroutine regions undiscovered: create functions manually (CreateFunctionCmd with SourceType.USER_DEFINED) after finding boundaries via BL-target scans; pyghidra transaction must be committed or project save fails and leaves a stale `.lock` (delete `sdfull.lock` to recover).
- **LESSON (twice-burned):** Ghidra headless (a) must NOT live in /tmp (tmpfs dies on reboot), and (b) its PROJECT must not be on tmpfs either — the first 5.5h full-analysis run completed ("Analysis succeeded", 4385s in the final phase alone) but **"Save failed"** because the project DB filled its 15G tmpfs; all analysis was lost. Rule: Ghidra projects go on disk (now `/home/tobagin/ghidra-projects/sdfull`), logs on disk, and Ghidra project paths cannot contain dot-prefixed elements (`.local` is rejected).
- **STATE:** Full auto-analysis RE-RUNNING on disk-backed storage (launched 20:24, ETA ~5.5h -> ~02:00; log `/home/tobagin/ghidra_sdfull2.log`). When saved: open with pyghidra, decompile the login wrapper (start ~0x75f1e04) and account function with real boundaries, find the cert-pin gate, derive the build-scoped patch offset, add it to the external patch table, rebuild citron, retest.
- **STATE (post-crash):** The machine crashed ~15:20 (analysis lost with it). Full Ghidra auto-analysis of main.elf RESTARTED (fresh project `/tmp/ghidra_proj_sd_full/sdfull`, ~3h ETA, log `/home/tobagin/ghidra_sdfull.log`). Note: /tmp is tmpfs — anything stored there dies on reboot (cost us the first Ghidra install copy). Login state machine decoded as coroutine resumption points (`adr x8,<addr>` stored at ctx+0x38-ish, tags at ctx+72): states include ChangeStateJob::Login / CleanupNetwork / Logout / FailureProcess.LoginJob — the FailureProcess handlers are at 0x760ed88/0x760f0f8. Next: when analysis finishes, decompile the giant wrapper (start 0x75f1e04) with real boundaries, find the cert-pin gate branch, derive the Stardew pin-bypass patch offset, add a build-scoped entry to the external patch table (mirroring kCertificateBypass), rebuild citron, retest.
- **CONFIRMED (memory, runtime):** With Stardew live at the 2321-4992 dialog, the main module image was located in guest RAM at two consistent copies (bases `0x7ec6bfe06000`, `0x7f472e006000`; derived from three rodata anchor strings). Guest code is JIT-translated (no native AArch64 execution), so hardware breakpoints on guest bytes are impossible; memory inspection works.
- **CONFIRMED (memory, runtime):** Chunked full-memory scan (37.7 GB readable): the `(ResultNetworkServiceAccountUnavailable)` formatter message and the NsaIdToken-failure strings exist ONLY as static module rodata — the formatter never executed at runtime. `_npln:/npln_config.json` exists ONLY as the static string — never opened, written, or materialized at runtime. Interpretation update: the string is a grpc-core service-config channel target (`_npln:` scheme), i.e., NPLN's gRPC service-config plumbing, not a title data file. The failing layer is therefore likely the SDK's gRPC channel/service-config initialization or its internal credential state machine — not the ACC IPC surface (clean), not the tenant transport (established), and not this diagnostic formatter itself.
- **CONFIRMED (offline, post-analysis):** Login state machine fully mapped (17 state transitions; handlers 0x760e0f0/0x760ebf0/0x760ef00/0x760f37c/0x760eae4/0x760e460/0x760fe90/0x760f26c/0x760e304/0x760e8b0/0x760ed88/0x760f0f8). The Login handler (0x760ebf0) calls an async job (constructed at 0x760a41c), checks its result at [sp+32]; on FAILURE it formats the result, compares a wrapper field ([x19+228]), sets [x19+256]=1 and field [+60]=2, and transitions to ChangeStateJob::FailureProcess.LoginJob::CompleteProcess — the 2321-4992 path. Ghidra image base is offset -0x160 from static ELF VAs (decompiler UNK labels need +0x160 to read file bytes); large UTF-16 managed-string regions sit inside .rodata around the SDK strings.
- **MEMORY (null result, informative):** At the 2321-4992 dialog, the SHA-256 of the served certificate (DER and SPKI forms) appears NOWHERE in guest RAM — the SDK never digested our certificate to memory, or the compare happened on an ephemeral stack. Memory forensics for the pin is exhausted; the decompile path (job-class hierarchy of the auth job) is the remaining route.
- **NEXT (concrete):** Find Stardew's pin-flag read (the analogue of S3's `LDRB W10,[X21,#0x38]` at S3-main 0x157B20). Approaches, in order: (1) trace what stores/reads the post-verify flag in the SDK's TLS context — locate NPLN SDK's SSL verify-callback installation (SSL_CTX_set_verify call sites reachable from the SDK's user-agent builder at 0x77727b0 / its module); (2) decompile the 0x776377c / 0x75f1e04 functions with a proper decompiler; (3) look for the LDRB-from-context-flag idiom near the SSL state machine. Once found: add a build-scoped Stardew entry to the established external patch mechanism (mirroring kCertificateBypass: force the flag read to 1), retest — expect HEADERS at the server for the first time. (2) optionally compare a working S3 boot's host-side sequence through the same instrumentation.

Hypothesis (updated):

The cancel originates in Stardew's own login state machine before the auth RPC is issued. Candidate failing checks: NPLN SDK initialization against the environment/config it expects (source of `_npln:/npln_config.json` unresolved), per-game id_token claims (aud), or an account-HLE result contract difference in the older SDK. Next step: symbolize and read the login-state-machine failure branch in the reconstructed ELF (offline, external), using the existing Shutdown-backtrace addresses as anchors if module bases are recoverable.

Next step: dump the game's RomFS (citron per-title Dump RomFS) and read `_npln:/npln_config.json` for the authoritative NPLN config (tenant, auth addresses, client id). If present, make citron's id_token claims per-title (env-gated, following the established external-hook pattern), retest, and expect the first real HEADERS frame at the probe.

Artifacts: sanitized conclusions only; all binaries/ELFs/dumps/config files remain external to every repository.

### Experiment 2026-08-31-7: Controlled NPLN TLS termination with sanitized HTTP/2 metadata logging

Hypothesis:

Stardew's 133-byte post-handshake ApplicationData record contains the HTTP/2 connection preface plus the first request HEADERS frame, so a local TLS termination point can record the first `:method`/`:path` (gRPC service/method), safe header names, frame types, and timing without decrypting anything outside the process or retaining payload content. The client's immediate (~1–2 ms) post-send shutdown may then be attributable to an observable cause (e.g., no acceptable server response, authority/tenant rejection) rather than remaining opaque.

Method:

1. Added `cmd/probe` + `internal/probe` to this repository: a loopback-only TLS terminator (in-memory self-signed RSA-2048 cert covering the tenant hostname; never written to disk) that answers only the minimum HTTP/2 framing required to keep the peer talking (server preface, SETTINGS ACK, PING ACK), and logs only: TLS version/cipher/ALPN, HTTP/2 frame types/flags/stream IDs, HPACK-decoded pseudo-headers (`:method`, `:path`, `:scheme`, `:authority`), other header NAMES with value lengths only, byte counts, and timing. Payload bodies, non-pseudo header values, keys, addresses, and certificates are never logged. The probe never answers an application request.
2. HPACK decoding uses `golang.org/x/net/http2/hpack` (BSD-3) for vetted Huffman-correct decoding; the observer remains dependency-free. Tests include an end-to-end redaction guarantee: a secret bearer token and request body bytes provably never appear in the log while `:path` does.
3. External citron change (dirty worktree, same established pattern): `GetNplnDebugProxyIp` was already defined but only wired into the `GetAddrInfoRequestImpl` chain; it is now also first in the `GetHostByNameRequestImpl` chain. Added a companion port-remap branch in `bsd.cpp` `ConnectImpl` (NEXTENDO_S3_DEBUG_PROXY_PORT, npln-host-scoped, unset by default) mirroring the Fall Guys / CTR:NF overrides, so the tap target need not own port 443.
4. Launch citron with `NEXTENDO_S3_DEBUG_PROXY_IP=127.0.0.1 NEXTENDO_S3_DEBUG_PROXY_PORT=18500`; NNCS/NAT traffic keeps its production Nextendo redirect; only hosts containing "npln" move to the local probe.
5. User opens the online co-op menu; probe log is then reviewed and only sanitized summaries enter this file.

Decision rule:

- A `:path` of the form `/pkg.Service/Method` observed reproducibly becomes the first **CONFIRMED** Stardew NPLN method; a matching minimal response is then designed in a follow-up experiment.
- If TLS completes but no HEADERS arrive, the 133-byte record is preface/SETTINGS-only and the failure is earlier in the channel setup; record frame types and close timing.
- If TLS fails against the local certificate, the compatibility boundary is not purely `X509_verify_cert` and the patch hypothesis must be refined.

Observation:

Three probe configurations were run against live client attempts; all produced identical client behavior.

- **Run A (probe answers nothing, empty server SETTINGS):** Each of 3 connections: TLS 1.2 `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256`, ALPN `h2`, client preface + client SETTINGS (ids: `ENABLE_PUSH`, `MAX_CONCURRENT_STREAMS`, `INITIAL_WINDOW_SIZE`, `MAX_FRAME_SIZE`, `MAX_HEADER_LIST_SIZE`, plus Nintendo-custom ephemeral id `0xfe03`), client ACK of server SETTINGS, then `RST_STREAM(stream=1, REFUSED_STREAM)` — **without ever sending HEADERS on stream 1** — then `WINDOW_UPDATE(0)` and FIN. Total lifetime 9–75 ms.
- **Run B (server SETTINGS byte-equivalent to the real Nextendo grpc-go server — single `MAX_FRAME_SIZE=16384`, no window updates; verified against grpc-go v1.81.1 source used by the production server):** Identical client behavior on all 3 connections.
- **Run C (probe answers every completed request stream with a valid trailers-only gRPC response: `:status=200`, `content-type=application/grpc`, `grpc-status=12` UNIMPLEMENTED; response path round-trip-tested against a real HPACK decoder):** Identical client behavior. The client cancels before any request can be answered.

Correlated citron flow (sanitized, metadata-only):

- **CONFIRMED:** Resolution/connect order at both startup (~[32s]) and menu attempt (~[465s]): `nncs1` → `nncs2` NAT checks (UDP, production Nextendo) → `g2122d301` resolved but **never connected** → tenant TLS ×3 with the identical pre-HEADERS abort.
- **CONFIRMED:** No account/BAAS/token host (`*baas.nintendo.com`, `*accounts.nintendo.com`) is ever resolved during the entire flow. The game never attempts tenant-token acquisition before the NPLN channel aborts.
- **CONFIRMED:** The external debug tap (`NEXTENDO_S3_DEBUG_PROXY_IP` + new `NEXTENDO_S3_DEBUG_PROXY_PORT` remap) redirected every tenant connect to the loopback probe; citron logged each 443→18500 remap.

Conclusion:

**CONFIRMED:** Stardew's NPLN client fully accepts the probe's TLS (with the build-scoped X.509 patch) and HTTP/2 layer — it ACKs the server SETTINGS — and then cancels its first RPC **before transmitting HEADERS**, deterministically (~1–11 ms), regardless of server SETTINGS content or application responses. This refines the earlier interpretation of the 2,483-byte-server-flight experiments: the 133-byte "ApplicationData" flight against the real Nextendo server was almost certainly this same preface/SETTINGS/ACK/RST/WINDOW_UPDATE cancel flight, **not an HTTP/2 request**. The blocker is client-local, upstream of the wire protocol: most plausibly a missing NPLN session prerequisite (tenant credential/environment activation), consistent with the observed absence of any BAAS/token step. Transport-level work alone cannot advance the protocol further; the next experiments must target the client's environment/credential acquisition path (NNCS answers, `g2122d301` role, token-source bypass) or compare against a working NPLN title through the same probe.

Artifacts:

- `cmd/probe/main.go`, `internal/probe/server.go`, `internal/probe/conn.go`, `internal/probe/probe_test.go` (this repository; sanitized metadata-only logging with end-to-end redaction tests; HPACK decode via `golang.org/x/net/http2/hpack` BSD-3, minimal HPACK literal encoder hand-written; observer remains dependency-free; `go` directive kept at 1.23.0).
- External citron changes (dirty worktree, GPL, unchanged policy): `GetNplnDebugProxyIp` wired into the `GetHostByNameRequestImpl` chain (it was only in `GetAddrInfoRequestImpl` before); `bsd.cpp` npln debug-proxy port remap (env-gated, default-off).
- Probe process log reviewed for payloads before sanitization: no payload bytes, header values (except the four pseudo-headers), tokens, addresses, or key material were recorded by the probe by construction.

### Experiment 2026-08-31-4: Active-update ExeFS export for offline TLS analysis

Hypothesis:

The exact Stardew build that exhibits the certificate rejection can be exported by Citron after its normal update-selection logic, avoiding guesses about which installed NCA is active.

Method:

Citron's existing `dump_exefs` and `dump_nso` debugging settings were enabled for one controlled boot. The first two attempts did not export because the Qt configuration's `dump_exefs\\default=true` and `dump_nso\\default=true` markers overrode the edited values; one attempt also had two Citron processes, and the older process rewrote the configuration. Both causes were identified and corrected. A single Citron process then booted Stardew with the value set to true and each `\\default` marker set to false.

Observation:

**CONFIRMED:** Citron selected installed update version `0.20.0` (raw version `1310720`) and successfully reconstructed its ExeFS. External-only exports were created under Citron's user-data dump directory. The active module build IDs are:

- `main`: `E7F845093E8CBC68DACF011CCB620D6667B5A20B`
- `sdk`: `C3C9BFAA757A1A23DC892EA38AB032B5C033D2E6`
- `rtld`: `45B2DAEB98C5E0130E1A5EA85FF5C1324E2BC999`

The main module contains an OpenSSL TLS/X.509 implementation with observable diagnostic symbol/string names including `ssl_verify_cert_chain`, `tls_process_server_certificate`, and `certificate verify failed`.

Offline reconstruction with the open-source `shuffle2/nx2elf` tool identified a function at main-relative offset `0x79B4C10` whose control flow matches OpenSSL `X509_verify_cert`: it validates the store context, runs chain verification, and returns `1` on success or `0`/`-1` on failure. Confidence: **HIGH**, pending runtime validation.

Conclusion:

**CONFIRMED:** All compatibility work must be scoped to the main build ID above. Offline analysis can proceed against the exact active module without using the flaky GDB stub or placing proprietary material in this repository.

Artifacts:

All ExeFS files, NSOs, reconstructed ELF files, disassembly databases, and future memory dumps remain outside every repository under Citron's local user-data/research paths. They must never be committed. ExeFS/NSO dumping was disabled after the successful export.

### Experiment 2026-08-31-6: Build-scoped X.509 compatibility patch

Hypothesis:

Returning success from the identified `X509_verify_cert` function will allow Stardew to complete TLS against the Nextendo replacement CA and emit its first HTTP/2/NPLN request.

Method:

The dirty external `../citron-nextendo` worktree now contains a guarded runtime patch in `src/core/loader/nso.cpp`. It activates only for title `0100E65002BB8000`, module `main`, build ID `E7F845093E8CBC68DACF011CCB620D6667B5A20B`, offset `0x79B4C10`, and the exact observed eight-byte original prologue. It replaces the function entry with AArch64 `mov w0, #1; ret`. Any title, module, build, bounds, or original-byte mismatch logs an error and leaves the module untouched.

Observation:

**CONFIRMED:** Citron rebuilt successfully and logged that the exact-build patch applied. Against the redirected Nextendo endpoint, Stardew then advanced beyond the previously failing server-certificate flight:

1. sent ClientHello with the correct Stardew tenant SNI;
2. received the 2,483-byte Nextendo server handshake flight;
3. sent a 126-byte TLS handshake flight beginning with handshake type `0x10` (ClientKeyExchange in the negotiated pre-TLS-1.3 framing observed here);
4. received a 233-byte server handshake response beginning with handshake type `0x04`;
5. sent a 133-byte TLS ApplicationData record;
6. then shut down the socket within approximately 1–2 ms.

The same progression repeated across multiple connections. Before the patch, Stardew shut down immediately after step 2 and emitted neither its client handshake flight nor ApplicationData.

Conclusion:

**CONFIRMED:** Offset `0x79B4C10` is a valid certificate-verification compatibility point for Stardew main build `E7F845093E8CBC68DACF011CCB620D6667B5A20B`. The patch moves the client across the TLS trust barrier and causes it to emit its first encrypted application request to Nextendo. This is the project's first confirmed transition from “TLS server flight rejected” to “TLS established/application data sent.”

**UNKNOWN:** The encrypted application request's HTTP/2/gRPC method and the reason for the immediate post-send shutdown are not yet identified. The next experiment should instrument the local/controlled NPLN TLS termination point or run a Stardew-specific local endpoint with research-only TLS key logging outside the repository, then record only sanitized HTTP/2 service/method and metadata-field names. Do not guess Splatoon RPCs.

Artifacts:

No proprietary bytes or extracted modules were added to this repository. The eight original prologue bytes exist only as a safety fingerprint in the external GPL Citron implementation; the replacement instructions are independently written interoperability behavior.

### Rejected Experiment 2026-08-31-5: Citron GDB stub

Hypothesis:

The GDB stub could provide a stable runtime view of the TLS validation failure.

Observation:

The user reported that this debugger path is flaky, consistent with the earlier startup stall while Citron waited for an attachment.

Conclusion:

**REJECTED:** Do not use the GDB stub for this research. Prefer reproducible offline analysis of the active-update export and controlled network experiments.

### Experiment 2026-08-31-3: Production-certificate control with outbound guard

Hypothesis:

Stardew rejects the redirected Nextendo server flight because its game-local TLS validation does not accept that endpoint's certificate identity/key. If the normal NPLN endpoint's certificate is accepted, the client will produce a post-server-flight TLS record.

Method:

An opt-in, exact-host Citron diagnostic (`NEXTENDO_STARDEW_TLS_PROBE=1`) resolves only `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net` normally. After receiving its TLS server flight, Citron observes only the type and length of the next client TLS record and suppresses it while returning success to the guest. This prevents a TLS Finished, HTTP/2 data, token, or authenticated request from reaching the external endpoint. All other Nextendo redirects remain enabled.

Observation:

**CONFIRMED:** The normal endpoint returned a 2,717-byte TLS server flight. Stardew then produced a 126-byte TLS handshake record after each observed server flight. Citron suppressed every such record as designed, so no TLS Finished, HTTP/2 data, token, or authenticated request was transmitted. The user observed that the client progressed for longer before eventually reporting `2321-4992`.

Conclusion:

**CONFIRMED:** Stardew accepts the normal NPLN endpoint's certificate sufficiently to continue its TLS handshake. It does not produce the equivalent post-server-flight record against the redirected Nextendo endpoint. Therefore the first Nextendo interoperability barrier is game-local certificate identity/key validation, not DNS, TCP, SNI, an NPLN RPC response, or client authentication.

Artifacts:

The diagnostic is an external change in the dirty `../citron-nextendo` worktree. No capture, TLS payload, certificate, key, token, or identifying address is stored in this repository.

### Experiment 2026-08-31-1: Establish a safe observation baseline

Hypothesis:

A protocol-neutral TCP/UDP observer with structured, redacted metadata can identify destination-facing transport behavior and safely advance research before any application protocol is known.

Method:

Implement configurable TCP and UDP listeners that record connection/datagram metadata and bounded payload properties without logging raw payloads by default. Exercise them with synthetic clients and verify deterministic, redacted output.

Observation:

- Synthetic TCP and UDP clients each sent 13 bytes to loopback port 18080.
- The observer emitted one JSON event per transport with `service` set to `unknown`, the local listener address, and `bytes: 13`.
- Events contained no payload, remote/client address, token, or client identifier.
- Automated race-enabled tests confirmed bounded reads (TCP: 4 bytes; UDP: 3 bytes) and payload-free serialization.

Conclusion:

The observation harness is ready for a controlled redirected-client experiment. This result confirms only the harness behavior; it provides no evidence about the retail client or Stardew protocol.

Artifacts:

- `cmd/observer/main.go`
- `internal/observer/observer.go`
- `internal/observer/observer_test.go`
- Synthetic command procedure in `README.md`
- Real packet captures must remain outside the repository.

### Experiment 2026-08-31-2: Citron retail-client baseline

Hypothesis:

Launching a legitimately supplied Stardew Valley installation in `../citron-nextendo` and entering the online co-op menu will expose a reproducible network or HLE-service transition that is absent from a title-screen control run.

Method:

1. Use the existing Citron build and its existing external user configuration; do not copy game content, firmware, keys, saves, credentials, or logs into this repository.
2. Preserve the dirty Citron worktree and make no source changes until its current hooks and configuration are understood.
3. Run a title-screen control, then open Co-op → Online Communication and record only sanitized log summaries.
4. If a hostname/transport appears, repeat once and compare. Keep raw logs/captures outside this repository.

Observation:

- **CONFIRMED:** `../citron-nextendo/build/use-nopgo/bin/citron` exists.
- **CONFIRMED:** The Citron worktree already contains numerous uncommitted changes, including socket, DNS, friend-service, and Nextendo UI changes. They must be preserved.
- **CONFIRMED:** The existing Citron binary launched successfully in the active Wayland graphical session on 2026-08-31. It remains running for interactive testing.
- Stardew launch/menu result: pending user interaction in the emulator window.
- **CONFIRMED:** The first Stardew launch did not reach the title screen. All four emulated CPU cores were idle and no Stardew network activity had occurred; this was a pre-network emulator-startup failure.
- **CONFIRMED:** Two Citron instances were running concurrently against the same user configuration and log. The first instance owned GDB stub port 6543; the later desktop-launched instance logged a critical `Address already in use` bind failure.
- **CONFIRMED:** The originally launched frozen instance was stopped with Ctrl-C; the later desktop-launched instance was deliberately preserved for a single-instance retry.
- **HIGH:** The port collision and shared-log interleaving made the first observation invalid as a controlled Stardew run. It does not establish that the dirty Citron source changes caused the failure.
- **CONFIRMED:** A single-instance retry reproduced the apparent freeze with Citron's external `use_gdbstub=true` setting. Port 6543 was listening, all guest CPU cores were suspended, and the log reported GDB server startup immediately before `KProcess::Run returned`.
- **CONFIRMED:** The external Citron user setting was changed to `use_gdbstub=false` for the next retry. No Citron source, game content, or repository artifact was changed.
- **Conclusion:** The freeze was debugger-wait behavior, not evidence of a Stardew/Nextendo protocol failure. Repeat the launch with the debugger disabled before investigating networking.
- **CONFIRMED:** With the GDB stub disabled, Stardew Valley reached an online communication attempt and displayed Switch error `2321-4992`: “A communication error has occurred. Please try again later.”
- **CONFIRMED:** This is the first observed network-facing client transition. The exact failing service/transport is pending sanitized log correlation; the error code alone does not identify the backend architecture.
- **CONFIRMED:** Observed DNS/service sequence includes `nncs1-lp1.n.n.srv.nintendo.net`, `nncs2-lp1.n.n.srv.nintendo.net`, `g2122d301.lp1.p.srv.nintendo.net`, and `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net`.
- **CONFIRMED:** NAT-check traffic uses UDP: a local ephemeral port sent synthetic-observed 16-byte datagrams to ports 33334 and 10025 across the two configured Nextendo NAT addresses. Payload contents are not retained here.
- **CONFIRMED:** The game-specific and NPLN transport connections use TCP port 443 and negotiate HTTP/2 (`h2`) with TLS 1.3 when independently probed without authentication data.
- **CONFIRMED:** During the failing client run, TCP connects succeeded and each ClientHello received a 2,483-byte server handshake beginning with ServerHello. The client then closed before sending TLS Finished or HTTP/2 data.
- **CONFIRMED:** The game-specific endpoint currently presents a self-signed `CN=nintendo-private-server` certificate; the NPLN endpoint presents a Nintendo-issued wildcard certificate covering the NPLN hostname. Certificate bodies were not stored.
- **CONFIRMED:** Stardew's NPLN tenant identifier is `t-9f607adf-lp1`; the generic game/P2P-monitoring identifier observed is `g2122d301`.
- **HIGH:** Stardew uses the NPLN control plane rather than assuming a traditional dedicated Stardew server. Whether gameplay becomes peer-to-peer after session establishment remains unconfirmed.
- **REJECTED (2026-08-31):** The shared redirected IP does not cause Stardew's observed TLS failure through a missing or wrongly inferred SNI. A metadata-only Citron diagnostic confirmed every observed ClientHello already carried `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net`, matching the requested NPLN tenant hostname.
- **CONFIRMED:** A 20-second raw-capture attempt failed for lack of `CAP_NET_RAW`; the created file was zero bytes and was deleted. No capture was committed.
- **USER-SUPPLIED:** A local copy of Nextendo infrastructure is available or already running for controlled Stardew experiments. Exact components and configuration are pending inventory; no credentials or environment values should be copied into this repository.
- **CONFIRMED:** Local `nextendo-account` is running on TCP 8099. No local NPLN/gRPC or NNCS process was initially running. Local TCP 443 is occupied by unrelated containerized Traefik infrastructure.
- **CONFIRMED:** On 2026-08-31, upstream `NextendoNetwork/nextendo-nncs`, `splatoon-3`, `sni-router`, and `nextendo-docs` were cloned as sibling repositories under `~/REPOS` for external inspection/runtime use. All four are PolyForm Shield 1.0.0. Reuse still requires preserving each repository's notices and checking compatibility rather than copying code implicitly.
- **CONFIRMED:** `splatoon-3` is the published Nextendo NPLN/gRPC server. Its documentation states that NPLN TLS runs inside the game over raw sockets and requires hostname redirection, a game-specific certificate-pinning/trust compatibility step, and an accepted identity.
- **HIGH:** Stardew's observed behavior—receiving the server TLS handshake, sending no Finished or HTTP/2 HEADERS, then showing `2321-4992`—matches the documented pre-RPC failure when the embedded NPLN client does not accept the redirected service certificate. A local server alone cannot pass this barrier.
- **Decision:** Do not adapt Splatoon-specific RPC handlers or captured-response consumers to Stardew before the Stardew client sends its first HTTP/2 HEADERS frame. First establish a clean-room client trust path and observe the requested RPC service/method.
- **License decision (corrected by user):** `stardew-nextendo` follows the other Nextendo game services under PolyForm Shield 1.0.0 with `Required Notice: Copyright 2026 Nextendo Network`. Preserve every upstream repository's existing license; Citron remains GPL. Do not silently relicense code across repositories.
- **CONFIRMED:** The rebuilt Citron diagnostic observed repeated ClientHello attempts where SNI injection was not applied and the IP-keyed fallback candidate was always `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net`. The current diagnostic cannot yet distinguish an already-present SNI extension from a parser rejection.
- **CONFIRMED:** A second metadata-only Citron diagnostic was built successfully on 2026-08-31. It extracts and logs only the ClientHello SNI hostname, allowing the next retry to distinguish an already-present SNI extension from injection/parser failure. No TLS payload, key material, or credentials are retained.
- **CONFIRMED:** On the subsequent client retry, the error remained `2321-4992`; all observed ClientHello attempts contained the correct NPLN tenant SNI. SNI injection was correctly skipped because the extension was already present.
- **SUPERSEDED:** Before the build-scoped compatibility patch, DNS, TCP, and SNI were correct but the client emitted no post-server-flight handshake or application request. Experiment 2026-08-31-6 crossed this barrier and confirmed subsequent TLS handshake and ApplicationData records.
- **CONFIRMED:** Each tested attempt sent a 213-byte ClientHello, received a 2,483-byte TLS 1.3 server flight beginning with ServerHello, and then initiated a socket shutdown within roughly 1 ms without sending a TLS alert, client Finished, or application data.
- **CONFIRMED:** An independent metadata-only TLS probe found that the redirected NPLN endpoint presents a Nintendo-CA-signed leaf whose common name belongs to a different tenant; wildcard coverage includes the Stardew tenant hostname. The system OpenSSL trust store does not trust the private Nintendo CA chain, which is not by itself evidence of how Stardew validates it.
- **CORRECTED/REFINED:** Public certificate metadata comparison shows the normal endpoint uses `CN=*.lp1.t.npln.srv.nintendo.net`, issued by `Nintendo CA - G3`, while the redirected endpoint uses a broad wildcard/Splatoon-tenant leaf issued by a distinct `Nintendo Class 2 CA - G3` replacement root. Both use 2048-bit RSA and SHA-256 signatures, and both cover Stardew's hostname. Combined with the guarded control experiment, this confirms CA-chain/key validation rather than hostname mismatch.
- **CONFIRMED:** TLS is implemented in the game's userspace stack over raw BSD sockets for this flow; Citron's system SSL trust configuration cannot make the game accept the replacement CA.
- **Decision:** A server-only certificate substitution cannot solve this boundary without the official CA signing key, which is unavailable and must never be sought or stored. Continue only with external, independently derived compatibility behavior; never commit proprietary binaries, dumps, certificates, or keys.
- **CONFIRMED:** The public `Ryujinx-Nextendo` compatibility inventory still marks Stardew Valley `online-broken;ldn-untested`, and its source contains no Stardew/NPLN tenant-specific compatibility entry beyond the title ID inventory.
- **CONFIRMED:** A 2026-08-31 public GitHub code search for the Stardew title ID and tenant ID found no published Stardew certificate compatibility patch. Results contained only title/compatibility inventories and unrelated forks. Published Nextendo client documentation describes certificate patches as game- and build-specific.
- **Decision:** Do not reuse Splatoon-specific guest patches, infer offsets from proprietary binaries, or guess RPC responses. Any game compatibility material needed for a controlled test must remain external to this repository and must comply with the clean-room policy.
- **Security:** The production-certificate control was disabled immediately after the result was obtained. Citron was restarted without `NEXTENDO_STARDEW_TLS_PROBE`, restoring Nextendo-only routing.

Conclusion:

Pending.

Artifacts:

Only sanitized conclusions will be recorded here. Citron user data and raw logs remain outside this repository.

## Confirmed Behaviors

- **CONFIRMED (observer only):** TCP and UDP can listen concurrently on the configured loopback host and port.
- **CONFIRMED (observer only):** Reads are capped by `STARDEW_OBSERVER_MAX_BYTES`.
- **CONFIRMED (observer only):** Structured observation events exclude payloads and remote/client addresses.
- None for the retail client or external services.

## Unconfirmed Hypotheses

- Stardew Valley may use a backend primarily as a control plane followed by peer-to-peer gameplay traffic. **SPECULATIVE** until traffic confirms it.
- A local TCP/UDP observation harness will be useful for identifying the first contacted transport. **MEDIUM** confidence as a research-method claim, not a protocol claim.

## Known Failures

- No client experiment has yet been run.
- No external capture or sanitized observation was supplied.
- The observer cannot identify a hostname on its own; DNS logging or an emulator/console routing observation is required.
- The observer deliberately sends no protocol response, so a redirected client is expected to time out or disconnect after the first transmission.

## Decisions

- Do not implement authentication, sessions, discovery, NAT traversal, relay, or gameplay handling without evidence.
- Default diagnostic logs will contain metadata only; raw payload logging is out of scope for the initial implementation.
- Synthetic fixtures only.
- Use Go 1.23 for the initial dependency-free observer, consistent with nearby independently written Nextendo research tools and easy single-binary deployment. This is an architectural choice, not copied code.
- Do not reuse game-specific assumptions from neighboring projects. The nearby Fall Guys project is MIT-licensed, but this repository's observer is being independently implemented; the Outbound project's Photon findings are not transferable evidence.

## Security / Secrets Notes

- Never commit production identifiers, tokens, certificates, keys, proprietary binaries/assets, raw captures, or identifying IP addresses.
- **User-authorized local research (2026-08-31):** Local extraction, disassembly, and memory inspection of the user's legitimately obtained Stardew installation may be used to derive an interoperability-only certificate compatibility behavior. All proprietary inputs, extracted NSOs, disassembly databases, and memory dumps must remain outside this repository, including outside ignored repository subdirectories. Only independently written conclusions, minimal patch metadata when legally appropriate, and synthetic tests may enter this repository.
- External research inputs must be referenced by local path or environment variable and kept outside the repository.

## Useful Commands

```sh
cp .env.example .env
set -a; . ./.env; set +a
go run ./cmd/observer
go test -race ./...
go vet ./...
printf 'synthetic-tcp' | nc 127.0.0.1 18080
printf 'synthetic-udp' | nc -u -w1 127.0.0.1 18080
cd ~/REPOS/nextendo-local && podman-compose --profile nncs up -d   # local stack
scripts/launch-ryujinx.sh            # shared HOST profile, hosting Stardew (thin wrapper over shared-docs/scripts/launch-ryujinx-host.sh)
scripts/launch-ryujinx.sh --joiner   # shared JOINER profile
scripts/launch-citron.sh             # citron (its own two-persona launchers)
podman logs -f nextendo-local_npln_1
```

## Next Steps

-1. **(session 4, latest) Implement Pia's nplnd relay service** — the last P2P gate. Facts: every
   console sends UDP datagrams (292–388 B) to `g2122d301.lp1.p.srv.nintendo.net`:34343 (port
   hardcoded in ELF fn 0x75dd5e4; the emulator routes that host to 127.0.0.1); Pia classes
   `nn::pia::nplnd::{NplnPlugin,NplndService,NplndProtocol,NplndRelayClient,NplndLoginJob,
   AttachMeshJob,DetachMeshJob,NplndHostMigrationJob,NplndPlayerInfo,IceServerConfigGetter}`
   (vtables via tools/vtfind.py: relay client 0xba6f1a8, protocol 0xba6eb40, service 0xba6f250,
   login job 0xba6e878). Joiner parks in `NplnBackgroundProcessJob::WaitConnectNetwork` until the
   relay answers. Plan: (a) capture payloads (`scratch/nplnd-34343.log`, armed with tcpdump -X);
   (b) decompile NplndRelayClient/NplndProtocol send+recv to get the login/attach-mesh/relay
   message formats and the expected replies; (c) write `cmd/nplnd` (UDP :34343) in this repo:
   login ack, attach-mesh ack, and packet relaying between attached stations of a session;
   (d) route `g2122d301.lp1.p.srv.nintendo.net` to it in the emulator route table (the shared
   launcher already resolves *.nintendo.net to NEXTENDO_SERVER_IP=127.0.0.1, so listening on
   127.0.0.1:34343 is enough) and add it to nextendo-local.

0. **(session 4) Live test of the lobby-data mechanism** — host + joiner on the shared profiles:
   a. Host a farm. While hosting, run `sudo python3 /mnt/media/nextendo-research/scratch/tools/
      readsess.py <host-ryujinx-pid>` (read-only): expect glue state 9; check whether the Session
      local station (+0xe0/+0xe8) is non-zero and equal to the host station (+0xf0/+0xf8). If it is
      zero, the publish gate is the station assignment (Pia mesh never created on our stack) and
      the next work item is the mesh/`CreateMeshJob` path; if it is non-zero and equal, the gate is
      elsewhere (dirty flag / C# `updateLobbyData` not called — check farm joinability in-game).
   b. Watch `podman logs -f nextendo-local_stardew_1` for a `[GS] write … fields=…prp…` line and
      the new `[GS] mirror prp -> farm …` line: that is the lobby-data flush landing. If it lands,
      the joiner's next Refresh should list the farm (blob now carries `key\nvalue\n` app data).
   c. If the flush never comes, the lazy diagnostic fallback is to append synthetic app data
      (`farmName\n…\nprotocolVersion\n1.6.15\nprivacy\nFriendsOnly\n…`, ≤0x1a4 bytes; the BE16
      header length at blob[0..1] stays 0x5c, only the total grows) to the returned blob
      server-side, to prove the joiner-side parser and reach JoinGameSession. Not implemented
      (synthetic data; the user previously declined a fake-farm variant).
   Server change made this session: gamesync writes carrying `prp`/`ip` are mirrored into the farm's
   GameSession (`internal/npln/gamesync.go` mirrorProps + test). Needs a `stardew` container redeploy
   (drops in-memory farms; host must re-host).

1. **Confirm the real join-list display path.** (historical — superseded by session 4) Trace the QueryGameSessions gRPC unary response
   callback that fills the session vector at `(mgr+0x2800)` in `FUN_07bb3d30`'s caller/coroutine —
   do NOT trust `FUN_07bb3d30` as the display path until confirmed (the contradiction in Experiment
   2026-09-02 says it is probably a sibling routine).
2. **Rule the presence angle in or out.** Check whether the Join UI lists a friend only when they
   show as "playing Stardew Valley" via account presence, independent of `QueryGameSessions`.
3. Once the display path is confirmed, do ONE clean runtime read of its parsed GameSession struct
   (`struct[0x30]` u16, `struct[0x40]` byte) against known values to fix the two field ids, then a
   one-shot server change. Reuse the memory method in Experiment 2026-09-02; scratch on `/mnt/media`.
4. After joining: implement the P2P/Pia peer connection (relay/STUN/TURN, address rewriting) and the
   host-quit cloud save (unimplemented save currently freezes the host on exit).
5. Re-decompile `FUN_07bb3d30` and `fn_076ecee4` only if needed; the clean copies are in
   `~/ghidra-projects/out2/`.

## Open Questions

- Which client function is the ACTUAL QueryGameSessions result-to-Join-list path? (`FUN_07bb3d30`
  is decompiled but contradicts the empirical result — likely a sibling.)
- Is the Join list gated on account presence ("friend playing Stardew"), not just query results?
- In the parsed GameSession 0x98 struct, is offset 0x30 `max` or `current`, and 0x40
  `can_participate` or `is_public`? (Determines the exact filter fix.)
- Does gameplay go peer-to-peer after session setup, and does the `_Pia_SystemData` blob need
  server-side rewriting for a reachable peer address under emulation?
- What does the host expect at quit to save the farm (cloud save) so it does not freeze?

## Session Log: 2026-08-31

Files created or changed:

- `.gitignore`, `.env.example`, `LICENSE.md`, `README.md`, `go.mod`
- `docs/architecture.md`, `docs/protocol.md`, `docs/experiments/README.md`
- `cmd/observer/main.go`
- `internal/observer/observer.go`, `internal/observer/observer_test.go`
- `handoff.md`

Tests performed:

- `go test -race ./...` — passed.
- `go vet ./...` — passed.
- Citron incremental build with metadata-only SNI diagnostic — passed.
- Manual loopback TCP and UDP synthetic smoke test — passed; both produced payload-free 13-byte observation events.
- Repository hygiene scan — passed; no capture/key/certificate/game-binary/dump extensions, files over 1 MiB, or credential-like content were found.

Remaining blocker:

The NPLN tenant channel reaches a fully functional HTTP/2 layer (client ACKs server SETTINGS) and then cancels its first RPC pre-HEADERS, deterministically, independent of server SETTINGS or responses. No BAAS/token host is ever contacted. The blocker is client-local: the NPLN SDK aborts before sending any application request, most plausibly for lack of a session/credential prerequisite (environment activation, tenant token) or because an earlier NNCS/environment check result was unacceptable.

Recommended next experiment:

Compare against a working NPLN title through the same probe. Run Splatoon 3 (already functional against production Nextendo) with the same debug tap (`NEXTENDO_S3_DEBUG_PROXY_IP` now also covers any "npln" host) and record its connection-start behavior: whether it sends HEADERS immediately, which services/methods it calls first, what its client SETTINGS contain, and whether it resolves account/BAAS hosts before its tenant. Differences versus Stardew isolate the client-local prerequisite. Do not retain tokens, payload bodies, certificates, keys, account identifiers, or raw captures.

- **Hypothesis:** A healthy NPLN client (Splatoon 3) sends HEADERS on a freshly opened tenant connection without requiring any server application response, and resolves a token/identity source before connecting; Stardew's divergence from that pattern identifies the missing prerequisite.
- **Smallest method:** Same loopback probe, same sanitized metadata logging, one controlled Splatoon 3 boot to the lobby; compare sanitized flow summaries (DNS order, SETTINGS ids, first frames) between titles.
- **Decision rule:** If Splatoon sends HEADERS where Stardew cancels, the transport is validated and the delta is Stardew's client-local prerequisite; the specific missing step becomes the next experiment target. If Splatoon also cancels pre-HEADERS against the probe, the probe's transport must be improved before any client-side conclusion is drawn.

## Emulator Launch Protocol

Family-wide, canonical version: `shared-docs/emulator-launch-protocol.md`
(symlinked into this repo as `shared-docs/`). Short form: autonomous
launch/kill/restart only when no other title's citron instance is running or
when only this project's own instance runs; otherwise ASK FIRST. Detect via
`pgrep -af "^/home/tobagin/REPOS/citron-nextendo/build"` and inspect each
process's cmdline (NSP path) and environ (`NEXTENDO_*` vars). Never
broad-`pkill` — patterns match the calling shell and other titles' sessions.

### Kills: target by game, never by binary path (incident 2026-08-31)

A binary-path kill (`pkill -f citron-nextendo/build`) terminated a
running Outbound session on the shared machine. When killing a citron
instance, kill ONLY pids whose cmdline contains THIS project's game
NSP path (check /proc/<pid>/cmdline per pid). When any other title's
instance is running, ask before launching or killing anything — memory
pressure alone (29 GB host) can OOM a foreign session.

## Session Log: 2026-09-01 (local stack)

- Found: `nextendo-local` containers up except `nncs`; the Stardew citron ran from the default profile with `NEXTENDO_API=https://account.tobagin.eu` — verified to be the LOCAL account container via the local Traefik (resolves to the podman network, LE cert), i.e. the right deployment — but not signed in (log: `PollInvitations: skipped, not linked`), and with the profile setting `nextendo_server_ip` still at the production IP (see the precedence lesson below).
- Started the `nncs` profile container (host network, UDP 10025/10125 + 33334 sinkhole). Verified: JWKS served with `kid nextendo-baas-key-1`, account API answering, npln cert SAN covers the tenant, `stardewhost` (pid 1800000005) present and verified in the local account store, Ryujinx's `nextendo_baas.pem` identical to the stack's signing key.
- Added `scripts/launch-citron.sh` (portable profile, nand/keys symlinked; `NEXTENDO_API=https://account.tobagin.eu` + trusted suffix, `NEXTENDO_SERVER_IP`/`NAT_IP` 127.0.0.1, npln tap 18500, JWKS port 18448, signing key from the stack) and `scripts/launch-ryujinx.sh` (`--root-data-dir ~/ryujinx-instances/stardew`, seeded from the main portable dir minus the production link, route table tenant→18500 / jku→18448). Both enforce the launch protocol and preflight the stack.
- Lesson (citron, source-verified in `sfdnsres.cpp` `GetConfiguredIp`): the profile SETTING `nextendo_server_ip`/`nextendo_nat_ip` wins over `NEXTENDO_SERVER_IP`/`NEXTENDO_NAT_IP`, and a fresh profile defaults both to the production IPs — the env vars alone are silently ignored (this is why NAT checks and the JWKS fetch kept going to production). The wrapper now pins the settings in `qt-config.ini` (`\default=false`) and refuses to launch if that did not take. Added to `shared-docs/citron-isolation.md`.
- Lesson (shared doc corrected, source-verified): Ryujinx detects `portable/` next to the BINARY only; per-title data dirs need `--root-data-dir`.
- Added `docs/local-stack.md`; README pointer.
- Test run (Experiment 2026-09-01-B): citron → 2321-4992 (pre-HEADERS cancel, emulator gap). Ryujinx → auth denied until two bundle fixes (account `NEXTENDO_DATA_DIR=/data` + `data/account/internal_net.conf` with a static account IP 10.89.1.100; `NEXTENDO_INTERNAL_KEY` removed from the account service), then auth PASSED and the RPC chain ran to `QueryGameSessions` → UNIMPLEMENTED → 2321-4224. `nextendo-local/compose.yml` was also being edited by another session during this work (backup `compose.yml.bak-*`).
- Wrapper additions: `launch-ryujinx.sh --menu` (main window only, sign in first, then load the game).
- Next: observe `QueryGameSessions` request fields, implement the first Stardew handler.

## Session Log: 2026-09-02

Built and confirmed:

- `cmd/npln` + `internal/npln/*` (server, identity, auth, friends, sessions, gamesync) and `proto/*`
  (copied NPLN bindings, notice preserved). `go build ./...`, `go vet ./...`, `go test -race
  ./internal/npln/` all pass. Identity round-trip test in `internal/npln/identity_test.go`.
- `scripts/launch-citron.sh`, `scripts/launch-ryujinx.sh` (`--menu` mode), `docs/local-stack.md`.
- `nextendo-local`: added `stardew` (18501) and `stun` (coturn) services; account bundle fixes
  (`NEXTENDO_DATA_DIR`, `internal_net.conf` + static IP, drop `NEXTENDO_INTERNAL_KEY`). npln cert
  reissued with SAN `gamesync.npln.nintendo.net`. `compose.yml.bak-*` backups left from concurrent
  edits by another session.
- Two verified local accounts made friends via the account API: `stardewhost` (1800000005),
  `stardewjoin` (1800000007). Second Ryujinx data dir `~/ryujinx-instances/stardew-join`.

Confirmed flows: authentication, friends, farm hosting + full gamesync transport (see Flow sections).

Open blocker: joining — the farm is returned and received but filtered out client-side. Filter
decompiled (Experiment 2026-09-02); a clean contradiction means the analyzed function is probably not
the true display path. Runtime memory capture works (sudo; guest RAM = largest `/dev/shm/Ryujinx-*`
shm; freeze-120ms-after-query to catch the parse). Scratch MUST be on `/mnt/media/nextendo-research/`
— filling `/tmp` tmpfs with dumps kills the shell tool (playbook §5); this happened this session.

Emulator instability: the second (joiner) Ryujinx instance died repeatedly (memory pressure on the
29 GB host with the full stack running). Host emulator freezes on quit (unimplemented cloud save).
GDB stub: read-only works, breakpoints crash the emulator — do not use breakpoints.

Lessons added to shared docs and project memory: shell-killing tmpfs rule, Ghidra function-vs-string
address convention, guest-RAM shm identification, HostMappedUnsafe mirror bases, the join-filter
model + the contradiction to resolve first.
