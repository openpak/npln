# Design — what Splatoon 3 needs from NPLN

Splatoon 3 (`0100C2500FC20000`) is an NPLN-era title: its online play runs over gRPC/HTTP2/TLS to
a per-title NPLN tenant, the same transport family as [`stardew-valley`](../../stardew-valley).
It is also the biggest title on OpenPak's list, and this document is deliberately more a map of
what we do not know than a specification.

## Confidence policy

Same labels the family uses. Nothing enters this document as a fact without a source.

- **CONFIRMED** — reproduced by a dated observation or test we can point at.
- **HIGH** — one good observation, or a first-party artifact, without a second run.
- **MEDIUM** — plausible reading with incomplete evidence.
- **LOW / SPECULATIVE** — inference; the reason an experiment exists.
- **UNKNOWN** — written as "unknown, needs a capture of X". Never filled in by guessing.

Allowed inputs are the ones in [`docs/clean-room-policy.md`](../../../docs/clean-room-policy.md):
our own observations, public protocol documentation, compatibly licensed code, and OpenPak's own
repositories. The NextendoNetwork per-title trees — including `~/REPOS/splatoon-3`, which is a
different repository from this one — are PolyForm Shield and were not opened for this work.

## What is established

| Fact | Value | Source | Confidence |
| --- | --- | --- | --- |
| Title id | `0100C2500FC20000` | upstream Ryujinx `TitleIDs.cs`, `docs/compatibility.csv` (`Openpak/emulators/ryujinx`) | CONFIRMED |
| NPLN tenant hostname | `t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net` | measured 2026-08-25: the retail title resolved this name to Nintendo's production address during a citron-cmd run; recorded in `Openpak/emulators/citron/src/core/hle/service/sockets/sfdnsres.cpp` | HIGH (one dated resolution) |
| Tenant id | `t-dce9377b-lp1` | leading label of the hostname above | HIGH |
| Transport | gRPC over HTTP/2 over TLS, in-game OpenSSL on raw BSD sockets — emulator SSL trust settings do not apply | [`npln-online-gate-playbook.md`](../shared-docs/npln-online-gate-playbook.md) | CONFIRMED (family-wide) |
| ALPN | client offers `grpc-exp` then `h2` | measured for Stardew, same SDK family, and the playbook records S3 offering the same | HIGH |
| Client-side gate | the NPLN SDK's certificate-acceptance flag: a never-set byte selects between an always-OK verify callback and a real check. Forcing the read to 1 is "the Splatoon 3 fix" (`LDRB W10,[X21,#0x38]` at S3-main `0x157B20` → `MOV W10,#1`) | family reverse-engineering notes carried in `stardew-valley/handoff.md` | HIGH |
| Failure code | `2321-4992` = client-side gRPC `UNAVAILABLE` (module 321, `(64 + grpc_status) * 64`) | playbook §Phase 0; the conversion table lives in each title's binary | CONFIRMED |
| Pre-gRPC platform hosts already served by OpenPak | `gw.hac.lp1.vermillion.srv.nintendo.net`, the `*.penne.srv.nintendo.net` family, `beach.*` | `Openpak/nx-baas` `main.go` host routing and `penne.go` — first-party, already deployed for the console link | CONFIRMED (that we serve them; not that S3 dials them) |

## What is unknown, and why the skeleton is small

Everything below is a question this repository cannot answer from a desk.

1. **Which `nn.npln.*` services Splatoon 3 calls, and in what order.** Unknown, needs a capture of
   one boot-to-lobby run against a tapped tenant port (evidence item 1). Stardew's flow was
   Auth → Friends → GameSessionService → Gamesync; Splatoon 3 is a matchmade title with an
   always-online lobby, so its flow is very likely wider, not the same.
2. **Whether the RPC metadata carries `npln-tenant-id: t-dce9377b-lp1`.** Unknown, needs the same
   capture. The code echoes whatever the caller sends and falls back to the constant.
3. **Whether the access token's `npln.app_id` claim is the title id.** It is for Stardew. Unknown
   for Splatoon 3, needs a capture (or a differential run: mint with the title id, then with
   something else, and see which one the client accepts).
4. **Whether Splatoon 3 dials Vermillion/Penne before its tenant.** The playbook records the S3
   chain as device init + `accounts/config` with `online_license` on Vermillion and login tickets
   on Penne. Stardew resolves its platform host and never dials it. Unknown for S3, needs a
   capture of the DNS + connect order (evidence item 1 answers this in the same run).
