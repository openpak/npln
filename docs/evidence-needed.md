# Evidence needed

In order. Each item says what it answers, how to gather it with tooling that already exists, and
the decision rule that tells you whether the run succeeded. Nothing here needs a console: items
1–5 run on this workstation against an emulator. Items 6–7 need two clients.

Before touching an emulator, read
[`emulator-launch-protocol.md`](../shared-docs/emulator-launch-protocol.md): the machine runs
shared host/joiner personas across every title, the game NSP path on the command line is the only
title identifier, and killing the wrong instance takes someone else's session with it. **Ask
first** if any other title is running.

Raw logs, dumps, certificates and captures stay outside this repository. Only sanitized
conclusions come back in, into `docs/design.md`.

---

## 1. Boot-to-lobby call order against a tapped tenant — the one that unblocks everything

**Answers:** design.md unknowns 1, 2, 4 and most of 6 in a single run. Which `nn.npln.*` services
Splatoon 3 calls, in what order, whether the metadata carries `npln-tenant-id`, and whether
Vermillion/Penne are dialled before the tenant.

**How.** Run this repository's `cmd/npln` on a local port with a certificate covering
`t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net`, point the game's tenant at it, boot to the lobby
once, and read the log. The server answers Auth and prints
`[RPC] UNIMPLEMENTED <method>` for everything else — that list *is* the answer.

```sh
go build -o /tmp/s3-npln ./cmd/npln
CERT_FILE=… KEY_FILE=… NPLN_LISTEN=127.0.0.1:21012 HEALTH_LISTEN=127.0.0.1:21013 \
  NX_INTERNAL_URL=http://127.0.0.1:20070 NX_INTERNAL_KEY=… NPLN_JWT_KEY=/some/persistent/path \
  /tmp/s3-npln
```

Redirect the client, one variable at a time:

- **Ryujinx (preferred).** Add a line to `nextendo_routes.env` in the instance's base dir:
  `t-dce9377b-lp1.lp1.t.npln.srv.nintendo.net=127.0.0.1:21012`. The `:port` part rewrites the
  outgoing port at connect time; first matching line wins; reloaded on mtime change. Launch with
  `shared-docs/scripts/launch-ryujinx-host.sh <NSP>` and `NEXTENDO_ROUTE=` set the same way
  `stardew-valley/scripts/launch-ryujinx.sh` does it — copy that wrapper, change the NSP and the
  route, do not write a new launcher.
- **citron.** `NEXTENDO_S3_DEBUG_PROXY_IP=127.0.0.1` (its DNS hook matches any host containing
  `npln`) plus `NEXTENDO_S3_DEBUG_PROXY_PORT=21012` (`bsd.cpp` remaps 443 → that port). Both are
  unset by default, so exactly one thing changes. `stardew-valley/scripts/launch-citron.sh` is
  the model, including the `qt-config.ini` pinning — citron prefers the profile *setting* over
  `NEXTENDO_SERVER_IP`, and a fresh profile silently points at production.

**Decision rule.** Success = at least one `[RPC]` line. `[CONN] begin (h2 established)` with zero
RPCs means the client cancelled before sending anything: that is the `2321-4992` family symptom
and item 2 below is the blocker, not the server. No `[TLS] ClientHello` at all means the redirect
did not take — check the DNS hook before anything else.

**Record:** the ordered method list, the metadata keys present (names only), and the DNS/connect
order from the emulator log. No payloads, no tokens.

## 2. The certificate-acceptance patch, confirmed for the current build

**Answers:** whether item 1 can produce RPCs at all.

The NPLN SDK pins in addition to chain validation. The family's fix is the verify-callback flag
(`LDRB W10,[X21,#0x38]` → `MOV W10,#1`); the recorded Splatoon 3 offset is S3-main `0x157B20`, but
**all patch work is scoped to one exact build id** and that offset is from an older build. Dump
the active-update `main` NSO, convert with `~/REPOS/nx2elf`, confirm the offset by control-flow
shape and original bytes, and record the build id next to it. Address mapping: ELF VA is 0-based;
flat-NSO/Ryujinx-IPS offset = VA + `0x100`; citron `nso.cpp` offset = VA; Ghidra address =
VA + `0x100000`.

