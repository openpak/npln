# Protocol: npln

The shared NPLN host and, under `titles/`, every OpenPak NPLN title. One module, one image
(`ghcr.io/openpak/npln`), one tag per release; a container runs one title, chosen by
`NPLN_TITLE`. NPLN is the gRPC-over-TLS transport family Nintendo Switch titles such as
Stardew Valley and Splatoon 3 use instead of NEX.

## Services

- Services beyond Stardew's set are opt-in per title through `npln.Options`: `Matchmaker`
  (matchmaking.v1 ticket pools and friend pools), `Messaging` (messaging.v1), `UGC`
  (ugcstore.v1 Ugcstore and Screening) and `Hydro` (hydro.v1 Datastore). Wonder and Peace Walker
  take all four; every other title is unchanged and logs `UNIMPLEMENTED` for them.
  `ConfigProperties` also makes gamesync's `mcn` the session's own config name; titles without
  it keep Stardew's `Farm4Player`.

## Bans

`bans` locks a banned OpenPak account out of every title
([`website/docs/ban-lookup.md`](../../../website/docs/ban-lookup.md)): no token is issued or
refreshed for it, calls carrying its token are refused with `PermissionDenied`, and its open
streams end within a minute. The source is nx-baas (`/internal/switch/identity` answers 403
`account_banned`), reached with the `NX_INTERNAL_URL` / `NX_INTERNAL_KEY` every title already
has. A title wires it with `bans.Default.Unary` / `bans.Default.Stream` in its server chain.
