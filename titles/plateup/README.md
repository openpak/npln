# PlateUp!

Switch application `01003F901A83E000`, selected with `NPLN_TITLE=plateup` in
the shared `nplnd` binary. Tenant TLS **21128**, HTTP `/health` **21129**,
direct session/Gamesync TCP **24560**. Deployed 2026-10-12 under captured
tenant **`t-a8f1d737-lp1`**. Public TLS/gRPC probes pass. At 17:34:52–55 UTC,
the Switch authenticated, created a lobby and connected to Gamesync, writing
session data. At 17:35:36–37, Harbor joined and both devices requested ICE;
the owner confirmed the Switch works online and Harbor can connect to it.
Sustained gameplay, reverse hosting and reconnect remain separately unverified.

The installed Switch update 1.5.2 (content version 1572864) uses **NPLN
matchmaking/Gamesync plus QUIC peer connections**, and NPLN leaderboards.
Photon is another platform's transport in this build, not a service the
Switch must reach after NPLN. See [online flow](docs/online-flow.md).

## Run

From `platforms/switch/games/npln`, with operator-supplied values:

```sh
export NPLN_TITLE=plateup
export NPLN_TENANT=tenants/t-a8f1d737-lp1 # optional override of captured default
export CERT_FILE=/path/to/plateup.pem KEY_FILE=/path/to/plateup.key
export NPLN_JWT_KEY=/path/to/state/plateup_jwt_es256.key
export NX_INTERNAL_URL=http://openpak-nx-baas:20070
# Supply NX_INTERNAL_KEY and NPLN_TURN_SECRET in a private environment.
export NPLN_STUN_HOST="$PUBLIC_HOST" NPLN_TURN_HOST="$PUBLIC_HOST"
export NPLN_RELAY_HOST="$PUBLIC_HOST" NPLN_RELAY_PORT=24560
go run ./cmd/nplnd
```

`PUBLIC_HOST` must be reachable by both devices. Use a separate signing-key
file/volume for this title. The TURN secret must match the existing coturn
service. `NPLN_LISTEN`, `HEALTH_LISTEN` and `NPLN_SESSION_LISTEN` override
the default listener addresses.

## First client run

1. Retry Online Play/Host after deployment. The initial Switch attempt at
   17:30:06 UTC reached nx-baas through its catch-all route and got 404 for
   NPLN Auth. The exact tenant now routes to the title's NPLN host.
2. Verify TLS and authentication through
   `t-a8f1d737-lp1.lp1.t.npln.srv.nintendo.net`. The direct Gamesync endpoint
   is published on TCP 24560, with the existing account and STUN/TURN services.
3. Record the Auth and GameSession/Gamesync calls, matchmaking config,
   session properties and ICE/QUIC behavior. The native constructor uses
   `DefaultMatchmakingConfig`; do not infer the complete wire request from
   that constant alone. Keep raw traces and credentials outside Git.
4. Verify host, invitation/join code, joining from a second device, gameplay,
   leave and rejoin. A successful creation ticket is only the lobby gate.
5. Implement LeaderboardService from its observed requests separately.
   The initial service set does not register that service. Establish whether
   a leaderboard failure prevents co-op before making it an online gate.

The SDK's maintenance strings do not establish a runtime maintenance call.
No Photon AppId mapping or Photon server change is required for the measured
Switch selection. Other platforms and older Switch updates need their own
verification.
