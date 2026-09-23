# Provenance

Every non-obvious fact this title relies on, and where it came from, as
[`docs/clean-room-policy.md`](../../../../../docs/clean-room-policy.md) asks.

Source keys:

- **N** — the earlier network `metal-gear-solid-peacewalker` server (commit `5f0da66`, read
  2026-09-23 from a clone outside the workspace), read for facts under the policy. source-available
  Shield 1.0.0: no code, comments, docs, structure, type or function names were copied or
  translated. The file named is where the fact is recorded there.
- **O** — OpenPak's own repositories (`servers/npln`, `ports.md`).

Nothing below has been observed by OpenPak yet: no boot, no capture of ours.

## Identity of the title

| Fact | Value | Source |
| --- | --- | --- |
| Title | METAL GEAR SOLID: Peace Walker, Master Collection version | N `README.md` |
| Title id / `npln.app_id` | `0100C6F01C4F8000` (lower-case in the token) | N `README.md`, `token_jwt.go` |
| NPLN tenant | `t-4f53195c-lp1`, host `t-4f53195c-lp1.lp1.t.npln.srv.nintendo.net` | N `README.md`, `token_jwt.go` |
| Client patch | the title pins Nintendo's certificate and rejects the local peer name; the patch lives in the emulators' NPLN patch tables, keyed by title and build id | N `README.md` |

## Services the title reaches

| Fact | Value | Source |
| --- | --- | --- |
| Services N serves | auth, friends, presence, gamesync, matchmaking (GameSessionService and Matchmaker), messaging, UGC store, hydro datastore, NNCS NAT check, STUN, TURN | N `main.go`, `README.md` |
| Which of those carry Peace Walker evidence | GameSessionService (capacity, join, FriendSearch), gamesync (`mp` signalling, `docs/__gs/m`), TURN. The messaging, UGC store, datastore and Matchmaker code in N is annotated as Wonder's (course queries, `CourseMatch`/`WorldMapMatch` configs); nothing there names a Peace Walker call | N `messaging.go`, `ugcstore.go`, `datastore.go`, `matchmaking.go` |
| NNCS | the client runs the usual NNCS NAT check before sessions | N `README.md`, `nncs.go`; OpenPak serves it as a shared production service (O `titles/stardew-valley/docs/production.md`) |

## Relay (TURN)

| Fact | Value | Source |
| --- | --- | --- |
| TURN is required | with a STUN server but no TURN server in `AllocateIceServerSet`, mesh setup stops in `AttachMeshJob::WaitSetupRelayAddress`; with an authenticated UDP TURN server `CreateMesh`/`JoinMesh` complete | N `README.md` |
| Credential form | N hands out a static username/password. The client only needs *valid* credentials for the advertised server, so OpenPak's shared-secret credentials for `openpak-turn` (coturn, `NPLN_TURN_SECRET`) serve the same purpose | N `turn.go`; O `titles/stardew-valley/npln/sessions.go` |

The Stardew service set already advertises STUN plus one authenticated UDP TURN server, so no new
relay is run for this title.

## Game sessions

| Fact | Value | Source |
| --- | --- | --- |
| Matchmaking config names | `Mmc_` followed by a mode and the lobby capacity as a trailing number: `Mmc_CoOps2` holds 2, `Mmc_CoOps4` holds 4 | N `README.md`, `signalling_cleanup_test.go`, `matchmaking.go` |
| When the capacity is taken from the name | when the creation ticket asks for no `max_participant_count` | N `matchmaking.go` |
| Session properties | N adds `_BaseConfigName` (the config's last path segment) and `_AliasSuffix` (empty) to a created session's properties, keeping the host's own; the host's room properties include `MatchMode` (`COOPS` in N's test) | N `matchmaking.go`, `signalling_cleanup_test.go` |
| Join answer | `JoinGameSession` answers with **only the joiner's** matched user session, carrying its matchmaking id token. Returning every member made the client pick the host's entry, which carries no token, and gamesync `IssueToken` then refused it. N records this as matching a captured Nintendo response | N `README.md`, `session_service.go` |
| Room documents | the client reads `docs/__gs/m` straight after creating its session and aborts with 2321-1536 when it is missing | N `gamesync_store.go` |

OpenPak's shared session code already answered a join with the joiner alone and already served
`docs/__gs/m`; the facts confirmed those rather than changed them.

## Friend discovery

| Fact | Value | Source |
| --- | --- | --- |
| Query | `QueryGameSessions` with `game_session_search_config` ending `FriendSearch`, one or more friend users, and a `MatchingKey` property | N `README.md`, `session_service.go` |
| Server's duty | refuse the search unless every named user is the caller or the caller's friend; apply the requested filters; return only active rooms the caller may see | N `README.md`, `session_service.go` |
| An unset property value | a property sent with no value is the search screen's "any" and matches every room | N `session_service.go` |
| Friend pools | Matchmaker tickets for `Friend*` configs carry `FriendGameSessionId`; the pooled session keeps that property and is not listed as a joinable room of its own | N `README.md`, `session_service.go`, `matchmaking.go` — not implemented here: the Stardew set has no Matchmaker service, and nothing in N ties those configs to Peace Walker rather than Wonder |

## Signalling

| Fact | Value | Source |
| --- | --- | --- |
| Envelope | the signalling write into the recipient's `…/__stu/<user session>` document carries `suid`, `susid`, `sussid`, `suscid` and, for Peace Walker, an `mp` map (SDP offer plus DTLS certificate) instead of Wonder's `pl` bytes | N `README.md`, `gamesync_store.go` |
| NAT traversal report | the client writes `docs/__mt/nat_traversal` with an array `a` of entries (`lu`, `ru`, `p2p` = `nn::pia::npln::NplnPlugin`, `rc` integer, `re` boolean) | N `gamesync_store.go` |

OpenPak's gamesync stores and relays whatever a player writes, field by field, so an `mp` map
reaches the recipient unchanged; the NAT traversal report is stored like any other document.

## Unexplained / open

- `mcn` in `docs/__stg/All` and `docs/__gs/f`: N serves the session's `_BaseConfigName` there;
  the Stardew set serves `Farm4Player` for every title, and Dinkum plays with that. Watch it on
  the first boot.
- Whether Peace Walker calls the Matchmaker service, messaging `RecvMessage` or the UGC store at
  all: N serves them, but its code for them is Wonder's. The first boot's `UNIMPLEMENTED` lines
  will say.
