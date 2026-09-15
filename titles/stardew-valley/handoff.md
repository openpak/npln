> **2026-09-08 — the OpenPak conversion is committed and the server is deployed.** The module is
> `openpak/stardew-valley`, imports and generated stubs follow, the licence is AGPL-3.0, and the
> deployment talks to nx-baas's internal API rather than a Nextendo account server. That work had
> been done on the workstation and never pushed, so the repository on GitHub could not build; a
> tag on 2026-09-08 exposed it. Build the container locally before tagging — CI builds the
> committed tree, not the working one.
>
> The server also serves `/health` on **21011** (plain HTTP, beside the gRPC/TLS tenant port),
> because the service port answers only TLS with h2 and a bare connection to it proves nothing.
> The website's status page checks that endpoint.
>
> Everything below predates the conversion and still says "nextendo" in places; the blocker list
> in particular is Nextendo-era and no longer describes this deployment.

# stardew-valley Handoff

## Project Objective

Build an independently written, open-source compatibility service for the network-facing behavior needed by Stardew Valley multiplayer on Nintendo Switch. This is clean-room interoperability work only; proprietary code, assets, binaries, credentials, keys, certificates, raw captures, and player-sensitive data must remain outside this repository.

## Current Status

- 2026-09-13 (session 16 — **eden bring-up: the whole citron/Ryujinx client recipe ported, the
  NPLN worker's wait located, the remaining gate named**). One long client-side session; no
  server changes.
  - **Ported into eden** (all verified live by logs or on the wire):
    1. Per-title BAAS binding — `openpak-client`'s login chain now walks `aauth
       /v5/application_auth_token` (nx-baas's existing handler) and posts `appAuthNToken` with
       the baas login, so the id_token a title receives carries `nintendo.ai` = the running
       title id and its version, not the captured VPS default. The cached token is keyed by
       the binding, so switching titles re-mints. Verified by decoding a replayed login:
       `ai=0100e65002bb8000`, `av=1.6.15.13`, `nnex` present.
    2. AF_INET6 (guest domain 28) sockets, dual-mode (`IPV6_V6ONLY=0`), v4-mapped
       connect/bind, sockaddr_in6 parsing in `ConnectImpl`, `sockaddr_storage` peername
       unwrap. Stardew's gRPC dials its first-choice IPv6 socket; the transport (TLS 1.2,
       ALPN `grpc-exp,h2`, SETTINGS, 15 s keepalives) now comes up on it end to end.
    3. The Stardew game patches (build-scoped, byte-fingerprinted, ported from citron's
       `nso.cpp`): X509_verify_cert bypass at 0x79B4C10 and the certificate-acceptance flag
       at 0x782F5D0. Loader logs both applied; verified live in guest RAM by scanning for
       the patched bytes.
    4. Sockopt set/get agreement: unimplemented options (Nintendo's 0x80000001
       linger-shaped 8-byte option above all) are feigned on set and echoed on get instead
       of answering SUCCESS then NOPROTOOPT — the NPLN stack verifies its socket options
       and abandons the connection on the mismatch.
    5. Zero-mask eventfd polls report readability (gRPC polls its wakeup eventfd with
       events=0), a one-time hold on the first npln resolution (the JIT-burst retention),
       and `[OpenPak] Module '<name>' loaded at guest <base>` logging so guest traces
       self-decode.
  - **Eliminated as eden's gate** (each fixed or proven equal to the working clients):
    identity/nnex, per-game claims, NAT check (both nncs instances answer), SNI routing
    (aauth + tenant certs verified from the workstation), the transport itself.
  - **The remaining gate, located exactly**: the title still never sends its first NPLN RPC
    (`nn.npln.auth.v1.Auth/IssuePrearrangedUserToken`; server sees `[CONN] begin (h2
    established)` then nothing). Eden's guest trace pins the NPLN background worker
    (thread 98) in a one-second timed loop at `svc 0x1C` (`WaitProcessWideKeyAtomic` — a
    condition-variable wait), pc `0x8eff56e8` (svc wrapper in the sdk module, guest base
    `0x8ef0a000`), called from `lr 0x8efc93b8` = **sdk+0xBF3B8**, waiting on a
    condvar+mutex pair at **main+0x85755d0** whose surrounding .bss is entirely zero at the
    freeze — state no init step ever produced. Corroborated once by the same thread
    faulting at PC=0 (a null call through that uninitialised state) when run under eden's
    gdb stub. The auth request is never even prepared: no `tenants/t-9f607adf-lp1/users/
    current` and no id_token anywhere in guest RAM at the freeze (both were present in the
    citron-era captures).
  - **Next**: name what should initialise main+0x85755d0 (Ghidra project `sdfull`, main VA
    0x85755d0, .bss; xrefs from the SDK init path — the null-callback slot is the same
    object family), diff that init's inputs against Ryujinx (acc/nifm/glue answers), or
    port citron's deferred-poll machinery (the one client-side repair not yet in eden,
    though eden's sliced polls already avoid the starvation it fixes).
  - Tooling note: eden's gdb stub suspends the title at boot and is not usable as-is; the
    working method is host-side `/proc/<pid>/mem` scans of the `/memfd:HostMemory` mappings
    (guest RAM), with the loader's new module-base log line providing guest-VA context.
- 2026-09-07 (session 15 — **moved to `Openpak/servers/stardew-valley`, identity switched from
  the Nextendo account server to OpenPak's `nx-baas`**). No emulator run this session.
  - Module is now `openpak/stardew-valley`. The only external dependency is nx-baas's internal
    API: `POST /internal/switch/identity` with `{"nnex": …}` (adapter proves the HMAC it signed
    with `NEX_SIGNING_KEY`) or `{"pid": …}` (already proven from our bearer) → `{pid,
    baas_user_id, nickname, friends[{pid, baas_user_id, nickname}]}`. Added in nx-baas
    (`internal.go`, mounted on its game API port 20070, guarded by `NX_INTERNAL_KEY`).
  - `NEXTENDO_ACCOUNT_URL`/`NEXTENDO_INTERNAL_KEY`/`NPLN_ALLOW_UNVERIFIED` are gone:
    `NX_INTERNAL_URL` (default `http://127.0.0.1:20070`) + `NX_INTERNAL_KEY`. The verified gate
    is implicit (nx-baas mints a projection only for active, verified accounts).
  - NPLN user id = `u-` + base32(sha256("npln-user:" + baas_user_id))[:12] — same 22-char shape
    the client accepted before; NSA id in friend lists = the BAAS user id. Refresh-token prefix
    is now `openpak-npln-refresh.`; default listener `:21010` per `Openpak/ports.md`.
  - **Relicensed AGPL-3.0-only** (`LICENSE`, matching the other OpenPak servers). The
    PolyForm-licensed generated bindings are gone: `proto/**/*.proto` is the schema recovered from
    the protocol descriptors (vendor options dropped, `go_package` ours), `proto/generate.sh`
    regenerates `*.pb.go` with stock protoc; tests pass on the regenerated code.
  - Not done: no live re-test on OpenPak. Next = point one Ryujinx profile's BAAS at nx-baas
    (21000, `ACCOUNT_BACKEND=openpak`) and route the tenant SNI to 21010, then re-run the
    session-14 list (host, list, join, 3 players, rejoin, host leave).
- 2026-09-05 (session 14 — **nnex-via-account-server flow verified with real logins, the whole
  session-12b hardening list passed, 3 players in one farm, and a first-join flakiness root-caused
  and fixed (`7896f63`)**). No RE this session; everything on the local stack with Ryujinx.
  - **New auth flow works live:** OutboundHost (pid 1800000003), OutboundJoiner (1800000004) and
    stardewhost (1800000005) all resolved through `/internal/pid-by-nex-token` with no
    `NEXTENDO_SECRET` anywhere. Endpoint answers 401 for a bogus token from inside the stardew
    container.
  - **Hardening results:** (1) host quits normally with the joiner inside → `[MM] host … left: farm
    … closed`, joiner's docs deleted and both keep-sessions closed within 1 s; the joiner shows the
    game's own "server closed the connection", no error applet. (2) Keep-alive: `kill -STOP` on the
    joiner → seat freed in **25 s** (15 s ping + 10 s timeout), others notified with DELETED pushes.
    `kill -STOP` on the host with two farmhands inside → farmhands left on their own at 13 s (Pia
    silence), server closed the farm at 24 s; `kill -CONT` afterwards took the host straight back
    to the main menu (unsaved day lost — it never saved the two cabins). (3) **Three players**:
    stardewhost joined as rank 3 (profile `~/ryujinx-instances/stardew`, needs
    `NEXTENDO_ALLOW_SHARED=1`), ranks/upcsid 1..4 served correctly; the joiner rejoined after its
    kill as rank 4 on the first attempt. A fourth seat exists (`stardew-join` profile = stardewjoin,
    pid 1800000007) but was not exercised.
  - **First-join flakiness root-caused (also seen 20:21 tonight and as the "stale joiner" in
    session 12):** the host's roster was CORRECT (seq, host id, session id, slot for the new
    upcsid), the joiner just dropped the first delivery, and Pia re-wrote the identical 207-B roster
    every second for 12 s — all swallowed by the per-stream content dedup in `w.push`, so
    WaitHostConstantId hit 12 s → 2318-1201. Fix `7896f63`: `deliver` (the write path) forces
    delivery for `__stu` mailbox docs; the 3-s re-push loop keeps its dedup (that loop was the real
    source of the session-12b write storm). After redeploy: 40 pushes in 15 min, joins first-try.
  - **Farm listing facts learned:** the farm list is a friends-only query (`users=` = the caller's
    friend list), so a new tester must be friended first — done for stardewhost via the account
    server's `/internal/friend-request` + `/internal/friend-accept` (`{"from":…,"to":…}` /
    `{"pid":…,"from":…,"accept":true}` from inside `nextendo-local-account-1`). The GAME then
    filters client-side on `_Pia_SystemData`: a friend's farm shows only if your id is in
    `farmhands` or `newFarmhands` is `True`. Cabins cost 4000g on this build; the host only
    republishes the properties on join/leave/day change, NOT when a cabin is built, so after
    building one make someone leave+rejoin (or sleep) before the newcomer refreshes.
  - **Save editing (host profile):** Switch saves live in
    `~/ryujinx-instances/host/bis/user/save/0000000000000006/{0,1}/Test_448041634/Test_448041634`,
    plain zlib over the PC-style XML, two identical journal copies — edit both while the game sits
    at the main menu. Added 40000g + 300 Wood/Stone (backup in the session scratchpad only).
  - **Left running at hand-off:** three Ryujinx instances (host 819639, joiner 962011, third
    851328) and the local stack. PRs #8 / #3 / #23 still open, zero comments. Box unchanged.
  - **NEXT:** (1) re-test the third player once the host republishes (joiner leave+rejoin) and
    try the 4th seat; (2) session-12b leftovers: the ~15 s `TurnJob::WaitServerConfig` delay, strip
    `NPLN_GS_DIAG` payload logging before any public deployment; (3) production bring-up once the
    five user-side blockers from session 13 clear.

- 2026-09-05 (session 13 — **production box prepped, three upstream PRs open, nnex proof moved to
  the account server, citron re-tested and still blocked at 2321-4992**). No RE this session.
  - **Commits here:** `d83dc12` drops coturn's `--relay-ip` (on OCI the public IP is 1:1 NAT and
    not on any interface, so binding relays on it fails; `--external-ip` alone is right).
    `753bfdb` removes `NEXTENDO_SECRET` entirely: `pidFromNexToken` now POSTs the nx2 token to the
    account server's `/internal/pid-by-nex-token` with `X-Internal-Key`; refresh tokens are MACed
    with a key derived from the persisted ES256 signing key. Local stack validated: the new image
    runs and the account container answers the endpoint from inside the compose network (a bogus
    token gets 401 "jeton nex invalide"). An emulator login on the new flow is still to be run.
  - **Upstream PRs (all from forks under `tobagin`, the user has no push rights on the org; all
    English, minimal, no attribution):**
    - nextendo-account **#8** `Add /internal/pid-by-nex-token` — the endpoint existed only in the
      account repo's UNCOMMITTED working tree (alongside Outbound's photon-room/invitations WIP,
      which stays there for the Outbound side to PR). Ours is the trimmed 22-line version.
    - sni-router **#3** `Route the Stardew Valley NPLN tenant to BACKEND_STARDEW` — the
      `deploy/sni-router-stardew.patch` file has header-less hunks (git apply rejects it); the PR
      is the applied version. After merge the main server needs `BACKEND_STARDEW=<box>:18501`.
    - Ryujinx-Nextendo **#23** `Stardew Valley: built-in patches for in-game TLS and the NPLN SDK`
      — `NextendoStardewPatches.cs` + the `ModLoader` hook were UNTRACKED locally and absent
      upstream: no official build could reach the tenant (in-game static OpenSSL rejects the
      replacement chain; then the SDK's "certificate accepted" byte cancels pre-HEADERS). Verified
      the working `~/ryujinx/Ryujinx` binary embeds the class. Trimmed to 66 lines, HLE builds
      clean. Method stays `Verser` to match the S3 class the loader calls.
  - **Production box = the user's `qw-eu` (ubuntu@145.241.199.19, OCI A1.Flex 4/24, Ubuntu 24.04
    arm64, London), NOT a dedicated VM:** still runs openpak (14 containers, localhost-only),
    qw-cloudflared (kept for tunnels), rookery-agent, host Caddy on 80/443, system zerotier
    (`tobagin-network`, 10.212.63.196). 29 `qw-*` quadlets stopped and parked in
    `~/.config/containers/systemd.disabled/`. Fixed a 44-day hung `apt-get update` (CLOSE-WAIT
    sockets, mirrors fine) that held the lock; unattended-upgrades then applied 98 packages.
    Prepped: repo rsynced to `~/stardew-nextendo` (no git remote; `--exclude .git --exclude
    deploy/oci/.env --exclude deploy/oci/certs`), iptables-persistent rules TCP 18501 / UDP 3478 /
    UDP 49152-65535, `deploy/oci/.env` (PUBLIC_IP, random TURN secret, INTERNAL_KEY still
    `change-me`), both images built with `podman compose build`, coturn UP and answering STUN
    locally, `podman-restart` user service enabled.
  - **Still blocked on the user:** (1) OCI security list ingress for those three port ranges —
    external probes never reach the VM (iptables counters stay 0); (2) cert+key from the Nextendo
    CA into `deploy/oci/certs/` (Ryujinx clients would accept anything thanks to patch #23, real
    hardware under Prelude would not); (3) `NEXTENDO_INTERNAL_KEY`; (4) merge+deploy PR #8 and
    reach `/internal/*` PRIVATELY — the production proxy returns 404 for every internal route and
    nextendo-docs/ARCHITECTURE.md says they must never be public; the box already sits on the
    user's ZeroTier network, so joining the main server to it and pointing `NEXTENDO_ACCOUNT_URL`
    at its ZeroTier address needs no proxy change; (5) sni-router PR #3 + backend env.
  - **citron re-test (both personas, local stack):** base 1.2.34 dies ~6 s in with an uncaught
    .NET AggregateException before any networking; on 1.6.15.13 both patches work (TLS completes,
    h2 established on the local server) and the game then closes every connection with zero RPCs
    → 2321-4992, identical to 2026-09-02. Upstream citron has neither the patches nor a fix;
    Ryujinx remains the only client. The joiner citron persona's `qt-config.ini` pointed at
    production and was pinned to 127.0.0.1 (backup `qt-config.ini.bak-prod-2026-09-05`).
  - **Environment notes:** ssh needs a tty or GUI for the key passphrase — `ksshaskpass` is now
    installed; recipe: `ssh-agent -a /tmp/claude-1000/agent-stardew.sock`, then
    `SSH_ASKPASS=/usr/bin/ksshaskpass SSH_ASKPASS_REQUIRE=force ssh-add ~/.ssh/id_ed25519`. gh's
    stored credential helper points at a missing `/usr/bin/gh`; push with
    `git -c credential.helper= -c credential.helper="!$(command -v gh) auth git-credential"`. The
    local stack now starts with `podman compose` (docker-compose provider → hyphenated names
    `nextendo-local-stardew-1`, image `localhost/nextendo-local-stardew`); build the Stardew image
    with `podman build -f deploy/oci/Dockerfile .` and tag both `nextendo-local-stardew` and
    `nextendo-local_stardew`. Emulators: start them to the game list, never pass the NSP.
  - **NEXT:** unchanged hardening list from session 12b (host quits with joiner inside, keepalive
    kill test, 3+ players), plus: run one emulator login on the new nnex flow locally, then the
    production bring-up once the five blockers above clear.

