# metal-gear-solid-peace-walker

METAL GEAR SOLID: Peace Walker, Master Collection version (`0100C6F01C4F8000`), on the shared
NPLN host. It serves Stardew Valley's NPLN service set (auth, friends, presence, game sessions,
gamesync) under Peace Walker's tenant, plus what the title is known to need beyond it,
including the Matchmaker, messaging, UGC store and hydro datastore the earlier implementation
registers for it. Every
fact and its source is in [`docs/provenance.md`](docs/provenance.md). OpenPak has not booted it
yet.

| Fact | Value |
| --- | --- |
| NPLN tenant | `tenants/t-4f53195c-lp1`, host `t-4f53195c-lp1.lp1.t.npln.srv.nintendo.net` |
| Ports | tenant 21114, health 21115, session 24530 ([ports.md](https://github.com/openpak/docs/blob/main/ports.md)) |
| Relay | TURN is required (without it the mesh stalls at `WaitSetupRelayAddress`); the shared `openpak-turn` coturn is advertised with `NPLN_TURN_SECRET` credentials, as for every title on the set |
| Lobby size | read from the config name: `Mmc_CoOps2` holds 2 |
| Session properties | `_BaseConfigName` / `_AliasSuffix` stamped on every created session (`npln.Options.ConfigProperties`) |
| Friend rooms | `QueryGameSessions` with `FriendSearch`: friends only, `MatchingKey` filtered |
| `mcn` | the session's own config name (`Mmc_CoOps2`), not Stardew's `Farm4Player` |
| Extra services | Matchmaker (ticket and friend pools), messaging, UGC store (`NPLN_UGC_FILE`), hydro datastore: the same shared implementations as Wonder's ([provenance](docs/provenance.md) says why) |
| TLS trust | served with a leaf chained to the OpenPak CA, which the console/emulator trusts via its system store; unverified until booted |

Any method the first boot calls that is still missing logs `UNIMPLEMENTED` with its name.

```sh
NPLN_TITLE=metal-gear-solid-peace-walker NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ../../cmd/nplnd
```
