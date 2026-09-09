# Provenance

Every non-obvious fact this repository relies on, and where it came from. Required by
[`docs/clean-room-policy.md`](../../../docs/clean-room-policy.md): it is what makes "we rewrote it
from the facts" checkable later instead of a claim.

Source keys:

- **N** — the NextendoNetwork `splatoon-3` server (`~/REPOS/splatoon-3`, commit `b6a8c25`), read
  for facts under the policy. PolyForm Shield 1.0.0: no code, comments, docs, structure, type or
  function names were copied or translated. Where a fact came from a comment there recording a
  capture, the capture is named so the underlying evidence is identifiable.
- **E** — OpenPak's own emulator trees (`Openpak/emulators/*`, GPL forks).
- **O** — OpenPak's own repositories (`nx-baas`, `servers/stardew-valley`, `ports.md`).
- **P** — public documentation (upstream Ryujinx data, Kinnay's NPLN pages, switchbrew).

## Identity of the title

| Fact | Value | Source |
| --- | --- | --- |
| Title id | `0100C2500FC20000` | P — upstream Ryujinx `TitleIDs.cs`, `docs/compatibility.csv` |
| NPLN tenant host | `t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net` | E — a dated DNS resolution (2026-08-25) in citron's `sfdnsres.cpp`; **independently confirmed** by N, which uses the same host for its TLS SNI, its session relay default and a 704-request console capture |
| Tenant id / `npln.tid` claim | `t-dce9377b-lp1` | N |
| Client's tenant field | the client sends the alias `tenants/current`; the server resolves it | N |
| `npln.app_id` claim | `0100c2500fc20000` — the title id, not a separate NPLN application id | N |

## Access token

The token OpenPak mints is its own; the *shape the client requires* is the fact.

| Fact | Value | Source |
| --- | --- | --- |
| Algorithm / header | ES256, `jku: jwkSets/nplnAccessToken`, a stable `kid` | N (matches what Stardew already measured — O) |
| Claims the client reads | `npln.aid`, `npln.app_id`, `npln.authorization{allow,deny,nso_restricted}`, `npln.ext_id` (16 hex digits), `npln.ext_id_type: 1`, `npln.tid` | N, from a 2026-06-28 capture of Nintendo's own token |
| Why it must be a real, decodable JWT | the client reads its online rights out of the claims. An undecodable or rights-less token makes the game declare itself offline — no stages — *even though every RPC succeeds*. This is a silent failure, not an error | N |
| `nso_restricted` | `false` for a licensed player | N |
| Signature verification is load-bearing | a server that does not verify the signature on inbound tokens lets anyone forge an `ext_id` and act as another player. N carries a regression test for exactly this | N |

Our implementation already matched this shape (it was derived from Stardew, same SDK family); the
facts above confirmed it rather than changed it. The one thing they did change: `tenants/current`
now resolves instead of being passed through — see `resolveTenant`.

## Transport

| Fact | Value | Source |
| --- | --- | --- |
| Transport | gRPC over HTTP/2 over TLS, in-game statically linked stack over raw sockets | N, O |
| No mutual TLS | the console never presents a client certificate; identity is token-based | N, from an auth capture |
| `npln-grpc-type` response header | present on **all 704** responses in a real-console capture, no exception. Value is the call's nature: `Unary`, `ServerStreaming`, `ClientStreaming`, `BidirectionalStreaming` | N |
| It must be attached with `SetHeader`, not `SendHeader` | forcing the header frame out early breaks the two-frame shape an absent resource needs (headers, then `NotFound` trailers) | N |
| Keepalive enforcement | the client pings often **and while no RPC is in flight** (it holds long-lived streams and pings to keep the NAT mapping alive). grpc-go's default policy (`MinTime` 5 min, `PermitWithoutStream` false) counts this as abuse and sends `GOAWAY(ENHANCE_YOUR_CALM)`, killing every in-flight RPC | N — measured: schedules, save and `ActivateUser` all died mid-call |
| Our setting | `MinTime: 5s`, `PermitWithoutStream: true` | derived from the above |

## Error codes

| Code | Meaning | Source |
| --- | --- | --- |
| `2321-4992` | client-side gRPC `UNAVAILABLE` (module 321, `(64 + grpc_status) * 64`) | O — the conversion table lives in the title binary |
| `2321-3072` | `FAILED_PRECONDITION`. Observed when a session is offered with fewer players than the mode requires | N |
| `2162-0001` | `ResultErrApplicationAborted` — a hard abort in the `nn.npln.Worker` thread. Seen from malformed resource paths, empty documents where a document was expected, and inconsistent schedule timestamps. This is a crash, not a rejection | N |
| `2307-2103` | the Splatoon 2 equivalent of the Penne frontline failure | N |

## Service surface

The NPLN schema Splatoon 3 speaks spans 20 services across `auth`, `friends`, `gamesync`,
`globalcounter`, `hydro`, `ikaros`, `leaderboard`, `maintenance`, `matchmaking`, `messaging`,
`timber`, `toyohr`, `ugcstore` (source: N's generated bindings, method paths only). `toyohr` is
the Splatoon-3-specific one — schedules, Splatfest, cloud save, lockers, replays, lobby messaging.

**What a working server actually registers** (N, its server construction) is a much shorter list:
`Auth`, `Friends`, `PresenceService`, `Matchmaker`, `GameSessionService`, `Gamesync`,
`Ugcstore`, `toyohr.Schedule`, `toyohr.FestService`. Everything else is answered by a fallback
handler that replays captured bytes.

**`Matchmaker` and `GameSessionService` are both used.** This settles the open question in the
previous revision of `design.md`:

- `Matchmaker` — public matchmaking. Measured flow: `ListLatencyMeasurementServers` →
  `CreateMatchmakingTicket` → `TrackMatchmakingTicket` (server-stream, states
  `SEARCHING` → `PLACING` → `SUCCEEDED`, the terminal message carrying a game session and a
  per-user session token) → `AllocateIceServerSet` for the STUN/TURN it needs to reach the host.
- `GameSessionService` — host-created rooms: private matches, room codes, invitations
  (`CreateGameSessionCreationTicket` and friends).

**Splatoon 3 is not pure peer-to-peer.** A matched ticket points at a dedicated session server
(an ip:port) that all players connect to over ICE. Source: N, from a real-console matchmaking
capture. A regular battle needs **8 players**; the game checks the roster in its own binary and
refuses to start below the mode's size, so pairing players in twos goes nowhere.

## The pre-gRPC REST chain

Splatoon 3 talks JSON/REST to two host families **before** any gRPC (source: N, from console
captures). In OpenPak this belongs to `nx-baas`, which already routes these hosts — see
[`design.md`](design.md) for what it does and does not answer correctly.

| Endpoint | Required behaviour | Source |
| --- | --- | --- |
| `POST /v1/devices/initialize` on `gw.hac.lp1.vermillion.srv.nintendo.net` | 204 No Content | N |
| `GET /v1/devices/vermillion-device-id` | `{"vermillionDeviceId": "<base64 of 16 bytes>"}` — **camelCase key**. A wrong key means no device id and an endless vermillion loop. The base64 must contain no `/` (the game uses the value as a path segment and a `/` crashes it) | N — both failures measured |
| `PUT /v1/devices/penne-id` | 204 No Content. A 404 here loops device setup forever and the tenant is never reached | N |
| `GET /v1/accounts/config` | `{"payload": "<base64 of a JSON object>"}` whose inner object carries `online_license.is_available: true`. **This is the online gate.** `false` parks the game at the lobby with `2321-4992` | N |
| `GET /v1/accounts/<other>` (e.g. `vphyms`) | 200 with the same `{"payload": …}` envelope, even if the inner object is empty. A 404 makes the game treat account setup as failed and restart the whole bootstrap with a new penne id — an infinite loop on the loading screen | N — no capture of this path exists; the 200 is what breaks the loop |
| `POST` to `fro-N.hac.lp1.penne.srv.nintendo.net` | the connection must be **held open** as a bidirectional stream (chunked, heartbeat) for room-code registration. Closing it immediately errors the client | N |
| Device id source | the SDK puts it in the User-Agent: `NintendoSDK Firmware/<ver> (platform:NX; did:<hex>; eid:lp1)` | N |

## Client-side prerequisites

Three conditions, all client-side, or the game abandons its own call before sending a single
HTTP/2 HEADERS frame (source: N, O):

1. the NPLN hosts resolve to our server;
2. the game's own certificate check is bypassed — the SDK reads a never-set "certificate
   accepted" byte and picks an always-OK verify callback when it is set (O: family notes,
   `LDRB W10,[X21,#0x38]`; **all patch work is scoped to one exact build id**, and N records no
   offset for a current build, so this still needs re-deriving — see `evidence-needed.md`);
3. it presents an identity the server's auth accepts.

## Status facts worth having

- The stage/mode rotation and Splatfest schedules in N are **replayed captured bytes with every
  timestamp shifted forward**, not generated from a model of the rotation. A stale schedule is
  rejected with `2321-4992` and the game shows "stage information is not available offline".
- Those captured bytes are Nintendo's and are not redistributable; N strips them and loads them
  from a directory the operator supplies. **OpenPak has no such corpus.** Schedules therefore
  cannot be ported — they have to be generated, or captured by us.
- `Gamesync/GetDocument` stays unary and answers `NotFound` for an absent document. Two attempts
  to imitate an observed empty success both crashed the game (`2162-0001` at ~72 s of boot). This
  is recorded as a **dead end**, not a design.
- PID and uid travel on **different channels**: the PID is in the access token, the uid in a
  request metadata header. Some long-lived streams arrive with a uid but no usable token, so a
  server that needs both must record the pairing at authentication time — the one point where
  both are visible. Not needed yet here; it will be the moment presence exists.
