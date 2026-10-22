# Go-Go Town!

Switch application `010095001B12A000`, selected by `NPLN_TITLE=go-go-town` in
the shared `nplnd` binary. Tenant TLS port **21126**, plain `/health` port
**21127**, direct session/Gamesync TCP port **24550**.

Deployed as `openpak-go-go-town` on 2026-10-12 under the hardware-captured
tenant `tenants/t-05cbf648-lp1`. Public TLS/gRPC checks pass on 443 and 24550;
**Switch host → Harbor join and two-player gameplay are confirmed** by the
owner's test and server join trace. Auth, Friends, Presence,
GameSessionService and Gamesync use Stardew's service set without extra
title options. Other
services remain unimplemented until a client trace establishes the need.
Static Maintenance strings alone do not establish a runtime call.

The owner confirmed that restarting the Switch cleared the update prompt
after the temporary version exception. The following online attempt
authenticated application version `00060000` and contacted the captured
tenant, which was then unrouted. After deployment and a fresh game launch,
the Switch hosted successfully and Harbor joined the same session.

The observed flow includes session creation/`SUCCEEDED` tracking, Gamesync
token issuance and `KeepUserSession` on both devices, `JoinGameSession`, ICE
allocation and document synchronization. No RPC errors or `UNIMPLEMENTED`
calls appeared during the inspected test. Reverse hosting and rejoin after
disconnect remain unverified.

## Capture and first run

1. Use an accepted game version. For the owner's current test, nx-baas has a
   temporary minimum override allowing installed version `00060000`; remove
   it after updating. Fully close/relaunch the game, enter online co-op and
   attempt Host. Record the installed version, UTC time and any numeric error.
   Use the console's existing OpenPak redirect setup.
2. Observe the DNS query or TLS ClientHello SNI at the OpenPak ingress. The
   expected hostname shape is `t-XXXXXXXX-lp1.lp1.t.npln.srv.nintendo.net`;
   derive `NPLN_TENANT=tenants/t-XXXXXXXX-lp1` from the observed name. Attribute
   it to this attempt, especially when other NPLN games are active. An
   update-required screen may stop before the tenant is resolved.
3. Add an exact Traefik `HostSNI` passthrough route for that hostname to the
   new title container's port 21126. Use an OpenPak-CA certificate covering
   the observed hostname. Keep raw captures and private keys outside Git.
4. Give the container its own persistent signing-key volume. Configure
   `NX_INTERNAL_URL` / `NX_INTERNAL_KEY` for OpenPak identity, and the existing
   STUN/TURN endpoints and TURN secret. Set `NPLN_RELAY_HOST` to the reachable
   server address, `NPLN_RELAY_PORT=24550` and publish TCP 24550. Open that port
   in the host/provider firewall: joiners dial it directly.
5. Watch `[TLS]`, `[RPC]`, `[RPC>]`, `[RPC<]` and `UNIMPLEMENTED` lines during a
   fresh Host attempt. Normal logs retain message outlines. If room config
   and properties need value-level inspection, use `NPLN_TRACE_BODIES=1` only
   for an isolated research run and keep those logs private. Record sanitized
   config/property conclusions before choosing title-specific options.
6. Test a second account on another device: discovery/invitation, join,
   gameplay, disconnect and rejoin. An authenticating client or a successful
   creation ticket alone does not establish working multiplayer.

After capture, run from `platforms/switch/games/npln` with operator-supplied values:

```sh
export NPLN_TITLE=go-go-town
export NPLN_TENANT=tenants/t-05cbf648-lp1 # captured default; override if needed
export CERT_FILE=/path/to/go-go-town.pem KEY_FILE=/path/to/go-go-town.key
export NPLN_JWT_KEY=/path/to/state/npln_jwt_es256.key
export NX_INTERNAL_URL=http://openpak-nx-baas:20070
# Supply NX_INTERNAL_KEY and NPLN_TURN_SECRET through the private environment.
export NPLN_STUN_HOST="$PUBLIC_HOST" NPLN_TURN_HOST="$PUBLIC_HOST"
export NPLN_RELAY_HOST="$PUBLIC_HOST" NPLN_RELAY_PORT=24550
go run ./cmd/nplnd
```

`PUBLIC_HOST` must be the server address reachable by both devices.
`NPLN_LISTEN`, `HEALTH_LISTEN` and `NPLN_SESSION_LISTEN` can override the defaults.

See [provenance](docs/provenance.md) for evidence and outstanding checks.

Production uses an isolated arm64 binary built from repository HEAD plus
only this title and its registry entry, mounted read-only over `/app/app` in
the new Quadlet. The base image is pinned; this is not a published release.
Updating the shared image alone will not replace the mounted binary. The
title has its own signing-key volume and OpenPak-CA leaf; TCP 24550 is
published and its host firewall rule is persisted.

Private deployment state and the guarded rollback script are on production
under `~/openpak/experiments/go-go-town-20261012/`. To remove this deployment:

```sh
python3 ~/openpak/experiments/go-go-town-20261012/rollback.py
```

Rollback retains the title's state and removes only its route, service and
firewall rule. It does not remove the separate nx-baas version exception.
