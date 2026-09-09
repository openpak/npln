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

## Friends and presence

Added when those services were built. Sources as above; N-sourced rows come from comments there
recording captures and dated measurements, named where they were given.

### `friends.v1.Friends`

| Fact | Detail | Source |
| --- | --- | --- |
| It must be dynamic | a fixed snapshot that does not match the logged-in identity stalls the game's session setup: the block list never becomes ready, so a room shows an empty roster with invitations and room codes disabled | N |
| `ListBlockingUsers` must answer | even empty. The client waits on it as part of that readiness | N |
| `FriendUser.relationship` | `presence_deliverable` and `presence_receivable` both true, as between real friends. Unset, the client can treat the friend as presence-less | O — measured for Stardew 2026-09-01, same service |
| Response size is load-bearing | the real service answers ~99 bytes for a new account and ~605 for an established one. Sending a whole large graph in one message — ~27 KB for 146 friends — was measured aborting the game's plaza resource-path parser (`2162-0001`), with the error context naming `SubscribeFriendUsers`. Splitting across stream messages is safe: the client accumulates them | N |
| An EMPTY friends response is dangerous | measured aborting the same plaza parser right after the second wave. A working server avoids it by falling back to a recorded non-empty structure | N |

On that last row: **OpenPak has no recorded structure to fall back to, and will not invent a
friend.** A player with no friends therefore gets an empty response and may hit the abort. The
server logs the condition loudly and `evidence-needed.md` makes it the first thing to test.

### `friends.v1.PresenceService`

| Fact | Detail | Source |
| --- | --- | --- |
| `Heartbeat` carries two durations | interval 30 s (field 1) and a deadline of 50 s (field 2). The client paces its ping on the interval and gives up after the deadline | N, from a fresh-account capture |
| Measured opening bytes | `1a 08 0a 02 08 1e 12 02 08 32` then `12 00` — heartbeat{interval 30 s, deadline 50 s} followed by an empty `enumeration_done` | N |
| Our schema declares the second field | as `google.protobuf.Duration deadline = 2`, which encodes to exactly those bytes. A unit test asserts the encoding, so the schema cannot drift off the measurement | O — this repository |
| Stream order | heartbeat → `presences` (only if non-empty) → `enumeration_done` → heartbeats | N |
| An empty `presences` message is never sent | it does not occur in observed traffic; a fresh account goes straight from the heartbeat to `enumeration_done` | N |
| `enumeration_done` is required | without it the client waits indefinitely for the rest of the list | N |
| The stream must push CHANGES | a subscription that sends only its opening snapshot leaves two friends with opposite views of each other, neither correcting. Measured 2026-08-15: a player who restarted stayed "offline" to a friend who had subscribed during the absence, while appearing online in the other direction | N |
| The resume token describes the ENUMERATED SET, not the delta | deriving it from just the changes made a 240-friend player's friend screen return a communication error and empty the list, while a 1-friend player saw nothing because their delta was always empty. The client cross-references the token against the set it holds | N |
| Targeted subscriptions | when the request names specific presences, answer with only those, or the client's cursor diverges | N |
| `KeepAlive` must answer every ping one-for-one | draining pings and heartbeating on an independent timer killed streams at ~60 s — the client gives up after two intervals with no answer to *its* ping, and the game reports a communication error although nothing was disconnected. Measured 2026-08-25 | N |
| A gRPC stream rejects concurrent `Send`s | so anything that both answers pings and ticks must serialise them. Our implementation answers only on receipt and so has one sender | N (the constraint), O (our design) |
| The client PUBLISHES its own state on `KeepAlive` | `UpdatePresence` carries the presence attributes. Discarding them is why a friend list can show placeholders and never a "Join" affordance | N |
| Presence attributes | a friend's presence carries ~13 attributes. `GameStatus` 1 = online, 2 = a room is open; `SessionId` is then the uuid the game passes to `JoinGameSession`; `MaxParticipants`/`CurrentParticipants` fill the roster count; `PlayerName`, `GameMode`, `Udemae`, `UsePassword` fill the line. The 1→2 transition with a non-empty `SessionId` is what makes "Join" appear. Measured 2026-08-15 | N |
| Updates are partial | so a server must merge them into what it already holds rather than replacing | N |
| ONLINE is never a guess | a player counts as online only while actually connected to this server. A wrong ONLINE sends a friend into a join that cannot succeed | N |
| Presence resource name | `<tenant>/users/<uid>/presence` | N |

