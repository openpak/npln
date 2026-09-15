> Historical (written for the Nextendo stack). Since 2026-09-07 identity comes from OpenPak's
> `nx-baas` over `NX_INTERNAL_URL`/`NX_INTERNAL_KEY`; see the README. Everything below about
> `NEXTENDO_ACCOUNT_URL`, `/internal/*` account routes and port 18501 is out of date.

# Local Nextendo stack (podman)

Everything Stardew needs from "Nintendo" runs locally as podman containers from
the [`nextendo-local` — the pre-port Nextendo local bundle, frozen outside this tree bundle. Both emulators are pointed
at it by the launch wrappers in `scripts/`; nothing reaches production.

## What runs

| Container (`nextendo-local_*`) | Host port | Role for Stardew |
|---|---|---|
| `account` | tcp 8099, and `https://account.tobagin.eu` via the local Traefik | identity: sign-in, the `nnex` token, verified-account gate |
| `stardew` (this repo, `cmd/npln`) | tcp 18501 | Stardew's NPLN gRPC/TLS: auth, friends, farm sessions — the tenant route points HERE |
| `npln` (splatoon-3 server) | tcp 18500 | reference NPLN server (Splatoon 3); used for Stardew only until 2026-09-01 |
| `baas-jwks` | tcp 18448 | JWK Set for the id_token `jku` fetch (`kid nextendo-baas-key-1`) |
| `nncs` (profile `nncs`, host network) | udp 10025/10125 (+33334 sinkhole) | NAT-check responder |
| `website` / `dashboard` | tcp 8081 / 8093 | browser sign-in (`/api` proxied) / monitoring |

Port 18443 belongs to another project's host-process `baas-jwks` (Among Us);
per-project instances coexist on different ports — leave it alone.

```sh
cd ~/REPOS/nextendo-local
podman-compose --profile nncs up -d          # start / recreate everything
podman-compose --profile nncs ps
podman logs -f nextendo-local_stardew_1      # [RPC]/[Auth]/[MM] lines + [MM][DIAG] full request dumps
podman-compose up -d --build stardew         # redeploy after a code change in this repo
podman logs -f nextendo-local_account_1
```

Health in one pass:

```sh
curl -sk https://127.0.0.1:18448/1.0.0/certificates | grep -o '"kid":"[^"]*"'
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8099/api/me        # 401 = up
echo | openssl s_client -connect 127.0.0.1:18500 -servername t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net 2>/dev/null | openssl x509 -noout -ext subjectAltName
ss -lun | grep -E ':(10025|10125) '
```

## The identity chain (why the emulator must sign in locally)

1. The emulator signs in to the local account server at `NEXTENDO_API` —
   citron: `https://account.tobagin.eu` (+ `NEXTENDO_API_TRUSTED_SUFFIX=tobagin.eu`;
   the name resolves to the local Traefik, LE cert, matches the stack's
   `NEXTENDO_BASE_URL`); Ryujinx: `http://127.0.0.1:8099` (its endpoint policy
   trusts only loopback or nextendo.network). Same container either way. The
   account's HMAC-signed `nnex` token is stored in the emulator profile.
2. The emulator's fabricated BAAS id_token carries that token in an `nnex`
   claim and is signed with `secrets/baas_signing_key.pem` — the same key
   `baas-jwks` publishes, so a local `jku` check also passes.
3. `npln` has the account server verify the `nnex` claim (`/internal/pid-by-nex-token`), which it signed with
   `account`, then requires the account to be verified (`/internal/npln-friends`).
   Local test account: `stardewhost`, pid 1800000005, verified
   (credentials outside the repo, `~/.local/share/stardew-nextendo-research/`).

An emulator signed in to a different deployment (production Nextendo) presents
a token the local `npln` cannot prove → `IssuePrearrangedUserToken` DENIED
(2321-5760). Not signed in at all → the client cancels pre-auth (2321-4992).

## Launch

```sh
scripts/launch-citron.sh            # portable profile ~/.local/share/stardew-nextendo-citron-profile
scripts/launch-ryujinx.sh           # shared HOST profile ~/ryujinx-instances/host, hosting Stardew
scripts/launch-ryujinx.sh --joiner  # shared JOINER profile ~/ryujinx-instances/joiner
scripts/launch-ryujinx.sh --menu    # main window only: sign in first, then load the game
```

Ryujinx now uses **two shared profiles for every game** (host + joiner), not a
dir per title — `scripts/launch-ryujinx.sh` is a thin wrapper over the family
launchers `shared-docs/ryujinx-{host,joiner}-launcher.sh`, baking in Stardew's
NSP and its route (`t-9f607adf-lp1…=127.0.0.1:18501`). See
[shared-docs/ryujinx-isolation.md](../../../docs/shared/ryujinx-isolation.md).

Both wrappers warn if a stack port is down, refuse to launch next to another
title's instance (`NEXTENDO_ALLOW_SHARED=1` to override after asking; the old
`STARDEW_ALLOW_SHARED=1` still works), and bake every `NEXTENDO_*` var in —
never launch the emulator bare for this project.

