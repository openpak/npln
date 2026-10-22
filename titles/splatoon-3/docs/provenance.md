# Provenance

Every non-obvious fact this repository relies on, and where it came from. Required by
[`docs/clean-room-policy.md`](https://github.com/openpak/docs/blob/main/clean-room-policy.md): it is what makes "we rewrote it
from the facts" checkable later instead of a claim.

Source keys:

- **N** — earlier research notes on the title's online services, kept outside this repository. They were used for facts only under the policy: no code, comments, docs, structure, type or function names were copied or translated.
- **E** — OpenPak's own emulator trees (`Openpak/emulators/*`, GPL forks).
- **O** — OpenPak's own repositories (`nx-baas`, `servers/stardew-valley`, `ports.md`).
- **P** — public documentation (upstream Ryujinx data, Kinnay's NPLN pages, switchbrew).
- **G** — the game's own code: Splatoon 3 v11.3.0 (`main` NSO, build id `28C4287A…`), read with
  our own disassembly (resource-name parsers, the abort sites behind 2162-0001). Module bases are
  recovered from the `svcBreak` site in `sdk` (offset `0x17574c`); `main` sits `0x61f9000` below it.
- **M** — measured: a Harbor console running the retail game against this server, server log plus
  the guest thread dump at the abort.

## Identity of the title

| Fact | Value | Source |
| --- | --- | --- |
| Title id | `0100C2500FC20000` | P — upstream Ryujinx `TitleIDs.cs`, `docs/compatibility.csv` |
| NPLN tenant host | `t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net` | E — a dated DNS resolution (2026-08-25) in citron's `sfdnsres.cpp`; **independently confirmed** by N, which records the same host in a 704-request console capture |
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

**What a working server actually registers** (N) is a much shorter list:
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

Added when those services were built. Sources as above
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
| The watched document is a mailbox | each console writes its own Pia contact blob into its own document; the server deposits **each peer's** blob into the document that console watches, one push per peer. | N — session captures, 2026-08-25 |
| The server does not know how a match went | eight consoles left a match together fourteen seconds after joining, with healthy matchmaking, TURN allocations at 100% and no server-side closure. Finding out what happened meant reading the consoles' own telemetry. A simulator would already know | N, 2026-08-22 |
| Consoles track membership themselves | the Pia status blob carries a byte that follows the session headcount, written by the console | N (inferred from five observations there, and labelled as such) |
| Nothing server-side holds game state | the notes list schedules, Splatfests, lockers, records, replays and saves — metadata around matches. No match state, no tick loop, no physics | N |
| NAT traversal is ordinary ICE | `AllocateIceServerSet` returns a STUN server and TURN servers with username/password; it points at a stock **coturn** using coturn's REST ephemeral-credential scheme | N |

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
| coturn ephemeral credentials | TURN `username = "<unix-expiry>:<user>"`, `password = base64(HMAC-SHA1(static-auth-secret, username))`, STUN/TURN on `3478`. The notes record `AllocateIceServerSet` pointing at a stock coturn using this scheme | N; P — coturn's published REST-auth (TURN REST API / RFC 7635 §2.2) |
| A partial ICE object | the client rejects the whole set and emits no STUN probe. So every field must be filled — measured the same day as the relay captures | N |
| Latency measurement | the client sends each listed server a 20-byte STUN Binding Request once a second for 5 s from one UDP socket and accepts any reply of 20+ bytes from the exact ip:port echoing its transaction id; names, regions and count are not validated. The Stardew set answered `127.0.0.1:18504` (an unset default): Harbor carried on, a real Switch (fw 23) closed its NPLN connection ~6 s after ListLatencyMeasurementServers and showed a communication error with no code. Now the STUN host and port | G (`main+0x5bd438`..`0x5bd678`, receive loop `0x5b7570`, matcher `0x5be2c0`) + M (2026-10-11, [H2] connection trace) |
| A created room's properties | the client's own properties plus `_BaseConfigName` (the matchmaking config's last segment) and `_AliasSuffix` (the room code); the config is echoed under the concrete tenant | N |
| Room capacity | a private battle room holds 10 (eight players, two spectators); the friend party that waits for matchmaking (`join_room_4_no_referee_config`) holds 4 | N |
| The match referee | at the start each console writes `…/@@RefereeRequest/<time>_<game session>` and watches `@@RefereeResult`; without a verdict the "getting ready" countdown ends in a communication error. At the end it writes `…/@@RefereeReport/<time>_<game session>/User/<user session>`. The verdict is one document `…/@@RefereeResult/<time>_<game session>` (first segment only), `request_id` = that suffix, `status` 0 at the start and 1 when decided, carrying the request's context and the report's `team_result`/`personal_result`; field inventory of a Turf War verdict: 25 keys | N (`gamesync.go`, notes on captures of real matches) |

## Boot-time toyohr services (2026-10-10)

Order measured (M) on a Harbor console with a linked account and no cloud save: IssuePrearrangedUserToken,
ListLatencyMeasurementServers, LobbyMessaging/RecvMessage, LobbyMessaging/SendMessage (an `NplnLogin` the
player addresses to itself), Friends/ActivateUser, PresenceService/KeepAlive,
MaintenanceScheduleService/SubscribeMaintenanceSchedules, UserScreening/GetViolation, the five Schedule
selects, Friends/SubscribeFriendUsers, CloudSave/GetSaveRecord, Schedule/SelectVsParams,
PresenceService/SubscribePresences, then Locker/SelectDocuments every 8 s once the game is up.

| Fact | Value | Source |
| --- | --- | --- |
| The game parses resource names and aborts (`AbortImpl`, 2162-0001 on thread `nn.npln.Worker`) when one does not parse | parsers for: `tenants/{t}/lobbies/{l}`, `tenants/{t}/targets/{x}/vsParams/{p}`, `…/targets/{x}/vsSchedules/{s}`, `…/targets/{x}/coopSchedules/{s}`, `…/targets/{x}/leagueSchedules/{s}`, `tenants/{t}/seasonSchedules/{s}`, `tenants/{t}/fests/{f}/entries/{e}`, `tenants/{t}/festResults/{r}`, `tenants/{t}/festSchedules/{s}`, `tenants/{t}/fests/{f}/decryptionKey`, `tenants/{t}/fests/{f}/breakingNews`, `tenants/{t}/replayCodes/{c}`, `tenants/{t}/reports/{r}`, `tenants/{t}/users/{u}/violations/{v}`, `tenants/{t}/tags/{t}`, `tenants/{t}/users/{u}/snapshots/{s}`, `tenants/{t}/coopScenarioCodes/{c}` | G (`main+0x7d1990`…`0x7d6e00`) |
| Schedule names before 2026-10-10 (`tenants/{t}/coopSchedules/{i}`, no target) were the boot abort | the coop-schedule parser aborted right after the selects | G + M |
| GetViolation with no violation | an OK answer without a message makes the SDK build a default violation with an empty name, which the violation parser rejects: abort. A non-OK status takes the SDK's error path, which does not abort; NOT_FOUND (after a headers frame) lets the boot continue | G (`main+0x7c7ce4` handler, `main+0x7d58a0` parser) + M |
| Nintendo's answer to GetViolation has no message and `content-length: 0` | the capture N describes never recorded trailers, so its status is not established; the parse above says it cannot be OK | N (capture notes) + G |
| GetSaveRecord for a player with no record | NOT_FOUND after the headers; the game continues to its first-run screens | M |
| LobbyMessaging | RecvMessage opens with a KeepAlive carrying a `{"v":1,"t":"<ms>-<n>"}` resume token; the game sends `NplnLogin` to itself and expects it back; `sender_user` is the resolved `tenants/t-dce9377b-lp1/users/<id>` | N (field names, the self-echo), M (the request: one receiver, the sender) |
| CloudSave write rules | partial `save_data`, maps merge recursively, other values replace, empty arrays are stored as null, the response timestamp is the next request's `update_time` | N (replayed captured writes) |
| Locker and Canola SelectDocuments | polled every 8 s while unanswered; an empty list is accepted | M |
| An absent resource (ugcstore document, fest entry/result/key, violation, save record) | NOT_FOUND after a headers frame, message `NotFound. reason: "<Kind>:"<name>" Not Found"`, details one `nn.npln.errdetails.NError` with a fresh 32-hex trace id (field 1), a fresh 16-hex id (field 19) and the generic not-found code (field 6 = 0). A bare NOT_FOUND made the game retry a missing GameRecord document without end | N (capture notes of a real console against Nintendo's service, `ugcstore_erreur_absente.go`) |
| FestService between Splatfests | SelectFestSchedule with an empty response is accepted at boot and on lobby entry | M |
| PresenceService/KeepAlive | the game acks every heartbeat at once; answering acks loops (~2,000 heartbeats a minute measured 2026-10-10). Heartbeat on a timer and on presence updates only, as Stardew's server does | M; O (`titles/stardew-valley/npln/presence.go`) |
| The schedule set the lobby accepts | the game stores at most 12 VS schedules and needs at least 5; every Bankara and X entry needs rule 1-4 and exactly 2 stages, regular exactly 2 stages; an empty League list passes, an empty Season list fails; any one kind failing fails the set | G (`main+0x32a28d0` schedule manager, `0x32a2e60`-`0x32a2fb4`, `0x32a48d8`, `0x32a5e8c`) |
| Ranked rules | four different ranked rules per slot, all from 1-4, never turf war; the list starts one slot before the present | N |
| Challenge (League) schedules | an empty list when no event runs, and the game accepts it. Measured together with the ranked-rule change above: before both, the lobby showed a communication error with no update banner; after both it shows "Multiplayer maps and modes have been updated!" first | N + G + M |
| Co-op `shift_id` | the shift's start as `YYYYMMDDhhmmss` | N |
| What the lobby checks before it opens (snapshot at `main+0x35340b0`, "now" = network clock seconds) | VS: the window covering now and the next two exist, the current one's stages exist in the VS stage table. Co-op: the shift covering now and the next two exist; the current one's stage exists in the co-op stage table, each main weapon exists or is -1 (random), the Grizzco weapon exists or is negative. Season: the last entry covers now and its field 2 is a season-table id (field 2 itself when <= 8, else `((n-1)&3)+5`). League: at most one event | G (`0x35340b0`, `0x352ec90`, `0x353703c`) |
| Table ids (v11.3.0 RSDB, looked up by `Id`) | VS stages 1-26; co-op stages 1, 2, 4, 6, 7, 8, 9 (Big Run 100, 102-107); seasons 0-9; weapons are not rows for -1 and -2 | G (romfs `VersusSceneInfo`, `CoopSceneInfo`, `SeasonInfo`, `WeaponInfoMain`) |
| Co-op monthly reward | the lobby's CoopReward step refuses the current shift unless `reward_type` is "Head", "Clothes" or "Shoes" and `reward_gear_id` (21000-21999) exists in that gear table (Head 21000-21005, 21007-21023; Clothes 21000-21006, 21008-21017; Shoes 21001-21009). Without it the lobby shows a communication error with no code (measured live: reward record +0x60 = 7) | G (`0x3879c00`, `0x37e98f0`; romfs `GearInfoHead/Clothes/Shoes`) + M |
| GameRecord/InitializeAttributes | called after a NOT_FOUND `CoopUserAttribute/Data` (or `VsUserAttribute/Data`) read; UNIMPLEMENTED shows 2321-4224. The game parses only `attributes`, every key optional (coop: `shift_id`, `limited_shift_id`, `mm_disable_limit`, `total_grade_point`, `smell`; VS: `season_id`, `udemae`, ...). Request/response field numbers from the notes' descriptors | G (`0x327fe6c`, `0x327ba70`, `0x327a230`) + N (descriptors) + M |
| Hammer `UserAttribute/Data` | read at login; any non-OK status is taken as `total_participants` = 0, no error raised | G (`0x32b4f20`, handler `0x32acd88`) |
| SelectVsParams | the SDK keeps only the name (parsed), the two timestamps and the attributes; `params_set_id` and both responses' `schedule_set_id` are dropped and never compared; the game reads only the attributes map, every key optional | G (`main+0x7b8620`, `0x7bbee0`, `0x32a4968`-`0x32a4dac`) |

## Proto provenance

The `proto/gamesync/**` and `proto/matchmaking/**` `.proto` files were copied from
`servers/stardew-valley/proto` (source O — OpenPak's own AGPL-3.0 repository) with only the
`go_package` path rewritten from `openpak/stardew-valley` to `openpak/splatoon-3`. The `.pb.go`
bindings are regenerated locally by stock `protoc`. This is a reuse of OpenPak's own reconstruction
of Nintendo's shared NPLN schema, not a second read of the source notes — the field
names/numbers/types were checked against N's descriptors (the tables above) and match. The
duplication is deliberate debt: see `design.md`, "Lift the shared NPLN layer".

The toyohr `cloud_save`, `lobby_messaging`, `userscreening`, `common`, `locker`, `canola`, `fest` and `game_record` schemas and
`maintenance/v1` (2026-10-10) take their field names, numbers and types from N's compiled descriptors
(facts: wire layout), written out as fresh `.proto` text in this repository's style. The game binary
carries no descriptors, only the method paths, which match (G).
