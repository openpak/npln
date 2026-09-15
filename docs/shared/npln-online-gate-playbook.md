# NPLN Online-Gate Playbook (worked example)

> Trimmed copy for this repository; the canonical document lives at
> `Openpak/servers/shared-docs/npln-online-gate-playbook.md` and governs. Last synchronised 2026-09-15.

This is the NPLN/gRPC-specific worked example of the general method — see
[online-compatibility-playbook.md](online-compatibility-playbook.md) for the
backend-agnostic layer model, tool inventory, and rules. Everything below is
real, executed procedure (Stardew Valley and Splatoon 3, 2026-08/09).

How to take a Switch title running under an OpenPak test emulator (the
citron fork **or the ryujinx fork**, `emulators/citron` /
`emulators/ryujinx`) whose online mode fails, diagnose **which layer** is
failing, and build the compatibility pieces in the right order. Written for
the assistant/agent doing the work on any OpenPak game project. The method
is title-agnostic for NPLN-era games (gRPC-over-TLS inside the game,
`nn.npln.*` services).

**Emulator status for NPLN:** both emulators run NPLN titles online against
production (Splatoon 3 works under citron AND Ryujinx). Ryujinx additionally
has the declarative `nextendo_routes.env` route table (historical name),
which makes it the low-friction client for local-server testing; citron
reaches the same result with its env hooks
(`NEXTENDO_S3_DEBUG_PROXY_IP`/`_PORT` — legacy per-title hook names still in
the fork source). When one emulator fails where it "always worked", suspect
the session (region selection, account state, single-instance hygiene)
before suspecting the stack — and differentially retest on the other
emulator.

Ground rules, non-negotiable:

- Clean-room: proprietary binaries, dumps, certificates, keys, tokens, raw
  captures stay **outside every repository**. Only independently written code
  and sanitized conclusions enter.
- Never guess protocol behavior from another title. Splatoon 3's RPC handlers
  are Splatoon's; its *infrastructure* (server stack, patch mechanism, probe
  pattern) is reusable.
- Follow the **Emulator Launch Protocol**
  (`emulator-launch-protocol.md`, this folder) before killing or restarting any
  citron instance; same courtesy for Ryujinx instances
  (`pgrep -af "^/home/tobagin/ryujinx/Ryujinx"`, never broad-`pkill`).

---

## Phase 0 — Baseline and symptom capture

1. Launch the title with the OpenPak redirects active and reproduce the
   online failure once.
   - citron: `enable_openpak=true` in the profile, or `OPENPAK_ENABLE=1`.
   - Ryujinx: `OPENPAK_SERVER_IP` / `OPENPAK_NAT_IP` env vars (unset vars
     fall back to loopback — set them or the Nintendo hosts all die).
2. Mine the emulator log for the ordered flow:
   - citron: the persona profile's `log/citron_log.txt` — beware: every
      citron start on the same profile truncates it; run **one instance per
      profile** and don't boot other titles mid-test. `DNS resolve` lines →
      the ordered host list (nncs1/nncs2 NAT checks, `g*` platform hosts,
      `t-*` NPLN tenants, baas/account hosts); `Connect fd=` lines → what
      actually dialed; ACC (`acc.cpp`) lines → account IPC health.
    - Ryujinx: `Logs/Ryujinx_<ver>_<date>.log` in the profile dir — one file
      per launch. `Trying to resolve:` lines, `[Nextendo] Connect target …`
      lines (historical log tags), ACC lines. No truncation between launches.
3. Record the exact error code. For NPLN titles, **`2321-4992` after a
   successful TLS handshake** is the family symptom of the client aborting
   its own first RPC — historically documented as certificate pinning, now
   refined: it is a client-side gRPC **UNAVAILABLE** (module 321, description
   = `(64 + grpc_status) * 64`; UNAVAILABLE→4992, FAILED_PRECONDITION→3072,
   INTERNAL→4608, UNKNOWN→384, UNAUTHENTICATED→5760 — the conversion table
   lives in the title's binary). The cancel source must still be localized:
   TLS-layer verify callback, post-handshake peer check, or the SDK's login
   orchestration tearing the channel down itself.

Deliverable: a sanitized flow summary in the project's `next-session.md` —
hosts, order, transports, error codes. No payloads.

## Phase 1 — Chain validation (X.509)

The game's TLS is **in-game** (statically linked OpenSSL speaking over raw
BSD sockets) — emulator SSL trust settings do not apply. Nintendo's chain
won't validate against a replacement server, so:

1. Dump the title's active ExeFS and convert the active-update `main` NSO to
   ELF (`~/REPOS/nx2elf`). Record the build ID — **all patch work is scoped to
   one exact build**.