## Schedules

| Fact | Detail | Source |
| --- | --- | --- |
| The service | `nn.npln.toyohr.v1.Schedule`, five calls: `SelectVsSchedules`, `SelectVsParams`, `SelectCoopSchedules`, `SelectSeasonSchedules`, `SelectLeagueSchedules` | N — method paths |
| Message shapes | request fields `target`/`tenant`, `etag`, `current_time`, and per-call `select_duration` / `vs_params_count` / `season_schedule_count`; responses carry `schedules`, an `etag`, and a leading bool we have no meaning for. Schedule entries carry `name`, `start_time`, `end_time`, `schedule_set_id` and per-kind settings (`regular_settings`, repeated `bankara_settings`, `x_settings`, `league_settings`; co-op `normal` with stage, boss, main weapons, kuma weapon, reward fields; league `slots` with their own windows) | N — field names, numbers and types |
| A STALE schedule set | is rejected by the game with `2321-4992` ("no current rotation") and the lobby reports that stage information is not available offline. This is the real stages blocker | N |
| An INCONSISTENT set | is **not** rejected — it aborts the game (`2162-0001`). All schedule kinds are read as one set and must share a single time base | N |
| `SelectVsParams` counts as a schedule kind | it carries timestamps of its own and must sit on the same base. Left out of the shift, its timestamps pointed months away from the rotation they describe and the lobby showed an error applet and stopped advancing | N |
| Fields we could not name | the leading bool on four responses, and several ints inside the settings and co-op messages. Kept as `field_N` in our schema per the family convention for unmeasured fields, rather than given invented names | O — this repository |

### The one thing here that is assumed, not established

Stage and rule **id numbering**. The generator emits rule ids 0–4 and stage ids 1..N. The rule
ordering (turf war, then the four ranked rules) follows the public SplatNet 3 naming, and stage
ids are assumed to run from 1. Whether the ids the NPLN tenant uses match that numbering is
**unverified**, which is why both are generator flags rather than constants: a capture can correct
them without a code change. A wrong id is expected to show the wrong or a blank stage rather than
to crash, but that expectation is itself untested. See `evidence-needed.md`.

### How OpenPak serves this

Code and data are separate. `internal/rotation` defines a rotation file, loads it and validates it;
`cmd/genrotation` writes a valid one anchored to real time; the server reads it at start-up.
Nothing recorded from the real service is in this repository, so the shipped path stays AGPL-safe
and needs no timestamp-shifting machinery. An operator holding their own recorded rotation points
`NPLN_ROTATION` at it instead.

Because an inconsistent set aborts the console rather than erroring, the loader **refuses to
start** the server on a rotation that would do that, naming the offending entry. A *missing* file
is only a warning: the rest of the server is still worth running, and the lobby simply reports that
stage information is unavailable. The validator is the one piece of non-trivial logic here and it
carries the tests.

## Relay or simulation? — settled: RELAY

The question that decides whether this title is reachable at all: does the session server the
matched players connect to **simulate the game**, or does it only **relay between consoles**?

**It relays. There is no server-side simulation.** The evidence, strongest first:

| Evidence | Detail | Source |
| --- | --- | --- |
| The consoles report peer-by-peer links | during a match each console writes a status document summarising its connection **to each other player individually**: the reporting plugin (`nn::pia::nplnd::NplnPlugin`), the local user, the *remote* user, a connection counter for that peer and an error flag for that peer. One console reported six such peers. A console talking to a simulator has **one** link, not six with independent state | N — capture of an official Splatfest **against Nintendo's own servers**, 2026-08-23 |
| The watched document is a mailbox | each console writes its own Pia contact blob into its own document; the server deposits **each peer's** blob into the document that console watches, one push per peer. In the reference's own words, that is how Pia learns who to join | N — session captures, 2026-08-25 |
| The server does not know how a match went | eight consoles left a match together fourteen seconds after joining, with healthy matchmaking, TURN allocations at 100% and no server-side closure. The reference had to start reading the consoles' own telemetry to find out what happened. A simulator would already know | N, 2026-08-22 |
| Consoles track membership themselves | the Pia status blob carries a byte that follows the session headcount, written by the console | N (inferred from five observations there, and labelled as such) |
| Nothing server-side holds game state | the reference implements schedules, Splatfests, lockers, records, replays and saves — metadata around matches. No match state, no tick loop, no physics | N — its own file inventory |
| NAT traversal is ordinary ICE | `AllocateIceServerSet` returns a STUN server and TURN servers with username/password; the reference points it at a stock **coturn** using coturn's REST ephemeral-credential scheme | N |

