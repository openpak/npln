# Design — what Splatoon 3 needs from NPLN

Splatoon 3 (`0100C2500FC20000`) is an NPLN-era title: online play runs over gRPC/HTTP2/TLS to a
per-title tenant, the same transport family as [`stardew-valley`](../../stardew-valley). It is by
a wide margin the largest title on OpenPak's list.

Every fact below has a source in [`provenance.md`](provenance.md), which is the ledger the
clean-room policy requires. This document is the plan built on those facts. Where something is
still unknown it says so; the previous revision of this file was mostly unknowns, and most of them
are now settled.

## The shape of the problem

Splatoon 3's online mode needs four layers, in this order. A failure in any one of them looks the
same from the outside — a communication error, usually `2321-4992` — so they have to be brought up
in order and confirmed one at a time.

1. **A REST bootstrap** on the Vermillion and Penne host families, before any gRPC exists: device
   initialisation, a per-device id, an account config carrying the online licence, a login ticket,
   and a persistent "frontline" connection that must stay open. In OpenPak this is
   [`nx-baas`](../../nx-baas)'s job — it already routes these hosts for the console link.
2. **The tenant's gRPC control plane** — this repository. Auth, friends, presence, matchmaking,
   game sessions, gamesync, and the Splatoon-specific `toyohr` services (schedules, Splatfest,
   cloud save, lockers, replays, lobby messaging).
3. **A signalling endpoint and NAT traversal.** Settled from the facts (`provenance.md`): the
   session server **relays, it does not simulate**. Consoles open a second gRPC connection to a
   session endpoint and use its document store as a *mailbox* — each console writes its Pia contact
   blob, the server copies each peer's blob into the document that console watches — and then
   connect **to each other** over Pia, directly or through TURN. So this layer is a signalling
   service plus a stock coturn, not a game server.
4. **Game content** — the stage and mode rotation, seasons, Splatfests. Without a *current*
   rotation the game reports that stage information is unavailable and stays offline, even when
   every RPC is succeeding.

## Service surface

The schema spans 20 services. A working server does not implement 20 — it registers nine and
answers the rest from recorded traffic:

| Service | Role | OpenPak status |
| --- | --- | --- |
| `auth.v1.Auth` | tokens; the identity gate | **implemented** |
| `friends.v1.Friends` | friend and block lists | **implemented** |
| `friends.v1.PresenceService` | `KeepAlive` (bidirectional), presence subscriptions | **implemented** |
| `matchmaking.v1.Matchmaker` | public matchmaking | not started |
| `matchmaking.v1.GameSessionService` | host-created rooms, room codes, invitations, ICE allocation | not started |
| `gamesync.v1.Gamesync` | the document/session transport | not started |
| `ugcstore.v1.Ugcstore` | player-published documents | not started |
| `toyohr.v1.Schedule` | stage/mode rotation | **implemented**, served from a generated rotation file |
| `toyohr.v1.FestService` | Splatfest | not started |

`Matchmaker` **and** `GameSessionService` are both used, for different things — public matchmaking
and private rooms respectively. The previous revision of this file flagged that as the standout
open hypothesis; it is now settled as a fact, with the measured public-matchmaking call order
recorded in `provenance.md`.

The friends and presence services are not optional decoration. A server that serves a *static*
friend/block snapshot stalls the game's session setup: the block list never becomes ready, and a
multiplayer room shows an empty roster with invitations and room codes disabled. They have to
reflect the logged-in identity.

## What is built

`cmd/npln`, unchanged in scope from the previous round and now confirmed correct where it could be
checked against the facts:

- plain-HTTP `/health` on `:21013`; TLS termination on `:21012` logging ClientHello SNI and ALPN;
- `nn.npln.auth.v1.Auth` against nx-baas's internal API — the client's BAAS id_token carries an
  `nnex` claim only nx-baas can verify, and only a recognised account gets an ES256 token.
  Fail-closed;
- `npln-grpc-type` stamped on every response with `SetHeader`, and a keepalive enforcement policy
  that tolerates this client's ping rate. Both were carried over from Stardew on the assumption
  they were family-wide; both are now confirmed as Splatoon 3 requirements with measured failure
  modes (`provenance.md`, "Transport");
- every other method logged and answered `UNIMPLEMENTED`.

Friends and presence are implemented on top of that, both dynamic:

- `Friends` serves the caller's own OpenPak graph, not a snapshot, and splits
  `SubscribeFriendUsers` across stream messages under a byte budget — the whole graph in one
  message is a measured crash of the game's plaza parser;
- `PresenceService` answers every `KeepAlive` ping one-for-one (heartbeating on an independent
  timer instead kills the stream at ~60 s), keeps the attributes the client publishes about
  itself, and pushes presence *changes* on the subscription rather than one opening snapshot;
- a PID↔user-id pairing is recorded at token issue, the only point where both are visible, because
  `KeepAlive` arrives with a user id and not always a usable token.

A player with **no** friends is a known risk rather than a solved case: an empty
`SubscribeFriendUsers` has been measured aborting the plaza parser, a working server avoids it by
falling back to recorded bytes, and we have none and will not invent a friend. The server logs the
condition; `evidence-needed.md` makes it the first thing to test.

One fix from an earlier round came directly out of the facts: the client names its tenant with the alias
`tenants/current`, not with its own id. Passing that through minted a token claiming `tid:
"current"`, which is no tenant at all. `resolveTenant` now resolves it.