2. Locate the chain-validation function (`X509_verify_cert` — findable via its
   OpenSSL diagnostic strings: `ssl_verify_cert_chain`,
   `certificate verify failed`) and confirm the offset by control-flow shape.
3. Add the guarded, build-scoped patch:
   - citron: C++ entry in `src/core/loader/nso.cpp` (title ID + module + build
     ID + original-byte fingerprint → e.g. `mov w0, #1; ret`). Offsets are
     program-image offsets (== ELF VAs, 0-based).
    - Ryujinx: IPS32 table in `src/Ryujinx.HLE/HOS/<Title>Patches.cs` (see
      `StardewPatches.cs`), keyed by build ID. **Offsets are flat-NSO file
      offsets = memory offset + 0x100** (`MemPatch.Patch` subtracts the
      protected offset). Verify original bytes in the flat NSO dump first.

Address mapping cheat sheet (Stardew 0.20.0 example): ELF VAs are 0-based;
flat NSO text file offset = VA + 0x100; Ryujinx IPS offset = VA + 0x100;
citron `nso.cpp` offset = VA; Ghidra address = VA + 0x100000 (PIE imported at
base 0x100000).

After the chain patch the client should complete the TLS handshake and emit
its HTTP/2 preface. If it still dies **before** the preface, the gate is
elsewhere (DNS, TCP, NNCS) — do not proceed to later phases.

## Phase 2 — Redirect NPLN hosts to a local terminator

- **Ryujinx (preferred): declarative route table.** Create
  `nextendo_routes.env` in the base dir (portable: next to
  `nextendo_account.txt`):

  ```ini
  t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net=127.0.0.1:18500
  ```

  First matching line wins; the `:port` part rewrites the game's outgoing
  port at connect time (both the kept-address and the lost-address/0.0.0.0
  gRPC cases). Reloaded on mtime change. Format details:
  `ryujinx-isolation.md` §2 in this folder (the per-fork route-table doc went
  with the pre-port repo).

- **citron: env-gated tap.** `NEXTENDO_S3_DEBUG_PROXY_IP=<ip>` (DNS: any host
  containing `npln`) + `NEXTENDO_S3_DEBUG_PROXY_PORT=<port>` (bsd.cpp port
  remap, 443 → your port). Everything else (NNCS NAT checks, account hosts)
  keeps its normal redirect, so you change exactly one variable at a time.

## Phase 3 — The sanitized probe

`servers/stardew-valley` ships `cmd/probe` + `internal/probe`: a loopback TLS
terminator that presents an in-memory certificate covering
`*.lp1.t.npln.srv.nintendo.net`, speaks minimal HTTP/2, and logs **metadata
only**: TLS version/cipher/ALPN, ClientHello SNI, frame types/flags/stream
IDs, the gRPC pseudo-headers (`:method`, `:path`, `:scheme`, `:authority`),
other header **names** with value lengths, byte counts, and timing. Bodies,
non-pseudo header values, keys, and addresses are never logged, and there are
end-to-end redaction tests proving it.

Run it on the tapped port and drive the game. Interpret the frames:

- `client preface ok` + client SETTINGS + ACK + then `RST_STREAM(REFUSED_STREAM)`
  and close ⇒ the client **cancelled its first RPC before sending it** — the
  gate is client-local (pin/account/orchestration), not transport.
- No preface / TLS failure ⇒ transport or trust problem — fix Phase 1 first.

Check direction carefully: frames logged by the probe's read loop are what the
CLIENT sent. citron's `SendImpl/RecvImpl/Shutdown` DIAG lines and Ryujinx's
bsd logs let you attribute the teardown to the guest or the server.

## Phase 4 — Eliminate server-side doubt

Before hunting client-side causes, prove the transport is complete by putting
the **full reference NPLN server** on the tapped port: `servers/splatoon-3`
is a complete `nn.npln.*` gRPC server (auth, friends, gamesync,
matchmaking) that accepts emulator-fabricated BAAS id_tokens.

```sh
go build -o /tmp/npln-server ~/REPOS/Openpak/servers/splatoon-3
# run with CERT_FILE/KEY_FILE covering *.lp1.t.npln.srv.nintendo.net,
# NPLN_LISTEN set to the tapped port, and the account-service URL wired up
# as in servers/splatoon-3/README.md (NEXTENDO_ACCOUNT_URL at the time of
# the original run)
```

Its `[stats] CONN` lines tell you instantly whether a peer reached the gRPC
layer and whether any RPC arrived. If a title still cancels pre-HEADERS
against a complete server, the cause is client-side. Period.

Also rule out the cheap confounders in one pass: account linked (local
account server, sign-in via the emulator's OpenPak menu,
`OPENPAK_API=http://127.0.0.1:8099` for citron), certificate identity shape
(exact tenant CN **and** wildcard variants — name-mimicry may or may not
matter for a given SDK version; test it before assuming), and
single-instance hygiene.