**Decision rule.** With the patch on, the client must emit its HTTP/2 preface and a HEADERS frame.
Preface but no HEADERS = the gate is elsewhere (item 3). Patch on/off is the differential — run
both, do not assume.

## 3. Sanitized probe run, if item 1 produces no RPCs

**Answers:** whether the client is cancelling its own first call.

`stardew-valley/cmd/probe` is a loopback TLS terminator with an in-memory certificate covering
`*.lp1.t.npln.srv.nintendo.net` — it is deliberately wildcard, so Splatoon 3's tenant passes
without changing anything. It logs metadata only: TLS version/cipher/ALPN, SNI, frame
types/flags/stream ids, gRPC pseudo-headers, other header *names* with value lengths. Run it on
the tapped port instead of `cmd/npln`.

**Decision rule.** `client preface ok` + SETTINGS + ACK + `RST_STREAM(REFUSED_STREAM)` + close =
client-local gate (pin, account state, or the SDK's login orchestration), and the server is
irrelevant — go back to item 2 and then to the playbook's Phase 5. Frames logged by the probe's
read loop are what the *client* sent; check direction before concluding.

## 4. Identity: does the account gate pass?

**Answers:** design.md unknowns 3 and 5.

Item 1 already exercises this — `[Auth] IssuePrearrangedUserToken pid=…` means nx-baas recognised
the player. If it logs `identity not provable`, the chain to check, in order: the emulator's
fabricated BAAS id_token carries an `nnex` claim (nx-baas must be the one that minted it), the
`NX_INTERNAL_KEY` matches nx-baas's, and the persona is linked. Note that both emulators hardcode
`aud` to Splatoon 3's client id — for this title that is the *right* value, unlike the Stardew
case where it was a suspect.

Then the `app_id` differential: mint with `AppID = 0100c2500fc20000`, reach the lobby; change the
constant to a wrong value, retest. If the client behaves identically, the claim is not checked and
design.md unknown 3 closes as "not enforced".

**Decision rule.** One variable per run. Record which value the client accepted.

## 5. NSO membership / online licence

**Answers:** design.md unknown 5.

Splatoon 3 requires NSO for online play, and OpenPak's nx-baas already serves the Vermillion
`accounts/config` (`online_license`) and NSO membership surfaces for the console link. If item 1
shows the game dialling `gw.hac.lp1.vermillion.srv.nintendo.net`, capture what it asks for and
whether it proceeds; if it never dials it, the gate is either in the token (`nso_restricted`) or
client-local. Differential: flip `nso_restricted` in the minted token and retest.

## 6. Two clients, one match

**Answers:** design.md unknown 6 — the session/P2P topology, and whether `Matchmaker` or
`GameSessionService` (or both) carry it.

Only after items 1–5. Host and joiner personas side by side
(`shared-docs/scripts/launch-{ryujinx,citron}-{host,joiner}.sh`), both pointed at the same server,
both trying to enter the same online mode. The `UNIMPLEMENTED` log from *two* clients in a lobby
attempt is a different and much more informative list than one client's.

**Decision rule.** Record the method list and the streaming ones especially — a server-streaming
`TrackMatchmakingTicket` or a bidirectional keepalive that the client opens and holds tells you
the shape of the matchmaking loop without decoding a single payload.

## 7. NAT/TURN and the game transport

Deferred. Ports `22210–22219` are reserved but nothing should be built there until item 6 shows
what the session layer actually asks for. The family already runs coturn in the local stack; the
Stardew experience (`upcsid`-keyed station tables, a ~15 s `TurnJob::WaitServerConfig` delay) is
prior art to read, not a design to copy.

---

## Not needed

- Reading `~/REPOS/splatoon-3`. It is a different, PolyForm Shield repository. If a question feels
  answerable only by opening it, that question is on this list instead.
- SplatNet 3 / `api.lp1.av5ja.srv.nintendo.net`. Public documentation exists (`s3s`, `imink`), but
  it is the smartphone-app surface, not the tenant, and no evidence yet suggests the game refuses
  online play without it.