5. **What the online-play gate actually checks.** Splatoon 3 requires an NSO membership for online
   play. Whether that is enforced by the tenant, by Vermillion's `online_license`, or by the
   client reading `nso_restricted` in our token is unknown, needs a capture. The console rig's
   NSO membership work already bit us once on the Photon side.
6. **The session/P2P topology.** Splatoon 3's matches are peer-to-peer with a host, coordinated
   through NPLN and Pia; whether it uses `GameSessionService` (Stardew's path), the
   `Matchmaker` service, or both, is unknown and needs a capture. The `Matchmaker` service exists
   in the NPLN schema — `CreateMatchmakingTicket` / `TrackMatchmakingTicket` (server-streaming) /
   `CancelMatchmakingTicket` / `CreateAcceptance` — and Stardew never registered it. That is the
   single most likely title-specific surface here.
7. **Anything about game content services** — gear, catalogue, Splatfest, SplatNet 3
   (`api.lp1.av5ja.srv.nintendo.net`, the smartphone-app GraphQL surface documented publicly by
   the `s3s`/`imink` projects). Out of scope for this repository: they are not on the NPLN tenant
   and are not needed to get two consoles into a match. Revisit only if a capture shows the game
   refusing to enter online play without them.

Deliberately **not** written down: any claim about Splatoon 3 RPC handlers, field meanings, or
response shapes. We have none, and the family rule is that a healthy title's behaviour is never
inferred from another title's.

## What is shared with Stardew, and should be lifted

Building this skeleton made the duplication concrete. Three things are not title-specific:

| Piece | Where it is now | Title-specific part |
| --- | --- | --- |
| The NPLN schema (`proto/`, `nn.npln.*`) | duplicated: full set in `stardew-valley/proto`, auth subset here | none — it is Nintendo's schema, identical for every tenant |
| Identity against nx-baas (`/internal/switch/identity`, the nnex claim, the `u-…` user id derivation) | duplicated | none |
| ES256 access/refresh tokens, the gRPC plumbing (`npln-grpc-type` header, keepalive policy, `unknownService` logging, `connTracer`) | duplicated | the `Tenant`, `AppID` and `kid` constants, three lines |

**Proposal: `servers/npln-common`, a Go module holding `proto/` + identity + token minting + the
server plumbing, with the per-title constants passed in.** Each title's repository then holds only
its own service handlers. This is worth doing *before* a third NPLN title starts, and it should be
done as a change to Stardew (the working, deployed implementation) that this repository then
follows — not as a rewrite here. Doing it now would mean editing a live server to serve a
repository that cannot yet answer a single real RPC, which is the wrong order.

Until then this repository carries the **auth subset only** (`proto/auth/v1`, two `.proto` files)
rather than a second full copy of the schema. That keeps the duplication small enough to delete in
one commit, and it is marked in [`proto/NOTICE.md`](../proto/NOTICE.md).

## What is built

`cmd/npln` — the same first step Stardew took, nothing past it:

- plain-HTTP `/health` on `:21013` (`{"status":"ok","service":"splatoon-3"}`), so the status page
  has something to ask; the service port answers only TLS+h2 and a bare connect proves nothing;
- TLS termination on `:21012`, the Splatoon 3 NPLN tenant port ([`ports.md`](../../../ports.md)),
  logging the ClientHello's SNI and ALPN — the first two facts any capture needs;
- `nn.npln.auth.v1.Auth` answered against nx-baas's internal API: the client's BAAS id_token
  carries an `nnex` claim only nx-baas can verify, we hand it back to
  `POST /internal/switch/identity` behind `X-Internal-Key`, and only a recognised account gets an
  ES256 token. Fail-closed: an unprovable identity gets `PERMISSION_DENIED`, never a token;
- every other method logged and answered `UNIMPLEMENTED`. **That log line is the deliverable.**
  A single boot-to-lobby run against this server prints Splatoon 3's real call order, which is
  precisely the thing this document has to leave blank today.

Not built, on purpose: friends, presence, matchmaking, game sessions, gamesync, NAT/TURN, the
Matchmaker service. Each of those would be a guess at a protocol we have not observed, and the
family has already paid for that lesson once.

## Ports

`21012` gRPC/TLS tenant, `21013` health, `22210–22219` reserved for game transport when there is
any. Claimed in [`ports.md`](../../../ports.md).
