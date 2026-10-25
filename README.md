# NPLN

OpenPak's NPLN Server.

| | |
| --- | --- |
| Network | Nintendo Switch |
| Protocol | NPLN (gRPC over TLS) |
| Image | `ghcr.io/openpak/npln` |
| Licence | AGPL-3.0-only |

## Titles

One module, one image: a container runs one title, chosen by `NPLN_TITLE`. Add a title by adding a
module under `titles/` and one line to `titles/titles.go`.

| Game | Title ID | Module |
| --- | --- | --- |
| [Bayonetta 3](https://openpak.org/titles#t-66707) | `01004A4010FEA000` | `titles/bayonetta-3` |
| [Core Keeper](https://openpak.org/titles#t-72356) | `010070201D1BA000` | `titles/core-keeper` |
| [Dinkum](https://openpak.org/titles#t-53688) | `0100A5A020D5E000` | `titles/dinkum` |
| [Endless Ocean Luminous](https://openpak.org/titles) | `010067B017588000` | `titles/endless-ocean-luminous` |
| [Go-Go Town!](https://openpak.org/titles#t-72358) | `010095001B12A000` | `titles/go-go-town` |
| [Hamster Playground](https://openpak.org/titles) | `010035901701A000` | `titles/hamster-playground` |
| [Human Fall Flat](https://openpak.org/titles) | `01000CA004DCA000` | `titles/human-fall-flat` |
| [METAL GEAR SOLID: Peace Walker](https://openpak.org/titles) | `0100C6F01C4F8000` | `titles/metal-gear-solid-peace-walker` |
| [Monster Hunter Rise](https://openpak.org/titles) | `0100B04011742000` | `titles/monster-hunter-rise` |
| [PlateUp!](https://openpak.org/titles#t-72373) | `01003F901A83E000` | `titles/plateup` |
| [Pokémon Legends: Arceus](https://openpak.org/titles) | `01001F5010DFA000` | `titles/pokemon-legends-arceus` |
| [Pokémon Scarlet and Violet](https://openpak.org/titles) | `0100A3D008C5C000` | `titles/pokemon-scarlet-violet` |
| [Splatoon 3](https://openpak.org/titles#t-3) | `0100C2500FC20000` | `titles/splatoon-3` |
| [Stardew Valley](https://openpak.org/titles#t-2) | `0100E65002BB8000` | `titles/stardew-valley` |
| [Super Mario Bros. Wonder](https://openpak.org/titles#t-8691) | `010015100B514000` | `titles/super-mario-bros-wonder` |
| [Super Mario Party Jamboree](https://openpak.org/titles#t-93) | `0100965017338000` | `titles/super-mario-party-jamboree` |
| [Terraria](https://openpak.org/titles#t-142) | `0100E46006708000` | `titles/terraria` |
| [Wavecade](https://openpak.org/titles) | `0100F7A017F4C000` | `titles/wavecade` |

## What it serves

| Endpoint | What | Served by |
| --- | --- | --- |
| `NPLN_LISTEN` (per title) | NPLN gRPC services over TLS | `cmd/nplnd`, then the title's `Run` |
| `HEALTH_LISTEN` (per title) | Plain `/health` | `cmd/nplnd` |

## Run

```sh
NPLN_TITLE=stardew-valley NPLN_TURN_SECRET=… CERT_FILE=… KEY_FILE=… go run ./cmd/nplnd
```

## Docs

- [Protocol](docs/protocol.md)
- [Evidence](docs/evidence.md)
- [Deploy](docs/deploy.md)
- [Notes](docs/notes.md)
- [Docs index](docs/README.md)

Live compatibility status: [openpak.org/titles](https://openpak.org/titles)

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
