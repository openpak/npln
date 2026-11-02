# Notes: npln

## Titles

| Title | `NPLN_TITLE` | Tenant port |
| --- | --- | --- |
| [`titles/core-keeper`](../titles/core-keeper) | `core-keeper` | 21130 (health 21131), session 24570 |
| [`titles/plateup`](../titles/plateup) | `plateup` | 21128 (health 21129), session 24560 |
| [`titles/stardew-valley`](../titles/stardew-valley) | `stardew-valley` | 21010 (health 21011) |
| [`titles/splatoon-3`](../titles/splatoon-3) | `splatoon-3` | 21012 (health 21013), session 22210 |
| [`titles/super-mario-bros-wonder`](../titles/super-mario-bros-wonder) | `super-mario-bros-wonder` | 21014 (health 21015), session 22220 |
| [`titles/dinkum`](../titles/dinkum) | `dinkum` | 21122 (health 21123), session 22230 |
| [`titles/human-fall-flat`](../titles/human-fall-flat) | `human-fall-flat` | 21124 (health 21125), session 22240 |
| [`titles/go-go-town`](../titles/go-go-town) | `go-go-town` | 21126 (health 21127), session 24550 |
| [`titles/metal-gear-solid-peace-walker`](../titles/metal-gear-solid-peace-walker) | `metal-gear-solid-peace-walker` | 21114 (health 21115), session 24530 |

## Design

- `proto/` is the one NPLN schema both titles share (Stardew's tree plus Splatoon's `toyohr`
  services). One copy is not tidiness: two copies of the same `.proto` file names in one
  binary would collide in the protobuf registry at start-up. Regenerate with
  `proto/generate.sh`.
- `titles/<game>/npln` is that title's service implementations. Stardew and Splatoon still
  carry their own auth, friends and gamesync code. Titles not yet observed run Stardew's set
  under their own tenant through `titles/stardewset` (Wonder, Dinkum, Human Fall Flat).

- Each title keeps its own README, docs and tools (`titles/stardew-valley/cmd/observer`,
  `cmd/probe`; `titles/splatoon-3/cmd/genrotation`) in its directory.

## Title IDs

Pokémon Scarlet and Violet: the module serves Scarlet's Title ID `0100A3D008C5C000`. Violet's ID (`01008F6008C5E000`) appears only in the module comment (`titles/pokemon-scarlet-violet/title.go`), so the title entry is not a Violet service.
