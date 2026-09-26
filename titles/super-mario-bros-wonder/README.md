# super-mario-bros-wonder

Super Mario Bros. Wonder (`010015100B514000`) on the shared NPLN host. It serves Stardew
Valley's verified NPLN service set (auth, friends, game sessions, gamesync) under Wonder's
tenant, plus the services the measured server's Wonder code and captures show it reaching:
the Matchmaker, messaging, the UGC store and the hydro datastore. Every fact and its source is
in [`docs/provenance.md`](docs/provenance.md); OpenPak has not booted it yet, so any method still
missing logs `UNIMPLEMENTED` and that log is the next work list.

| Fact | Value | Source |
| --- | --- | --- |
| NPLN tenant | `tenants/t-ba973ec6-lp1`, host `t-ba973ec6-lp1.lp1.t.npln.srv.nintendo.net` | Kinnay's NintendoClients wiki, "NPLN Servers" (read 2026-09-11); cross-checked against Splatoon 3's `dce9377b`, which the same table lists and this project had observed |
| Tenant port | 21014 (health 21015), session 22220 | [ports.md](https://github.com/openpak/docs/blob/main/ports.md) |
| TLS trust | served with a leaf chained to the OpenPak CA, which the console/emulator trusts via its system store; unverified until booted | OpenPak |
| Matchmaker | `CourseMatch_*` pools of 4, `WorldMapMatch_*` / `FriendWorldMapMatch_*` of 12, grouped by config, `MatchingKey` and friend room; friend pools (`FriendGameSessionId`) only for members of their `FriendMatch` base room, never listed | [provenance](docs/provenance.md) |
| Session properties | `_BaseConfigName` / `_AliasSuffix` stamped (`npln.Options.ConfigProperties`), so gamesync's `mcn` is the session's config name | [provenance](docs/provenance.md) |
| Messaging | `RecvMessage` opens with a 95 s keep-alive, then every 45 s; messages between friends or session mates, acks back to the sender | [provenance](docs/provenance.md) |
| UGC store | documents in `NPLN_UGC_FILE`; the course query (`ku` by `` `ut` `` desc, 30) and general Firestore-style queries; writes only by the owner, size-limited; attachments not served | [provenance](docs/provenance.md) |
| Hydro datastore | in memory; owner-only changes unless the content grants UPDATE | [provenance](docs/provenance.md) |

Deployed 2026-09-11 as `openpak-wonder` (tenant cert `deploy/keys/wonder.pem` from the OpenPak
CA, Traefik `HostSNI` passthrough). Not console-verified. `NPLN_TENANT` overrides the tenant if a
capture ever disagrees with the public list.

```sh
NPLN_TITLE=super-mario-bros-wonder NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ../../cmd/nplnd
```
