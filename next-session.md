# Next session — servers/npln

Updated 2026-09-15.

The shared NPLN host and, under `titles/`, every OpenPak NPLN title in one image
(`ghcr.io/openpak/npln`, container picks a title via `NPLN_TITLE`). Three titles:
`stardew-valley` (live, console-verified), `splatoon-3` (early, never run against
retail), `super-mario-bros-wonder` (Stardew's service set under Wonder's tenant
`t-ba973ec6-lp1`; deployed, not console-verified — v1.2.1 pins its certificate, so
the console needs a client patch). Public repo, hosted release runners; five tags,
latest `v0.4.0` = HEAD (2026-09-13). The working tree has one modified file and
today's standardization files.

## Where things stand

- Recent commits: session endpoints for Stardew and Wonder (a created farm can
  actually be joined), `grpc-exp` offered (the protocol the console asks for),
  releases on hosted runners.
- Dirty: `titles/stardew-valley/handoff.md` gained session 16 (2026-09-13/14) plus
  a same-night addendum — an eden emulator bring-up session, no server changes:
  - Ported into eden and verified: per-title BAAS binding (id_token carries
    `nintendo.ai` = running title id), IPv6/dual-mode sockets, the Stardew game
    patches, sockopt set/get agreement, eventfd/deferred-poll fixes.
  - The remaining gate, located exactly: the title never sends its first NPLN RPC
    (`IssuePrearrangedUserToken`); the NPLN worker thread parks in a 1 s condvar
    loop (`WaitProcessWideKeyAtomic`, caller sdk+0xBF3B8) on a heap-dynamic
    condvar/mutex (per-run offset near main+0x85755d0) that is initialised and
    waiting for an event that never fires. No tenant/current or id_token in guest
    RAM at the freeze.
- Untracked: `CHANGELOG.md` (generated 2026-09-15), `docs/README.md`, `prds/`.
- Federation (workspace `prds/federation-prd.md`): Stardew's STUN/TURN come from
  the player's region node; the gamesync relay shares this process's room state
  and stays home. F0–F2 built 2026-09-09, awaiting deployment.

## Open questions

- What should initialise the condvar the NPLN worker waits on — which init step,
  running where, fires the event? (eden client-side; see session 16 addendum.)

## Next steps

1. Continue the eden gate: xref the SDK init path in the Ghidra `sdfull` project
   for the object behind main+0x85755d0, or diff the SDK's init inputs against
   Ryujinx (acc/nifm/glue answers), or port citron's deferred-poll machinery.
2. Commit the handoff session-16 update and the `CHANGELOG.md` / `docs/` / `prds/`
   standardization files.
3. Deploy-side: federation node bring-up when the operator side is ready.

## Pointers

- `README.md` — title table (ports, tenants, status), the one-schema rule in
  `proto/`, `cmd/nplnd` shape.
- `titles/stardew-valley/handoff.md` — session 16 + addendum at the top; the full
  session history (1–15, Nextendo-era included) below it.
- `titles/splatoon-3/`, `titles/super-mario-bros-wonder/` — per-title docs.
- Workspace `prds/federation-prd.md` (NPLN row: ICE set per session, gamesync home).
