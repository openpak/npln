# Evidence needed

Rewritten after the clean-room policy changed to allow reading the NextendoNetwork server for
facts. Most of what this file used to ask for is now answered in
[`provenance.md`](provenance.md) and did not need a capture after all. What is left is the part no
document can supply: whether OpenPak's own stack satisfies this client.

Before touching an emulator, read
[`emulator-launch-protocol.md`](../shared-docs/emulator-launch-protocol.md) — shared host/joiner
personas, the game NSP path is the only title identifier, and **ask first** if another title is
running. Raw logs, dumps and captures stay outside this repository.

## 1. The certificate-acceptance patch, for the current build

**Blocks everything else.** Without it the client completes TLS and abandons its own call before
sending a HEADERS frame, and no server-side work is observable.

The SDK reads a never-set "certificate accepted" byte and selects an always-OK verify callback when
it is set. An offset exists for an older Splatoon 3 build (`LDRB W10,[X21,#0x38]`, S3-main
`0x157B20`); **all patch work is scoped to one exact build id**, so it must be re-derived for the
build actually installed. Dump the active-update `main` NSO, convert with `~/REPOS/nx2elf`, confirm
by control-flow shape and original bytes, record the build id beside the offset.

Address mapping: ELF VA is 0-based; flat-NSO / Ryujinx IPS offset = VA + `0x100`; citron
`nso.cpp` offset = VA; Ghidra address = VA + `0x100000`.

**Decision rule.** Patch on/off is the differential — run both. With it on, the client must emit
its HTTP/2 preface *and* a HEADERS frame.

## 2. First contact against our auth

**Answers:** whether nx-baas's identity projection is one this client accepts.

Run `cmd/npln` with a certificate covering the tenant host, point the game at it, boot once.

```sh
go build -o /tmp/s3-npln ./cmd/npln
CERT_FILE=… KEY_FILE=… NPLN_LISTEN=127.0.0.1:21012 HEALTH_LISTEN=127.0.0.1:21013 \
  NX_INTERNAL_URL=http://127.0.0.1:20070 NX_INTERNAL_KEY=… NPLN_JWT_KEY=/persistent/path \
  /tmp/s3-npln
```

Redirect, one variable at a time:

- **Ryujinx (preferred).** A line in `nextendo_routes.env`:
  `t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net=127.0.0.1:21012`. First match wins, the `:port`
  rewrites the outgoing port, reloaded on mtime change. Copy `stardew-valley/scripts/launch-ryujinx.sh`
  and change the NSP and route — do not write a new launcher.
- **citron.** `NEXTENDO_S3_DEBUG_PROXY_IP=127.0.0.1` (its DNS hook matches any host containing
  `npln`) and `NEXTENDO_S3_DEBUG_PROXY_PORT=21012`. `stardew-valley/scripts/launch-citron.sh` is
  the model, including the `qt-config.ini` pinning — a fresh citron profile silently prefers the
  production IPs over the environment.

**Decision rule.** `[Auth] IssuePrearrangedUserToken pid=…` means the identity chain works end to
end. `identity not provable` → check, in order, that the id_token's `nnex` claim was minted by the
*same* nx-baas, that `NX_INTERNAL_KEY` matches, and that the persona is linked. `[CONN] begin` with
zero RPCs → go back to item 1. No `[TLS] ClientHello` → the redirect did not take.

**Record:** the ordered `UNIMPLEMENTED` method list. That is the real work queue for this
repository, in the order the game wants it.

## 3. The nx-baas REST bootstrap, on hardware

**Answers:** whether the four defects in `design.md` are the only ones.

The pre-gRPC Vermillion/Penne chain is nx-baas's, it is currently wrong for this title in four
identified ways, and it is console-facing — so this is a scoped nx-baas change validated against
hardware, not a change to make blind. The console link works today and must keep working.

**Decision rule.** Fix one endpoint, re-link a console, confirm the link still works, then boot
Splatoon 3 and see how much further it gets. The failure signatures are distinctive: an endless
bootstrap loop that never reaches the tenant points at the device id or the `vphyms` 404; reaching
the lobby and stopping there with `2321-4992` points at `online_license`.

## 4. Schedules: capture or generate

**Answers:** the largest unbudgeted piece of the title.

The rotation cannot be ported — the only working implementation replays recorded bytes we do not
have and could not redistribute. Two options, and this needs a decision before it is discovered
late:

- **Capture.** One console, one boot, record `toyohr.Schedule` responses. Legally ours to hold, not
  ours to ship, and it still needs the timestamp-shifting machinery to stay current.
- **Generate.** Model the rotation format and synthesise a schedule. More work up front, no
  capture dependency, and the only option that survives being open source.

**Decision rule.** Either way the acceptance test is the same: the game shows a current rotation in
the lobby instead of reporting that stage information is unavailable. Note that inconsistent
timestamps across a schedule set are not rejected but *crash* the game (`2162-0001`), so this needs
a real test rather than eyeballing.

## 5. Two clients in a match

Only after 1–4, and after friends, presence, matchmaking and a session host exist. A regular battle
needs **eight** players — the game checks the roster itself and refuses below the mode's size — so
a two-client test can validate the matchmaking call sequence and the ICE allocation, but it cannot
start a real battle. Plan for that rather than reading the refusal as a bug.

---

## Not needed

- A capture to establish the tenant, the token claim shape, the call order, or the error codes.
  Those are settled — see `provenance.md`.
- SplatNet 3 (`api.lp1.av5ja.srv.nintendo.net`). The smartphone-app GraphQL surface, publicly
  documented by `s3s`/`imink`, not on the tenant, and not needed to reach a match.