So the three infrastructure roles are:

1. **Signalling / rendezvous** — `gamesync` is a document store the consoles watch. Its job is to
   copy each console's Pia contact blob into its peers' mailboxes. This is the piece with real
   protocol work in it.
2. **NAT traversal** — STUN and TURN, from `AllocateIceServerSet`. A stock coturn satisfies it.
3. **Session bookkeeping** — the mutable session document: address, port, capacity, current
   players, whether a password is set.

The match itself runs on the consoles, over Nintendo's Pia peer-to-peer layer, directly where the
NAT allows it and through TURN where it does not.

**What this rules out.** This is not the Fall Guys situation. Nothing here requires reimplementing
game logic, so `gamesync` and `GameSessionService` are ordinary protocol work and the remaining
infrastructure is a TURN server we already know how to run.

**One correction to an earlier note in this repository.** A previous revision of `design.md` said
Splatoon 3 "is not pure peer-to-peer" and that players "connect to a dedicated session server over
ICE". That over-read the source. The accurate statement: consoles open a **second gRPC connection
to a session endpoint for gamesync signalling**, and connect **to each other** for gameplay. The
dedicated endpoint hosts the mailbox, it does not host the match.

### The session endpoint

| Fact | Detail | Source |
| --- | --- | --- |
| It is a separate listener | the client's Pia/NPLN layer dials `GameSession.Host:Port` directly, not through the tenant's front door, and speaks `nn.npln.gamesync.v1.Gamesync` there | N |
| Establishment order | `Gamesync/IssueToken` (exchange the matchmaking id-token for a session token), then `KeepUserSession`, a bidirectional stream that holds the session connected, plus a watch on the document store | N |
| The host does not write first | after subscribing it **waits** for its own user session document to appear, then reads the mutable session data. Answering "deleted" or empty leaves the host stuck on a connecting screen forever; the documents must be pushed as existing, with a concrete typed value per key — an empty-fields document crashed the worker | N |
| Certificate name | the client presents a hostname of its own for this endpoint, and a single-label wildcard does not cover a multi-label name, so the certificate has to be checked against what the client actually asks for. Two candidate names appear in the record (`gs.nintendo.net`, and a gamesync name compiled into the title). **Unverified which applies to our build** — measure the SNI before issuing a certificate | N, E |

## gamesync wire facts

Added when the mailbox was built. All shapes from N (method paths, message field names/numbers/types
in its generated bindings); the schema is Nintendo's and shared across NPLN tenants.

| Fact | Detail | Source |
| --- | --- | --- |
| Service methods | `nn.npln.gamesync.v1.Gamesync`, 13: `IssueToken`, `RefreshToken`, `GetDocument`, `ReadDocuments`, `ListDocuments`, `QueryCollectionIds`, `WriteDocuments`, `LazyWriteDocuments`, `BeginTransaction`, `CommitTransaction`, `RollbackTransaction`, `KeepUserSession` (bidi stream), `CreateRound` | N |
| `IssueToken` | request `{user_session, matchmaking_id_token}`, response `{Token{user_session, access_token, refresh_token, ttl}}` — exchanges the matchmaking id-token (minted by the matchmaking services) for a session token | N |
| Document shape | `Document{name, fields: common.MapValue, create_time, update_time}`. A field value is `common.Value` — a oneof over null/bool/int64/float/double/timestamp/string/bytes/array/map/reference. This is why "every field must be a concrete type" is expressible: an untyped field is an unset oneof | N |
| `KeepUserSession` | bidi. Client sends `{name, echo | update_target{target} | delete_target{name}}`; server sends `{echo | document_change | target_change}`. This is the watch | N |
| `Target` | `{name, documents: {documents[]} | collection: {collection}}` — a watch on named documents or on a collection prefix. `name` is the target id echoed back in changes | N |
| `DocumentChange` | `{target_id, document_change_type, document}`; type enum `EXIST=1`, `UPDATED=2`, `DELETED=3` | N |
| `TargetChange` | `{target_id, target_change_type, cause: google.rpc.Status}`; type enum `UPDATED=1`, `LISTED=2`, `DELETED=3`, `FAILED=4`. `LISTED` is how the client learns the initial enumeration of a target is complete | N |

