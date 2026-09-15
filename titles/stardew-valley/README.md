# stardew-valley

An early-stage, independent clean-room interoperability project investigating the network services needed by Stardew Valley multiplayer on Nintendo Switch.

Status (2026-09-07): the NPLN server (`cmd/npln` — `nn.npln.auth`, `nn.npln.friends`, `nn.npln.matchmaking.GameSessionService`, `nn.npln.gamesync`) hosts, lists, joins and runs three-player farms with rejoin, host-leave and stale-seat cleanup (verified on Ryujinx, `handoff.md` session 14). Identity now comes from the OpenPak stack: the client's BAAS id_token is minted by [`nx-baas`](../../../../nx-baas) (the Switch adapter over the `account` core), and this server proves its `nnex` claim and reads the friend graph through nx-baas's internal API. Not yet re-run end to end on OpenPak: an emulator profile pointed at nx-baas is the next step. The repository also keeps the research tools that got here: a metadata-only TCP/UDP observer and a sanitized TLS/HTTP2 probe.

This project is not affiliated with or endorsed by Nintendo, ConcernedApe, Stardew Valley, Chucklefish, or any original service provider. It contains no proprietary game code, assets, SDK material, binaries, certificates, keys, firmware, extracted files, credentials, or raw packet captures.

## Requirements

- Go 1.23 or newer

## NPLN server

```sh
go build ./cmd/npln          # NPLN_LISTEN=:21010 CERT_FILE=… KEY_FILE=… NX_INTERNAL_URL=http://127.0.0.1:20070 NX_INTERNAL_KEY=…
```

| Env | Meaning |
| --- | --- |
| `NPLN_LISTEN` | gRPC/TLS listener, default `:21010` (Stardew tenant, [ports.md](../../../../ports.md)); gamesync shares it |
| `NX_INTERNAL_URL` | nx-baas game/internal API, default `http://127.0.0.1:20070` |
| `NX_INTERNAL_KEY` | nx-baas's `NX_INTERNAL_KEY`, sent as `X-Internal-Key` |
| `NPLN_JWT_KEY` | persisted ES256 key for the access tokens |
| `NPLN_RELAY_*`, `NPLN_STUN_*`, `NPLN_TURN_*`, `NPLN_LATENCY_HOST` | session endpoints handed to the game |

Auth flow: the game sends its BAAS id_token; the `nnex` claim inside it is signed by nx-baas
(`NEX_SIGNING_KEY`). This server POSTs that claim to `/internal/switch/identity`, gets back
`{pid, baas_user_id, nickname, friends[]}`, and only then issues its own NPLN tokens. The
BAAS user id doubles as the NSA id in friend lists. `proto/` holds the NPLN schema and the bindings generated
from it (`proto/generate.sh`, see `proto/NOTICE.md`).

## Observer (research tool)

```sh
cp .env.example .env
set -a; . ./.env; set +a
go run ./cmd/observer
```

The safe defaults bind TCP and UDP listeners to `127.0.0.1:18080`. To generate synthetic observations:

```sh
printf 'synthetic-tcp' | nc 127.0.0.1 18080
printf 'synthetic-udp' | nc -u -w1 127.0.0.1 18080
```

Each JSON event records transport, event type, local listener address, and bounded byte count. It intentionally omits remote addresses and payload contents. The observer does not answer with guessed protocol data.

Configuration is through `STARDEW_OBSERVER_TCP_ADDR`, `STARDEW_OBSERVER_UDP_ADDR`, `STARDEW_OBSERVER_READ_TIMEOUT`, and `STARDEW_OBSERVER_MAX_BYTES`. Set an address to an empty value to disable that listener. Do not expose the observer outside a controlled research network.

## Local stack

OpenPak side: the `account` core (20000/20001), `nx-baas` with
`ACCOUNT_BACKEND=openpak` and `NX_INTERNAL_KEY` set (console TLS 21000, internal
API 20070), and this server on 21010 behind Traefik's SNI passthrough. The
emulator launchers (`scripts/launch-ryujinx.sh`, `scripts/launch-citron.sh`)
still wrap the Nextendo-era family launchers and BAAS routing; pointing a
profile at nx-baas is the open client-integration item.
[docs/local-stack.md](docs/local-stack.md) describes the previous (Nextendo)
local stack.

## Test

```sh
go test ./...
go vet ./...
```

## Research workflow

State a hypothesis and the smallest experiment in [handoff.md](handoff.md) before changing protocol behavior. Record observations immediately, label confidence, and use synthetic fixtures. Raw captures and sensitive or proprietary research inputs must remain outside the repository and be referenced through local configuration only.

The living [handoff.md](handoff.md) is part of the implementation and records current knowledge, experiments, failures, decisions, and next steps. For applying the same method to other titles and backends, see the [Online Compatibility Playbook](../../docs/shared/online-compatibility-playbook.md) and its [NPLN worked example](../../docs/shared/npln-online-gate-playbook.md). Protocol-specific modules will be added only when controlled observations justify them.

## Contribution and clean-room policy

Contributions must be independently written from lawful black-box observations, public documentation, or sanitized user-supplied facts. Do not submit decompiled or reconstructed proprietary source, extracted symbols or headers, SDK/game files, copyrighted assets, production certificates, keys, tokens, account/device/player identifiers, identifying addresses, memory dumps, or unsanitized captures.

If uncertain whether an artifact is safe to commit, keep it outside the repository and document only the interoperability behavior it demonstrates.

## License

AGPL-3.0-only — see `LICENSE`. `proto/NOTICE.md` covers the schema and the one Apache-2.0 file.
