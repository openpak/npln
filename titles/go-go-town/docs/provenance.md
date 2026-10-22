# Evidence and outstanding checks

## 2026-10-12 — initial registration

- **Confirmed static evidence:** the local multiplayer sweep identifies
  application `010095001B12A000` and native NPLN Auth, Friends, Gamesync,
  Maintenance and matchmaking/GameSession strings. No literal tenant was
  recovered. Sanitized source:
  [workspace research sweep](../../../../../docs/research/research-switch-multiplayer-sweep-2026-10-12.md).
- **Hypothesis:** Stardew's service set is a useful starting point. No
  runtime call sequence, room configuration, properties or host/join behavior
  has been established for this title. Additional services and configuration
  differences must follow observation.
- **Hardware capture pending:** the owner reported that the installed game
  requires an update before online play. The first attempted run is not
  host/join evidence. Capture the tenant after updating and restarting.
- **Passive capture result:** a bounded ingress ClientHello capture was
  started for the attempt, then stopped when the update requirement was
  reported. No NPLN tenant hostname was found. This does not establish which
  check blocked online startup. Private artifact:
  `~/openpak/experiments/go-go-town-20261012/clienthello.pcap` on the server.
- **Implementation:** registered with no default tenant; the existing
  `stardewset.Tenant` guard requires a captured `NPLN_TENANT`. Ports 21126,
  21127 and 24550 are reserved. No production route or certificate is
  provisioned until the tenant is observed.
- **Validation:** `go test ./...`, `go build ./cmd/nplnd` and
  `git diff --check` pass. These establish server registration/build health,
  not Go-Go Town! multiplayer compatibility.

First decision rule: an attributed DNS query or TLS SNI establishes the
tenant. Subsequent authenticated RPCs establish the requested service set;
a two-device host/join/gameplay test establishes multiplayer compatibility.

## 2026-10-12 — current-version testing exception

The owner requested testing without downloading the update. Production aauth
confirmed installed version `00060000` and required version `00070000`.
nx-baas now has the temporary operator override
`NX_VERSION_GATE_OVERRIDES=010095001b12a000=00060000`. Live internal checks
accept the installed version, reject older versions, and retain normal
enforcement for another title. No game or console files were modified.
Remove the exception after updating; it does not establish NPLN compatibility.
The ingress tenant capture was restarted for a fresh Host attempt.

The owner still sees an update-required dialog when going online. The
server's internal check remains allowed, but no fresh Go-Go Town! aauth
request or tenant hostname was observed after the override. The console's
Pegasus recommended-version query received the existing catalog-missing
404. A full console restart was requested to test cached state; the cause
of the remaining dialog is unconfirmed. The hardware result supersedes any
assumption that the operator exception alone permits online entry.

## 2026-10-12 — tenant captured and server deployed

- **Hardware result:** the owner confirmed that a full Switch restart cleared
  the update-required dialog. Pressing Go Online then appeared to do nothing.
- **Confirmed tenant:** at 16:39:49 UTC, aauth logged application
  `010095001b12a000`, version `00060000`; the same attempt immediately opened
  TLS with SNI `t-05cbf648-lp1.lp1.t.npln.srv.nintendo.net` and sent NPLN Auth
  requests. The hostname is also in the bounded ClientHello capture. Other
  titles were active, so their tenant names were excluded from attribution.
- **Observed routing gap:** nx-baas issued an on-demand certificate for this
  unrouted SNI and received the NPLN Auth POSTs. It is not an NPLN gRPC host.
  This explains why the captured attempt did not reach the prepared services;
  it does not establish every reason the game might still fail online.
- **Deployment at 16:46 UTC:** the new `openpak-go-go-town` Quadlet hosts the
  captured tenant on 21126, health on 21127 and direct session on 24550. The
  exact SNI now routes through Traefik to this title. A title-specific leaf
  chains to the existing OpenPak CA and covers the tenant and Gamesync names.
  The title has its own persistent signing-key volume. The session TCP port
  is published, and its INPUT allowance is persisted.
- **Build:** isolated repository snapshot at
  `ed41a6ea7b6e3e8e8336134ad897262badddd798` plus only Go-Go Town!'s title and
  registry entry. All tests pass in that snapshot. The arm64 binary's SHA-256
  is `2535b642a4186fb444792b0d3658e2d9c05997a864cfa929a3ef3d232b6628f3`.
  The existing base image is pinned and the binary is mounted over `/app/app`;
  no other title's container was restarted or replaced.
- **External checks:** `/health` reports `go-go-town`; TLS chain/hostname
  verification and `GameSessionService/ListLatencyMeasurementServers` pass
  through public port 443 and direct port 24550. Both return resources under
  `tenants/t-05cbf648-lp1` and header `npln-grpc-type: Unary`.
- **Still pending:** fresh hardware online entry, requested service sequence,
  room config/properties, two-device host/join, gameplay, disconnect/rejoin.

Private artifacts: `scratch/go-go-town-20261012/` locally, and
`~/openpak/experiments/go-go-town-20261012/` on production. Deployment state,
routing backup and a guarded rollback script are retained there. The earlier
capture/registration notes above are historical, not the current deployment
status.

## 2026-10-12 16:51 UTC — hardware hosting confirmed

The Switch created a game session at 16:51:46 UTC. Its creation ticket
tracked to `SUCCEEDED`, advertising the direct endpoint on TCP 24550.
The client connected to Gamesync, obtained its session token, opened
`KeepUserSession`, subscribed to session documents and wrote player/session
state. `GetGameSession` polls continued at 16:51:56 and 16:52:06, with no
subsequent disconnect or handler error in the inspected window. The preceding
online attempt also activated Friends and queried available sessions.

This confirms hardware online entry and hosting on the initial service set.
A second player joining and actual multiplayer gameplay remain unverified.

## 2026-10-12 16:53 UTC — Switch and Harbor played together

The owner confirmed connecting from Harbor and playing together with the
Switch. The server trace records `JoinGameSession` at 16:53:33 UTC into the
session created by the Switch, a second Gamesync token/session stream, and
ICE allocation calls for both participants. No RPC errors or unimplemented
methods were recorded in the inspected test window. The two Gamesync
streams closed later at 16:54:08 and 16:54:22.

**Confirmed:** Switch hosting, Harbor joining, and two-player gameplay on
the initial Stardew service set with the captured tenant. The owner's
observation establishes gameplay; the server trace establishes join and
session activity. Reverse hosting, longer sessions and disconnect/rejoin
have not been validated. The temporary nx-baas version exception remains
active for the installed Switch version `00060000`.