## matchmaking wire facts

| Fact | Detail | Source |
| --- | --- | --- |
| `Matchmaker` methods | `CreateMatchmakingTicket` (→ `MatchmakingTicket`), `TrackMatchmakingTicket` (server-stream of `MatchmakingTicket`), `CancelMatchmakingTicket` (→ Empty), `CreateAcceptance` (→ `Acceptance`) | N |
| `GameSessionService` methods | 18, including `CreateGameSessionCreationTicket`, `TrackGameSessionCreationTicket` (stream), `CancelGameSessionCreationTicket`, `GetGameSession`, `BatchGetGameSessions`, `QueryGameSessions`, `JoinGameSession`, `SyncGameSession`, `ListUserSessions`, `GetUserSession`, `IssueMatchmakingIdToken`, `IssueUserDelegationToken`, `IssuePublicKey`, `CreateGameSessionShortAlias`, `GetGameSessionShortAlias`, `AllocateIceServerSet`, `ListLatencyMeasurementServers` | N |
| **`AllocateIceServerSet` and `ListLatencyMeasurementServers` are on `GameSessionService`** | not on `Matchmaker`. So the public-matchmaking flow crosses services — the ticket rides `Matchmaker`, the STUN/TURN and latency servers come from `GameSessionService`. This refines the flow recorded under "Service surface" above | N — method paths |
| `MatchmakingTicket.state` | `SEARCHING=1`, `PLACING=2`, `SUCCEEDED=3`, `TIMED_OUT=4`, `FAILED=5`, `CANCELLED=6`, `REQUIRING_ACCEPTANCE=7`, `DECLINED=8`. The terminal `SUCCEEDED` carries a `game_session` and `matched_user_sessions[]`, each with a `matchmaking_id_token` | N |
| `GameSession` | `{name, max_participant_count, current_participant_count, can_participate, is_public, password, state, host, port, create_time, properties: MapValue, user_sessions[]}`; state `CREATING=1`, `ACTIVE=2`, `TERMINATING=3`, `TERMINATED=4`. `host`/`port` is where the console dials the session endpoint (gamesync) | N |
| `IceServerSet` | `{name, stun_server, turn_servers[], ttl, update_time, client_cache_duration}`. `StunServer{host, port, protocol}`; `TurnServer{host, port, protocol, username, password}`; protocol enum `UDP=1, TCP=2, TLS=3` | N |
| coturn ephemeral credentials | TURN `username = "<unix-expiry>:<user>"`, `password = base64(HMAC-SHA1(static-auth-secret, username))`, STUN/TURN on `3478`. The reference points `AllocateIceServerSet` at a stock coturn using this scheme | N; P — coturn's published REST-auth (TURN REST API / RFC 7635 §2.2) |
| A partial ICE object | the client rejects the whole set and emits no STUN probe. So every field must be filled — measured the same day as the relay captures | N |

## Proto provenance

The `proto/gamesync/**` and `proto/matchmaking/**` `.proto` files were copied from
`servers/stardew-valley/proto` (source O — OpenPak's own AGPL-3.0 repository) with only the
`go_package` path rewritten from `openpak/stardew-valley` to `openpak/splatoon-3`. The `.pb.go`
bindings are regenerated locally by stock `protoc`. This is a reuse of OpenPak's own reconstruction
of Nintendo's shared NPLN schema, not a second read of the Shield-licensed tree — the field
names/numbers/types were checked against N's descriptors (the tables above) and match. The
duplication is deliberate debt: see `design.md`, "Lift the shared NPLN layer".
