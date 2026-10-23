# Provenance

Every non-obvious fact this title relies on, and where it came from, as
[clean-room policy](https://docs.openpak.org/contributors/clean-room/) asks.

Source keys:

- **N** — earlier research notes on the title's online services, kept outside this repository. They were used for facts only under the policy: no code, comments, docs, structure, type or function names were copied or translated.
- **O** — OpenPak's own repositories (`platforms/switch/games/npln`, `ports.md`).

Nothing below has been observed by OpenPak yet: no boot, no capture of ours.

## Identity of the title

| Fact | Value | Source |
| --- | --- | --- |
| Title | METAL GEAR SOLID: Peace Walker, Master Collection version | N |
| Title id / `npln.app_id` | `0100C6F01C4F8000` (lower-case in the token) | N |
| NPLN tenant | `t-4f53195c-lp1`, host `t-4f53195c-lp1.lp1.t.npln.srv.nintendo.net` | N |
| TLS trust | served with a leaf chained to the OpenPak CA, which the console/emulator trusts via its system store; unverified until booted | O |

## Services the title reaches

| Fact | Value | Source |
| --- | --- | --- |
| Services N serves | auth, friends, presence, gamesync, matchmaking (GameSessionService and Matchmaker), messaging, UGC store, hydro datastore, NNCS NAT check, STUN, TURN | N |
| Which of those carry Peace Walker evidence | GameSessionService (capacity, join, FriendSearch), gamesync (`mp` signalling, `docs/__gs/m`), TURN. The messaging, UGC store, datastore and Matchmaker code in N is annotated as Wonder's (course queries, `CourseMatch`/`WorldMapMatch` configs); nothing there names a Peace Walker call | N |
| NNCS | the client runs the usual NNCS NAT check before sessions | N; OpenPak serves it as a shared production service (O `titles/stardew-valley/docs/production.md`) |

## Relay (TURN)

| Fact | Value | Source |
| --- | --- | --- |
| TURN is required | with a STUN server but no TURN server in `AllocateIceServerSet`, mesh setup stops in `AttachMeshJob::WaitSetupRelayAddress`; with an authenticated UDP TURN server `CreateMesh`/`JoinMesh` complete | N |
| Credential form | N hands out a static username/password. The client only needs *valid* credentials for the advertised server, so OpenPak's shared-secret credentials for `openpak-turn` (coturn, `NPLN_TURN_SECRET`) serve the same purpose | N; O `titles/stardew-valley/npln/sessions.go` |

The Stardew service set already advertises STUN plus one authenticated UDP TURN server, so no new
relay is run for this title.

## Game sessions

| Fact | Value | Source |
| --- | --- | --- |
| Matchmaking config names | `Mmc_` followed by a mode and the lobby capacity as a trailing number: `Mmc_CoOps2` holds 2, `Mmc_CoOps4` holds 4 | N |
| When the capacity is taken from the name | when the creation ticket asks for no `max_participant_count` | N |
| Session properties | N adds `_BaseConfigName` (the config's last path segment) and `_AliasSuffix` (empty) to a created session's properties, keeping the host's own; the host's room properties include `MatchMode` (`COOPS` in N's test) | N |
| Join answer | `JoinGameSession` answers with **only the joiner's** matched user session, carrying its matchmaking id token. Returning every member made the client pick the host's entry, which carries no token, and gamesync `IssueToken` then refused it. N records this as matching a captured Nintendo response | N |
| Room documents | the client reads `docs/__gs/m` straight after creating its session and aborts with 2321-1536 when it is missing | N |

OpenPak's shared session code already answered a join with the joiner alone and already served
`docs/__gs/m`; the facts confirmed those rather than changed them.

## Friend discovery

| Fact | Value | Source |
| --- | --- | --- |
| Query | `QueryGameSessions` with `game_session_search_config` ending `FriendSearch`, one or more friend users, and a `MatchingKey` property | N |
| Server's duty | refuse the search unless every named user is the caller or the caller's friend; apply the requested filters; return only active rooms the caller may see | N |
| An unset property value | a property sent with no value is the search screen's "any" and matches every room | N |
| Friend pools | Matchmaker tickets for `Friend*` configs carry `FriendGameSessionId`; the pooled session keeps that property and is not listed as a joinable room of its own | N; implemented in the shared Matchmaker (facts in Wonder's `docs/provenance.md`) |

## Signalling

| Fact | Value | Source |
| --- | --- | --- |
| Envelope | the signalling write into the recipient's `…/__stu/<user session>` document carries `suid`, `susid`, `sussid`, `suscid` and, for Peace Walker, an `mp` map (SDP offer plus DTLS certificate) instead of Wonder's `pl` bytes | N |
| NAT traversal report | the client writes `docs/__mt/nat_traversal` with an array `a` of entries (`lu`, `ru`, `p2p` = `nn::pia::npln::NplnPlugin`, `rc` integer, `re` boolean) | N |

OpenPak's gamesync stores and relays whatever a player writes, field by field, so an `mp` map
reaches the recipient unchanged; the NAT traversal report is stored like any other document.

## Services beyond the Stardew set (2026-09-23)

| Fact | Value | Source |
| --- | --- | --- |
| What N registers for this title | Matchmaker, Messaging, Ugcstore, Datastore, next to the services the Stardew set already has (Screening is not registered) | N |
| Their schemas and behaviour | recorded once, in Wonder's provenance, since N's code for them is Wonder's | [`../../super-mario-bros-wonder/docs/provenance.md`](../../super-mario-bros-wonder/docs/provenance.md) |
| `mcn` in `docs/__stg/All` and `docs/__gs/f` | the session's `_BaseConfigName` | N |

Decision: Peace Walker gets all four services (`npln.Options` `Matchmaker`, `Messaging`, `UGC`,
`Hydro`). N is the server this title was brought up against and it registers them for it; a
service the title never calls costs nothing, while a call answered `UNIMPLEMENTED` could stall
it. Its sessions already stamp config properties, so `mcn` now serves the session's config name
(`Mmc_CoOps2`) as N does; titles without config properties keep `Farm4Player`.

## Unexplained / open

- Whether Peace Walker calls the Matchmaker, messaging or the UGC store at all: N serves them,
  but its code for them is Wonder's and nothing there names a Peace Walker call. The first boot's
  log will say.
