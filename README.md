# splatoon-3

NPLN game server for **Splatoon 3** (`0100C2500FC20000`) on Nintendo Switch. NPLN, not NEX — same
transport family as [`stardew-valley`](../stardew-valley), a much larger service surface, and far
less of it observed.

**Status: skeleton.** It starts, terminates TLS on the Splatoon 3 NPLN tenant port, answers
`nn.npln.auth.v1.Auth` against [`nx-baas`](../../nx-baas), and logs every other method as
`UNIMPLEMENTED`. **No part of it has been run against the retail game.** Friends, presence,
matchmaking, game sessions, gamesync and NAT/TURN are not implemented, because nothing has been
observed to implement them from — see [`docs/design.md`](docs/design.md) for what is known and
[`docs/evidence-needed.md`](docs/evidence-needed.md) for how to find out the rest.

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
NextendoNetwork per-title trees — including the unrelated `~/REPOS/splatoon-3` — are PolyForm
Shield and were not opened for this work. Inputs are our own observations, public protocol
documentation, compatibly licensed code, and OpenPak's own repositories. Everything asserted in
`docs/design.md` carries a source and a confidence label; anything unmeasured is written as
"unknown, needs a capture of X".

AGPL-3.0-only, English only, Go 1.25, no secrets in the repository.

This project is not affiliated with or endorsed by Nintendo, Splatoon 3's publisher, or any
original service provider. It contains no proprietary game code, assets, SDK material, binaries,
certificates, keys, firmware, extracted files, credentials, or raw packet captures.
