# Ports — npln (trimmed)

> Trimmed copy for this repository; the canonical document lives at
> `Openpak/ports.md` and governs. Last synchronised 2026-09-15.

One block per concern, nothing below 20000, nothing at or above 27000 (Photon-Nextendo and
Steam live there). Every service reads its listeners from `<SVC>_HTTP_ADDR`, `<SVC>_GRPC_ADDR`,
`<SVC>_METRICS_ADDR`; the values below are the defaults and the local-run convention. Each
service owns a block of ten: +0 HTTP, +1 gRPC, +2 metrics/pprof, +3..+9 spare.

## NPLN tenants

| Port | Service | Hostnames (SNI) |
| --- | --- | --- |
| 21011 | Stardew server health (plain HTTP `/health`, not console-facing) | — |
| 21012 | Splatoon 3 NPLN tenant | `t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net` |
| 21013 | Splatoon 3 server health (plain HTTP `/health`, not console-facing) | — |
| 21014 | Super Mario Bros. Wonder NPLN tenant (deployed 2026-09-11, not console-verified) | `t-ba973ec6-lp1.lp1.t.npln.srv.nintendo.net` |
| 21015 | Super Mario Bros. Wonder server health (plain HTTP `/health`) | — |
| 21122 | Dinkum NPLN tenant (deployed 2026-09-18 as `openpak-dinkum`, not console-verified; tenant from a Ryujinx boot; health 21123, session 22230) | `t-35b7d576-lp1.lp1.t.npln.srv.nintendo.net` |
| 21124 | Human Fall Flat NPLN tenant (deployed 2026-09-18 as `openpak-human-fall-flat`, not console-verified; tenant from a Ryujinx boot; health 21125, session 22240) | `t-5cbc0f31-lp1.lp1.t.npln.srv.nintendo.net` |

Other services' rows live in the canonical ports.md.
