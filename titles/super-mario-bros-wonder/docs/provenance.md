# Provenance

Every non-obvious fact this title relies on beyond Stardew Valley's service set, and where it
came from, as [clean-room policy](https://docs.openpak.org/contributors/clean-room/) asks.

Source keys:

- **N** — earlier research notes on the title's online services, kept outside this repository. They were used for facts only under the policy: no code, comments, docs, structure, type or function names were copied or translated.
- **D** — the protocol descriptors recorded in N: service, method,
  message and field names, numbers and types. OpenPak's `.proto` files were written from a
  listing of those descriptors and regenerated with `proto/generate.sh`; no generated file was
  copied.
- **K** — Kinnay's NintendoClients wiki, "NPLN Servers".
- **O** — OpenPak's own repositories and dumps.
- **I** — inferred by OpenPak from the schema alone, with no observation behind it. Every
  **I** row is a guess to check against the first boot's log.

Nothing below has been observed by OpenPak yet: no boot, no capture of ours.

## Identity of the title

| Fact | Value | Source |
| --- | --- | --- |
| Title id | `010015100B514000` | O (v1.2.1 dump) |
| NPLN tenant | `t-ba973ec6-lp1` | K, cross-checked in O's v1.2.1 dump (2026-09-18) |
| TLS trust | served with a leaf chained to the OpenPak CA, which the console/emulator trusts via its system store; unverified until booted | O |

## Schemas

| Package | Services and methods | Source |
| --- | --- | --- |
| `nn.npln.matchmaking.v1` | `Matchmaker`: CreateMatchmakingTicket, TrackMatchmakingTicket (server stream), CancelMatchmakingTicket, CreateAcceptance. OpenPak's existing `matchmaker.proto` already matched the descriptor field for field | D `matchmaker.pb.go` |
| `nn.npln.messaging.v1` | `Messaging`: RecvMessage (server stream of message / ack / keep_alive), SendMessage, SendAck | D `messaging.pb.go` |
| `nn.npln.ugcstore.v1` | `Ugcstore`: GetDocument, BulkGetDocuments (server stream), RunQuery (server stream), CommitDocuments, IssueAttachmentUri, BulkIssueAttachmentUri (bidi stream), IssueUploadUri, CreateDocumentShortAlias, GetDocumentShortAlias, BulkGetDocumentShortAliases. `Screening`: CreateReport, IssueUploadUri. The query and write messages are Firestore-shaped (StructuredQuery, FieldFilter operators 1–10, UnaryFilter IS_NAN=2 / IS_NULL=3, WriteOperation with attachments) | D `ugcstore.pb.go`, `query.pb.go`, `write.pb.go`, `screening.pb.go`, `resources.pb.go` |
| `nn.npln.hydro.v1` | `Datastore`: CreateContent, GetContent, DeleteContent, SearchContent, TrackContent (server stream), TransformContent, BatchCreateContent, BatchDeleteContent, BulkDeleteContent, BatchTransformContent, BulkTransformContent. `Content` = name, user, permissions (READ=1, UPDATE=2), times, `payload` bytes, `attributes` map<int32,int32>, etag, expire_time/ttl | D `datastore.pb.go`, `resources.pb.go` |

Unexplained: `BatchTransformContentRequest.requests` is typed as `BatchTransformContentRequest`
itself (not `TransformContentRequest` as in the Bulk variant). Kept as the descriptor says.
The Screening service is in the descriptors but N does not register it.

## Matchmaker (public and friend pools)

| Fact | Value | Source |
| --- | --- | --- |
| Config names | `CourseMatch_20221202`, `WorldMapMatch_20221202`, `FriendWorldMapMatch_20230428`, `FriendMatch` (the last one a GameSessionService room) | N |
| Pool | tickets with the same config, the same `MatchingKey` user attribute and the same friend room share one game session until it is full; different keys never share | N |
| Capacity | `WorldMapMatch*`, `FriendWorldMapMatch*`, `FriendMatch*`: 12; `CourseMatch*`: 4; anything else 4 | N |
| Ticket | exactly one user definition, the caller's own; the answer is SEARCHING with the config spelled under the concrete tenant | N |
| Track | streams SEARCHING and PLACING (no session, no matched users), then SUCCEEDED with the game session and every member as a matched user session; only the caller's entry carries a matchmaking id token, and only when `include_id_token_users` names the caller (or `current`) | N |
| A repeated ticket | a player already in a pool's session is answered with that session rather than taking a second seat | N |
| Pool session properties | `_BaseConfigName` (config's last segment), `_AliasSuffix` (empty), `MatchingKey` from the ticket | N |
| Friend pools | a ticket for a `Friend*` config must carry a `FriendGameSessionId` user attribute naming the base room; the room must be active and the caller already a member of it; a non-`Friend` config carrying the attribute is refused. The pool's session keeps `FriendGameSessionId`, is not public, and is never listed or joinable as a room of its own | N |
| Rooms from `FriendMatch` | not public | N |

OpenPak keeps pool sessions in the same store as GameSessionService rooms, so gamesync,
`JoinGameSession` and leaving work on them unchanged. One difference, OpenPak's choice: a pool
session closes when its last member leaves, not when its first one does (a pool has no host).

## `mcn`

| Fact | Value | Source |
| --- | --- | --- |
| `mcn` in `docs/__stg/All` and `docs/__gs/f` | the session's `_BaseConfigName` | N |

OpenPak serves it for titles that stamp config properties (Wonder, Peace Walker). Every other
title on the set keeps `Farm4Player`, which Stardew and Dinkum play with.

## Messaging

| Fact | Value | Source |
| --- | --- | --- |
| RecvMessage `user` | `tenants/current/users/current` observed; a user other than the caller is refused (PermissionDenied) | N |
| Opening | headers, then a `keep_alive` with `idle_timeout` 95 s straight away | N |
| Cadence | a `keep_alive` every 45 s after that (N sends `idle_timeout` 0 on those); without the opening one and the cadence Wonder abandons the stream again and again | N |
| SendMessage, SendAck, message and ack delivery | not implemented by N, never observed | N |

What OpenPak does beyond N is **I**: a message sent to users is queued for each receiver and
pushed on their RecvMessage stream with a resume token; `need_ack` messages stay queued until
the receiver acks them; an ack is delivered to the original sender as a `RecvAck` naming the
receiver. A sender may only address friends (nx-baas friend list) or players it shares a game
session with.

## UGC store

| Fact | Value | Source |
| --- | --- | --- |
| The course query | after a course: `RunQuery` with parent `tenants/current/documents/ke/<n>/sdv/<n>/sh/<n>` (numbers; `ke/1/sdv/3/sh/2937190396` captured), `from` collection `ku`, order by `` `ut` `` descending, limit 30, no filter, no rule context | N |
| Empty result | one response carrying only `read_time` (Firestore's documented marker) | N |
| Field paths | back-quoted (`` `ut` ``), as in gamesync | N; O gamesync |
| Everything else | N answers only that query and leaves every other method unimplemented | N |

Beyond that query OpenPak's behaviour is **I**, read from the Firestore-shaped schema: documents
are stored, queries filter/order/limit/cursor over them, and a write is accepted only from the
document's owner (its first writer), only when every user-id-shaped path segment is the caller's,
and only within size limits. Attachments and upload/attachment URIs are not served.

## Datastore (hydro)

| Fact | Value | Source |
| --- | --- | --- |
| Created names | `<parent>/contents/<id>`, create and update time set by the server | N |
| A missing content | `GetContent` answers an empty content carrying the requested name, not NotFound | N |
| Search | returns stored contents, at most 50 | N |
| Meaning of Wonder's payloads | unknown; N assumes nothing | N |

The rest (owner and permissions enforced on change, etags, transforms of `payload` and
`attributes.<n>`, TrackContent notifying on change) is **I**.
