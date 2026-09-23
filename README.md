# npln

The shared NPLN host and, under `titles/`, every OpenPak NPLN title. One module, one image
(`ghcr.io/openpak/npln`), one tag per release; a container runs one title, chosen by
`NPLN_TITLE`. NPLN is the gRPC-over-TLS transport family Nintendo Switch titles such as
Stardew Valley and Splatoon 3 use instead of NEX.

| Title | `NPLN_TITLE` | Tenant port | Status |
| --- | --- | --- | --- |
| [`titles/stardew-valley`](titles/stardew-valley) | `stardew-valley` | 21010 (health 21011) | live, console-verified |
| [`titles/splatoon-3`](titles/splatoon-3) | `splatoon-3` | 21012 (health 21013), session 22210 | early, not run against retail |
| [`titles/super-mario-bros-wonder`](titles/super-mario-bros-wonder) | `super-mario-bros-wonder` | 21014 (health 21015), session 22220 | early: Stardew's service set under Wonder's tenant `t-ba973ec6-lp1`, plus the Matchmaker (course/world-map and friend pools), messaging, UGC store and hydro datastore; deployed, not console-verified. Served with a leaf chained to the OpenPak CA, which the console/emulator trusts via its system store; unverified until booted |
| [`titles/dinkum`](titles/dinkum) | `dinkum` | 21122 (health 21123), session 22230 | registered: Stardew's service set; tenant unknown, so `NPLN_TENANT` is required; NPLN 1.43.3 with WebRTC/QUIC P2P add-ons |
| [`titles/human-fall-flat`](titles/human-fall-flat) | `human-fall-flat` | 21124 (health 21125), session 22240 | registered: Stardew's service set; tenant unknown, so `NPLN_TENANT` is required; NPLN 1.41.1, Pia 6.42.0 |
| [`titles/metal-gear-solid-peace-walker`](titles/metal-gear-solid-peace-walker) | `metal-gear-solid-peace-walker` | 21114 (health 21115), session 24530 | registered, never booted: Stardew's service set under tenant `t-4f53195c-lp1`, with config-name lobby sizes, FriendSearch and config properties, plus the Matchmaker, messaging, UGC store and hydro datastore. Served with a leaf chained to the OpenPak CA, which the console/emulator trusts via its system store; unverified until booted |

## Shape

- `cmd/nplnd` owns what every title needs identically: the TLS listener on the tenant port
  (`NPLN_LISTEN`, `CERT_FILE`, `KEY_FILE`), the plain `/health` (`HEALTH_LISTEN`) and the
  `NPLN_TURN_SECRET` check. It then hands the listener to the title's `Run`.
- `proto/` is the one NPLN schema both titles share (Stardew's tree plus Splatoon's `toyohr`
  services). One copy is not tidiness: two copies of the same `.proto` file names in one
  binary would collide in the protobuf registry at start-up. Regenerate with
  `proto/generate.sh`.
- `titles/<game>/npln` is that title's service implementations. Stardew and Splatoon still
  carry their own auth, friends and gamesync code. Titles not yet observed run Stardew's set
  under their own tenant through `titles/stardewset` (Wonder, Dinkum, Human Fall Flat).
- Services beyond Stardew's set are opt-in per title through `npln.Options`: `Matchmaker`
  (matchmaking.v1 ticket pools and friend pools), `Messaging` (messaging.v1), `UGC`
  (ugcstore.v1 Ugcstore and Screening) and `Hydro` (hydro.v1 Datastore). Wonder and Peace Walker
  take all four; every other title is unchanged and logs `UNIMPLEMENTED` for them.
  `ConfigProperties` also makes gamesync's `mcn` the session's own config name; titles without
  it keep Stardew's `Farm4Player`.
- The UGC store keeps its documents in one JSON file, `NPLN_UGC_FILE` (default
  `ugcstore.json` in the working directory; point it into the data volume, e.g.
  `/app/data/ugcstore.json`). Everything else is in memory.
- Each title keeps its own README, docs and tools (`titles/stardew-valley/cmd/observer`,
  `cmd/probe`; `titles/splatoon-3/cmd/genrotation`) in its directory.

## Bans

`bans` locks a banned OpenPak account out of every title
([`website/docs/ban-lookup.md`](../../website/docs/ban-lookup.md)): no token is issued or
refreshed for it, calls carrying its token are refused with `PermissionDenied`, and its open
streams end within a minute. The source is nx-baas (`/internal/switch/identity` answers 403
`account_banned`), reached with the `NX_INTERNAL_URL` / `NX_INTERNAL_KEY` every title already
has. A title wires it with `bans.Default.Unary` / `bans.Default.Stream` in its server chain.

## Run

```sh
NPLN_TITLE=stardew-valley NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ./cmd/nplnd
```

Every environment variable a title read before is unchanged; `NPLN_TITLE` is the only new one.
The former `stardew-valley` and `splatoon-3` repositories are archived; their history is merged
here.
