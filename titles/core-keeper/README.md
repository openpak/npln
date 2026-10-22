# Core Keeper

Switch application `010070201D1BA000`, measured with update 1.3.0.3 (content
version 2424832). `NPLN_TITLE=core-keeper` selects its initial shared NPLN host.

| Setting | Value |
| --- | --- |
| Tenant | `tenants/t-b0546eeb-lp1` |
| Tenant hostname | `t-b0546eeb-lp1.lp1.t.npln.srv.nintendo.net` |
| Tenant / health ports | 21130 / 21131 |
| Direct session / Gamesync port | 24570 TCP |
| Initial services | Auth, Friends, Presence, GameSession, Gamesync |

The tenant was captured from Harbor's **Start Online Game** flow on 2026-10-12,
using an existing linked OpenPak console profile. The title preserves incoming
session config names through `ConfigProperties`; its native session manager
uses `ckMatchmakingConfigs` rather than Stardew's configuration.

The production service is `openpak-core-keeper`. Its dedicated tenant route,
OpenPak CA certificate, persistent signing-key volume and published session port
are installed. Health and public TLS/h2 probes pass. This deployment mounts a
locally built arm64 binary at `/app/app`; it is not a published image release.
The deployment directory is
`/home/ubuntu/openpak/experiments/core-keeper-20261012/` on the OCI host.

Physical-console inspection selects `PlayFabPartyNetworking`, including with
cross-play disabled. The owner can enter an online world, but Harbor discovery
and invitation delivery remain unresolved. Successful NPLN authentication and
friend retrieval do not establish that an online Party session was created.

See [the provenance record](docs/provenance.md) for evidence and limitations.
The authentication service specified in
[PlayFab requirements](docs/playfab-requirements.md) was installed at 17:56 UTC
on 2026-10-12 as the separate `openpak-playfab` container. Its source and
[deployment/check evidence](../../../playfab/docs/deployment.md) are under
`servers/playfab`. nx-baas forwards this title's existing TLS host to it.
The initial login/region failures are resolved: login and entity exchange have
returned 200, and the optional Party QoS service now advertises a real public
UDP 3075 beacon. The fresh physical-console test at 22:33 UTC received QoS 200
and successfully probed that beacon. Guarded inspection at 22:37 UTC found no
current session in `SwitchPlayFabPartySessionManager`; no invite submission or
Party allocation request was observed in the associated server logs. Party
allocation/relay remains unimplemented, and end-to-end hosting, discovery,
invitation delivery and joining are not yet verified.
