# metal-gear-solid-peace-walker

METAL GEAR SOLID: Peace Walker, Master Collection version (`0100C6F01C4F8000`), on the shared
NPLN host. It serves Stardew Valley's NPLN service set (auth, friends, presence, game sessions,
gamesync) under Peace Walker's tenant, plus what the title is known to need beyond it. Every
fact and its source is in [`docs/provenance.md`](docs/provenance.md). OpenPak has not booted it
yet.

| Fact | Value |
| --- | --- |
| NPLN tenant | `tenants/t-4f53195c-lp1`, host `t-4f53195c-lp1.lp1.t.npln.srv.nintendo.net` |
| Ports | tenant 21114, health 21115, session 24530 ([ports.md](../../../../ports.md)) |
| Relay | TURN is required (without it the mesh stalls at `WaitSetupRelayAddress`); the shared `openpak-turn` coturn is advertised with `NPLN_TURN_SECRET` credentials, as for every title on the set |
| Lobby size | read from the config name: `Mmc_CoOps2` holds 2 |
| Session properties | `_BaseConfigName` / `_AliasSuffix` stamped on every created session (`npln.Options.ConfigProperties`) |
| Friend rooms | `QueryGameSessions` with `FriendSearch`: friends only, `MatchingKey` filtered |
| Client patch | **needed**: the title pins Nintendo's certificate |

Not served, because nothing ties them to Peace Walker yet: the Matchmaker service (and its
`FriendGameSessionId` friend pools), messaging `RecvMessage`, the UGC store and the hydro
datastore. If the first boot calls any of them, it logs `UNIMPLEMENTED` with the method name.

```sh
NPLN_TITLE=metal-gear-solid-peace-walker NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ../../cmd/nplnd
```
