# Deploy: npln

## Image and ports

The image is `ghcr.io/openpak/npln`, built from the multi-stage `Dockerfile` (`go build ./cmd/nplnd`,
Alpine 3.20 runtime) and run as uid 65532. Tenant listeners are on ports >= 20000, so no capability is
needed to bind. Health is a plain `/health` on each title's health port.

| Title | `NPLN_TITLE` | Tenant port | Health port | Session port |
| --- | --- | --- | --- | --- |
| stardew-valley | `stardew-valley` | 21010 | 21011 | — |
| splatoon-3 | `splatoon-3` | 21012 | 21013 | 22210 |
| super-mario-bros-wonder | `super-mario-bros-wonder` | 21014 | 21015 | 22220 |
| super-mario-party-jamboree | `super-mario-party-jamboree` | 21100 | 21101 | 22250 |
| pokemon-scarlet-violet | `pokemon-scarlet-violet` | 21102 | 21103 | 22260 |
| pokemon-legends-arceus | `pokemon-legends-arceus` | 21104 | 21105 | 22270 |
| monster-hunter-rise | `monster-hunter-rise` | 21106 | 21107 | 22280 |
| bayonetta-3 | `bayonetta-3` | 21110 | 21111 | 22290 |
| endless-ocean-luminous | `endless-ocean-luminous` | 21112 | 21113 | 24500 |
| metal-gear-solid-peace-walker | `metal-gear-solid-peace-walker` | 21114 | 21115 | 24530 |
| terraria | `terraria` | 21116 | 21117 | 24540 |
| hamster-playground | `hamster-playground` | 21118 | 21119 | 24510 |
| wavecade | `wavecade` | 21120 | 21121 | 24520 |
| dinkum | `dinkum` | 21122 | 21123 | 22230 |
| human-fall-flat | `human-fall-flat` | 21124 | 21125 | 22240 |
| go-go-town | `go-go-town` | 21126 | 21127 | 24550 |
| plateup | `plateup` | 21128 | 21129 | 24560 |
| core-keeper | `core-keeper` | 21130 | 21131 | 24570 |

## Connectivity

Set `NPLN_CONNECTIVITY_URL` and `NPLN_CONNECTIVITY_TOKEN_FILE` (private 0600 mounted
file) to enable it. With integration configured, allocation failures return gRPC
Unavailable; there is no silent fallback to locally minted credentials. When both
settings are absent, the legacy `NPLN_TURN_SECRET` path remains available for local
development and rollback. Existing TURN allocations retain their normal lifetime.

## Federation routing

Federation routing: set `OPENPAK_ROUTING_URL` (account core, e.g. `http://openpak-account:8000`)
and `OPENPAK_ROUTING_KEY` to read the core's routing table (`internal/routing`, a copy of the
account core's resolver). Every member of a session then gets the STUN/TURN of the session
**host's** region node (host country from the adapter identity, test-node access from the core's
allow-list); with the connectivity service configured, it is sent the host's country instead.
Splatoon 3's latency candidates whose GCP region has a STUN node measure that node. With the URL
unset, or no nodes, everything is answered from home as before. `OPENPAK_REGIONS_URL` is gone.

## cmd/nplnd

- `cmd/nplnd` owns what every title needs identically: the TLS listener on the tenant port
  (`NPLN_LISTEN`, `CERT_FILE`, `KEY_FILE`), the plain `/health` (`HEALTH_LISTEN`) and the
  `NPLN_TURN_SECRET` check. It then hands the listener to the title's `Run`.

## UGC store

- The UGC store keeps its documents in one JSON file, `NPLN_UGC_FILE` (default
  `ugcstore.json` in the working directory; point it into the data volume, e.g.
  `/app/data/ugcstore.json`). Everything else is in memory.

## Run

```sh
NPLN_TITLE=stardew-valley NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ./cmd/nplnd
```

Every environment variable a title read before is unchanged; `NPLN_TITLE` is the only new one.
The former `stardew-valley` and `splatoon-3` repositories are archived; their history is merged
here.