The access-token claim shape — `aid`, `app_id`, `authorization`, `ext_id`, `ext_id_type`, `tid`,
with `jku: jwkSets/nplnAccessToken` — was already right, because it came from Stardew and the SDK
is the same. Worth stating plainly: this is the one part of the stack where the client's failure
is *silent*. It reads its online rights out of these claims; a token it cannot decode reads as "no
rights" and the game declares itself offline while every RPC returns success.

## Two blockers that are not code

**Schedules were not ported — they are generated.** A working server replays recorded response
bytes with the timestamps shifted forward. Those bytes are Nintendo's and are not redistributable,
so OpenPak splits code from data instead: `internal/rotation` defines and validates a rotation
file, `cmd/genrotation` writes a valid one anchored to real time, and the server reads it at
start-up. Nothing captured enters the repository, and an operator with their own recorded rotation
points `NPLN_ROTATION` at it.

The validation is strict for a specific reason. A stale set is *rejected* by the game with a
visible error; an inconsistent set — kinds whose windows disagree about the present — is not
rejected at all, it aborts the console. So a rotation that would abort the console is a refusal to
start, with the offending entry named; a missing rotation is only a warning, because the rest of
the server still works.

**The pre-gRPC REST chain is nx-baas's, and it is currently wrong for this title.** nx-baas routes
the Vermillion and Penne hosts today and answers them well enough for the console link, but
measured against what Splatoon 3 requires it has four defects, each of which independently prevents
the game from ever reaching the tenant:

| nx-baas today | What Splatoon 3 needs |
| --- | --- |
| any `gw.` GET → `{}` | `GET /v1/devices/vermillion-device-id` → `{"vermillionDeviceId": "<base64, 16 bytes, no '/'>"}`. Wrong key → no device id → endless bootstrap loop |
| any `gw.` GET → `{}` | `GET /v1/accounts/config` → `{"payload": base64(json)}` with `online_license.is_available: true`. This is the online gate |
| `/accounts/vphyms` → **404** | 200 with the same `{"payload": …}` envelope. The 404 makes the game restart its whole bootstrap with a new penne id, forever |
| no `fro-` route (falls to 404) | the frontline `POST` must be held open as a stream for room-code registration |

Details and sources are in `provenance.md`. Deliberately **not** fixed from here: nx-baas is
deployed and serves the console link, these are console-facing responses, and changing them blind
without hardware in front of us risks the working link to fix a title nobody has run yet. It is a
scoped nx-baas change with its own hardware test, not a drive-by.

## Lift the shared NPLN layer

There are now two real NPLN servers to compare, and the comparison makes the case that the previous
revision could only argue from one side. Identical in both, and title-independent:

| Piece | Evidence it is not title-specific |
| --- | --- |
| The NPLN schema (`nn.npln.*`) | it is Nintendo's, one schema across tenants; Stardew and Splatoon 3 use the same messages for auth, friends, matchmaking and gamesync |
| Identity against nx-baas — the `nnex` claim, `/internal/switch/identity`, the `u-…` user id derivation | byte-identical logic in both, differing in nothing |
| ES256 access and refresh tokens | the claim shape is the SDK's, not the game's. The only per-title values are `tid` and `app_id` |
| gRPC plumbing — `npln-grpc-type`, the keepalive policy, `UNIMPLEMENTED` logging, connection tracing | carried from Stardew to here unchanged, then independently confirmed as Splatoon 3 requirements with their own measured failure modes. That is the strongest evidence available that it is SDK behaviour rather than either title's |

**Proposal: `servers/npln-common`, a Go module holding `proto/` plus identity, token minting and
the server plumbing, with the per-title constants passed in.** Each title's repository then holds
only its own handlers — which for Splatoon 3 is the entire interesting part, and for Stardew is
already written.

Sequencing: do it as a change to Stardew, the working and deployed implementation, and have this
repository follow. Doing it the other way round means editing a live server to suit one that has
not yet answered a real RPC. It is worth doing before the friends/presence/matchmaking work here
starts, because that is the point at which the duplicated surface stops being three constants and
starts being three services.

Until then this repository carries the **auth subset only** of the schema — two `.proto` files,
deletable in one commit — rather than a second full copy.

## Still unknown

Short list now, and none of it blocks the next step:

1. The certificate-acceptance patch offset for the **current** game build. An offset exists for an
   older build; all patch work is build-id scoped, so it has to be re-derived. Evidence item 1.
2. Whether OpenPak's identity model satisfies the client end to end — our auth is nx-baas's
   `nnex` projection, not the account service a working server talks to. The shapes match; the run
   has not happened.
3. Which hostname the client presents to the session endpoint. Two candidates appear in the
   record and a single-label wildcard covers neither automatically, so the SNI has to be measured
   before a certificate is issued. What the endpoint must *speak* is now established: gamesync.
4. Everything about the rotation format, if we generate schedules rather than capture them.

## Ports

`21012` gRPC/TLS tenant, `21013` health. The session/gamesync endpoint is a **separate listener**
the console dials directly, and takes `22210` out of this title's reserved NPLN block. STUN/TURN
needs no port of its own here: coturn lives in the shared `22900–22999` helper block. Claimed in
[`ports.md`](../../../ports.md).