Measured dead ends (do not re-walk blind): serving the exact-tenant-CN
certificate; server SETTINGS byte-equivalent to production; a valid
trailers-only gRPC response; the full reference server with a linked account.
All produced the identical pre-HEADERS cancel.

## Phase 5 — The client-side hunt (pin, identity, orchestration)

NPLN SDKs pin the expected server certificate **in addition to** chain
validation, and their login orchestration can abort the auth RPC before it is
ever serialized. Worked Stardew findings to reuse:

1. **The verify-callback flag.** The SDK's SSL-context setup reads a never-set
   "certificate accepted" byte (`LDRB W10,[X21,#0x38]`) and selects between an
   always-OK verify callback (flag set) and a real-check callback that only
   tolerates expiry-class errors (X509_V_ERR 9/10/11 — console clock skew).
   No code ever stores the flag: it is a build-disabled option. Forcing the
   read to 1 selects the always-OK path — the Splatoon 3 fix, and the same
   edit works for Stardew (see the patches above).
2. **Error-code localization.** The grpc-status→`nn::Result` table in the
   title's binary maps UNAVAILABLE→2321-4992 etc. (Stardew: 17-entry u32 table
   at rodata VA 0x98CA1AC). Knowing 2321-4992 = client-side UNAVAILABLE tells
   you the failure is a channel/subchannel teardown, not a server rejection.
3. **Socket-level attribution.** The emulator's socket logs show who closes
   first. citron `ShutdownImpl fd=N how=2` called by the guest milliseconds
   after the preface send ⇒ the SDK cancelled the call and tore the channel
   down itself ⇒ look at the orchestration, not the wire.
4. **Runtime diagnostics in guest RAM.** At the failure dialog, scan rw
   mappings: static diagnostic strings that appear as RUNTIME copies were
   formatted/executed (" (Auth RPC result)" materialized 18x; "Transport
   closed"/"Socket closed" completed the message). Static-only strings mark
   code that never ran.
5. **Memory forensics for tokens.** At the dialog, find the fabricated
   id_token (anchor `"aud":"` ) and compare its claims against what ACC IPC
   served (`GetAccountId` = linked uid). citron/`OPENPAK_BAAS_SUB` can align
   `sub`; `aud` is hardcoded to S3's client id in both emulators — a
   per-title `aud` check is a candidate gate if identity alignment fails.
6. **Full Ghidra headless analysis** for the deeper hunt. Operational rules
   (disk-backed project, no dot-paths, hours of runtime, pyghidra
   function-creation, base 0x100000) are in the general playbook, section 5.
7. **Gateway/pre-gRPC identity.** NPLN titles may consult REST identity
   gateways before/alongside the tenant gRPC (S3:
   `gw.hac.lp1.vermillion.srv.nintendo.net` device init + accounts/config with
   `online_license`, `val.hac.lp1.penne.srv.nintendo.net` login tickets;
   Stardew resolves its `g2122d301.lp1.p.srv.nintendo.net` but never dials
   it). A route-table line (`gw…=ip[:port]`) plus the reference server's
   vermillion/penne REST handlers cover this experimentally.

Verify each candidate by differential: change one thing (cert CN exact vs
wildcard, patch on/off, account linked/unlinked, emulator A/B, token `sub`
aligned/random), retest, log.

## Operational gotchas (all paid for)

- `/tmp` is tmpfs: it dies on reboot and cannot hold multi-GB analysis
  databases or logs. Disk-backed paths for anything you keep.
- Ghidra: disk-backed project, no dot-prefixed paths, base-0x100000 mapping,
  manual function creation in undiscovered regions, commit the pyghidra
  transaction (stale `.lock` otherwise). Full details: general playbook §5.
- Never broad-`pkill` citron or Ryujinx patterns (self-match + other titles'
  sessions); follow the launch protocols.
- One emulator instance per data dir; concurrent instances truncate shared
  logs (citron) and clobber config.
- Ryujinx: force X11 for the game window (`env -u WAYLAND_DISPLAY
  XDG_SESSION_TYPE=x11 GDK_BACKEND=x11`) or Vulkan can BadMatch; its first
  NPLN resolution is deliberately held until the JIT burst calms
  (`MaybeDelayNplnInit`) — citron needs `NEXTENDO_NPLN_DELAY_MS` for the same
  race, and its delay blocks every socket IPC (single-threaded bsdsocket).
- Rate limits exist on the local account server's registration endpoint;
  create test accounts well before you need them.

## Recording results

Every experiment goes into the project's `next-session.md`: hypothesis,
method, decision rule, observation (with confidence labels), artifacts note.
If a run fails, that is a result — record what was eliminated. Raw logs,
dumps, and certificates stay outside the repository, referenced only by
local path.