First run of each profile: sign in from the emulator's Nextendo menu with the
shared account — host profile → `OutboundHost` (pid 1800000003), joiner →
`OutboundJoiner` (pid 1800000004), the family-wide host/joiner identities (see
`shared-docs/conventions.md`). The link is stored per profile, so the
production link in `~/.config/citron` / `~/ryujinx/portable` is untouched.
NOTE: `OutboundHost`/`OutboundJoiner` are mutual friends AND verified on the
local account server (done 2026-09-02) — Stardew's auth gate passes for them.
(To verify a local account: `GET http://127.0.0.1:8099/api/verify?token=…`, an
HMAC token bound to id+email under the account server's `NEXTENDO_SECRET` — see the recipe in
`shared-docs/ryujinx-isolation.md`.)

Verify on the running pid before trusting a result:

```sh
pgrep -af "^/home/tobagin/REPOS/citron-nextendo/build"   # or ^/home/tobagin/ryujinx/Ryujinx
tr '\0' '\n' < /proc/<pid>/environ | grep NEXTENDO_ | sed 's/=.*//'
readlink -f /proc/<pid>/cwd                              # citron: the profile dir
```

Measured 2026-09-01: Ryujinx gets through auth and on to Stardew's own RPCs;
citron still cancels pre-HEADERS (2321-4992) on the same stack — use Ryujinx.

Success signal in the `stardew` log: `[Auth] IssuePrearrangedUserToken pid=1800000005 …`,
then `[Friends] …`, then `[MM]` lines. Any `[RPC] UNIMPLEMENTED …` line names
the next handler to write; `[MM][DIAG]` dumps show exactly what the game sent.

## Bundle fixes that were required (2026-09-01)

- `account` needs `NEXTENDO_DATA_DIR=/data` (the internal guard reads
  `internal_net.conf` there; the server's own `NEXTENDO_DATA` is a file path).
- `data/account/internal_net.conf` = `10.89.1.0/24 10.89.1.100`: allow the
  compose subnet, refuse the "published port" source. Host-published traffic
  reaches the container FROM ITS OWN IP (rootlessport), so the account container
  has a static IP (`ipv4_address: 10.89.1.100`) — otherwise the rule silently
  breaks on every recreate.
- `NEXTENDO_INTERNAL_KEY` must NOT be set on the account service: the
  npln-friends handler then demands an `X-Internal-Key` header the NPLN server
  never sends. Network rule authenticates callers instead.

Verify: `podman exec nextendo-local_npln_1 wget -qO- "http://account:8080/internal/npln-friends?pid=1800000005"`
→ JSON with `"verified":true`; from the host the same URL on :8099 must be 403.

## Gotchas

- citron profile shares `nand/` and `keys/` with `~/.local/share/citron` via
  symlinks (25 GB, and the installed 0.20.0 update the patches are scoped to).
  One citron at a time on that NAND.
- Ryujinx only honours `portable/` next to its binary; the two shared profiles
  (`~/ryujinx-instances/host` and `…/joiner`) are selected with
  `--root-data-dir`, which the launchers pass. One Ryujinx per profile dir;
  host + joiner run side by side.
- `NNCS_SERVER_IP` in `nextendo-local/.env` is only the address the responder
  reports back; loopback probes classify fine with it as is.
