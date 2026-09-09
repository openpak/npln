# splatoon-3

NPLN game server for **Splatoon 3** (`0100C2500FC20000`) on Nintendo Switch. NPLN, not NEX — same
transport family as [`stardew-valley`](../stardew-valley), a much larger service surface, and far
less of it observed.

**Status: early.** It starts, terminates TLS on the Splatoon 3 NPLN tenant port, and answers
`nn.npln.auth.v1.Auth`, `nn.npln.friends.v1.Friends` and `nn.npln.friends.v1.PresenceService`
against [`nx-baas`](../../nx-baas), logging every other method as `UNIMPLEMENTED`. **No part of it
has been run against the retail game.** Matchmaking, game sessions, gamesync and the
Splatoon-specific `toyohr` services are not implemented.

What the title actually requires is now documented rather than guessed:
[`docs/design.md`](docs/design.md) is the plan, [`docs/provenance.md`](docs/provenance.md) is the
fact ledger with a source for every claim, and [`docs/evidence-needed.md`](docs/evidence-needed.md)
is what still has to be found out by running something. Two things outside this repository block
the title: the pre-gRPC REST bootstrap is `nx-baas`'s and is currently wrong for Splatoon 3 in four
identified ways, and the stage rotation has to be generated or captured because it cannot be
ported. Both are in design.md.

## Run

```sh
go build ./cmd/npln
```

| Env | Meaning |
| --- | --- |
| `NPLN_LISTEN` | gRPC/TLS listener, default `:21012` ([ports.md](../../ports.md)) |
| `HEALTH_LISTEN` | plain-HTTP `/health`, default `:21013`; empty disables it |
| `CERT_FILE` / `KEY_FILE` | TLS cert covering `t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net` |
| `NX_INTERNAL_URL` | nx-baas game/internal API, default `http://127.0.0.1:20070` |
| `NX_INTERNAL_KEY` | nx-baas's `NX_INTERNAL_KEY`, sent as `X-Internal-Key` |
| `NPLN_JWT_KEY` | persisted ES256 key for the access tokens (a restart otherwise invalidates every live session) |
| `NPLN_FRIENDS_CHUNK_BYTES` | byte budget for one friend-list stream message, default 4096. The whole graph in one message is a measured crash |

Auth flow: the game sends its BAAS id_token; the `nnex` claim inside it was signed by nx-baas. This
server POSTs that claim to `/internal/switch/identity`, gets back `{pid, baas_user_id, nickname,
friends[]}`, and only then issues its own NPLN tokens. An identity nx-baas cannot prove gets
`PERMISSION_DENIED`, never a token.

`proto/` holds the auth subset of the NPLN schema and the bindings generated from it
(`proto/generate.sh`, see [`proto/NOTICE.md`](proto/NOTICE.md)). The schema and the identity/token
code are not title-specific and are duplicated from `stardew-valley`; design.md proposes lifting
them into one `servers/npln-common` module before a third NPLN title starts.

## Test

```sh
go build ./... && go vet ./... && go test ./...
```

## Clean room

Read [`docs/clean-room-policy.md`](../../docs/clean-room-policy.md) before writing a line. The
NextendoNetwork `splatoon-3` server is PolyForm Shield 1.0.0 and was read **for facts only** —
ports, hostnames, endpoint paths, wire field names, error codes, flows, and what actually runs. No
code, comments, docs, structure, type or function names were copied or translated. Every fact taken
is recorded with its source in [`docs/provenance.md`](docs/provenance.md), which is what makes
"rewritten from the facts" checkable rather than a claim. Anything still unmeasured is written as
unknown, not filled in by guessing.

AGPL-3.0-only, English only, Go 1.25, no secrets in the repository.

This project is not affiliated with or endorsed by Nintendo, Splatoon 3's publisher, or any
original service provider. It contains no proprietary game code, assets, SDK material, binaries,
certificates, keys, firmware, extracted files, credentials, or raw packet captures.