- 2026-09-04 (session 12 — **ROOT CAUSE FOUND AND FIXED SERVER-SIDE: Pia keys its NAT/TURN station
  tables on the `upcsid` field of each participant's `__pus` document, the consoles write `upcsid: 0`
  into their own document, and our server served that placeholder back to the peer, so the joiner
  built the host's station with id 0 and could never set up a relay to it. After forcing
  `ussid/ucsid/upcsid = rank` on every served member document, a join went through: both NAT tables
  hold each other's real ids, the joiner's glue reads state 9 (same as the host), no error applet.**
  Whole session was static RE + live polling on the same joiner (pid 302386) / host (pid 3211831).
  - **Session-11 belief corrected: the network connect does complete on the joiner.** The
    `NplnBackgroundProcessJob` (NplnProtocol+0x260, vtable `0xba5e150`) runs
    StartConnectNetwork(`bpWaitConn_7ab5140`, arms slot 25) → WaitConnectNetwork(`bpDisp_7aafd40`,
    polls an async request at job+0x27d8) → the request's completion callback is
    `nn::pia::nplnd::NplnPlugin` slot 5 (`g8540_7c38540`, via `ga35c_7c3a35c`): it takes the
    participant list, uses the **lowest `upcsid` as the host**, sets role 1/2, adds one station per
    participant (`addsta_7c38f70` → `natAdd_76fc9f0(NplnProtocol+0x1460, upcsid)`), and then
    `connOk_7c3c380` **starts the AttachMeshJob** (`am0_7c3db30`). Participant entry layout (parser
    `pusParse_7b572e8`): +0 valid, +8 `uid`, +0x48 `ussid`, +0x4c `ucsid`, +0x50 `upcsid`, +0x58
    `pgn`, +0x78 `pusa`; getters `0x7b5743c/54/5c/64` read valid/ussid/ucsid/upcsid.
  - **The 12-s failure = `AttachMeshJob::WaitHostConstantId` (`am4_7c3ed28`)**, deadline 12000 ms set
    in `am3_7c3e4c0` (10000/15000 only in mode 0x1b). It polls NplnFacade slot 0x220
    (`hostIdGet_75f0964` → NplnProtocol+0x198/+0x1a0 = host {constant id, index}). Constant ids are
    the account principal ids: `0x6b49d203` = 1800000003 (host user), `0x6b49d204` = the joiner. On
    timeout am4 cancels the connect and the code lands in NplnPlugin+0x6a4, which `bpErrMap_7ab04c0`
    copies into the job (that is why `0x648e` with am4's location `0x250a6b1c438` sat in the job).
  - **The host id reaches the joiner via the 207-B mailbox roster** (`01 11 …`), handled by
    NplnProtocol slot 117 (`rcRecv_7c427c8` → `np40a4_75f40a4`). Wire header (BE, 30 B,
    `rosterDes_75ff0d0`): ver u8, type u8 (0x11), len u16, seq u32 @4, hostIdx u16 @8, hostConstId u64
    @0xa, sessionId u64 @0x12, flag u8 @0x1a, stationCount u16 @0x1b (=8), bool @0x1d, then 8×22-B
    station slots. Accept path stores +0x198/+0x1a0, +0x15c = seq, and acks with type 0x12
    (`rosterAck_75f35f4`); a duplicate seq only re-acks; any failed gate drops silently. Gates: type,
    length, seq > +0x15c, `np+0x200->vtbl[0x50]`/ideq with the caller's id, count == +0x121e,
    non-empty table at +0x1b0, per-station parse, and the joiner's requested network id (+0x1268,
    `{ff×8, upcsid, 0x4e96}`) present among the slots. The session-id check (slot 0x3a0) and the
    +0x311 "connected" gate (slot 0x3b8) are both `return 0` stubs for NplnProtocol. The joiner learns
    the 8-byte session id from the first accepted roster (`rcRecv` sets NplnSessionProperty+0x90).
  - **Live runs this session** (`nplnpoll.py`, `rosterpoll.py`, `turnpoll.py`, `nattbl.py`,
    `sessid.py`, `bpread.py`, `livevt.py` in `scratch/tools/`; logs `nplnpoll-joiner-s12.log`,
    `rosterpoll-joiner-s12.log`, `turnpoll-joiner-s12*.log`):
    - 16:36 (stale joiner): roster never accepted, no ack, WaitHostConstantId → 0x648e at 12 s.
    - 17:17 (after the joiner's objects were freed): roster accepted in <1 s (seq 6, host id/idx
      `6b49d203/0x17`, my id `6b49d204/0x2e`), AttachMeshJob state 4 for 15 s (TurnJob:
      WaitServerConfig → StepResolveServerAddress; the joiner's TURN Allocate only went out at +15.5 s),
      then WaitSetupRelayAddress (`am10_7c3fb04`, waits TurnProtocol slot 3 `tsec3_7abb1d8` = every
      ring station in state 4) for 14.5 s until the TURN watchdog `e648eA_7ab9870` raised 0x648e
      (station state <2 for >14000 ms). **The TurnProtocol work ring held one station id: 0**, and
      the live NAT table (`nattbl.py`) showed the joiner's host entry as `{id=0, peer=0, state=0xe}`
      while the host had no joiner entry at all. `rr1_76fb810` pushes every +0x1460 entry in state
      0xe into the ring, so the id came straight from the `__pus` `upcsid` field.
    - Server docs at that moment: host `__pus` = `ucsid:1, upcsid:0 (then an empty map), ussid:1`,
      joiner = `ucsid:2, upcsid:0/{}, ussid:2` — written by the consoles themselves; our
      `fields()` returned the stored doc verbatim (the synthesized `userSessionFields` only applies
      when nothing is stored).
  - **Fix (`internal/npln/gamesync.go` `withStation`, deployed 17:26 UTC):** every served
    `__pus`/`__us` document gets `ussid`, `ucsid`, `upcsid` overlaid with the participant's rank.
    Rebuilt manually: compose's `dockerfile_inline` is not supported by the docker-compose provider
    podman uses here — write the Dockerfile to a temp file, `podman build -t
    localhost/nextendo-local-stardew:latest -f <file> <repo>`, then `podman compose up -d --no-build
    --force-recreate stardew`. The restart wipes the in-memory farm; the host must re-host.
  - **Result (17:27 join, host re-hosted):** roster pushes both ways within the same second, NAT
    tables correct on both sides (`joiner: [1]{id=1,peer=6b49d203,state=0xd}`, `host:
    [1]{id=2,peer=6b49d204,state=0xd}`), AttachMeshJob object went idle 0.25 s after the host id
    landed, joiner glue state 9, streams stayed open, no error window; live jobs show
    `WanConnectNetworkJob::CompleteProcess`, `StartupSessionJob::CompleteProcess`,
    `NatTraversalJob::ProcessSuccess`. User confirmed: **both consoles show as connected.**
    Transport verified live: direct UDP both ways between the two Pia ports (10.87.0.2:53066 ↔
    :63021, ~16 pkt/s each way, 61–317 B), `pktstats.py` = ~1500 decrypt-ok / 0 fail on each side,
    `readsess.py` local+host stations assigned on both (host `6b49d203`/21, joiner `6b49d204`/42),
    host wrote `__gs/m` `_Pia_SystemData` with participant count 2.
  - **CO-OP WORKS END TO END (user-confirmed ~17:40 UTC): the joiner is in the host's farm, both
    players see each other, movement and actions flow both ways.** The minute or so of small-packet
    traffic right after glue state 9 was just the world sync settling, not a blocker. Stardew
    Valley Switch multiplayer runs fully on this clean-room NPLN server + coturn/nncs stack.
  - **Session 12b (same day, 17:37–21:20 UTC) — disconnect/rejoin hardening, three more server
    fixes, all deployed and verified live:**
    - **Flaky rejoin (2318-1201 on roughly every other rejoin) — ROOT CAUSE + FIX.** The
      per-push logging showed the write path (`WriteDocuments` → `deliver()`) pushed the
      console-written `__pus` document to both consoles with the placeholder `upcsid=0` — the
      rank overlay only applied to listings and wake re-pushes. For one push the peers saw a
      participant with index 0 (= "host"), and whether the plugin ran inside that window was a
      race. Fix: `withStationLocked` is applied on the write-path delivery too (commit 6380077).
      After it: 5/5 quick quit→rejoin cycles connected within 1 s (21:10–21:14), including a
      "reload the game with the character online" case, which the game turns into a normal
      delete+close. Slot order in `WanConnectionStatus` no longer matters (a host-first order
      succeeded at 21:09:58).
    - **Farm never closed when the host quit** (QueryGameSessions kept listing it): nothing
      removed matchmaking members on stream close. `dropMember` (sessions.go) now removes the
      member on gamesync stream close and closes the farm when the host (rank 1) goes — first
      version compared the full user name with the short uid and never matched ("2 left"); fixed
      to compare `lastSeg` (b59864b). Verified: "host … left: farm … closed", next query → 0.
    - **Vanished consoles**: the gRPC server only *permitted* client pings; it never probed.
      Added `KeepaliveParams{Time 15 s, Timeout 10 s}` (server.go) so a console that dies without
      closing its connection loses its seat within ~25 s (not yet exercised live).
    - Diagnostic logging added: every `__pus` push (`fields=…, upcsid=…`) and `target N LISTED`.
      DELETED pushes print `upcsid=0` because they carry no fields — not a placeholder.
    - **Environment lessons (cost ~1 h):** the emulators must be launched through
      `scripts/launch-ryujinx.sh` on the `host`/`joiner` profiles signed into OutboundHost
      (1800000003) / OutboundJoiner (1800000004). Those profiles had lost `nextendo_account.txt`
      (bare launches, "identity not provable (no valid nnex claim)"); the original passwords were
      never recorded, so they were reset via the account service's `/api/forgot` + `/api/reset`
      (dev mode logs the reset link) — new credentials in
      `~/.local/share/stardew-nextendo-research/local-accounts.txt`. Signing in from the Nextendo
      menu created a SECOND emulator user profile (new user id → empty save set → "the farm is not
      there"); fix = set `profile_user_id=00000000000000010000000000000000` in
      `nextendo_account.txt` and remove the duplicate from `system/Profiles.json` (the Test farm
      is save dir 6, owner user …0001, per `bis/system/save/8000000000000000/1/imkvdb.arc`).
      `pkill -f` patterns that match your own bash kill the launcher; `pkexec` prompts time out
      when the user is away.
    - Tools this part: `leavepoll.py`, `netstpoll.py` (roster gates + relay ring), `sttbl.py`,
      `nattbl.py`; logs `leavepoll-*.log`, `netstpoll-joiner*.log`.
  - **Production plan written (`docs/production.md`, `deploy/oci/`, `deploy/sni-router-stardew.patch`):**
    official emulator builds map `*.nintendo.net` to `NEXTENDO_SERVER_IP` with the port untouched
    (the `host=ip:port` route file is local-testing only — `DnsMitmResolver.cs`), so the tenant
    lands on the main server's `:443` and sni-router needs a `t-9f607adf-lp1` rule; gamesync/relay
    is reached at `NPLN_RELAY_HOST:PORT` directly (no DNS); the box must run the PATCHED coturn;
    the account server is called over `NEXTENDO_ACCOUNT_URL` with `X-Internal-Key` (9c5b193).
    Late finding of the same evening: the last rejoin drop was a SECOND server bug — `withStation`
    merged the host's MAILBOX blob (`__stu/<uss>` `pl`, written by others) into the host's member
    doc; removed (39271c1). Keepalive drop measured live: frozen console gone in 19 s.
  - **NEXT (hardening):** (1) host quits **while the joiner is still inside** — not yet observed
    (the one attempt had the joiner leave first); watch for `NplndHostMigrationJob` and what the
    joiner shows; (2) exercise the keepalive drop: kill a console outright and confirm the seat is
    freed in ~25 s; (3) a second joiner (3+ players); (4) the ~15 s TurnJob delay before its first
    Allocate (`TurnJob::WaitServerConfig`); (5) strip `NPLN_GS_DIAG` payload logging before any
    public deployment (it prints station blobs).
  - Still open, lower priority: why the TurnJob needs ~15 s before its first Allocate
    (`TurnJob::WaitServerConfig` polls the `IceServerConfigGetter` slot 0x30 until it stops returning
    0x10408); with NAT traversal now succeeding the relay path may not matter on a LAN.
  - Hex-arithmetic reminder that bit me twice more: Ghidra `func_0x07cXXXXX` is VA `0x7bXXXXX`
    (subtract 0x100000, borrow across the nibble).

- 2026-09-04 (session 11 — **the mailbox is DECRYPTED, both directions, live: the per-session
  transport key is recovered and the "78-B mailbox packets" are wan::NatTraversalProtocolMessages.
  Signaling is HEALTHY — both consoles exchange candidate addresses and CONSUME them. Two
  session-10 beliefs are REFUTED: the joiner DOES act on the host's reply, and it DOES run a
  NatTraversalJob. The real structural gap: the joiner never gets a local station.**):
  the host had crashed and was relaunched (`host-run11.log`, pid was 3211831); joiner reused
  (pid 302386). One clean host+join, captured at the moment the joiner wrote `__mt/nat_traversal`.
  - **Per-session key (both consoles, symmetric): `2d90ce940bc277399b42f8c6b0764d14`.** It is NOT
    from the static keytab — it lives only in RAM at `SessionPacketReader+0x14` / `SessionPacketWriter
    +0x14` (both classes `nn::pia::session::…`), 16 bytes, and `+0x10`=1 means encryption on. AES-GCM;
    nonce = `u32(NplnProtocol+0x2d8 -> +0x90)` prefix `|| packet+0x15` (8-B header nonce). Derivation
    traced statically: send path `wos3_7702e64`/`wis3_7704308` build the nonce via `nid_75f2410`
    (reads `net+0x2d8 -> +0x90`), `net` = `WanInputStream+0x60` = the NplnProtocol.
  - **How to read it live (no wire capture needed):** `tools/vtscan.py --range=10000000-20000000 <pid>
    13f22928 13f229d8` finds the live `SessionPacketReader`(vt `0x13f22928`) and `SessionPacketWriter`
    (vt `0x13f229d8`); `tools/pktring.py <pid> <reader> <writer>` dumps key, nonce prefix, the
    StationManager (`transport+0x120`, class `SessionStationManagerInternal`; local at `+0xa0`,
    station list head `+0x20`/node `{+8 next,+0x10 station}`, station `{+0x10 addr,+0x38 id,+0x40 u16
    idx,+0x48 state}`) AND the receive ring (`reader+0x48`: array `+0x10` stride `0x1d18`, cap `+0x18`,
    start `+0x24`; packet: `+0x8` magic `64 98 ab 32`, `+0xc` flags(bit7=still-encrypted, so 0x10 =
    already decrypted in place), `+0xe`/`+0x10` u16 dst/src station index, `+0x14` footsz, `+0x15`
    nonce8, `+0x1d` tag8, `+0x4d` body, `+0x1c90` len, `+0x1ce0` consumed). `tools/livering.py <pid>`
    does the scan+dump in one shot. Full decode + parser in `scratch/mailbox-decoded-1624.md`,
    `livering-{host,joiner}-postfail.txt`. **These are keys/captures — scratch only, never the repo.**
  - **Decoded messages (body = `0600` <type> <payload> … `<stationId4><idx2>`):**
    - type `0x2a` = "advertise my address": the station's two candidates `10.87.0.2:P` and
      `127.0.0.1:P`, then its station id. Host→joiner advertised `10.87.0.2:62297`/`127.0.0.1:62297`
      (station `6b49d203`, idx 23); joiner→host advertised `10.87.0.2:57168`/`127.0.0.1:57168`
      (station `6b49d204`, idx 41).
    - type `0x29` = connectivity-check / candidate-pair (carries candidate pairs, other reflexive
      ports e.g. 52089/58171 host, 64295/55710 joiner).
    - **Both rings read `consumed=01` for every message** — reception AND processing happen both ways.
      So the relay signaling path works end to end; the session-10 "joiner never acts on the host's
      reply / never sends" reading was wrong (it was measuring a stale/torn-down instance).
  - **The real gap, seen live at failure:** host `StationManager.local(+0xa0)` = assigned (id
    `6b49d203`, idx 23, state 2); **joiner `StationManager.local(+0xa0)` = 0x0** — the joiner never
    creates its own local station, so no mesh member exists on its side and `AttachMeshJob` funnels to
    `CompleteFailure` after the 30-s deadline.
  - **`piajobs.py 302386` at failure shows the joiner DID run the NAT path:** live strings include
    `NatTraversalJob::WaitNatTraversal`, `NatTraversalJob::ProcessSuccess`, `TurnJob::WaitServerConfig`,
    `TurnJob::StepResolveServerAddress`, `NplndLoginJob::WaitLogin`, `AttachMeshJob::CompleteFailure`.
    So a `wan::NatTraversalJob` exists on the joiner (refuting session 10's "no NatTraversalJob ever
    exists on the joiner"). It even reaches `ProcessSuccess`, yet no local station is assigned.
  - **NEXT:** find where the joiner is supposed to create/assign its local station and why it doesn't.
    Concrete: (1) on the host, `local` station is built during connect; on the joiner it stays null —
    decompile the station-manager "add/assign local station" path (`SessionStationManagerInternal`
    ctor + the add-station call reached from `AttachMeshJob`/`NatTraversalJob::ProcessSuccess`) and
    read, live during a stall, why the joiner's branch is skipped. (2) The type `0x2a`/`0x29` handler
    that turns a received advertise into a station entry: the receive dispatch is
    `disp11_7669920` -> `hdisp_766a0f0` (inserts into the per-reader message list) and the
    station lookup is `findidx_7679c40`(by u16 index)/`findaddr_7679d40`(by address). Check whether the
    joiner ever calls the "create local station" vs only "peer station" path. `pktring.py` already
    reads the station table live, so bracket a stall and watch `local(+0xa0)`.
  - Tools added this session (`scratch/tools/`): `pktstats.py` (per-thread Pia decrypt-ok/fail
    counters, table `G_MAIN+0xbd50740` stride `0x16c`, ok`+0xac`/fail`+0xb0`), `vtscan.py`
    (`--range=lo-hi` fast window scan for a vtable-fns value), `pktring.py`, `livering.py`. New
    decompiles in `~/ghidra-projects/out3/` (relay send/recv, header crypto `hdrA..hdrE`, packet
    reader vtables `rxA_7668ce0`/`tailc_76429f0`/`pr13`/`spr*`, stream nonce `nid`/`wos3`/`wis3`,
    station lookups). NOTE: `pkexec` runs from `/root` — always pass tool paths ABSOLUTE.

- 2026-09-04 (session 10 — **the "state-7 stall" was a modal error applet; 2318-1201 decoded exactly;
  one real server bug (ghost stations) FOUND AND FIXED; the true blocker re-characterised on a clean
  run: the joiner never acts on the host's mailbox reply**):
  - **Not a hang.** `hyprctl clients -j` showed the joiner window `Error Code: 2318-1201`; the user
    confirmed Join → Connecting → 1201 → glue parks at 7 until OK. Sessions 9c-9f watched a dialog.
    Always check for an `Error Code:` window before calling anything a stall.
  - **2318-1201 = Pia result `0x648e` = AttachMeshJob wait-for-connect TIMEOUT.** The immediate 2318
    (`0x90e`) occurs at exactly one site (`0x75e982c`, Pia's error-code builder, so module 318 is
    Pia's own); the 23 KB converter `0x75e7144` returns full codes as ints (`0x648e→23181201`,
    `0x6488→23181200`, `0x648f→23181202`). `0x648e` is emitted by `am4_7c3ed28` when the tick passes
    the deadline at job+0x128 while polling `facade->vtbl[0x220]` (= NplnProtocol+0x198, the pending
    connect result, still all-zero live) — location `0x250a6b1c438` is stamped in the live job
    objects. `0x6479` is the same timeout with the inner object still busy; `0x6c05` = cancelled.
  - **Pointer chain resolved live** (`scratch/tools/readtask2.py`, `readdeep.py`): taskCtx → sess →
    inner(+0x80) → *(+0x18) → cbSession `0x160e8900` (`nn::pia::session::Session`) → mgr(+0x68) →
    (+0x80)+0x30 = **`nn::pia::npln::NplnFacade` `0x14e963c8`** (slot 8 = `0x75ef280`) → facade+0x30 =
    **`nn::pia::npln::NplnProtocol` `0x15e15c28`** (`fstart_7814`) → `ncnstart_30f4` →
    `NetConnectNetworkJob` in `WaitConnectNetwork` (`ncnwait_3268`). Every layer is one nested async
    wrapper with the same {state, status} object shape; `c8c4` copies status(+4) straight into the
    Result, so `0x648e` was visible as "busy status" all along. NplnProtocol fields: +0x128 Session,
    +0x130 NplnFacade, +0x1460 station table (count@0, entry-ptr array@8, local id@0x18; entry =
    {id@0, peer@8, state@0x10}), **+0x1470 `nn::pia::nplnd::NplndRelayClient`**, **+0x1500
    `nn::pia::turn::TurnProtocol`** (secondary vtable `0xba5e678`), +0x1268/+0x1288 requested/current
    network id `{ff×8, index, 0x4e96}`.
  - **Server bug FIXED (`internal/npln/gamesync.go`, deployed 15:34 UTC):** a departing joiner's
    `DeleteDocument` on `__pus/<uss>` was relayed as UPDATED-with-empty-fields and a dropped stream
    sent nothing, so the host's Pia session kept a ghost station and wrote 78-B NAT-traversal
    messages into a dead mailbox (`__stu/03a2f353…`) for 40+ minutes; every later joiner got NO 207-B
    roster, never wrote to the host mailbox, and timed out — that ghost state is what 9c-9f measured.
    `deliver()` now takes a kind and pushes `DocumentChange_DELETED`; stream close deletes+pushes
    `__pus`/`__us`. Verified live: fresh join gets the roster in <1 s, 78-B messages flow both ways,
    host stops writing after the DELETED push. Container rebuilt manually (dockerfile in compose is
    `golang:1.26-alpine` + `go build ./cmd/npln`; note the compose file has several
    `dockerfile_inline` blocks — copy the stardew one, not the first).
  - **Real blocker, on a CLEAN run** (`scratch/rejoin2-lo.pcap`, `joinstrace2.log`,
    `mailbox-1534.txt`): host → joiner 207-B roster (`01 11 00 b0`, stations {ff×8|1, 0x4e96} host and
    {ff×8|2, 0x4e96} joiner, host constant id `6b49d203`), joiner → host 9-B `01 12 … 02 01`, then
    78-B `32ab9864 90 2200 <svid> …` both ways every ~5 s (joiner svid 0, host svid 0x12; later
    variants `9053`/`9032`). Host starts a `wan::NatTraversalJob` and probes the joiner's Pia port
    (93-B, 127.0.0.1 and 10.87.0.2, 14 s). **The joiner's Pia socket (fd 315, a fresh bind per join)
    never attempts a single sendto and receives nothing but NNCS** — no NatTraversalJob ever exists on
    the joiner (`piajobs.py`), it just re-sends the 78-B request every 5 s until the 30-s deadline
    (12 s on a stale joiner). Both consoles get a valid TURN Allocate Success (+10/+15 s) and never use
    it; the joiner's `__mt/nat_traversal` report `{rc:10, re:true}` (decoded in `natrepcc_7c40560.c`:
    13 = no TurnProtocol, 11 = TurnProtocol errored, 12 = facade result set, station state 0xd → 1,
    0xe → 10, or 2 if the relay still reports allocated) is written after cleanup, so it only says
    "NAT traversal failed". :34343 datagrams are telemetry only (host-create, and at failure).
  - **Connect-path logic (static):** `NplnProtocol` slot 122 (`rr4_76fab68`, shared with WanProtocol/
    NplndProtocol) picks `TurnProtocol->vtbl[0x38]` only when `tsec8_7abba54` finds the station in
    TurnProtocol's per-station table (+0x8d0, stride 0x238) in state 4, else
    `NplndRelayClient->vtbl[0x50]` (`rc10_7c43084` → `relaysend_b_7c3c218` → `rsend_7c3a820`, which
    appends a byte and hands the packet to the sender at client+0x18 `vtbl[0x80]` addressed by
    stationInfo+0xb4) — i.e. **the 78-B mailbox packets ARE the NplndRelayClient channel**; the
    "relay" is our gamesync mailbox. Relay flag (Session+0x529 ← setup byte +10 ← Session+0x144)
    is 1 on BOTH consoles (ctor default `set144a_7609010`, glue sets it again via
    `set144b_760bfe0` from `gluesess_1ac1260`), so it is not the host/joiner discriminator.
  - **Why the joiner ignores the host's reply is the open question.** The host decrypts the joiner's
    packets fine (it probes), so the key is symmetric. Brute force of the 16 static keytab keys ×
    header layouts × nonce suffixes (incl. 0x4e96, host constant id) against the captured 78-B
    packets: zero hits (same as session 4) — the key is per-session and lives only in memory.
  - Tools this session: `readtask2.py` (corrected chain), `readdeep.py`, `natpoll.py` (fixed to poll
    NplnProtocol; the one run polled the wrong object), `mailbox-1534.txt` (full pl bytes both ways,
    from the server's DIAG log). Root python has no numpy: `pkexec env PYTHONPATH=/home/tobagin/.local/lib/python3.14/site-packages python3 …`
    for `piajobs.py`/`jobdump.py`/`facdump.py`. `gdec.py` tags must not contain the VA.

- 2026-09-04 (session 9e/9f — **SYSCALL TRACE, PROPERLY CORRELATED: the UDP sockets meant to carry
  the P2P/mesh connection are created and bound, then NEVER connected or sent on, for the entire
  stall — confirmed at the socket-API level, not just inferred from the wire**): user asked whether
  we could read the process's network activity directly rather than infer from the wire; installed
  `strace` (`pkexec pacman -S strace`, not present on this box before).
  - `ss -tuapn` (needs `pkexec` — Ryujinx is non-dumpable, same reason memory reads need root) showed
    the joiner (pid 302386) holds a few UNCONN UDP sockets on ephemeral ports plus one ESTAB TCP to
    `127.0.0.1:8099` (the account API, routine). Unfiltered `strace -f -e trace=network` was useless —
    drowned in continuous `recvmsg`/`recvfrom` EAGAIN polling (netlink-shaped payloads, .NET
    runtime/interface-change-notification noise) plus benign SIGSEGV chatter from Ryujinx's own JIT
    fault-handling (always present, unrelated to networking). Narrowed to
    `-e trace=connect,sendto,sendmsg,socket,bind`.
  - **First pass (session 9e) was a false start, corrected by the user**: ran the narrowed trace for
    20s against the pid from the earlier packet-capture session and got zero hits — but the user then
    said they'd actually been sitting at the main menu, and a `readtask.py` read moments later showed
    glue state 4 (idle), not 7. That first "zero hits" result was uncorrelated with the actual stall
    and is worthless on its own — **always confirm the live glue state brackets the trace window**,
    don't trust a trace result without checking what state the game was actually in.
  - **Session 9f, done right**: started `pkexec strace -f -tt -e trace=connect,sendto,sendmsg,socket,
    bind,getsockopt,setsockopt -o joinstrace.log -p 302386` BACKGROUNDED (`run_in_background` — lets
    the polkit prompt resolve without a blocking-tool-call timeout, and lets the trace run for as long
    as needed) BEFORE the user attempted the join, stopped it after, and immediately confirmed via
    `readtask.py` that glue state really was 7 (local task state 5, busy status 3/`0x648e`) right at
    that moment — properly bracketed this time. Reading the full log: thread **302887** is the one
    doing Stardew's actual networking (every other thread's socket churn is Ryujinx's own
    AF_NETLINK/AF_UNIX interface-monitoring noise, or routine 8099 account-API reconnects). Its
    complete timeline: creates+binds a UDP socket (`t=04.7s`, ephemeral port) → two TCP `connect()`s
    to the NPLN server `127.0.0.1:18501` (`t=15.3s`, `t=15.4s`, both succeed) → three MORE UDP
    sockets created+bound at `t=14.6s/15.3s/32.9s` (exactly the local-port-allocation pattern you'd
    expect before ICE candidate gathering) → one more TCP `connect()` to `127.0.0.2:18501`
    (`t=20.9s`, matches this stack's `NPLN_RELAY_HOST=127.0.0.2` setting) → **then NOTHING**: no
    `connect()`, `sendto()`, or `sendmsg()` on any of those four UDP sockets for the rest of the trace
    (through `t=50s`, well past the confirmed state-7 read).
  - **Verdict, now airtight**: the P2P/mesh UDP sockets are allocated (socket+bind) but never used —
    never connected, never sent on. This isn't "packets get lost," it's "no attempt is ever made,"
    confirmed at the syscall level and properly time-correlated with a live state-7 read for the first
    time. Combined with session 9d's packet capture (nothing on the wire) and 9c's memory read (task
    believes it started), all three independent methods now agree precisely.
  - **How to apply**: strace is installed on n5air. Filter to
    `-e trace=connect,sendto,sendmsg,socket,bind` (unfiltered `trace=network` is unusable noise on a
    .NET/JIT process like Ryujinx); background it with `pkexec` + `run_in_background` rather than
    foreground it; ALWAYS bracket with a `readtask.py` glue-state read taken right after stopping the
    trace, don't assume the process was in the state you think it was.

- 2026-09-04 (session 9d — **PACKET CAPTURE, SAME SITTING: the vtable-dispatched connect call never
  touches the network at all — 19 seconds of total silence right through the stall window**): user
  ran `pkexec tcpdump -i any -w scratch/state7.pcap udp` (system-wide, no host filter — necessary
  since `pkexec`/`sudo` needs a GUI approval and a host-filtered capture can't be adjusted after the
  fact), then ran host+join. Capture ran 45s (`15:38:00`–`15:38:45`), 1.7GB, 648K packets — almost
  entirely unrelated Sunshine/Moonlight video-stream traffic from testing session 9's Sunshine fix
  (616K packets to port 47998) plus routine LAN/DNS/ZeroTier noise. Filtered to loopback + this
  host's own LAN IP (`10.87.0.2`) and read in full — small enough (dozens of packets) to eyeball
  every line rather than trust a display filter. **Complete Stardew-relevant timeline:**
  - `t=12.1s`: NNCS NAT-check burst (`127.0.0.1:51050` ↔ `:33334`/`:10025`, ~20×16-byte packets in
    <3ms) — the join attempt's startup probe.
  - `t=17.2s`: one TURN exchange on port 3478, decoded by hand from the raw STUN header bytes (no
    parser available): message type `0x0004` = **Refresh Request** (not a fresh Allocate — an
    allocation from earlier was already live), got the routine `438 Stale Nonce` challenge
    (`ERROR-CODE` attribute value `0000 0426`), retried with the challenge's nonce, and **succeeded**
    (`0x0104` Refresh Success, `LIFETIME=0x258`=600s). Routine keepalive, not a failure signal.
  - `t=26.4s`–`26.6s`: two 308-byte Pia monitoring packets (port 34343, decrypted with `piadec.py`
    after regenerating a `tcpdump -X` text log — it wants that format, not a raw pcap) — same
    mostly-`0xFF`-padded shape as routine telemetry seen in prior sessions, not conclusively tied to
    a failure event.
  - `t=26.6s` → `t=45s` (end of capture): **nothing.** No further STUN/TURN, no ChannelBind, no
    CreatePermission, no Send/Data indication, no direct UDP to any peer address, from either side.
  - **Correlation with the live task state**: read the same joiner process (pid unchanged) with
    `readtask.py` immediately after stopping the capture. First read looked like the glue had moved to
    state 1 with the task-context pointer null — but a second, immediate re-read came back state 7
    again with the SAME task object address (`0x14e915b8`) and IDENTICAL field values (local state 5,
    busy status 3, status code `0x648e`) as session 9c's original read. **The state-1/null read was a
    spurious/transient misread** (likely a stale `candidate bases` cache hit or a scan caught mid a
    Ryujinx memory-mapping shuffle), not a real change — always take two reads before trusting an
    unexpected transition. Corrected conclusion: the task has sat in the exact same
    started-but-silent state continuously from session 9c's read, through the entire capture window,
    to now — i.e. genuinely permanently stuck, not a timeout-and-reset.
  - **Verdict**: the task marks itself started (local state 5) and sits "busy" (status 3) indefinitely,
    but the vtable-dispatched connect call (`cb30`'s `target->vtbl[0x20]`, `f1f8`'s
    `facilityObj->vtbl[0x40]`) **never emits a single packet**, ever, for as long as we've now watched
    it. That points at a failure BEFORE the socket layer — most likely it can't resolve/obtain a peer
    address to connect to at all — rather than a reachability/NAT/relay problem on the wire. This
    changes the next-step priority: resolving the concrete vtable implementation (to see why it
    doesn't even attempt a send) is now more promising than further packet analysis.
  - Tools: `tcpdump -r <pcap> -w <out> <filter>` to shrink before analysis (don't run `tshark` —not
    installed; also no `capinfos`); `piadec.py` needs a `tcpdump -X` text log, not a raw `.pcap`,
    regenerate with `tcpdump -r file.pcap -X -nn > file.log` first.

- 2026-09-04 (session 9c — **LIVE READ, SAME SITTING: the state machine is NOT the bug — the async
  connect genuinely started and is genuinely still pending at the real network layer, below anything
  decompiled so far**): user hosted + joined live (Ryujinx host/joiner already running, joiner driven
  to Co-op → Join and left to stall). One clean `pkexec`-root read via a new tool
  (`scratch/tools/readtask.py`, reads the global task-context pointer + the whole chain traced in
  9/9b) while the joiner sat in the stall:
  - **Glue state = 7**, confirmed (matches the visible stall).
  - **taskCtx local state (+0x60) = 5** — this field is ONLY ever set by `b160`/`sessB` AFTER
    `func_0x0770d948` returns SUCCESS (session-9b trace: `if (*param_1 != 0) return;` guards it). Since
    it reads 5, the ENTIRE static gate chain traced in session 9b actually ran and succeeded once:
    `d948`'s `SessionB+0x3c==4` gate passed, `cb30` passed its busy/capability gates and its first
    virtual call (`target->vtbl[0x20]`), and `f1f8` passed its own null-check and second virtual call
    (`facilityObj->vtbl[0x40]`), recorded a start tick, and installed its poll continuation.
  - **The `c8c4`-polled status object: state(+0)=3 (in the busy range {2,3,4}), status(+4)=0x648e**
    — NOT one of the five known terminal constants (`0xa467,0xcc63,0xac64,0xc47f,0xc485`). So the task
    is not secretly terminal-and-unread; it is legitimately, still, actively waiting.
  - **Correction to session 9b's live-read plan**: `d948`'s gate is NOT on the object I'd been calling
    "Session" at `taskCtxPtr+0x40` directly — it's on `*(long*)(thatObject+0x80)+0x3c` (one more hop),
    and `cb30`'s actual "Session" argument is reached through yet another hop off THAT object (a
    global fallback substituted if the derived pointer is null). My first script version read
    `+0xd8` etc. off the wrong (too-shallow) object; harmless here since the state=5/status=0x648e
    result already answers the real question, but the exact 3-hop chain (`taskCtxPtr+0x40` →
    `+0x80` deref → `+0x3c` gate / `+0x18` chain with global fallback) needs re-deriving precisely
    before trying to read `+0xd8`/the vtable pointers meaningfully.
  - **CONCLUSION: the SwitchNetworkGlue/async-task state machine traced across sessions 8-9c is
    exonerated.** It is not where the bug lives. The bug is in whatever the two vtable-dispatched
    calls actually do at the socket/protocol level — i.e. back to a P2P/mesh-connectivity question,
    consistent with session 4's late finding that the host probes the joiner's UDP port every 500ms
    while the joiner never answers until relay allocation succeeds. **NEXT: a live packet capture
    during the exact same stall** (`sudo tcpdump` in the user's own terminal per the existing lesson,
    not `pkexec`) to see whether the joiner emits ANY connect/punch traffic at all once state 7 is
    reached, and to what address/port. This is a different, more promising angle than continuing to
    decompile the vtable targets blind.
  - Tool added: `scratch/tools/readtask.py` (root, read-only, no freeze — extends `readsess.py` with
    the task-context global `G_TASKCTX_PTR = G_MAIN + 0xe5fb568`).

- 2026-09-04 (session 9b — **traced the state-7 task all the way to its concrete "start connecting"
  call; the last hop is a vtable dispatch that needs a live read, not more static tracing**): follow-on
  to session 9 in the same sitting, still static-only.
  - The "dead end" flagged in session 9 (`func_0x0773cb30` resolving to a huge unrelated function with
    zero callers) was **my own hex-subtraction mistake**, not a tooling gap: `0x0773cb30 - 0x100000 =
    0x763cb30`, not `0x663cb30` as I'd computed by hand. At the correct VA it resolves cleanly with
    exactly one caller (`d948`, as expected) and decompiles to a normal, sane function. Re-decompiling
    `d948`/`d0b8` afterward also fixed their calls to `cbc8`/`cb30` to show real names instead of
    `func_0x…` placeholders (`~/ghidra-projects/out3/cbc8_760cbc8.c`, `cb30_763cb30.c`).
  - **`cbc8_760cbc8`**: trivial precondition check (`session+0x80` non-null, `param_3` non-null),
    returns error `0x10407` on a null arg (a distinct code from the `0x10408` "not ready" seen
    elsewhere — `0x10407` = null-argument, `0x10408` = wrong-state/busy, consistently across every
    function in this chain).
  - **`cb30_763cb30`** (the function `d948` calls to actually start the op) is the real gate chain:
    (1) null-check `param_3` (the target/peer descriptor); (2) **virtual call** `target->vtbl[0x20]
    (result, target, isInitiatorFlag)` — the first real network dispatch, `isInitiatorFlag` read from
    `session+0x510` and defaulted true if `session+0x511` is unset; failure here returns the error
    immediately; (3) if that succeeds, re-fetch the task/state object at `session+0xd0` and check
    `*task == 1` (busy — same "1 = already active" sentinel confirmed from session 9's sessA/sessB
    read) → error; (4) check two more session flags (`+0x528`, `+0x52a`, capability/already-started
    gates) → error if unset/already-done; (5) only then, lazily re-init the task via `stnsetupA` and
    call **`func_0x077421f8`** — the actual "start creating the connection" primitive — and on success
    stamp `session+0xd8 = 4` (this is the exact field `d948` gates on being `== 4` before it will even
    attempt this whole chain — so `0xd8` is the session's own small state counter, and 4 means "mesh
    connect kicked off") and `session+0x52a = 1` (one-shot latch).
  - **`f1f8_76421f8`** (`func_0x077421f8`, the deepest node reached): after a null-arg check, makes
    **a second virtual call** — `(*(*(long**)(mgr+0x10])+0x30))->vtbl[0x40](result, that_obj,
    mgr+0x12)` — on a sub-object reached through the manager, at a DIFFERENT vtable slot (+0x40).
    On success: stores the task pointer into the manager, sets a one-byte flag at `mgr+0xc9`, calls
    `stnsetupB` on the task (mirrors `d948`'s use of `stnsetupB`), does a second virtual call
    (`mgr->vtbl[0x10](mgr, 1)`, `AddRef`/`Start`-shaped), **records a start tick**
    (`func_0x076e49c0` → stored at `mgr+0xf0`) — genuine timeout/elapsed-time bookkeeping, not a
    stub — then swaps in a continuation pointer (`mgr[7]=&UNK_0774234c`, the address right after this
    function — the classic "resume here on the next poll tick" pattern already seen in `d948`/`f1f8`'s
    siblings).
  - **Where static tracing stops**: both virtual calls (`cb30`'s vtbl+0x20 on the *target*, `f1f8`'s
    vtbl+0x40 on a *facility sub-object*) are polymorphic — the concrete implementation depends on
    which class was constructed at runtime, which is NOT statically knowable without either (a) a
    live read of the vtable pointer to identify + decompile the concrete class, or (b) finding a
    single unique vtable literal for that interface in `.rodata` if only one implementation exists in
    this binary (unchecked). This is the natural handoff point to a live capture.
  - **NEXT (needs a live joiner, stuck in glue state 7)**: read, in order of value: (1) the task's
    start tick (`mgr+0xf0` off the object at `session+0x68`) vs current tick — confirms whether the op
    genuinely started and how long it's been stuck; (2) the `c8c4`-compared status field (task+4) —
    is it one of the five known terminal constants (task secretly done, downstream bug) or something
    else (still in flight); (3) the vtable pointer at `*(long**)(session[0x10]+0x30)` / at the
    `f1f8`-reached sub-object — read the pointer itself (no need to follow it into code) to at least
    get a class identity to grep for in the binary's RTTI/typeinfo strings.
  - Decompiles this round: `cbc8_760cbc8.c`, `cb30_763cb30.c` (corrected from session 9's wrong one),
    `f1f8_76421f8.c`, plus re-decompiled `d948_760d948.c`/`d0b8_760d0b8.c` with fixed callee names.

- 2026-09-04 (session 9 — **VA-resolution bug fixed; `func_0x0770b160`/`c8ac`/`c8c4` decompiled clean;
  the state-7 async task's starter and its network primitive are now traced two levels deep**):
  no emulator run this session (static RE only). Root cause of session 8's bad decompiles found: the
  addresses in critbuilder's decompiled C body (`func_0x0770b160` etc.) are **Ghidra addresses**
  (VA+0x100000), so `gdec.py`/`ehfuncs.py` (which key off raw ELF VA) must be called with the address
  **minus 0x100000**, not the literal `func_0x…` digits. Session 8 fed the raw digits straight through
  (an implicit double-add of the base), landing 1 MB off in an unrelated function — that's the whole
  "wrong function" bug, not a flaw in the eh_frame method itself.
  - Correctly resolved and cleanly decompiled (`~/ghidra-projects/out3/`): `b160_760b160.c`
    (=`func_0x0770b160`), `c8ac_760c8ac.c`, `c8c4_760c8c4.c`. Call graph now matches the session-8
    model exactly: `b160` is called only from `gluecreate_1ac5070` (the glue's 4→7 transition);
    `c8ac`/`c8c4` are called only from `critbuilder_1ac2350`'s poll branches.
  - **`b160` (called "sessB") and its twin `sessA_760af18` (called from state 4→? elsewhere) are a
    matched pair**: both take `(result_out, taskCtxPtr, extra)`, both gate on `*(taskCtxPtr+0x40)`
    (a Session pointer) being non-null and on a state field `*(int*)(taskCtxPtr+0x58 deref)` != 1
    ("already running" guard), both lazily call `stnsetupA` to init the state object once, then call a
    "real op" function (`sessA`→`func_0x0770d0b8`, `sessB`→`func_0x0770d948`), and on success both
    register the task with an executor (lock mutex at `session+8+0x78`, init a list at `session+0x18`
    if untouched, append via `func_0x076dafc0`, unlock) and stamp the task-local state — `sessA` sets
    it to 3, `sessB` to 5. This confirms the session-8 "nn::async::Executable" theory as the actual
    mechanism, not a guess.
  - **`c8ac_760c8ac`** = the poll predicate: `state - 2 < 3` (unsigned), i.e. "busy" while the
    task-local state ∈ {2,3,4}. **`c8c4_760c8c4`** = the result reader: compares a *second* field
    (the task's detail/status code, a hash-like value, not the small state int) against five literal
    constants (`0xa467,0xcc63,0xac64,0xc47f,0xc485`); if it matches one of the five, copies the real
    payload (an 8-byte + 4-byte pair — looks like `nn::Result{module,description}`) out; otherwise
    defaults to a zero/"unknown" result. **These five constants are the next lead**: they're almost
    certainly the task's terminal-status enum (something like Initialized/Executing/Canceling/
    Canceled/Completed or similar), and knowing which one the joiner's stuck task actually holds would
    show whether it's still "running" (never reaches a terminal status — matches state 7 hanging
    forever) or is secretly terminal-but-unread.
  - **One level deeper, decompiled `d948_760d948.c`** (`sessB`'s "real op" call, i.e. what
    `gluecreate`'s state-7 task actually starts): gates on `*(int*)(taskCtxPtr[0x10]+0x3c) == 4` (the
    Pia *Session* object, reached via a different pointer chase, must itself be in state 4) before
    calling `func_0x0773cb30(result, sessionAddrPtr, param_4)` — the actual network primitive. On
    success it swaps in a new vtable/continuation pointer (`taskCtxPtr[7]=&UNK_0770db60`), sets the
    task-local state field to 5 (matches `sessB`'s caller), stores `param_3` (a station-ish value) and
    calls `stnsetupB`, then invokes a virtual method (index 0x10, "Start"/"AddRef"-shaped) on the task
    object. `sessA`'s twin `d0b8_760d0b8.c` was decompiled too but not yet read closely.
  - **Dead end, flagged rather than chased further**: tried to decompile `func_0x0773cb30` (the actual
    network primitive `d948` calls) and its own precondition check `func_0x0770cbc8`. For BOTH, my
    `ehfuncs.func_of()` resolves the address to the *middle* of a much larger containing function with
    **zero BL callers found anywhere in .text** — i.e. nothing in the binary appears to call that
    "containing function"'s start via a direct branch, which shouldn't be possible if `d948` really
    calls it. Likely cause: these two targets are called indirectly (PLT/GOT stub, or a relocation my
    BL-only scanner doesn't see) rather than via a direct `BL`, so `gdec.py`'s "containing eh_frame
    range" heuristic silently gives the wrong function even though the b160/c8ac/c8c4/d948 case above
    was genuinely correct. Don't trust `ehfuncs.func_of()` on a call target until its BL-caller list is
    non-empty and includes the expected caller — that's the real discriminator, not just "does a range
    contain the address."
  - Tools unchanged (`/mnt/media/nextendo-research/scratch/tools/{ehfuncs,gdec}.py`); new decompiles in
    `~/ghidra-projects/out3/` alongside session 8's.
- 2026-09-04 (session 8 — **live struct offset chase (jobPtr+0xE8 etc.) DEAD-ENDS on 3 job types;
  second wire capture reproduces the settle-flag but the "error code" region reads zero, not a
  code — new hypothesis: Pia/mesh may be SUCCEEDING and the game itself rejects post-attach**):
  Ryujinx host/joiner were freshly relaunched this session; local stack containers already up.
  - **Live-read `jobPtr+0xE8` (the offset session 7 derived) on all three container candidates —
    all zero.** Confirmed via `jobdump.py 302386 <prefix>`, cross-validated layout using the shared
    `+0x08 ptr` (0x14e876b0, identical across every job type) and `+0x88 NplndFacade` (0x14e963c8)
    fields as anchors:
    - `NplnBackgroundProcessJob::CompleteFailureProcess` — jobPtr+0x28 (the "valid inner sub-object"
      gate) reads **null**, so mkReport's write path can't have fired; this job is NOT the
      container session 7 guessed.
    - `JoinSessionJobBase::CompleteFailure` (PREVIOUS state `JoinSessionJob::ProcessSendMonitoringData`
      — matches the funnel name exactly) — jobPtr+0x28 also **null**; jobPtr+0xE8 reads
      `0xffffffffffffffff` (looks like an unset sentinel, not a written code); the self-reference
      field at jobPtr+0xE0 (which the "otherwise" branch is supposed to write) is **0**, meaning
      that branch never executed for this instance either.
    - `AttachMeshJob::CompleteFailure` (PREVIOUS state also `ProcessSendMonitoringData`) — jobPtr+0x24
      (the unconditional `stepresult` write, independent of the gated mkReport path) reads **0**.
  - **Session 7's own arithmetic has a bug worth flagging, not resolving further:** it derives
    `(jobPtr+0x28)+0x18*4` and separately states the result is `(jobPtr+0x28)+0xC0` — `0x18*4=0x60`,
    not `0xC0`. Whichever is right, neither offset produced a plausible code on live reads above, so
    don't trust this derivation without re-deriving from the decompile.
  - **Pivoted to a value-search instead of more offset-guessing:** `piajobs.py`'s live job-state scan
    incidentally also caught what look like Pia **result-name strings** in the same rodata window —
    `Protocol`, `ResourceExhausted`, `DataLoss`, `Unknown`, `Ok` (3 occurrences each — matching the
    3 shm-mirror count of our failing jobs) and `Cancelled` (24, too common to be specific). Searched
    ±0x2c0 bytes around all three job objects' bases for a pointer to any of these exact string
    addresses — **no hits**. Either the reference is transient (register/local, never stored back to
    the object) or it lives outside the window searched.
  - **Second live wire capture** (`scratch/mon-fail2.pcap` → `mon-fail2.txt`, decrypted via
    `piadec.py`/`scratch tools`): 2 reports, 123ms apart, same 929-byte shape as session 7.
    - **Reproduces session 7's settle-flag independently:** decrypted offset **0x31a** goes
      `0xff -> 0x01` between the two reports — second session confirming the same field.
    - Elapsed-timer field at **0x1a4** reads `0x2f86` = 12166 ms, consistent with the "fresh joiner
      fails in ~12-13s" timing already established.
    - The only OTHER region that flips from all-`0xff` (unset) to real data exactly when the job
      settles is **0x30f-0x316** (8 bytes) — but it lands on **all-zero**, not a distinguishing code.
  - **Pattern across both live-memory and wire-decrypt evidence this session: every candidate "error
    code" location reads either the unset sentinel (0xFFFFFFFF) or 0 — never a nonzero Pia error.**
  - **NEW HYPOTHESIS, not yet tested:** the Pia/mesh layer may be reporting SUCCESS (result 0 = Ok)
    for this join, and the actual failure decision happens in GAME-SIDE logic after attach succeeds
    (e.g. a version or session-data validation check), not inside Pia's job/monitoring system at
    all. This would explain why no nonzero error code has turned up anywhere probed so far. Testable
    without more struct-offset guessing: check whether `AttachMeshJob`/`JoinSessionJob` ever reach a
    state indicating the mesh was actually established (station assignment, per session 4's
    `readsess.py` local/host station fields) shortly before `CompleteFailure`, and look for
    game-layer (not Pia-layer) checks — e.g. `_Pia_SystemData`/session validation callbacks — that
    run right after attach and could themselves synthesize the failure.
  - **Environment note:** `pkexec` for short-lived reads (`readsess.py`, `jobdump.py`) authorizes
    without a visible prompt reliably; `pkexec tcpdump` (long-running) repeatedly hit a GUI polkit
    dialog that closed/timed out before the user could respond — worked only when the user ran
    `sudo tcpdump` directly in their own interactive terminal instead of through this tool's `pkexec`.
    Prefer that path for any future long-running root capture.
  - **STATE MACHINE FOUND (static RE, `~/ghidra-projects/out3/critbuilder_1ac2350.c`, guest VA
    0x9fc8350) — this is the real mechanism, not a guess.** This function is `SwitchNetworkGlue`'s
    per-tick driver; it `switch`es on `state(+0x68)` and for state **7** does:
    ```c
    if (iVar9 == 7) {
      uVar11 = func_0x0770c8ac(uVar19);        // poll: is the one pending async task done?
      if ((uVar11 & 1) != 0) {                  // done
        func_0x0770c8c4(&uStack_a40,uVar19);    // fetch its int result code into uStack_a40
        if ((int)uStack_a40 != 0) {             // nonzero => error
          ...fn_76e6c78 (format) + func_0x01bc395c (== glueerr_1ac395c, sets state=0xc)...
        }
        *(param_1+0x68) = 9;  *(param_1+0x6d) = 1;   // zero => SUCCESS: state->9, isHost=1
      }
      // else: task not ready yet — state simply stays 7, nothing else happens this tick
    }
    ```
    `uVar19 = uRam000000000e6fb568` is a single shared "current pending async task" global reused
    across states 2/5/6/7/0xb/0xc — same poll/fetch pair (`func_0x0770c8ac`/`func_0x0770c8c4`)
    everywhere, just checked at different states. **The joiner is stuck at 7 because that task's
    ready-poll never flips true** — not because a result is present and hidden; there is nothing to
    decrypt or offset-hunt for at this state, the task genuinely never completes. `gluecreate_1ac5070`
    (state 4->7, see below) is what starts it. The host reaching 9 with `isHost=1` set only happens
    via this exact same branch, confirming the reading.
  - **`gluecreate_1ac5070_1ac5070.c`** (guest VA 0x9fcb070) is the state 4->7 transition: gated on
    entry `state==4`, branches on `mode(+0x70)` byte, then calls `func_0x0770b160(&iStack_68, uVar2,
    ...)` where `uVar2 = uRam000000000e6fb568` — the SAME global `critbuilder`'s state-7 branch later
    polls. This is almost certainly "start async task", i.e. the call that arms the thing state 7
    waits on.
  - **Tried and FAILED to resolve `func_0x0770b160`/`func_0x0770c8ac`/`func_0x0770c8c4` this
    session — do not trust their names as fact.** `gdec.py`'s approach (find the eh_frame range
    *containing* the call-target VA, decompile the function starting at that range's beginning) gave
    wrong answers here: VA `0x770b160` resolved to a function spanning `0x770b110..0x770b1e8` that is
    an `nn::async::Executable` DESTRUCTOR (confirms the subsystem uses Nintendo SDK's
    `nn::async::Executable` task framework — that part is real and useful), not a task-starter.
    VA `0x770c8ac` and `0x770c8c4` both resolved to the SAME function (`0x770c784..0x770c90c`),
    which decompiles as a generic field-copy/swap operator unrelated to polling. Likely cause: these
    call targets aren't at clean eh_frame-registered function entries (mid-function labels, or
    `ehfuncs.py`'s lookup attributed them to the wrong adjacent range) — `gdec.py` always decompiles
    the START of whatever range it finds, not the exact requested VA, so a wrong range silently gives
    a wrong function with no error. Needs a different resolution method (e.g. checking Ghidra's own
    disassembly at the exact VA instead of trusting the eh_frame containing-range heuristic) before
    building anything further on what these three calls actually do.
  - **HYPOTHESIS TESTED AND REFUTED, same session:** ran `readsess.py` on host and joiner right after
    a live failure. Host: glue `state(+0x68)=9` ("session up"), local/host station both
    `0x6b49d203`/idx 21 (assigned, matching). Joiner, same moment: `state(+0x68)=7` (not 9), local
    AND host station both **0x0/0** — no station was ever assigned. So the "Pia succeeds, game
    rejects after" idea from earlier in this session is wrong: the mesh attach genuinely never
    completes on the joiner. This IS useful, ungated signal though — the joiner's glue state stalls
    at **7**, never reaching the host's **9**. That's a small, nameable enum (values 0-9 seen so
    far), not a speculative struct offset — pinning down what states 7/8/9 mean from the state
    machine's transition function is a tractable next step, unlike the job-struct chase above.

- 2026-09-03 (session 7 — **local stack rebuilt after a machine reset; live `:34343` telemetry
  CAPTURED AND DECRYPTED for the first time; the error-code field traced to a struct offset, not
  yet read live**): this machine had lost state since session 6c — nextendo-local's containers
  were down, `tcpdump`, `python-cryptography`, and a JDK were no longer installed, and passwordless
  sudo was gone. All rebuilt (see **Environment rebuild** below). Fresh joiner still fails in a
  consistent **12-13 s**, not session 6c's 25-30 s — that earlier timing was not a reliable
  constant, staleness alone doesn't explain it (this joiner was freshly relaunched each time).
  - **Captured `udp port 34343` during a live failing join and decrypted it** (`piadec.py`) —
    confirms session 6c's lead was right in kind: the joiner DOES send Pia monitoring reports
    during the failing `AttachMeshJob`, at exactly the moment `KeepUserSession` closes. Two reports
    landed in one capture window, ~140ms apart, both inflating to the same 929-byte shape as
    session 5's "login bundle" (now understood per session 6 to be the generic monitoring-report
    format, not a login). Raw decrypted bytes are in `scratch/34343-session7b.log` (never the repo).
  - **Traced the report-building call chain via clean decompile** (`~/ghidra-projects/out3/`:
    `stepresult_75dbd00.c`, `stepmakereport_75dcd84.c`, `reportbuf_75dfc40.c`, `tickA_75de150.c`):
    `reportbuf_75dfc40()` returns `&reportArray[idx]` with a **0x3e0-byte stride** (matches the
    929-byte decoded shape). The entry point is `stepresult(jobPtr, resultCode)` at ELF `0x75dbd00`
    (no diag string of its own): it writes two tick/duration values into the CURRENT report buffer
    at buffer**+0xb0** and **+0xb4** (via `tickA 0x75de150` = a ratio of two globals, likely a
    ms-per-tick conversion), sets `*(int*)(jobPtr+0x24) = resultCode`, and — only if the job has a
    valid inner sub-object at `jobPtr+0x28` and a couple of gates pass (`fn_76db7a4`, `fn_76d7aa0`
    at +0x5f8/+0x618) — calls `mkReport_75dcd84(out, jobPtr+0x28, jobPtr, resultCode)`.
  - **`mkReport` (`MonitoringDataSendJob::StepMakeReport`, diag string confirms the name) gates on
    the JOB POINTER being present**, not on the result code as session 6c's function-name alone
    suggested: `param_3==0` (null job) → returns pending code `0x10407`; `job->+0x10==1` (a
    detach/shutdown flag) → returns `0x10408` (both look like generic Pia async-pending codes, not
    errors — consistent with session 6's note that `0x10408` is `TurnJob`'s ordinary "pending"
    return). Otherwise it stores `(jobPtr+0x28)[0x17] = jobPtr` (a self-reference, 8 bytes at
    struct-relative **+0xB8**) and `*(u32*)((jobPtr+0x28)+0x18*4) = resultCode` — **so the actual
    Pia result/error code for whatever job called `stepresult` lands at (jobPtr+0x28)+0xC0, i.e.
    jobPtr+0xE8**, a location on the FAILING JOB ITSELF, not inside the wire report buffer we can
    decrypt. This is a different, more direct target than decoding the wire bytes.
  - **`jobPtr` here is some outer container job that embeds a `MonitoringDataSendJob`-like
    sub-object at +0x28 and two more sub-objects at +0x5f8/+0x618** — NOT AttachMeshJob itself
    (AttachMeshJob's own layout, measured in session 6c, has its diag pointer at +0x48 and no +0x28
    sub-object in that convention). Best guess from prior sessions' job traces: this is
    `NplnBackgroundProcessJob` (the container session 4/5/6 already saw wrapping `WaitConnectNetwork`
    / `AttachMeshJob`), but that is UNCONFIRMED — the next live read should verify the class name via
    `jobdump.py <joinpid> NplnBackgroundProcessJob` (or try container prefixes seen in job traces:
    `NplnBackgroundProcessJob`, `JoinSessionJob`) and then dump **container+0xE8** as a `u32` right
    after a join fails. That single field, if read, should be the actual Pia error/result code
    directly — no wire decrypt needed.
  - **Byte-diffing the two captured wire reports (secondary, weaker lead — superseded by the struct
    trace above but noted in case it's still useful):** the two reports differed at decoded-byte
    offset **0x1a4/0x301 = 0x2f77 = 12151 (decimal)**, i.e. milliseconds — matching the observed
    ~12.15 s failure almost exactly, so that field is very likely an elapsed/uptime counter, not the
    error. A flag byte at **~0x31a** is `0xff` (absent) in the earlier report and `0x01` in the
    final one — plausibly a "job settled" flag, not itself an error code.
  - **Environment rebuild this session** (do these once per fresh machine/session, they were all
    missing): nextendo-local's `dockerfile_inline` services (`stardew`, `stun`, `nncs`) fail to
    build under the `docker-compose` CLI plugin (`podman compose`) with a spurious
    "Dockerfile path" symlink error — **build them manually** with `podman build -t
    nextendo-local-<svc> -t nextendo-local_<svc> -f <dockerfile> <context>` (the compose file's
    `dockerfile_inline:` block, copied to a temp file, or `./stun`'s real Dockerfile as-is), then
    `podman compose up -d --no-build` / `podman compose --profile stun --profile nncs up -d
    --no-build` picks up the pre-built images. **`nncs` (the `nncs` profile) must be started too** —
    without it the NAT-check UDP probes at boot time out and Stardew shows an error applet before
    ever reaching NPLN; the launcher already warns about this but it's easy to miss. Packages
    needed and NOT preinstalled: `tcpdump`, `python-cryptography` (for `piadec.py`), `jdk-openjdk`
    (for `pyghidra`/`gdec.py` — **use the pinned `~/.local/opt/jdk-21.0.12.1+1` via `JAVA_HOME`, not
    a freshly-installed newer JDK**, since Ghidra 12.1.3 expects it). No standing passwordless-sudo
    file this session — **use `pkexec <cmd>` per-command instead** (per-machine choice, not a
    project convention change). Also: the shared-profile launcher's `NEXTENDO_ALLOW_SHARED=1` guard
    fires normally when host+joiner are both intentionally up — expected, not a bug.
  - **NEXT:** live-read `container+0xE8` (u32) on the object found by `jobdump.py <joinpid>
    NplnBackgroundProcessJob` (verify the class name first — it may need a different prefix)
    immediately after a join fails; that should be the literal Pia error/result code. Do this before
    any more wire-report byte-diffing — the struct trace is a much shorter path to the same answer.

- 2026-09-03 (session 6c — **joiner staleness is a real confounder; AttachMeshJob object layout
  MEASURED; the failure funnel hides the original error, but telemetry should carry it**):
  five live joins driven by the user against a relaunched host. Read-only probing only.
  - **STALENESS MATTERS — always restart the joiner before a test.** The ~9 h joiner instance failed
    in **12 s**; freshly launched joiners last **25-30 s**, sustain a bidirectional mailbox (78 B and
    94 B messages BOTH ways, every few seconds) and get **successful TURN ALLOCATEs on both
    consoles**. Runs: 16:09:27→39 (12 s, stale) · 18:15:33→18:16:02 · 18:20:19→49 · 18:23:41→53 ·
    18:27:57→18:28:27. Session 6b's "12 s" figure came from the stale instance and should not be
    treated as the normal failure time.
  - **AttachMeshJob object layout, MEASURED in guest RAM** (not inferred — `scratch/tools/jobdump.py`
    then `jobdiff.py`; one confirmed object, snapshot `scratch/jobsnap_1473981_NOW.txt`):
    ```
    +0x000 vtable            0x13f1d640
    +0x008 ptr               0x14e876b0
    +0x030 arg               +0x038 CURRENT step fn   0x10144414 = fn 0x7c3e414
    +0x048 CURRENT state string
    +0x050 saved arg         +0x058 PREVIOUS step fn  0x10144010 = fn 0x7c3e010 (am2)
    +0x068 PREVIOUS state string
    +0x088 NplndFacade       0x14e963c8   (matches facdump's facade address — the identity check)
    +0x0a0 1 · +0x0a8/+0x0b0 0xffffffff patterns · +0x100 0x0000000100000000
    +0x110 0xffffffffffffffff · +0x128/+0x130 tick counters · +0x138 0xc3
    ```
  - **The failure funnel loses the original error.** The one real job was in `CompleteFailure`,
    entered FROM `ProcessSendMonitoringData` (previous step fn `0x7c3e010`). Every failing step
    routes through that telemetry step before completing, so the saved-state pair only ever shows
    the last two hops of the funnel — never the step that actually failed. The Result at
    `+0x100/+0x108/+0x110` reads as SUCCESS (code 0, location `0xffffffff…`), so the error is NOT
    stored there by the time the job settles.
  - **Two traps that cost this session three runs and several bogus readings — do not repeat:**
    (1) **`+0xd2` is NOT a step id.** It measures 0 on genuine jobs. Three probe hypotheses were
    built on it (step id, multi-offset search, structural filter) and all rejected every real
    object. Only the raw dump settled the layout. (2) Ryujinx maps guest RAM at **several host
    addresses**, so one object appears 3× (0x7e1b…, 0x7e9b…, 0x7f1b…), and the PREVIOUS-state field
    at +0x68 creates a phantom "object" at +0x20. Dedupe by guest offset before counting.
  - **Tooling** (all in `scratch/tools/`, never the repo): `jobdump.py <pid> [prefix]` dumps raw
    bytes around each matched state pointer — this is what revealed the layout, and it is the tool
    to trust. `jobdiff.py <pid> [prefix] [--tag NAME]` snapshots every matching object in full to
    `scratch/jobsnap_<pid>_<tag>.txt` for diffing. `attachprobe.py` is **superseded and unreliable**
    — it encodes the wrong `+0xd2` assumption; delete or ignore it.
    Practical notes: a scan is ONE linear pass over ~11 GB and takes 30-60 s, so it can miss a short
    window entirely — but job objects PERSIST after the failure, so snapshot right AFTER the join
    dies rather than trying to catch it live. And never `pkill -f attachprobe`: the pattern matches
    the calling shell's own command line and kills it (exit 144); kill by pid.
  - **NEXT — offline first, no console needed.** The error must be found where the funnel puts it,
    and there is a strong lead: the funnel's own step is `ProcessSendMonitoringData`, i.e. it builds
    a **monitoring report** — and that report is exactly the `:34343` datagram whose AES-128-GCM
    encryption session 6 already broke (static keytab, `scratch/tools/piadec.py`). So the failure
    reason is likely READABLE: capture `udp port 34343` during a join (`tcpdump -X`), decrypt the
    datagram sent at the moment of failure, and read the error out of the telemetry payload. That
    closes the loop with the session-6 finding that `:34343` is telemetry. Do this before any more
    live probing. Secondary: read am2 (`0x7c3e010`) and the funnel entry points (am10 `0x7c3fb04`,
    am11 `0x7c3fd08`) to find which offset receives the Result BEFORE `ProcessSendMonitoringData`
    overwrites the state pair.
  - **Server:** still no proven defect. Across all five joins it answered every RPC, relayed the
    mailbox both ways, and coturn allocated for both consoles. The one field group we invent and
    have never validated remains `docs/__gs/f` (`addr`=127.0.0.2, `p`=18501 — OUR gRPC endpoint, not
    a Pia relay; plus `rs`, `mcn`, `maxu`). Suspicious given the joiner passes through
    `WaitSetupRelayAddress`, but unproven — do not change it speculatively.

- 2026-09-03 (session 6b — **LIVE host-vs-joiner job differential: the joiner dies in `AttachMeshJob`,
  before NAT traversal ever starts**): host relaunched (`scratch/host-run8.log`), farm
  `3a7e9415-5bf9-401d-8721-dc991225f1dc` hosted, joiner (the long-running instance, ~9 h uptime)
  joined at 16:09:27 and failed in **12 s** — faster than the ~25 s stalls of session 4.
  Both consoles read live with `readsess.py` + `piajobs.py` while it happened.
  - **HOST (healthy):** glue state 9, Session +0x84 = 1, local station == host station
    (`0x6b49d203` idx 23). Jobs: `NatTraversalJob::ProcessSuccess` **and** `WaitNatTraversal`,
    `WanCreateNetworkJob::WaitResolution`, `CreateSessionJob::CompleteProcess`,
    `UpdateSessionPropertyJob`/`NetUpdateNetworkPropertyJob::WaitUpdateNetworkProperty` (the lobby
    flush working), `NplndLoginJob::WaitLogin` — **and `TurnJob::WaitServerConfig`**.
  - **JOINER (failed):** glue state 7 during the attempt, then 1 (torn down); all stations 0.
    Jobs: **`AttachMeshJob::CompleteFailure`**, `NplnBackgroundProcessJob::WaitConnectNetwork`,
    `NetConnectNetworkJob::WaitConnectNetwork`, `TurnJob::WaitServerConfig` **and**
    `TurnJob::StepResolveServerAddress`, then `ChangeStateJob::CleanupSession` +
    `NplndLoginJob::WaitLogout`. **No `NatTraversalJob` state present at all.**
  - **Two conclusions.** (1) `TurnJob::WaitServerConfig` is a NORMAL RESIDENT STATE — the healthy
    host sits in it too. Session 5's "the joiner is blocked in WaitServerConfig waiting on an nplnd
    relay config" is dead, and the joiner even advanced to `StepResolveServerAddress`. (2) The joiner
    fails inside **`AttachMeshJob`**, i.e. BEFORE NAT traversal is ever started — which is why no
    joiner-originated transport probe has ever appeared in any capture. The probe absence is a
    symptom, not the fault.
  - **Server side is clean and rules itself out:** the joiner did the full sequence (JoinGameSession,
    IssueToken, KeepUserSession, `__pus` write, `AllocateIceServerSet`, `GetDocument docs/__gs/f`,
    `__stu` write) and RECEIVED the host's 207-B roster push (station id `0x6b49d203`) at 16:09:27.
    It then closed its KeepUserSession at 16:09:39 without ever answering. Note the host keeps
    rewriting its roster into the joiner's mailbox once a second for a full minute afterwards; those
    writes are correctly NOT pushed (change-only dedup) since the content is identical.
  - **NEXT:** read the failing job's error. `AttachMeshJob` keeps its result at **job+0x100**
    (`fn_76e6de4(param_1 + 0x100, ...)` in `am1 0x7c3dc60`, `am10 0x7c3fb04`, `am11 0x7c3fd08`), and
    the step id is the byte at **job+0xd2** (1 = fixed-data, 7 = CreateMesh, 9/10 = JoinMesh,
    11 = failure, 16 = CompleteFailure). Extend `piajobs.py` to print, for each job object whose
    +0x48 diag pointer is an `AttachMeshJob::` string, the u32 at +0x100 and the byte at +0xd2 — that
    names the failing step and its Pia error code directly. **Written this session:**
    `scratch/tools/attachprobe.py [host|joiner|pid] [--watch [secs]]` does exactly that (finds each
    job by its +0x48 diag pointer, prints +0xd2 step id and the +0x100 result, `--watch` loops so it
    can be armed BEFORE the join click; `PIA_JOB_PREFIX` selects a different job class). **Its
    memory-scanning path is UNVALIDATED** — both emulators exited before it could be run against a
    live process, so the first job of the next session is to confirm it against a known-good state
    (e.g. `PIA_JOB_PREFIX=NatTraversalJob attachprobe.py host` on a hosting console should print
    `ProcessSuccess` with result 0) before trusting anything it says about a failure.
    The 12 s window is enough if the probe is armed before the join click. Candidate first suspect: `WaitGameSessionFixedData` (step 1) reading
    our synthesized `docs/__gs/f` (`addr` = `127.0.0.2`, `p` = `18501`, `rs` = sha256 of the gsid,
    `mcn` = "Farm4Player"), since that is the first thing the job does and the only field group we
    invent wholesale. **Also re-run with a FRESHLY restarted joiner** — this instance had ~9 h uptime
    and several prior failed joins, and it died faster than session 4's, so staleness is a live
    variable that must be excluded before trusting the 12 s figure.

- 2026-09-03 (session 6 — **the `:34343` "nplnd relay" was TELEMETRY; that plan is cancelled**):
  static RE only, no emulator driven. Three results, in order of consequence.
  - **`:34343` is Pia's MONITORING server, not nplnd.** Sessions 4 and 5 built their whole plan on
    "every console sends 292-388 B UDP to `g2122d301.lp1.p.srv.nintendo.net:34343`, that is the
    nplnd P2P login, implement `cmd/nplnd` and answer it". That is wrong. The port constant `0x8627`
    lives in fn `0x75dd5e4`, and the function that installs that step names itself: fn `0x75dd45c`
    references the diag string at `0x987e580` =
    **`MonitoringServerAddressResolveJob::StepResolveMonitoringServerAddress`**. The datagrams
    themselves are built by `MonitoringDataSendJob`: its step table is at `0xbd50800..0xbd50828`,
    `StepMakeReport` = fn `0x75dcd84` (diag string `0x988000c`), `StepSendReport` = fn `0x75dce98`
    (diag string `0x988759e`), and `StepSendReport` calls the AES/deflate builder `encB 0x75dbdf0`
    twice (dry-run for size, then for real) — the very function session 5 reversed and labelled
    "the nplnd login builder". So those datagrams are fire-and-forget **telemetry reports**, which
    is exactly why nobody answers them and why the HOST reaches glue state 9 with them unanswered.
    **`cmd/nplnd` must NOT be built.** The Pia AES-128-GCM work from session 5 stays valid and is
    still the way to read these packets; only the naming and the plan were wrong.
  - **The joiner's TURN config is NOT starved by nplnd either.** Session 5 argued
    `TurnJob::WaitServerConfig` waits on an nplnd login reply via `IceServerConfigGetter`. The live
    facade dump taken that session (`scratch/facdump_joiner_1899686/facade_*.bin`) already contains
    our coturn host `10.87.0.2` twice, the credential
    `1788388968:tenants/t-9f607adf-lp1/users/u-rxroqs444xrkmhpny2na` and its base64 HMAC — i.e. the
    ICE set our gRPC `AllocateIceServerSet` returned is sitting in the facade. `TurnJob` polls the
    getter through vtable slot `0x30` (`turnPoll 0x7ac049c` -> `IceServerConfigGetter` slot 6,
    `ice6 0x7c3d2c0`) and returns "pending" (5) only while that call yields `0x10408`; the getter's
    accessors (`ice2 0x7c3d150`, `ice3 0x7c3d208`) gate on the byte at getter+0xf11, and `ice6`
    gates on the cached-flag byte at +0x1e2/+0xf10 — a plain async-request state machine, no nplnd
    dependency in the path. Confirmed on the wire too: in `scratch/udp-join3.log` the JOINER does a
    full STUN+TURN exchange from `10.87.0.2:63525` (20 B Binding, 176 B Allocate, 84 B replies).
  - **What the joiner actually fails to do: answer the host's probes.** Same capture: the host
    sends 93-byte transport probes `10.87.0.2:57630 -> 10.87.0.2:64157` 22 times over ~11 s and the
    joiner (bound `Udp/64157`, `joiner-run7.log:742`) sends nothing back, ever — there is no
    joiner-originated 93-byte packet in any capture. Mailbox signaling is healthy and finite (the
    21:28 join: host 207-B roster, joiner 9-B `01 12`, then 78-B `32ab9864` messages both ways
    every ~5 s, then only host->joiner) so the change-only push fix from session 4 holds. Pia jobs
    mapped this session for the next step: `AttachMeshJob` steps `0x7c3db30` (GetGameSessionFixedData),
    `0x7c3dc60` (WaitGameSessionFixedData), `0x7c3fb04`/`0x7c3fd08` (CreateMesh/JoinMesh + WaitJoinMesh),
    `0x7c3feec` (WaitJoinMesh body); `NatTraversalJob` `0x76fbdb0` (StartNatTraversal), `0x76fbe58`
    (Wait), `0x76fc088` (Retry), `0x76fc240` (ProcessSuccess), request sender `0x76fd4a0`, station
    table walk `0x76fcc90`/`0x76fcd24`/`0x76fcd90`. The joiner's mailbox reader is
    `sturead 0x7b57718` (fields suid/susid/sussid/suscid/pl, path `__pgn/<pgn>/__stu`) consumed by
    `stuConsume 0x7b54bb0`.
  - **NEXT:** find why the joiner never emits a transport probe. Two concrete lines: (a) drive a live
    join and dump `NatTraversalJob`/`AttachMeshJob` state with `tools/piajobs.py` at 1 s intervals to
    see which step it parks in and whether the station table (`0x76fcc90` walk) ever gets the host's
    entry — the 207-B roster carries station ids 1+2, so check that `stuConsume` accepted it;
    (b) decrypt the 78-B mailbox messages. Their key is NOT the static keytab (verified: brute-forced
    the 16 table keys plus `rs`/gsid/HMAC derivations against 17 captured packets, zero GCM tag
    matches), so it is a per-session key — find where the session transport key is installed
    (`hdrDcaller 0x766b8d0` / `hdrEcaller 0x766914c` pass a key object at `param_1+0x50`, class
    `nn::pia::transport::PacketWriter`/`PacketReader`) and read it from live guest memory.
    Also note `docs/__gs/f` `rs` is ours to choose (sha256 of the gsid) and the game cross-checks it
    between watch and point reads, so it must stay stable.

- 2026-09-03 (session 5 — **Pia P2P packet encryption BROKEN; nplnd is the joiner's relay-config gate**):
  the `nn::pia` on-wire encryption is fully reversed, so every `32 ab 98 64` datagram (the nplnd
  `:34343` login and the mailbox NAT-traversal messages) can now be decrypted and read.
  - **Crypto (reproducible, clean-room from the local research binary):** per-message **AES-128-GCM**.
    Header format = Pia version 15 (kinnay wiki "6.32 - 7.2"): `magic(4) | flags(1) | dvid(2) |
    svid(2) | pid(2) | footsz(1) | nonce(8) | tag(8)`. **Key** = `keytab[ hdr[14] & 0x0f ]` from a
    static 16x16 byte table at ELF rodata `0x98c2eae` (read by AES setup `fn 0x75dbdf0`, table base
    `&UNK_099c2eae + (b&0xf)*0x10`). **Nonce (12B)** = `hdr[6:14]` (8B header nonce) `|| LE32(0xfa85d05b)`
    (the fixed nplnd network id, literal `uStack_7c=0xfa85d05b` in `fn 0x75dbdf0`). **Body** = raw
    **DEFLATE** with 1 prefix byte skipped (`zlib wbits=-15`, plaintext[1:]); the `flags` low nibble is
    the version/compress marker. Packet build/parse: `hdrD 0x75eaf04` (encrypt+deflate), `hdrE 0x75eb1d4`
    (decrypt+inflate), `hdrC 0x75eac20` (magic/len check), header init `hdrA 0x75eaa8c`.
  - **Decrypted the 3 captured `:34343` login datagrams**: each inflates to an identical-shaped **929-byte
    Pia login bundle** (`2a 2c 00 ff 01 03 a0 …`, `0xff` = absent-field padding; per-session bytes in
    `0x0b0..0x3a0` carry station/candidate/port data — e.g. `00 00 27 10` = port 10000, `00 00 03 e8` =
    1000). Tool: `scratch/tools/piadec.py <tcpdump-Xlog>` (loads the key table from the binary at runtime;
    the table and decrypted blobs stay in `scratch/`, never the repo).
  - **Re-scoped the blocker with a live mid-join job dump** (`joinprobe.sh` + `piajobs.py`, read-only):
    the **HOST reaches glue state 9 / `NatTraversalJob::ProcessSuccess` / session up with its OWN nplnd
    login equally unanswered** — so nplnd login is NOT a hard gate by itself. The **JOINER** sits at glue
    7 / session 0 / stations 0, parked in `NatTraversalJob::WaitNatTraversal` **and** `TurnJob::WaitServerConfig`
    **and** `JoinSessionJob::WaitConnectNetwork`. The joiner needs a **relay** (it can't reach the host
    directly under emulation); the TURN server **config** in NPLN mode is delivered by nplnd's
    `IceServerConfigGetter` (a member of the 0x23d0-byte `NplndFacade`, ctor `fn 0x7c3bb7c`), not by our
    gRPC `AllocateIceServerSet`. So `TurnJob::WaitServerConfig` waits on the nplnd login reply → no relay
    candidate → NAT traversal never completes → join stalls 2318-1201. The host doesn't need a relay, which
    is why it proceeds without nplnd.
  - **NEXT (needs the user to drive a live join to test):** build a minimal `cmd/nplnd` UDP service on
    `127.0.0.1:34343` that (a) decrypts the login (crypto above), (b) returns the login-ack the client's
    receive path expects, carrying an ICE/TURN server config that points both consoles at our patched
    coturn (`10.87.0.2:3478`, HMAC creds). Still to RE for the response: the nplnd login **reply** format
    — read it from the nplnd socket reader (socket opened by `fn 0x75dd5e4`, port `0x8627`) and
    `NplndLoginJob` steps (vtable `0xba6e878`; `ExecuteCore 0x75e3910` runs step fn at job+0x38, success
    sets job+0xe bit0). Route `g2122d301.lp1.p.srv.nintendo.net` → 127.0.0.1 (already resolved there).
    Clean decompiles in `~/ghidra-projects/out3/` (hdr*/pr*/rc*/sv*/nplndlogin*/cry*/enc*/facade*/bigctor);
    tooling in `scratch/tools/` (piadec.py, facdump.py, ehfuncs/xref/gdec/vtfind/vtclass/readsess/piajobs).

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

-8. **(session 10) START HERE: decrypt the 78-B mailbox packets with the live per-session key, then
   find why the joiner never turns the host's reply into a `wan::NatTraversalJob`.** Everything
   before this is settled (see the session-10 entry): signaling is healthy, the ghost-station server
   bug is fixed, the joiner's Pia socket never sends. Concrete plan:
   a. Get the key: the sender object at `NplndRelayClient+0x18` (`rsend_7c3a820` calls its
      `vtbl[0x80]`) builds the Pia packet; session 4 located the key object at `PacketWriter+0x50`
      (`hdrDcaller 0x766b8d0` / `hdrEcaller 0x766914c`, classes `nn::pia::transport::PacketWriter`/
      `PacketReader`). Read it live from the joiner (pid 302386 — reuse `readdeep.py`'s `cls()` to
      walk NplndRelayClient `0x15e394b8` → sender → +0x50) DURING a join (objects may be rebuilt per
      join), and also check the nonce suffix (the mailbox packets are not the :34343 format —
      `piadec.py`'s `hdr[4:6]`-as-plen layout does not apply; header is probably
      magic|flags|dvid|svid|pid|footsz|nonce8 = 20 B).
   b. Decrypt both directions from `scratch/mailbox-1534.txt`: what the joiner requests (candidates it
      advertises) and what the host answers (its candidates: expect 10.87.0.2:60134 / 127.0.0.1:60134).
   c. Find the joiner's receive path: `sturead_7b57718` → `stuConsume_7b54bb0` → NplndRelayClient
      receive → dispatch to `wan::NatTraversalProtocol` (vtable `0xba1f0f8`), and where it would create
      `wan::NatTraversalJob` (vtable `0xba1f628`; `xref.refs_to` found no direct adrp ref — look for the
      ctor via `vtable+0x10` or a factory). Candidate reasons the reply is dropped: svid/dvid mismatch
      against the station table, a candidate filter rejecting addresses equal to the joiner's own
      (both consoles share 10.87.0.2 and mapped 127.0.0.1), or the message type (`dvid` 0x22/0x53/0x32)
      not being the one the joiner waits for.
   d. Poll live with the fixed `natpoll.py` during the join (station states 0xd/0xe, TurnProtocol
      per-station table, whether `+0x14ca` flips) — run it with `pkexec … > log` and expect output only
      at exit (python buffers).
   e. Do NOT re-investigate: the relay flag (1 on both), TURN (Allocate Success both sides), NNCS (same
      answers both sides), :34343 (telemetry), the gRPC "relay" leg to 127.0.0.2 (just a second
      channel), or the 9c-9f "never sends" traces (ghost-state artefacts).

-7. **(session 9f, SUPERSEDED by session 10)** resolve the concrete vtable implementation — the connect logic sets up
   its UDP sockets and then never uses them.** CONFIRMED three independent ways now (session 9c's
   live memory read, 9d's packet capture, 9f's properly-correlated syscall trace — see the session 9f
   entry above for the exact socket/connect/bind timeline): the async connect task marks itself
   started (taskCtx local state 5) and sits "busy" (poll state 3, status `0x648e`, not a terminal
   code); the TCP session to the NPLN server (`18501`, both `127.0.0.1` and `127.0.0.2`) connects
   fine; several UDP sockets get created and bound (the local-port-allocation step you'd expect before
   ICE gathering); and then **none of those UDP sockets are ever connected or sent on**, for the
   entire stall. This is NOT a reachability/NAT/relay problem — a call that never sends a packet isn't
   blocked on the network, it's failing (or looping/no-opping) before it gets that far. session 4's
   "host probes joiner every 500ms" finding is from an OLDER symptom (an explicit failure code) and
   no longer describes this stall; don't assume it still applies.
   a. **Re-derive the exact pointer chain precisely** (session 9c's version was one hop too shallow):
      `d948`'s gate is `*(long*)(*(long*)(taskCtxPtr+0x40)+0x80)+0x3c == 4`, and `cb30`'s actual
      "Session"-like argument (where `+0xd8`/`+0x510`/`+0x528`/`+0x52a` live) is reached through a
      further `+0x18`-chain off `*(long*)(taskCtxPtr+0x40)+0x80` with a global fallback if that
      resolves to null (see `d948_760d948.c`, the `lVar3`/`lVar1` logic). `readtask.py`'s current
      `+0x40`-direct read is the SHALLOWER object (still useful — it's what `d948` gates on directly
      — but rename/relabel before extending the script further, it is not `cb30`'s argument).
   b. Once that chain is right, read (still live, same joiner if still up — pid was unchanged across
      sessions 9c/9d, it does not reset on its own): the vtable pointer at `cb30`'s dispatch target
      (`*(long*)(session[0x10])`, then `+0x30`) and at the `f1f8`-reached sub-object off
      `session+0x68`. Just the pointer values — use them to grep `.rodata` RTTI/typeinfo strings for
      a class name, then decompile the concrete `vtbl[0x20]`/`vtbl[0x40]` implementations with
      `gdec.py` at that class's actual code (ELF VA, remember to subtract `0x100000` from any
      Ghidra-space address using `python3 -c "print(hex(x-0x100000))"`, never by hand — see (d)).
   c. Also worth a quick check while already reading live memory: is `session+0xd8` actually 4 right
      now (the `d948` gate)? If it reads something else, the "local state 5" observation might mean
      a DIFFERENT `sessA`/`sessB`-shaped call succeeded than the one traced in 9b, which would need
      re-checking which of the two actually ran.
   d. Session 7/8's struct-offset chase (`jobPtr+0xE8` etc.) is superseded by the state-machine
      finding — don't resume it.
   e. **Live-read discipline (lesson from 9d/9e/9f, revised)**: the glue state genuinely moves around
      between reads — seen at 1 (session 9d, right after a capture), 7 (9c, 9d's re-read, 9f), and 4
      (9e, while the user was actually sitting at the main menu). Session 9d's "flip back to 7 right
      away" was called a misread at the time; given 9e's later confirmed-idle (state 4) read, that
      call is now uncertain — it may equally have been a real idle moment the user then backed out of
      again. Don't assume any single read reflects the current state without asking the user or
      re-reading; never trust a trace/capture result without a `readtask.py` read taken right after it
      to confirm what state the game was actually in during the window.
   f. **Tooling note**: the "zero BL callers" dead end from session 9's first pass was a hex-arithmetic
      mistake on the agent's part (`0x0773cb30-0x100000` computed by hand as `0x663cb30` instead of the
      correct `0x763cb30`), not a real gap in `ehfuncs.func_of()`.
   g. Capturing packets: `pkexec tcpdump -i any -w <path> udp` run in the background (`run_in_background`
      in this tool) works fine for a short bounded capture despite the earlier "unreliable" note — that
      note was about `pkexec`+long-running processes racing a GUI dialog under a blocking tool call;
      backgrounding it sidesteps that. Filter broadly at capture time (no host filter — can't fix a
      missed packet after the fact) and narrow afterward with `tcpdump -r in -w out <filter>`;
      `piadec.py` wants a `tcpdump -X` text log, not a raw `.pcap` (regenerate with
      `tcpdump -r file.pcap -X -nn > file.log` first). No `tshark`/`capinfos` on this box.

-2. **(session 6c, SUPERSEDED by session 8 above) decrypt the failure telemetry.** The joiner's `AttachMeshJob` funnels
   every failing step through `ProcessSendMonitoringData` before `CompleteFailure`, so the job object
   no longer names the step that failed. But that funnel step BUILDS A MONITORING REPORT, and the
   report is the `:34343` datagram whose AES-128-GCM we already broke. Procedure, no live probing
   needed beyond one capture:
   a. Restart BOTH emulators (a stale joiner changes the symptom — see the session-6c entry).
   b. Arm `sudo tcpdump -X -i any udp port 34343 -w scratch/mon-fail.pcap` BEFORE the join click.
   c. Host a farm, join, let it fail (~25-30 s on a fresh joiner).
   d. Decrypt with `scratch/tools/piadec.py` and read the error out of the report body. The datagram
      sent AT the failure is the one that matters; earlier ones are routine telemetry.
   If the report does not carry it, fall back to static RE: read am2 (`0x7c3e010`) and the funnel
   entry points am10 (`0x7c3fb04`) / am11 (`0x7c3fd08`) to find which offset receives the Result
   BEFORE the state pair is overwritten. Do NOT resume guessing job offsets — `+0xd2` is not a step
   id, and three probes built on that assumption all failed.

-1. **(session 6) CANCELLED: do not implement `cmd/nplnd`.** The `:34343` traffic this item was
   built on is Pia telemetry (`MonitoringServerAddressResolveJob` fn 0x75dd5e4 /
   `MonitoringDataSendJob` step table 0xbd50800), not an nplnd relay login, and it is unanswered on
   real hardware too. See the 2026-09-03 session-6 status entry.

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
- Lesson (citron, source-verified in `sfdnsres.cpp` `GetConfiguredIp`): the profile SETTING `nextendo_server_ip`/`nextendo_nat_ip` wins over `NEXTENDO_SERVER_IP`/`NEXTENDO_NAT_IP`, and a fresh profile defaults both to the production IPs — the env vars alone are silently ignored (this is why NAT checks and the JWKS fetch kept going to production). The wrapper now pins the settings in `qt-config.ini` (`\default=false`) and refuses to launch if that did not take. Added to the citron isolation playbook (now `docs/shared/` here, canonical `docs/playbooks/`).
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

### Session 16 addendum (same night, the state-machine hunt)

- The NPLN worker (guest thread 98) parks in a 1-second condvar loop: `svc 0x1C`
  (`WaitProcessWideKeyAtomic`), pc in the sdk module's svc wrapper, called from
  sdk+0x149330's containing function (decompiled: it compares the pollfd array's
  first u32 against a mask cached in its TLS at +0x1B0 — `(mask & 0xBFFFFFFF)` —
  mismatch = error path without waiting; match = infinite svc wait). The waited
  condvar/mutex at main+0x85F5D0 (per-run heap offsets differ; this run
  main@0x80c7c000, x0=0x8f27b5d0) is POPULATED (counters 1,1,1,1; id 0x10dce;
  tick; key material) — the SDK initialized and waits for an event that never
  fires. The earlier zeroed read was a stale-offset artifact; the object is
  heap-dynamic per run.
- The "(Auth RPC result)" strings sit at main VA 0x9867A43 and 0x9885BA7 (flat
  NSO offsets +0x100) with NO static references found (no ADRP, no pointers) —
  table-driven at runtime. Ghidra's sdfull project has 0 recorded xrefs too.
- Ryujinx root cause found and fixed separately: its Bsd answered Pia UDP
  receives with ETIMEDOUT; ported the Nextendo IClient (commit 3bab55478 in
  emulators/ryujinx) — verified to full auth + QueryGameSessions + farm list.
- eden artifacts: `~/opencode-scan/` — sdk_frozen.bin (SDK module image),
  lr_context.bin, wait_object.bin, find_base.py, capture_frozen.py,
  sdk_analysis.py, ryu_state.py, gold_obj.py, main_state_machine.py
  (+ main_sm.txt: 2848 lines of captured decompile).
- Next: the state machine's mask check needs the SDK's TLS+0x1B0 cached value
  read at the freeze (eden's Trace line can be extended to dump the waiting
  thread's TLS), OR the vtable walk from sdk+0x778220 in Ghidra with the
  sdfull-style analysis run over sdk_frozen.bin.


### 2026-09-14 investigation: correct the wait interpretation before continuing

- The latest Eden checkout already includes deferred polling (`cf4ea9d638`) and
  TLS/poll diagnostics (`91e92fc93f`); session 16's port/read-TLS next steps are stale.
- Offline disassembly of `~/opencode-scan/sdk_frozen.bin` confirms sdk+0x1492e8
  and sdk+0x149354 are infinite/timed condition-variable wait wrappers. They mask
  the mutex owner word with `0xbfffffff` and compare it with the current SDK thread
  object's handle at +0x1b0. These are NOT pollfd-array/mask checks. Reaching their
  svc 0x1c call means the ownership comparison passed.
- The getter at sdk+0x140c20 reads TPIDRRO_EL0, then loads the SDK Thread pointer
  from TLS+0x1f8. Its captured GOT pointer at sdk+0xb70020 is 0x8f3e5c20,
  implying the saved SDK image's base was 0x8f2a5000. Do not use another run's base.
- `physical_core.cpp` currently reads `context.tpidr + 0x1b0` and labels x1's
  condvar word `array_word`. Dynarmic's GetContext sets `context.tpidr` from
  TPIDR_EL0, whereas this SDK getter uses TPIDRRO_EL0. The diagnostic therefore
  has both the wrong TLS source and a missing pointer dereference. Its logged
  `cached_mask=0 / array_word=1` does NOT establish a guest mismatch.
  Correct measurement: use `thread->GetTlsAddress()`, read Thread* at +0x1f8,
  then handle at Thread+0x1b0; compare against x2 and the masked mutex at x0.
- The pre-launch log (ended 2026-09-14 11:52 local) shows thread 98 repeatedly
  returning to nonblocking BSD Poll(fd=5, events=In, timeout=0), about every five
  seconds, interspersed with timed condition-variable waits. It is not permanently
  stuck in one syscall. Its role as the auth-blocking worker needs stronger proof.
- Separate source-level candidate: BSD deferred Poll only gets rescheduled by
  eventfd writes; sockets.cpp has explicitly removed its timer heartbeat. Host
  socket readiness and finite deadlines have no independent wakeup source. Also,
  the deadline branch returns errno TIMEDOUT with ret=0. Neither observation is
  yet proven to explain this title's auth stall; do not call either the root cause.
- No emulator behavior or server changes made in this investigation. User requested
  launch: Eden started to its game list with systemd user unit `eden-stardew-debug`
  (pid 593177). A fresh in-game online reproduction is the next step.


### 2026-09-14 fresh reproduction: DNS length bug found and repaired, retest pending

- Fresh Eden run pid 593177 faulted at 48.771 s: guest thread **97** attempted
  execution at zero immediately after Connect(fd=4) succeeded. LR and saved
  registers were zero. The host remained alive and suspended that guest thread.
  Thread 98 continued its timer loop; it was not the faulting thread in this run.
- Live stack recovered main+0x7888278 / +0x78888ac / +0x7888ac0 (callback
  executor), using a validated guest mapping, not guessed heap offsets. External
  helper: `~/opencode-scan/live_wait_check.py`. Before-fix log saved privately as
  `~/opencode-scan/eden-before-dns-length-fix.log.gz`.
- Existing earlier instruction traces in `/tmp/eden-return-overwrite-decoded.txt`
  and `/tmp/eden-guest-fault-trace.txt` identify the corrupting operation: resolver
  callback main+0x77aa1f0 passes a resolved address length of 0x100 through
  main+0x77aa560 to main+0x7868060, which copies 256 bytes into a 128-byte address
  buffer on its caller's stack, overwriting the saved return address. Its return
  subsequently restores LR=0. Trace main base inferred from unique callback
  prologue: 0x80fbd000. Decompiled functions are in ~/ghidra-projects/out3/overflow_*.
- **Source defect:** Eden `SockAddrIn` is padded to **0x100**, explicitly asserted
  in sockets.h. `SerializeAddrInfo` advertised `sizeof(SockAddrIn)` as ai_addrlen
  but emitted only 16 address bytes. Its sin_len cast also truncated 256 to zero.
  Corrected BOTH fields to an explicit 16-byte IPv4 wire size in sfdnsres.cpp.
  This agrees with the actual emitted record and the working Ryujinx serializer.
- Incremental `cmake --build emulators/eden/build-openpak --target eden -j 4`
  passed; git diff --check passed. No server or game-binary modifications.
  Restarted Eden to the game list in user unit `eden-stardew-dns-fix` for a fresh
  online test. **Do not yet claim full online success: post-fix retest pending.**


### 2026-09-14 online reached; poll wakeup repair and Android rebuild

- User confirmed the DNS-fixed build reaches the game online. No null-address
  guest faults appeared in that run. Initial connection still progressed in
  bursts and looked frozen between them.
- Live evidence for the delay: a TCP socket had 46 bytes queued unread while its
  guest Poll(eventfd + socket, timeout=-1) was deferred. Poll retries were driven
  only by eventfd writes, often five seconds apart. The socket had no independent
  readiness notification into ServerManager.
- Added opt-in `ServerManager::StartDeferralPolling(10ms)`, enabled by the BSD
  service only. It signals the deferral event only while requests are pending.
  Uses KernelCore::RunOnHostCoreThread so event signaling has a registered kernel
  thread; shares the manager's stop token and joins before event/session cleanup.
  Eventfd writes still signal immediately. Finite deferred poll expiry now returns
  ret=0 / errno=SUCCESS instead of TIMEDOUT.
- DNS serializer regression test exercises two canonical-name records, checking
  address lengths, sin_len/family, canonical-name positioning and next-record
  boundary. Desktop build passed and `[openpak]` tests passed (27 assertions,
  3 cases). Existing unrelated worktree changes preserved.
- Rebuilt desktop launched to game list as `eden-stardew-poll-fix` after the
  previous process had exited. User's in-game latency confirmation is pending.
- Android mainlineRelease build succeeded once; final rebuild incorporates the
  testability refactor to SerializeAddrInfo. Build logs live privately under
  ~/opencode-scan/poll-wakeup-*.log. APK verification/artifact path recorded below
  when final packaging completes.

- Final Android build passed (`assembleMainlineRelease`). APK copied to
  `emulators/eden-apk/stardew-network-fix-2026-09-14/eden-openpak.apk` with SHA256SUMS.
  Signature and 16-KiB zip alignment checks passed. Same signing certificate as
  eden-apk/v0.2.0; package dev.eden.eden_emulator, versionCode 33779828,
  versionName v0.0.4-rc2-954, arm64-v8a, minimum Android API 33.
- Live desktop retest pid 1660234: 1006 observed poll samples, median retry
  **10.08 ms**, zero guest execution faults. Server TCP connection receive queue
  was empty when sampled. This verifies the missing periodic wakeup is repaired;
  extended gameplay latency and Android-device testing remain user-side checks.
