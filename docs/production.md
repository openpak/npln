# Production: plugging stardew-nextendo into the live Nextendo Network

Target measured for this guide: one public arm64 VM (OCI Ampere A1, 4 OCPU / 24 GB, Ubuntu).
Files: `deploy/oci/`. Nothing here needs the local podman stack.

## How a production client reaches this server

1. **Tenant host.** The game resolves `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net` and connects
   to it on **443**. The Nextendo emulator's DNS layer maps every `*.nintendo.net` to
   `NEXTENDO_SERVER_IP` (IP only, port untouched — the per-host `host=ip:port` routes are a
   local-testing feature, not something official builds ship). A real console under Prelude gets
   the same via Atmosphère's hosts file. So the connection lands on the main Nextendo server's
   `:443`, where **sni-router** forwards the raw TLS stream by SNI. It needs one rule for the
   Stardew tenant — `deploy/sni-router-stardew.patch` — pointing at this box's `:18501`.
2. **Gamesync / session relay.** Not resolved via DNS: the console connects to the `host:port`
   this server puts in the GameSession (`NPLN_RELAY_HOST/PORT`), with SNI
   `gamesync.npln.nintendo.net`. Point it straight at this box's public IP and the same port; no
   router rule, no conflict with Splatoon 3 using the same SNI.
3. **STUN/TURN.** Addresses handed out by this server (`NPLN_STUN_HOST` / `NPLN_TURN_HOST`); the
   consoles talk to coturn directly. Pia omits REQUESTED-TRANSPORT in ALLOCATE, so use the
   patched build in `deploy/oci/Dockerfile.coturn`.
4. **NAT check (nncs2-*), account, BAAS, friends.** Shared production services; nothing to deploy.

## Server-to-server

- `NEXTENDO_INTERNAL_KEY` = the account server's off-network caller key, sent as `X-Internal-Key`.
  The account server proves the `nnex` claim (`POST /internal/pid-by-nex-token`) and serves the
  friend graph (`/internal/npln-friends`); both routes must be reachable through its public
  proxy. The account secret never lives on this box.

## Certificates

`CERT_FILE`/`KEY_FILE` must cover both `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net` and
`gamesync.npln.nintendo.net`, issued by the CA the Nextendo emulator builds and Prelude trust.
sni-router never opens the stream, so only this box holds the key.

## Firewall (OCI security list / NSG + ufw on the VM)

| proto | port | who |
| --- | --- | --- |
| TCP | 18501 | tenant gRPC (from sni-router) and gamesync (from consoles) |
| UDP | 3478 | STUN/TURN |
| UDP | 49152–65535 | TURN relay range |
| UDP | latency probe port | `NPLN_LATENCY_PORT` if enabled |

OCI VMs sit behind 1:1 NAT: coturn runs with `--external-ip=$PUBLIC_IP`.

## Bring-up

```sh
cd deploy/oci && cp example.env .env   # fill in
mkdir certs && cp npln.pem npln.key certs/
docker compose up -d --build           # arm64 builds natively; nothing is image-arch specific
docker compose logs -f stardew         # expect: "NPLN server listening on :18501"
```

Then upstream the sni-router rule, and set `BACKEND_STARDEW=<PUBLIC_IP>:18501` on the router.

## Verify

1. `openssl s_client -connect PUBLIC_IP:18501 -servername t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net`
   shows the tenant SAN.
2. From a production emulator: host a farm → log shows `IssuePrearrangedUserToken pid=…`,
   `[MM] Create`, the join shows `push … pl=9B` within a second (roster acknowledged), then
   `[MM] … left farm` / `host … left: farm … closed` on quit.
3. A console that vanishes without closing is dropped by keepalive in ~25 s (measured 19 s).

## Do not ship

- `NPLN_GS_DIAG=1` (logs station blobs and payloads).
- `NPLN_ALLOW_UNVERIFIED=1`.
- The self-signed local CA/certs from the podman stack.
