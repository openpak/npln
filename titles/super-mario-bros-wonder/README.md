# super-mario-bros-wonder

Super Mario Bros. Wonder (`010015100B514000`) on the shared NPLN host. It serves Stardew
Valley's verified NPLN service set (auth, friends, game sessions, gamesync) under Wonder's
tenant; nothing Wonder-specific has been observed yet, so every unknown method logs
`UNIMPLEMENTED` and that log is the next work list.

| Fact | Value | Source |
| --- | --- | --- |
| NPLN tenant | `tenants/t-ba973ec6-lp1`, host `t-ba973ec6-lp1.lp1.t.npln.srv.nintendo.net` | Kinnay's NintendoClients wiki, "NPLN Servers" (read 2026-09-11); cross-checked against Splatoon 3's `dce9377b`, which the same table lists and this project had observed |
| Tenant port | 21014 (health 21015) | [ports.md](../../../../ports.md) |
| Client patch | **needed** | v1.2.1 (build id `FF773E90972D544EB79406EAA65396D53C43EFB9`) pins the NPLN certificate and rejects two peer names; an unpatched console never reaches OpenPak |

Deployed 2026-09-11 as `openpak-wonder` (tenant cert `deploy/keys/wonder.pem` from the OpenPak
CA, Traefik `HostSNI` passthrough). Not console-verified. `NPLN_TENANT` overrides the tenant if a
capture ever disagrees with the public list.

```sh
NPLN_TITLE=super-mario-bros-wonder NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ../../cmd/nplnd
```
