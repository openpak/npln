# stardew-nextendo

An early-stage, independent clean-room interoperability project investigating the network services needed by Stardew Valley multiplayer on Nintendo Switch.

Status (2026-09-02): the client authenticates against this project's own NPLN server (`cmd/npln` — `nn.npln.auth`, `nn.npln.friends`, `nn.npln.matchmaking.GameSessionService`, `nn.npln.gamesync`) on the local Nextendo stack and **hosts a farm end to end**, including the full gamesync session transport. The one open blocker is **joining**: the joiner's `QueryGameSessions` returns the friend's farm and the client receives it intact, but a client-side filter drops it before the Join list; the filter is decompiled and the remaining work is narrowed to confirming the exact display path (see `handoff.md`, Experiment 2026-09-02). Peer-to-peer gameplay is not yet reached. The repository also keeps the research tools that got here: a metadata-only TCP/UDP observer and a sanitized TLS/HTTP2 probe.

This project is not affiliated with or endorsed by Nintendo, ConcernedApe, Stardew Valley, Chucklefish, or any original service provider. It contains no proprietary game code, assets, SDK material, binaries, certificates, keys, firmware, extracted files, credentials, or raw packet captures.

## Requirements

- Go 1.23 or newer

## NPLN server

```sh
go build ./cmd/npln          # NPLN_LISTEN=:18501 CERT_FILE=… KEY_FILE=… NEXTENDO_ACCOUNT_URL=…
```

Deployed as the `stardew` container of the local stack; see [docs/local-stack.md](docs/local-stack.md).
`proto/` holds the generated NPLN protobuf bindings (see `proto/NOTICE.md`).

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

The full Nintendo-side infrastructure (account, NPLN, JWKS, NAT-check) runs
locally as podman containers; `scripts/launch-citron.sh` and
`scripts/launch-ryujinx.sh` (`--joiner` for the second player) start one of the
two shared host/joiner emulator profiles against it — thin wrappers over the
family launchers in `shared-docs/scripts/`. See
[docs/local-stack.md](docs/local-stack.md).

## Test

```sh
go test ./...
go vet ./...
```

## Research workflow

State a hypothesis and the smallest experiment in [handoff.md](handoff.md) before changing protocol behavior. Record observations immediately, label confidence, and use synthetic fixtures. Raw captures and sensitive or proprietary research inputs must remain outside the repository and be referenced through local configuration only.

The living [handoff.md](handoff.md) is part of the implementation and records current knowledge, experiments, failures, decisions, and next steps. For applying the same method to other titles and backends, see the [Online Compatibility Playbook](docs/online-compatibility-playbook.md) and its [NPLN worked example](docs/npln-online-gate-playbook.md). Protocol-specific modules will be added only when controlled observations justify them.

## Contribution and clean-room policy

Contributions must be independently written from lawful black-box observations, public documentation, or sanitized user-supplied facts. Do not submit decompiled or reconstructed proprietary source, extracted symbols or headers, SDK/game files, copyrighted assets, production certificates, keys, tokens, account/device/player identifiers, identifying addresses, memory dumps, or unsanitized captures.

If uncertain whether an artifact is safe to commit, keep it outside the repository and document only the interoperability behavior it demonstrates.

## License

Released under the [PolyForm Shield License 1.0.0](LICENSE.md), matching the other Nextendo game services. Preserve the required notice when distributing copies or changes.
