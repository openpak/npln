# npln

The shared NPLN host and, under `titles/`, every OpenPak NPLN title. One module, one image
(`ghcr.io/openpak/npln`), one tag per release; a container runs one title, chosen by
`NPLN_TITLE`. NPLN is the gRPC-over-TLS transport family Nintendo Switch titles such as
Stardew Valley and Splatoon 3 use instead of NEX.

| Title | `NPLN_TITLE` | Tenant port | Status |
| --- | --- | --- | --- |
| [`titles/stardew-valley`](titles/stardew-valley) | `stardew-valley` | 21010 (health 21011) | live, console-verified |
| [`titles/splatoon-3`](titles/splatoon-3) | `splatoon-3` | 21012 (health 21013), session 22210 | early, not run against retail |

## Shape

- `cmd/nplnd` owns what every title needs identically: the TLS listener on the tenant port
  (`NPLN_LISTEN`, `CERT_FILE`, `KEY_FILE`), the plain `/health` (`HEALTH_LISTEN`) and the
  `NPLN_TURN_SECRET` check. It then hands the listener to the title's `Run`.
- `proto/` is the one NPLN schema both titles share (Stardew's tree plus Splatoon's `toyohr`
  services). One copy is not tidiness: two copies of the same `.proto` file names in one
  binary would collide in the protobuf registry at start-up. Regenerate with
  `proto/generate.sh`.
- `titles/<game>/npln` is that title's service implementations. Stardew and Splatoon still
  carry their own auth, friends and gamesync code; sharing those is the next step once a third
  NPLN title needs them, not before.
- Each title keeps its own README, docs and tools (`titles/stardew-valley/cmd/observer`,
  `cmd/probe`; `titles/splatoon-3/cmd/genrotation`) in its directory.

## Run

```sh
NPLN_TITLE=stardew-valley NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ./cmd/nplnd
```

Every environment variable a title read before is unchanged; `NPLN_TITLE` is the only new one.
The former `stardew-valley` and `splatoon-3` repositories are archived; their history is merged
here.
