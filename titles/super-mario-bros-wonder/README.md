# super-mario-bros-wonder

Super Mario Bros. Wonder (`010015100B514000`) on the shared NPLN host. **Scaffold.** It serves
Stardew Valley's verified NPLN service set (auth, friends, game sessions, gamesync) under
Wonder's tenant, and nothing Wonder-specific has been observed yet.

## What has to be found out before a console can use it

| Fact | Status | Where it comes from |
| --- | --- | --- |
| NPLN tenant id (`tenants/t-XXXXXXXX-lp1`) | **unknown**; `NPLN_TENANT` is required and the server refuses to start without it | the `npln-tenant-id` metadata on any RPC, or the `t-…` label of the SNI the console dials on 443 |
| Which NPLN services the title actually calls | unknown | `UNIMPLEMENTED` log lines once a console talks to it |
| Client patch | needed | v1.2.1 (build id `FF773E90972D544EB79406EAA65396D53C43EFB9`) pins the NPLN certificate and rejects two peer names; without a patch the console never reaches OpenPak. Public patched emulators exist for this build (evidence only, not input) |

## Run

```sh
NPLN_TITLE=super-mario-bros-wonder NPLN_TENANT=tenants/t-XXXXXXXX-lp1 NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ../../cmd/nplnd
```

Once the tenant is known: a cert for `t-XXXXXXXX-lp1.lp1.t.npln.srv.nintendo.net`, a Traefik
`HostSNI` passthrough router to `openpak-wonder:21014`, and a row in `ports.md`.
