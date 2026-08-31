# stardew-nextendo Handoff

## Project Objective

Build an independently written, open-source compatibility service for the network-facing behavior needed by Stardew Valley multiplayer on Nintendo Switch. This is clean-room interoperability work only; proprietary code, assets, binaries, credentials, keys, certificates, raw captures, and player-sensitive data must remain outside this repository.

## Current Status

- 2026-08-31: The workspace was found completely empty and was not a Git repository.
- 2026-08-31: A minimal Go research scaffold and protocol-neutral TCP/UDP observer were implemented and exercised with synthetic traffic.
- 2026-08-31: An empty Git repository was initialized after the scaffold was created; no commit was made.
- 2026-08-31: Stardew's startup reaches NNCS NAT checks and the NPLN tenant over redirected networking. DNS, TCP, SNI, TLS server-flight behavior, and the certificate-trust failure were measured.
- 2026-08-31: A clean-room, exact-build Citron compatibility patch crossed the TLS trust boundary. Stardew now completes the client handshake and sends its first encrypted application record to Nextendo, then immediately closes. The first HTTP/2/gRPC method remains unknown.
- 2026-08-31: A controlled loopback TLS-termination probe proved the client's first encrypted record is a pre-HEADERS cancel flight (preface/SETTINGS/ACK/RST/WINDOW_UPDATE), not an HTTP/2 request. The client cancels its first RPC before transmission regardless of server behavior; the blocker is client-local, upstream of the wire protocol.
- No farm hosting, discovery, joining, peer connectivity, or gameplay traffic is confirmed yet.

## Architecture

The initial component is a dependency-free Go process with independent TCP and UDP listeners and a shared structured-event sink. Socket handling is separate from event serialization. It reads a bounded amount of traffic, logs byte counts but not contents or remote addresses, and sends no guessed response. Service modules will be introduced only when observations justify them.

## Environment

- Workspace: `/home/tobagin/REPOS/stardew-nextendo`
- Session date: 2026-08-31
- Nearby Nextendo-related repositories exist and are being reviewed only for independently written, license-compatible infrastructure concepts. No code has been copied.

## Discoveries

### Repository baseline

- **CONFIRMED:** The project directory initially contained no files or directories.
- **CONFIRMED:** The project directory initially had no Git metadata.
- **CONFIRMED:** Neighboring workspaces include several Nextendo-related projects; their relevance and licensing have not yet been assessed.
- **CONFIRMED:** `../fallguys-nextendo` is an MIT-licensed, independently described clean-room Go project using a metadata-only HTTP observer as its first milestone.
- **CONFIRMED:** `../outbound-nextendo` documents a different game's Photon-specific behavior. It explicitly warns against assuming Nintendo NEX; none of its Photon findings are evidence about Stardew Valley.
- **CONFIRMED:** No Stardew-specific infrastructure or observation was found in the empty project directory.

## Protocol Notes

- **CONFIRMED:** Stardew uses the Nintendo NPLN control plane over TLS on TCP 443 and negotiates HTTP/2 when independently probed.
- **CONFIRMED:** NNCS NAT-check traffic uses UDP ports 33334 and 10025 with 16-byte datagrams in the observed startup flow.
- **CONFIRMED:** Stardew's embedded NPLN client negotiates TLS 1.2 with `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256` and ALPN `h2`, sends client SETTINGS including a Nintendo-custom ephemeral id `0xfe03`, ACKs server SETTINGS, and then cancels its first RPC before transmitting HEADERS — regardless of server SETTINGS content or application responses (see Experiment 2026-08-31-7). The application protocol is expected to be HTTP/2/gRPC from public NPLN documentation, but no Stardew HEADERS/service/method has yet been sent to any server.
- **CONFIRMED:** No account/BAAS/token host resolution occurs in the observed flow; the NPLN tenant is contacted directly after NNCS NAT checks.

## Hostname / Service Inventory

- `nncs1-lp1.n.n.srv.nintendo.net` — **CONFIRMED**, NNCS/NAT-related lookup.
- `nncs2-lp1.n.n.srv.nintendo.net` — **CONFIRMED**, NNCS/NAT-related lookup.
- `g2122d301.lp1.p.srv.nintendo.net` — **CONFIRMED**, game/P2P-monitoring identifier; exact role remains unknown.
- `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net` — **CONFIRMED**, Stardew NPLN tenant endpoint over TCP 443/TLS.

## Endpoint Inventory

No HTTP/2 path or gRPC method is confirmed. TLS ApplicationData is now observed, making sanitized TLS termination/method logging the next experiment.

## Authentication Flow

Unknown. No authentication material is present or required for the initial passive experiment.

## Startup Flow

Unknown.

## Multiplayer Menu Flow

Unknown.

## Farm Hosting Flow

Unknown.

## Farm Discovery Flow

Unknown.

## Farm Join Flow

Unknown.

## Invitation Flow

Unknown.

## Peer Connectivity

Whether gameplay traffic is peer-to-peer is an **UNCONFIRMED HYPOTHESIS**.

## NAT Traversal

Unknown. Standard STUN/TURN behavior must not be assumed.

## Session Lifecycle

Unknown.

## Disconnect / Reconnect Behavior

Unknown.

## Experiments

### Experiment 2026-08-31-9: Static identification of the certificate-acceptance gate and build-scoped pin-bypass patch

Hypothesis:

The deterministic pre-HEADERS cancel (2321-4992) is the NPLN SDK's own certificate-acceptance check, the same mechanism the Splatoon 3 patch bypasses (`LDRB W10,[X21,#0x38]` at S3-main 0x157B20 → `MOV W10,#1`). Stardew's main embeds the identical SDK code; locating it offline yields the Stardew patch offset.

Method (offline analysis; completed Ghidra project + raw disassembly):

1. Full Ghidra auto-analysis of main.elf completed and SAVED on disk (project `/home/tobagin/ghidra-projects/sdfull`, log `/home/tobagin/ghidra_sdfull2.log`, ~7.7 GB). Address mapping resolved empirically: the reconstructed ELF is PIE with 0-based VAs; Ghidra imports it at image base 0x100000 (Ghidra address = VA + 0x100000; the earlier session's "-0x160" note was a misreading). In the flat NSO, text file offset = VA + 0x100 (NSO header retained); the analysis-time "ELF VA" and "program_image offset" of the existing X509 patch are the same number.
2. Searched the text segment for the exact S3 idiom `LDRB W10,[X21,#0x38]` (`3940E2AA`): 2 hits, exactly one in the SDK/TLS region (VA 0x782F5D0) and one in low rtld/libc code. No `STRB [xN,#0x38]` exists anywhere in text — the flag is never set; it is a build-disabled option, permanently 0.
3. Disassembled the containing function (VA 0x782f2e0, called from 0x7822f3c/0x7825410; not auto-discovered by Ghidra, boundaries found via BL-target scan): it is the SDK's SSL-context setup. It creates an SSL_CTX, calls `SSL_CTX_set_verify` (identified as the tiny setter at VA 0x78e5310 storing mode+callback at ctx+352/+400, mode=1 = SSL_VERIFY_PEER) with `callback = flag ? always_ok_stub(0x782fac0: mov w0,#1; ret) : real_check_stub(0x782fad0)`. The real-check stub is the classic OpenSSL verify callback `int cb(int preverify_ok, X509_STORE_CTX*)`: preverify_ok → 1; otherwise returns 1 only when `X509_STORE_CTX_get_error(ctx)` ∈ {9,10,11} (cert-not-yet-valid/expired/notBefore-field — console clock-skew tolerances), else 0. So flag=0 (always, in this build) installs a callback that rejects any chain-erroring certificate other than expiry-class.
4. Identified the error-code machinery: the grpc-status→nn::Result converter near VA 0x77702c0-0x7770460 contains a 17-entry u32 table at VA 0x98CA1AC mapping gRPC status codes to NPLN results with module 321: OK→2000-0, UNKNOWN→2321-384, FAILED_PRECONDITION→2321-3072, INTERNAL→2321-4608, UNAVAILABLE→**2321-4992**, UNAUTHENTICATED→2321-5760, etc. The observed dialog code 2321-4992 is therefore a client-side gRPC UNAVAILABLE — the call failed without a server response (channel/subchannel security setup), matching the probe's cancelled-stream signature and the splatoon-3 server repository's independent notes on the same code.
5. Derived the patch: VA 0x782F5D0, original `AA E2 40 39` (`ldrb w10,[x21,#0x38]`) → `2A 00 80 52` (`mov w10,#1`), exactly mirroring kCertificateBypass semantics. Added as a second guarded, build-scoped entry (title 0100E65002BB8000, module main, build E7F845093E8CBC68DACF011CCB620D6667B5A20B, exact original-byte fingerprint) in the external citron loader patch block alongside the X509 patch; citron rebuilt successfully.

Observation:

- **CONFIRMED (static):** flag read at 0x782F5D0 is the unique S3-analogue in the binary; the never-set flag (no `strb` anywhere) proves the always-accept path is dead code in this build, exactly as in S3.
- **CONFIRMED (static):** 2321-4992 = gRPC UNAVAILABLE via the converter table — the cancel is produced client-side before/without a server response.
- **PENDING RUNTIME:** retest with both patches (X509 + flag bypass) through the loopback probe; expected outcome is the first client HEADERS at the server. Not yet run at handoff time (another title's citron instance was on the machine; Launch Protocol requires asking first).
- **OPEN LEADS if the flag bypass does not clear the gate:** (a) the S3 server work documented a pre-gRPC REST "vermillion/penne" device-identity step (devices/initialize, vermillion-device-id, accounts/config with `online_license.is_available`) whose failure also surfaces as 2321-4992 with zero gRPC HEADERS; Stardew resolves `g2122d301.lp1.p.srv.nintendo.net` (the `*.lp1.p.srv.nintendo.net` family) but never connects to it — worth tapping separately; (b) grpc TSI post-handshake peer checks beyond OpenSSL verification (peer property/fingerprint compares) can be decompiled next (the setup function's second setter call at VA 0x78e4dd0 stores an unrelated parser helper, not a pin callback).

Decision rule:

- HEADERS observed at the probe ⇒ the certificate-acceptance flag was the last client-local gate; proceed to RPC cataloging (which services/methods Stardew requests first) and start implementing responses.
- Identical pre-HEADERS cancel ⇒ the gate is further upstream (vermillion device identity or TSI peer checks); follow the open leads above, one variable per run.

Artifacts: patch metadata and offsets recorded here are independently derived interoperability conclusions; original-byte fingerprints live only in the external GPL citron worktree. No proprietary bytes entered this repository.

### Experiment 2026-08-31-8: Client-side gate isolation (account link, local account, id_token claims)

Hypothesis:

The universal pre-HEADERS cancel (both titles, any server) is caused by a client-side credential gate rather than transport behavior; a linked Nextendo account and/or correct per-game BAAS id_token claims will clear it.

Method:

1. Production control: fresh citron without the tap envs; Splatoon 3 lobby attempt against production Nextendo.
2. Created a dedicated local account (`stardewhost`, pid 1800000005) on the local nextendo-account (127.0.0.1:8099) via `/api/register`, verified via the dev-mode logged link. Credentials stored outside any repository under `~/.local/share/stardew-nextendo-research/`. (`stardewjoin` pending the server's registration rate limit.)
3. Relaunched the Stardew citron instance with `NEXTENDO_API=http://127.0.0.1:8099` (citron's `BaseUrl()` accepts loopback for the account API) plus the tap envs; user signed in via **NexTendo → Sign in** (OAuth code emitted for pid 1800000005, confirmed in the account server log).
4. Offline, authorized local analysis of the already-exported main module (external artifacts under `~/.local/share/citron/research/stardew-0.20.0/`): strings/JSON scan of the reconstructed ELF.

Observation:

- **CONFIRMED:** Splatoon 3 fails against production (2321-4992) from a freshly first-booted, unlinked profile — the earlier "S3 works" assumption did not hold for this environment at that time.
- **CONFIRMED:** After linking (`stardewhost`), citron serves `GetAccountId` (linked Network ID) and issues a signed BAAS id_token (1405 bytes) via `EnsureIdTokenCacheAsync`/`LoadIdTokenCache` immediately before the attempt — and Stardew STILL cancels pre-HEADERS (RST REFUSED_STREAM, identical signature, 12:56:15). Account link alone does not clear the gate.
- **CONFIRMED (offline):** Stardew's main module embeds the complete standard NPLN method surface — `nn.npln.auth.v1.Auth` (IssueToken, IssuePrearrangedUserToken, RefreshAnonymousUserToken, …), `nn.npln.friends.v1.Friends`, `nn.npln.gamesync.v1.Gamesync`, `nn.npln.matchmaking.v1.{Matchmaker,GameSessionService}` — plus the tenant hostname format `%s.%s%s.t.npln.srv.nintendo.net:443` and a `_npln:/npln_config.json` config-file reference (file lives in game data, not in main).
- **CONFIRMED (offline/cross-repo):** citron and Ryujinx-Nextendo both hardcode the fabricated BAAS id_token `aud` to `ed9e2f05d286f7b8` (with iss `e0d67c509fb203858ebcb2fe3f88c2aa.baas.nintendo.com`, random `sub`, game-agnostic claims). No emulator presents a per-game audience. Ryujinx-Nextendo independently marks Stardew `online-broken`.
- **CONFIRMED (probe, SNI):** Stardew offers ALPN `[grpc-exp, h2]` (grpc-exp first), same as Splatoon 3.

Hypothesis (current):

Stardew's NPLN SDK validates the fabricated id_token locally before its first auth RPC and rejects it on game-specific claims — most plausibly `aud` (BAAS client id), which is hardcoded to another title's value. The expected value likely lives in the game's `npln_config.json` (game data, not main). This would explain: identical pre-HEADERS aborts for both titles pre-link, Stardew failing post-link, S3-works/Stardew-broken across both emulators, and Ryujinx-Nextendo's `online-broken` marking.

- **CONFIRMED ( conclusive negative):** Against the FULL reference NPLN server (splatoon-3 implementation: complete `nn.npln.*` services, emulator-token-accepting auth, tenant-covering cert) run locally on the tapped port with the linked local account, Stardew completes TLS+h2 ("h2 established — reached the gRPC layer") and then closes with **ZERO RPCs received** — identical pre-HEADERS cancel, 2321-4992. Combined with runs A–C: the gate is definitively inside Stardew's NPLN SDK initialization, before any request exists. Server-side completeness is irrelevant.
- **CONFIRMED (offline):** The cloned nextendo-nncs reference server implements ONLY the UDP NAT-check responder (Pia NatDetectionJob, 16-byte probes, NAT classification) — it delivers no environment/config data. The `_npln:/npln_config.json` file is not in the base RomFS dump (Content/ assets only) and not persisted in emulated NAND; it is an in-memory SDK mount.
- **CONFIRMED (offline):** Stardew main embeds its own Pia/NPLN login state machine diagnostics (`ChangeStateJob::WaitInitializeNetworkSdk`, `ChangeStateJob::Login`, `LoginJob::FailureProcess`, `(Auth RPC result)`, `Failed to get NsaIdToken cache. Need call EnsureNetworkServiceAccountAvailableAsync()`) — a local wrapper around the SDK whose failure branch produces the observed behavior. ADRP+ADD xref anchors located for these strings (code refs at ~0x760cf24, 0x776bea0, 0x7788354, 0x7610994/0x7610d38 in main build E7F845…).
- **CONFIRMED (memory):** Guest-RAM inspection (authorized) found the citron-fabricated id_token DECODED in the SDK heap (`"aud":"ed9e2f05d286f7b8"` with iss/iat/exp/jku/jti/di claims) — the token is delivered AND parsed; no validation-error text nearby. `_npln:/npln_config.json` does not exist in guest RAM, the base RomFS dump (Content/ assets only), or emulated NAND — the mount is never materialized at runtime.
- **CONFIRMED (citron HLE):** `IManagerForApplication::EnsureIdTokenCacheAsync` is a stub returning success + async interface; `LoadIdTokenCache` serves the real token; `EnsureNetworkServiceAccountAvailableAsync` is not implemented in citron's ACC tables. The id_token `aud` remains hardcoded (`ed9e2f05d286f7b8`) with a random `sub` and a `jku` pointing at the real Nintendo BAAS certificates URL.
- **CONFIRMED (log):** The complete ACC IPC sequence around the abort is clean (CheckAvailability, GetAccountId ×3, EnsureIdTokenCacheAsync, async HasDone/GetResult, LoadIdTokenCache with 1405-byte token, GetProfile/GetBase, GetUserExistence, GetBaasAccountManagerForApplication, InitializeApplicationInfoV2) — no unknown commands, no error results. Account-side IPC is formally satisfied.
- **CONFIRMED (offline, static):** With corrected LOAD-segment mappings (LOAD0 covers VAs < 0x7c46000; an earlier mapping bug produced garbage windows), located the wrapper's diagnostic formatter: code at 0x776b438/0x776be20/0x776bf28 formats `(ResultNetworkServiceAccountUnavailable)` when handed an nn::Result with module field 0x7c (124 = Account) and description in [200, 269]. All three refs live in one ~34KB function at 0x776377c that also references the NsaIdToken-failure string; the Pia login state machine (ChangeStateJob/LoginJob refs) lives in a ~110KB function at 0x75f1e04. `_npln:/npln_config.json` turned out to be grpc-core service-config plumbing (a channel target string), not a title config — not a differentiator vs S3.
- **CONFIRMED (experiment):** Name-mimicry ruled out: serving a certificate whose CN is EXACTLY the tenant hostname (vs wildcard) produced the identical pre-HEADERS abort (2321-4992, zero RPCs at the full local server). Stardew's pin is hash/DER-based, not a name compare — a build-scoped binary patch (S3-style) is required.
- **BREAKTHROUGH (root cause identified, HIGH confidence):** The citron Nextendo fork contains `src/core/loader/nextendo_s3_patches.cpp` (mirrored from Ryujinx-Nextendo's `NextendoS3Patches.cs`) whose own documentation states: without the integrated patch, "the game's certificate PINNING remains active" and "NPLN fails with **2321-4992** after a successful TLS handshake." This is EXACTLY the Stardew symptom — and the S3 patch is a single 4-byte IPS entry: at S3-main offset 0x00157B20, `LDRB W10,[X21,#0x38]` → `MOV W10,#1` (Ryujinx notes: "64-byte-unique signature in 100 MB"). I.e., the NPLN SDK's post-handshake pin check reads a stored "certificate accepted" flag; the patch forces it true. Diagnosis: **Stardew's NPLN SDK performs the same style of local certificate-pin check; our X.509_verify_cert chain-validation patch satisfied chain validation but not the pin, so the SDK aborts with 2321-4992 before any RPC — on every server, regardless of account state.** S3 works on emulators solely because its pin check is patched.
- **CONFIRMED (offline):** Stardew's pin is not name-based: no "Nintendo CA"/"Nintendo Class 2"/"nintendo-private-server" strings exist in main (0 hits), and the tenant hostname is assembled at runtime (`%s.%s%s.t.npln.srv.nintendo.net:443`) — so serving mimicry certs cannot pass it; a build-scoped binary patch (or equivalent) is required, exactly like S3's.
- **CONFIRMED (offline):** All four direct BL callers of Stardew's X509_verify_cert (0x78df090, 0x78df67c, 0x78fdf14, 0x79b724c) are OpenSSL-internal (surrounding code raises ERR_LIB_SSL=20 errors with statem file/line args) — the pin check consumes the handshake result elsewhere (S3-style stored flag), not by calling X509_verify_cert directly.
- **TOOLING:** devkitA64 `aarch64-none-elf-objdump -D -b binary -m aarch64 --adjust-vma=...` works for raw windows; capstone 5.0.7 + pyghidra available (persistent Ghidra 12.1.3 at `~/.local/opt/ghidra_12.1.3_PUBLIC`; headless analysis command in `/home/tobagin/ghidra_sdfull.log`). radare2/Ghidra-from-pip unavailable.
- **TOOLING (mapping, settled 2026-08-31):** reconstructed main.elf is PIE with 0-based VAs; Ghidra imports at base 0x100000 (Ghidra addr = VA + 0x100000). Flat NSO keeps its 0x100-byte header: text file offset = VA + 0x100; rodata file offset = VA + 0x300 in the observed region — verify segment deltas from the NSO segment table, do not assume. Ghidra full analysis left the SDK/login coroutine regions undiscovered: create functions manually (CreateFunctionCmd with SourceType.USER_DEFINED) after finding boundaries via BL-target scans; pyghidra transaction must be committed or project save fails and leaves a stale `.lock` (delete `sdfull.lock` to recover).
- **LESSON (twice-burned):** Ghidra headless (a) must NOT live in /tmp (tmpfs dies on reboot), and (b) its PROJECT must not be on tmpfs either — the first 5.5h full-analysis run completed ("Analysis succeeded", 4385s in the final phase alone) but **"Save failed"** because the project DB filled its 15G tmpfs; all analysis was lost. Rule: Ghidra projects go on disk (now `/home/tobagin/ghidra-projects/sdfull`), logs on disk, and Ghidra project paths cannot contain dot-prefixed elements (`.local` is rejected).
- **STATE:** Full auto-analysis RE-RUNNING on disk-backed storage (launched 20:24, ETA ~5.5h -> ~02:00; log `/home/tobagin/ghidra_sdfull2.log`). When saved: open with pyghidra, decompile the login wrapper (start ~0x75f1e04) and account function with real boundaries, find the cert-pin gate, derive the build-scoped patch offset, add it to the external patch table, rebuild citron, retest.
- **STATE (post-crash):** The machine crashed ~15:20 (analysis lost with it). Full Ghidra auto-analysis of main.elf RESTARTED (fresh project `/tmp/ghidra_proj_sd_full/sdfull`, ~3h ETA, log `/home/tobagin/ghidra_sdfull.log`). Note: /tmp is tmpfs — anything stored there dies on reboot (cost us the first Ghidra install copy). Login state machine decoded as coroutine resumption points (`adr x8,<addr>` stored at ctx+0x38-ish, tags at ctx+72): states include ChangeStateJob::Login / CleanupNetwork / Logout / FailureProcess.LoginJob — the FailureProcess handlers are at 0x760ed88/0x760f0f8. Next: when analysis finishes, decompile the giant wrapper (start 0x75f1e04) with real boundaries, find the cert-pin gate branch, derive the Stardew pin-bypass patch offset, add a build-scoped entry to the external patch table (mirroring kCertificateBypass), rebuild citron, retest.
- **CONFIRMED (memory, runtime):** With Stardew live at the 2321-4992 dialog, the main module image was located in guest RAM at two consistent copies (bases `0x7ec6bfe06000`, `0x7f472e006000`; derived from three rodata anchor strings). Guest code is JIT-translated (no native AArch64 execution), so hardware breakpoints on guest bytes are impossible; memory inspection works.
- **CONFIRMED (memory, runtime):** Chunked full-memory scan (37.7 GB readable): the `(ResultNetworkServiceAccountUnavailable)` formatter message and the NsaIdToken-failure strings exist ONLY as static module rodata — the formatter never executed at runtime. `_npln:/npln_config.json` exists ONLY as the static string — never opened, written, or materialized at runtime. Interpretation update: the string is a grpc-core service-config channel target (`_npln:` scheme), i.e., NPLN's gRPC service-config plumbing, not a title data file. The failing layer is therefore likely the SDK's gRPC channel/service-config initialization or its internal credential state machine — not the ACC IPC surface (clean), not the tenant transport (established), and not this diagnostic formatter itself.
- **CONFIRMED (offline, post-analysis):** Login state machine fully mapped (17 state transitions; handlers 0x760e0f0/0x760ebf0/0x760ef00/0x760f37c/0x760eae4/0x760e460/0x760fe90/0x760f26c/0x760e304/0x760e8b0/0x760ed88/0x760f0f8). The Login handler (0x760ebf0) calls an async job (constructed at 0x760a41c), checks its result at [sp+32]; on FAILURE it formats the result, compares a wrapper field ([x19+228]), sets [x19+256]=1 and field [+60]=2, and transitions to ChangeStateJob::FailureProcess.LoginJob::CompleteProcess — the 2321-4992 path. Ghidra image base is offset -0x160 from static ELF VAs (decompiler UNK labels need +0x160 to read file bytes); large UTF-16 managed-string regions sit inside .rodata around the SDK strings.
- **MEMORY (null result, informative):** At the 2321-4992 dialog, the SHA-256 of the served certificate (DER and SPKI forms) appears NOWHERE in guest RAM — the SDK never digested our certificate to memory, or the compare happened on an ephemeral stack. Memory forensics for the pin is exhausted; the decompile path (job-class hierarchy of the auth job) is the remaining route.
- **NEXT (concrete):** Find Stardew's pin-flag read (the analogue of S3's `LDRB W10,[X21,#0x38]` at S3-main 0x157B20). Approaches, in order: (1) trace what stores/reads the post-verify flag in the SDK's TLS context — locate NPLN SDK's SSL verify-callback installation (SSL_CTX_set_verify call sites reachable from the SDK's user-agent builder at 0x77727b0 / its module); (2) decompile the 0x776377c / 0x75f1e04 functions with a proper decompiler; (3) look for the LDRB-from-context-flag idiom near the SSL state machine. Once found: add a build-scoped Stardew entry to the established external patch mechanism (mirroring kCertificateBypass: force the flag read to 1), retest — expect HEADERS at the server for the first time. (2) optionally compare a working S3 boot's host-side sequence through the same instrumentation.

Hypothesis (updated):

The cancel originates in Stardew's own login state machine before the auth RPC is issued. Candidate failing checks: NPLN SDK initialization against the environment/config it expects (source of `_npln:/npln_config.json` unresolved), per-game id_token claims (aud), or an account-HLE result contract difference in the older SDK. Next step: symbolize and read the login-state-machine failure branch in the reconstructed ELF (offline, external), using the existing Shutdown-backtrace addresses as anchors if module bases are recoverable.

Next step: dump the game's RomFS (citron per-title Dump RomFS) and read `_npln:/npln_config.json` for the authoritative NPLN config (tenant, auth addresses, client id). If present, make citron's id_token claims per-title (env-gated, following the established external-hook pattern), retest, and expect the first real HEADERS frame at the probe.

Artifacts: sanitized conclusions only; all binaries/ELFs/dumps/config files remain external to every repository.

### Experiment 2026-08-31-7: Controlled NPLN TLS termination with sanitized HTTP/2 metadata logging

Hypothesis:

Stardew's 133-byte post-handshake ApplicationData record contains the HTTP/2 connection preface plus the first request HEADERS frame, so a local TLS termination point can record the first `:method`/`:path` (gRPC service/method), safe header names, frame types, and timing without decrypting anything outside the process or retaining payload content. The client's immediate (~1–2 ms) post-send shutdown may then be attributable to an observable cause (e.g., no acceptable server response, authority/tenant rejection) rather than remaining opaque.

Method:

1. Added `cmd/probe` + `internal/probe` to this repository: a loopback-only TLS terminator (in-memory self-signed RSA-2048 cert covering the tenant hostname; never written to disk) that answers only the minimum HTTP/2 framing required to keep the peer talking (server preface, SETTINGS ACK, PING ACK), and logs only: TLS version/cipher/ALPN, HTTP/2 frame types/flags/stream IDs, HPACK-decoded pseudo-headers (`:method`, `:path`, `:scheme`, `:authority`), other header NAMES with value lengths only, byte counts, and timing. Payload bodies, non-pseudo header values, keys, addresses, and certificates are never logged. The probe never answers an application request.
2. HPACK decoding uses `golang.org/x/net/http2/hpack` (BSD-3) for vetted Huffman-correct decoding; the observer remains dependency-free. Tests include an end-to-end redaction guarantee: a secret bearer token and request body bytes provably never appear in the log while `:path` does.
3. External citron change (dirty worktree, same established pattern): `GetNplnDebugProxyIp` was already defined but only wired into the `GetAddrInfoRequestImpl` chain; it is now also first in the `GetHostByNameRequestImpl` chain. Added a companion port-remap branch in `bsd.cpp` `ConnectImpl` (NEXTENDO_S3_DEBUG_PROXY_PORT, npln-host-scoped, unset by default) mirroring the Fall Guys / CTR:NF overrides, so the tap target need not own port 443.
4. Launch citron with `NEXTENDO_S3_DEBUG_PROXY_IP=127.0.0.1 NEXTENDO_S3_DEBUG_PROXY_PORT=18500`; NNCS/NAT traffic keeps its production Nextendo redirect; only hosts containing "npln" move to the local probe.
5. User opens the online co-op menu; probe log is then reviewed and only sanitized summaries enter this file.

Decision rule:

- A `:path` of the form `/pkg.Service/Method` observed reproducibly becomes the first **CONFIRMED** Stardew NPLN method; a matching minimal response is then designed in a follow-up experiment.
- If TLS completes but no HEADERS arrive, the 133-byte record is preface/SETTINGS-only and the failure is earlier in the channel setup; record frame types and close timing.
- If TLS fails against the local certificate, the compatibility boundary is not purely `X509_verify_cert` and the patch hypothesis must be refined.

Observation:

Three probe configurations were run against live client attempts; all produced identical client behavior.

- **Run A (probe answers nothing, empty server SETTINGS):** Each of 3 connections: TLS 1.2 `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256`, ALPN `h2`, client preface + client SETTINGS (ids: `ENABLE_PUSH`, `MAX_CONCURRENT_STREAMS`, `INITIAL_WINDOW_SIZE`, `MAX_FRAME_SIZE`, `MAX_HEADER_LIST_SIZE`, plus Nintendo-custom ephemeral id `0xfe03`), client ACK of server SETTINGS, then `RST_STREAM(stream=1, REFUSED_STREAM)` — **without ever sending HEADERS on stream 1** — then `WINDOW_UPDATE(0)` and FIN. Total lifetime 9–75 ms.
- **Run B (server SETTINGS byte-equivalent to the real Nextendo grpc-go server — single `MAX_FRAME_SIZE=16384`, no window updates; verified against grpc-go v1.81.1 source used by the production server):** Identical client behavior on all 3 connections.
- **Run C (probe answers every completed request stream with a valid trailers-only gRPC response: `:status=200`, `content-type=application/grpc`, `grpc-status=12` UNIMPLEMENTED; response path round-trip-tested against a real HPACK decoder):** Identical client behavior. The client cancels before any request can be answered.

Correlated citron flow (sanitized, metadata-only):

- **CONFIRMED:** Resolution/connect order at both startup (~[32s]) and menu attempt (~[465s]): `nncs1` → `nncs2` NAT checks (UDP, production Nextendo) → `g2122d301` resolved but **never connected** → tenant TLS ×3 with the identical pre-HEADERS abort.
- **CONFIRMED:** No account/BAAS/token host (`*baas.nintendo.com`, `*accounts.nintendo.com`) is ever resolved during the entire flow. The game never attempts tenant-token acquisition before the NPLN channel aborts.
- **CONFIRMED:** The external debug tap (`NEXTENDO_S3_DEBUG_PROXY_IP` + new `NEXTENDO_S3_DEBUG_PROXY_PORT` remap) redirected every tenant connect to the loopback probe; citron logged each 443→18500 remap.

Conclusion:

**CONFIRMED:** Stardew's NPLN client fully accepts the probe's TLS (with the build-scoped X.509 patch) and HTTP/2 layer — it ACKs the server SETTINGS — and then cancels its first RPC **before transmitting HEADERS**, deterministically (~1–11 ms), regardless of server SETTINGS content or application responses. This refines the earlier interpretation of the 2,483-byte-server-flight experiments: the 133-byte "ApplicationData" flight against the real Nextendo server was almost certainly this same preface/SETTINGS/ACK/RST/WINDOW_UPDATE cancel flight, **not an HTTP/2 request**. The blocker is client-local, upstream of the wire protocol: most plausibly a missing NPLN session prerequisite (tenant credential/environment activation), consistent with the observed absence of any BAAS/token step. Transport-level work alone cannot advance the protocol further; the next experiments must target the client's environment/credential acquisition path (NNCS answers, `g2122d301` role, token-source bypass) or compare against a working NPLN title through the same probe.

Artifacts:

- `cmd/probe/main.go`, `internal/probe/server.go`, `internal/probe/conn.go`, `internal/probe/probe_test.go` (this repository; sanitized metadata-only logging with end-to-end redaction tests; HPACK decode via `golang.org/x/net/http2/hpack` BSD-3, minimal HPACK literal encoder hand-written; observer remains dependency-free; `go` directive kept at 1.23.0).
- External citron changes (dirty worktree, GPL, unchanged policy): `GetNplnDebugProxyIp` wired into the `GetHostByNameRequestImpl` chain (it was only in `GetAddrInfoRequestImpl` before); `bsd.cpp` npln debug-proxy port remap (env-gated, default-off).
- Probe process log reviewed for payloads before sanitization: no payload bytes, header values (except the four pseudo-headers), tokens, addresses, or key material were recorded by the probe by construction.

### Experiment 2026-08-31-4: Active-update ExeFS export for offline TLS analysis

Hypothesis:

The exact Stardew build that exhibits the certificate rejection can be exported by Citron after its normal update-selection logic, avoiding guesses about which installed NCA is active.

Method:

Citron's existing `dump_exefs` and `dump_nso` debugging settings were enabled for one controlled boot. The first two attempts did not export because the Qt configuration's `dump_exefs\\default=true` and `dump_nso\\default=true` markers overrode the edited values; one attempt also had two Citron processes, and the older process rewrote the configuration. Both causes were identified and corrected. A single Citron process then booted Stardew with the value set to true and each `\\default` marker set to false.

Observation:

**CONFIRMED:** Citron selected installed update version `0.20.0` (raw version `1310720`) and successfully reconstructed its ExeFS. External-only exports were created under Citron's user-data dump directory. The active module build IDs are:

- `main`: `E7F845093E8CBC68DACF011CCB620D6667B5A20B`
- `sdk`: `C3C9BFAA757A1A23DC892EA38AB032B5C033D2E6`
- `rtld`: `45B2DAEB98C5E0130E1A5EA85FF5C1324E2BC999`

The main module contains an OpenSSL TLS/X.509 implementation with observable diagnostic symbol/string names including `ssl_verify_cert_chain`, `tls_process_server_certificate`, and `certificate verify failed`.

Offline reconstruction with the open-source `shuffle2/nx2elf` tool identified a function at main-relative offset `0x79B4C10` whose control flow matches OpenSSL `X509_verify_cert`: it validates the store context, runs chain verification, and returns `1` on success or `0`/`-1` on failure. Confidence: **HIGH**, pending runtime validation.

Conclusion:

**CONFIRMED:** All compatibility work must be scoped to the main build ID above. Offline analysis can proceed against the exact active module without using the flaky GDB stub or placing proprietary material in this repository.

Artifacts:

All ExeFS files, NSOs, reconstructed ELF files, disassembly databases, and future memory dumps remain outside every repository under Citron's local user-data/research paths. They must never be committed. ExeFS/NSO dumping was disabled after the successful export.

### Experiment 2026-08-31-6: Build-scoped X.509 compatibility patch

Hypothesis:

Returning success from the identified `X509_verify_cert` function will allow Stardew to complete TLS against the Nextendo replacement CA and emit its first HTTP/2/NPLN request.

Method:

The dirty external `../citron-nextendo` worktree now contains a guarded runtime patch in `src/core/loader/nso.cpp`. It activates only for title `0100E65002BB8000`, module `main`, build ID `E7F845093E8CBC68DACF011CCB620D6667B5A20B`, offset `0x79B4C10`, and the exact observed eight-byte original prologue. It replaces the function entry with AArch64 `mov w0, #1; ret`. Any title, module, build, bounds, or original-byte mismatch logs an error and leaves the module untouched.

Observation:

**CONFIRMED:** Citron rebuilt successfully and logged that the exact-build patch applied. Against the redirected Nextendo endpoint, Stardew then advanced beyond the previously failing server-certificate flight:

1. sent ClientHello with the correct Stardew tenant SNI;
2. received the 2,483-byte Nextendo server handshake flight;
3. sent a 126-byte TLS handshake flight beginning with handshake type `0x10` (ClientKeyExchange in the negotiated pre-TLS-1.3 framing observed here);
4. received a 233-byte server handshake response beginning with handshake type `0x04`;
5. sent a 133-byte TLS ApplicationData record;
6. then shut down the socket within approximately 1–2 ms.

The same progression repeated across multiple connections. Before the patch, Stardew shut down immediately after step 2 and emitted neither its client handshake flight nor ApplicationData.

Conclusion:

**CONFIRMED:** Offset `0x79B4C10` is a valid certificate-verification compatibility point for Stardew main build `E7F845093E8CBC68DACF011CCB620D6667B5A20B`. The patch moves the client across the TLS trust barrier and causes it to emit its first encrypted application request to Nextendo. This is the project's first confirmed transition from “TLS server flight rejected” to “TLS established/application data sent.”

**UNKNOWN:** The encrypted application request's HTTP/2/gRPC method and the reason for the immediate post-send shutdown are not yet identified. The next experiment should instrument the local/controlled NPLN TLS termination point or run a Stardew-specific local endpoint with research-only TLS key logging outside the repository, then record only sanitized HTTP/2 service/method and metadata-field names. Do not guess Splatoon RPCs.

Artifacts:

No proprietary bytes or extracted modules were added to this repository. The eight original prologue bytes exist only as a safety fingerprint in the external GPL Citron implementation; the replacement instructions are independently written interoperability behavior.

### Rejected Experiment 2026-08-31-5: Citron GDB stub

Hypothesis:

The GDB stub could provide a stable runtime view of the TLS validation failure.

Observation:

The user reported that this debugger path is flaky, consistent with the earlier startup stall while Citron waited for an attachment.

Conclusion:

**REJECTED:** Do not use the GDB stub for this research. Prefer reproducible offline analysis of the active-update export and controlled network experiments.

### Experiment 2026-08-31-3: Production-certificate control with outbound guard

Hypothesis:

Stardew rejects the redirected Nextendo server flight because its game-local TLS validation does not accept that endpoint's certificate identity/key. If the normal NPLN endpoint's certificate is accepted, the client will produce a post-server-flight TLS record.

Method:

An opt-in, exact-host Citron diagnostic (`NEXTENDO_STARDEW_TLS_PROBE=1`) resolves only `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net` normally. After receiving its TLS server flight, Citron observes only the type and length of the next client TLS record and suppresses it while returning success to the guest. This prevents a TLS Finished, HTTP/2 data, token, or authenticated request from reaching the external endpoint. All other Nextendo redirects remain enabled.

Observation:

**CONFIRMED:** The normal endpoint returned a 2,717-byte TLS server flight. Stardew then produced a 126-byte TLS handshake record after each observed server flight. Citron suppressed every such record as designed, so no TLS Finished, HTTP/2 data, token, or authenticated request was transmitted. The user observed that the client progressed for longer before eventually reporting `2321-4992`.

Conclusion:

**CONFIRMED:** Stardew accepts the normal NPLN endpoint's certificate sufficiently to continue its TLS handshake. It does not produce the equivalent post-server-flight record against the redirected Nextendo endpoint. Therefore the first Nextendo interoperability barrier is game-local certificate identity/key validation, not DNS, TCP, SNI, an NPLN RPC response, or client authentication.

Artifacts:

The diagnostic is an external change in the dirty `../citron-nextendo` worktree. No capture, TLS payload, certificate, key, token, or identifying address is stored in this repository.

### Experiment 2026-08-31-1: Establish a safe observation baseline

Hypothesis:

A protocol-neutral TCP/UDP observer with structured, redacted metadata can identify destination-facing transport behavior and safely advance research before any application protocol is known.

Method:

Implement configurable TCP and UDP listeners that record connection/datagram metadata and bounded payload properties without logging raw payloads by default. Exercise them with synthetic clients and verify deterministic, redacted output.

Observation:

- Synthetic TCP and UDP clients each sent 13 bytes to loopback port 18080.
- The observer emitted one JSON event per transport with `service` set to `unknown`, the local listener address, and `bytes: 13`.
- Events contained no payload, remote/client address, token, or client identifier.
- Automated race-enabled tests confirmed bounded reads (TCP: 4 bytes; UDP: 3 bytes) and payload-free serialization.

Conclusion:

The observation harness is ready for a controlled redirected-client experiment. This result confirms only the harness behavior; it provides no evidence about the retail client or Stardew protocol.

Artifacts:

- `cmd/observer/main.go`
- `internal/observer/observer.go`
- `internal/observer/observer_test.go`
- Synthetic command procedure in `README.md`
- Real packet captures must remain outside the repository.

### Experiment 2026-08-31-2: Citron retail-client baseline

Hypothesis:

Launching a legitimately supplied Stardew Valley installation in `../citron-nextendo` and entering the online co-op menu will expose a reproducible network or HLE-service transition that is absent from a title-screen control run.

Method:

1. Use the existing Citron build and its existing external user configuration; do not copy game content, firmware, keys, saves, credentials, or logs into this repository.
2. Preserve the dirty Citron worktree and make no source changes until its current hooks and configuration are understood.
3. Run a title-screen control, then open Co-op → Online Communication and record only sanitized log summaries.
4. If a hostname/transport appears, repeat once and compare. Keep raw logs/captures outside this repository.

Observation:

- **CONFIRMED:** `../citron-nextendo/build/use-nopgo/bin/citron` exists.
- **CONFIRMED:** The Citron worktree already contains numerous uncommitted changes, including socket, DNS, friend-service, and Nextendo UI changes. They must be preserved.
- **CONFIRMED:** The existing Citron binary launched successfully in the active Wayland graphical session on 2026-08-31. It remains running for interactive testing.
- Stardew launch/menu result: pending user interaction in the emulator window.
- **CONFIRMED:** The first Stardew launch did not reach the title screen. All four emulated CPU cores were idle and no Stardew network activity had occurred; this was a pre-network emulator-startup failure.
- **CONFIRMED:** Two Citron instances were running concurrently against the same user configuration and log. The first instance owned GDB stub port 6543; the later desktop-launched instance logged a critical `Address already in use` bind failure.
- **CONFIRMED:** The originally launched frozen instance was stopped with Ctrl-C; the later desktop-launched instance was deliberately preserved for a single-instance retry.
- **HIGH:** The port collision and shared-log interleaving made the first observation invalid as a controlled Stardew run. It does not establish that the dirty Citron source changes caused the failure.
- **CONFIRMED:** A single-instance retry reproduced the apparent freeze with Citron's external `use_gdbstub=true` setting. Port 6543 was listening, all guest CPU cores were suspended, and the log reported GDB server startup immediately before `KProcess::Run returned`.
- **CONFIRMED:** The external Citron user setting was changed to `use_gdbstub=false` for the next retry. No Citron source, game content, or repository artifact was changed.
- **Conclusion:** The freeze was debugger-wait behavior, not evidence of a Stardew/Nextendo protocol failure. Repeat the launch with the debugger disabled before investigating networking.
- **CONFIRMED:** With the GDB stub disabled, Stardew Valley reached an online communication attempt and displayed Switch error `2321-4992`: “A communication error has occurred. Please try again later.”
- **CONFIRMED:** This is the first observed network-facing client transition. The exact failing service/transport is pending sanitized log correlation; the error code alone does not identify the backend architecture.
- **CONFIRMED:** Observed DNS/service sequence includes `nncs1-lp1.n.n.srv.nintendo.net`, `nncs2-lp1.n.n.srv.nintendo.net`, `g2122d301.lp1.p.srv.nintendo.net`, and `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net`.
- **CONFIRMED:** NAT-check traffic uses UDP: a local ephemeral port sent synthetic-observed 16-byte datagrams to ports 33334 and 10025 across the two configured Nextendo NAT addresses. Payload contents are not retained here.
- **CONFIRMED:** The game-specific and NPLN transport connections use TCP port 443 and negotiate HTTP/2 (`h2`) with TLS 1.3 when independently probed without authentication data.
- **CONFIRMED:** During the failing client run, TCP connects succeeded and each ClientHello received a 2,483-byte server handshake beginning with ServerHello. The client then closed before sending TLS Finished or HTTP/2 data.
- **CONFIRMED:** The game-specific endpoint currently presents a self-signed `CN=nintendo-private-server` certificate; the NPLN endpoint presents a Nintendo-issued wildcard certificate covering the NPLN hostname. Certificate bodies were not stored.
- **CONFIRMED:** Stardew's NPLN tenant identifier is `t-9f607adf-lp1`; the generic game/P2P-monitoring identifier observed is `g2122d301`.
- **HIGH:** Stardew uses the NPLN control plane rather than assuming a traditional dedicated Stardew server. Whether gameplay becomes peer-to-peer after session establishment remains unconfirmed.
- **REJECTED (2026-08-31):** The shared redirected IP does not cause Stardew's observed TLS failure through a missing or wrongly inferred SNI. A metadata-only Citron diagnostic confirmed every observed ClientHello already carried `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net`, matching the requested NPLN tenant hostname.
- **CONFIRMED:** A 20-second raw-capture attempt failed for lack of `CAP_NET_RAW`; the created file was zero bytes and was deleted. No capture was committed.
- **USER-SUPPLIED:** A local copy of Nextendo infrastructure is available or already running for controlled Stardew experiments. Exact components and configuration are pending inventory; no credentials or environment values should be copied into this repository.
- **CONFIRMED:** Local `nextendo-account` is running on TCP 8099. No local NPLN/gRPC or NNCS process was initially running. Local TCP 443 is occupied by unrelated containerized Traefik infrastructure.
- **CONFIRMED:** On 2026-08-31, upstream `NextendoNetwork/nextendo-nncs`, `splatoon-3`, `sni-router`, and `nextendo-docs` were cloned as sibling repositories under `~/REPOS` for external inspection/runtime use. All four are PolyForm Shield 1.0.0. Reuse still requires preserving each repository's notices and checking compatibility rather than copying code implicitly.
- **CONFIRMED:** `splatoon-3` is the published Nextendo NPLN/gRPC server. Its documentation states that NPLN TLS runs inside the game over raw sockets and requires hostname redirection, a game-specific certificate-pinning/trust compatibility step, and an accepted identity.
- **HIGH:** Stardew's observed behavior—receiving the server TLS handshake, sending no Finished or HTTP/2 HEADERS, then showing `2321-4992`—matches the documented pre-RPC failure when the embedded NPLN client does not accept the redirected service certificate. A local server alone cannot pass this barrier.
- **Decision:** Do not adapt Splatoon-specific RPC handlers or captured-response consumers to Stardew before the Stardew client sends its first HTTP/2 HEADERS frame. First establish a clean-room client trust path and observe the requested RPC service/method.
- **License decision (corrected by user):** `stardew-nextendo` follows the other Nextendo game services under PolyForm Shield 1.0.0 with `Required Notice: Copyright 2026 Nextendo Network`. Preserve every upstream repository's existing license; Citron remains GPL. Do not silently relicense code across repositories.
- **CONFIRMED:** The rebuilt Citron diagnostic observed repeated ClientHello attempts where SNI injection was not applied and the IP-keyed fallback candidate was always `t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net`. The current diagnostic cannot yet distinguish an already-present SNI extension from a parser rejection.
- **CONFIRMED:** A second metadata-only Citron diagnostic was built successfully on 2026-08-31. It extracts and logs only the ClientHello SNI hostname, allowing the next retry to distinguish an already-present SNI extension from injection/parser failure. No TLS payload, key material, or credentials are retained.
- **CONFIRMED:** On the subsequent client retry, the error remained `2321-4992`; all observed ClientHello attempts contained the correct NPLN tenant SNI. SNI injection was correctly skipped because the extension was already present.
- **SUPERSEDED:** Before the build-scoped compatibility patch, DNS, TCP, and SNI were correct but the client emitted no post-server-flight handshake or application request. Experiment 2026-08-31-6 crossed this barrier and confirmed subsequent TLS handshake and ApplicationData records.
- **CONFIRMED:** Each tested attempt sent a 213-byte ClientHello, received a 2,483-byte TLS 1.3 server flight beginning with ServerHello, and then initiated a socket shutdown within roughly 1 ms without sending a TLS alert, client Finished, or application data.
- **CONFIRMED:** An independent metadata-only TLS probe found that the redirected NPLN endpoint presents a Nintendo-CA-signed leaf whose common name belongs to a different tenant; wildcard coverage includes the Stardew tenant hostname. The system OpenSSL trust store does not trust the private Nintendo CA chain, which is not by itself evidence of how Stardew validates it.
- **CORRECTED/REFINED:** Public certificate metadata comparison shows the normal endpoint uses `CN=*.lp1.t.npln.srv.nintendo.net`, issued by `Nintendo CA - G3`, while the redirected endpoint uses a broad wildcard/Splatoon-tenant leaf issued by a distinct `Nintendo Class 2 CA - G3` replacement root. Both use 2048-bit RSA and SHA-256 signatures, and both cover Stardew's hostname. Combined with the guarded control experiment, this confirms CA-chain/key validation rather than hostname mismatch.
- **CONFIRMED:** TLS is implemented in the game's userspace stack over raw BSD sockets for this flow; Citron's system SSL trust configuration cannot make the game accept the replacement CA.
- **Decision:** A server-only certificate substitution cannot solve this boundary without the official CA signing key, which is unavailable and must never be sought or stored. Continue only with external, independently derived compatibility behavior; never commit proprietary binaries, dumps, certificates, or keys.
- **CONFIRMED:** The public `Ryujinx-Nextendo` compatibility inventory still marks Stardew Valley `online-broken;ldn-untested`, and its source contains no Stardew/NPLN tenant-specific compatibility entry beyond the title ID inventory.
- **CONFIRMED:** A 2026-08-31 public GitHub code search for the Stardew title ID and tenant ID found no published Stardew certificate compatibility patch. Results contained only title/compatibility inventories and unrelated forks. Published Nextendo client documentation describes certificate patches as game- and build-specific.
- **Decision:** Do not reuse Splatoon-specific guest patches, infer offsets from proprietary binaries, or guess RPC responses. Any game compatibility material needed for a controlled test must remain external to this repository and must comply with the clean-room policy.
- **Security:** The production-certificate control was disabled immediately after the result was obtained. Citron was restarted without `NEXTENDO_STARDEW_TLS_PROBE`, restoring Nextendo-only routing.

Conclusion:

Pending.

Artifacts:

Only sanitized conclusions will be recorded here. Citron user data and raw logs remain outside this repository.

## Confirmed Behaviors

- **CONFIRMED (observer only):** TCP and UDP can listen concurrently on the configured loopback host and port.
- **CONFIRMED (observer only):** Reads are capped by `STARDEW_OBSERVER_MAX_BYTES`.
- **CONFIRMED (observer only):** Structured observation events exclude payloads and remote/client addresses.
- None for the retail client or external services.

## Unconfirmed Hypotheses

- Stardew Valley may use a backend primarily as a control plane followed by peer-to-peer gameplay traffic. **SPECULATIVE** until traffic confirms it.
- A local TCP/UDP observation harness will be useful for identifying the first contacted transport. **MEDIUM** confidence as a research-method claim, not a protocol claim.

## Known Failures

- No client experiment has yet been run.
- No external capture or sanitized observation was supplied.
- The observer cannot identify a hostname on its own; DNS logging or an emulator/console routing observation is required.
- The observer deliberately sends no protocol response, so a redirected client is expected to time out or disconnect after the first transmission.

## Decisions

- Do not implement authentication, sessions, discovery, NAT traversal, relay, or gameplay handling without evidence.
- Default diagnostic logs will contain metadata only; raw payload logging is out of scope for the initial implementation.
- Synthetic fixtures only.
- Use Go 1.23 for the initial dependency-free observer, consistent with nearby independently written Nextendo research tools and easy single-binary deployment. This is an architectural choice, not copied code.
- Do not reuse game-specific assumptions from neighboring projects. The nearby Fall Guys project is MIT-licensed, but this repository's observer is being independently implemented; the Outbound project's Photon findings are not transferable evidence.

## Security / Secrets Notes

- Never commit production identifiers, tokens, certificates, keys, proprietary binaries/assets, raw captures, or identifying IP addresses.
- **User-authorized local research (2026-08-31):** Local extraction, disassembly, and memory inspection of the user's legitimately obtained Stardew installation may be used to derive an interoperability-only certificate compatibility behavior. All proprietary inputs, extracted NSOs, disassembly databases, and memory dumps must remain outside this repository, including outside ignored repository subdirectories. Only independently written conclusions, minimal patch metadata when legally appropriate, and synthetic tests may enter this repository.
- External research inputs must be referenced by local path or environment variable and kept outside the repository.

## Useful Commands

```sh
cp .env.example .env
set -a; . ./.env; set +a
go run ./cmd/observer
go test -race ./...
go vet ./...
printf 'synthetic-tcp' | nc 127.0.0.1 18080
printf 'synthetic-udp' | nc -u -w1 127.0.0.1 18080
```

## Next Steps

1. Run a legitimate retail-client startup and multiplayer-menu test with controlled DNS logging; record sanitized query names, order, timestamps, and outcome.
2. Redirect only an observed candidate hostname to the observer in a controlled network and identify TCP/UDP transport and destination port without retaining payloads.
3. Add a protocol-specific response only after the first client transmission and disconnect behavior have been independently summarized.

## Open Questions

- Which hostname is contacted when the online multiplayer menu opens?
- Which transports and ports are contacted, and in what order?
- Does the backend carry only discovery/control traffic or gameplay traffic too?
- Are friend presence, invitations, NAT traversal, or relay services required?

## Session Log: 2026-08-31

Files created or changed:

- `.gitignore`, `.env.example`, `LICENSE.md`, `README.md`, `go.mod`
- `docs/architecture.md`, `docs/protocol.md`, `docs/experiments/README.md`
- `cmd/observer/main.go`
- `internal/observer/observer.go`, `internal/observer/observer_test.go`
- `handoff.md`

Tests performed:

- `go test -race ./...` — passed.
- `go vet ./...` — passed.
- Citron incremental build with metadata-only SNI diagnostic — passed.
- Manual loopback TCP and UDP synthetic smoke test — passed; both produced payload-free 13-byte observation events.
- Repository hygiene scan — passed; no capture/key/certificate/game-binary/dump extensions, files over 1 MiB, or credential-like content were found.

Remaining blocker:

The NPLN tenant channel reaches a fully functional HTTP/2 layer (client ACKs server SETTINGS) and then cancels its first RPC pre-HEADERS, deterministically, independent of server SETTINGS or responses. No BAAS/token host is ever contacted. The blocker is client-local: the NPLN SDK aborts before sending any application request, most plausibly for lack of a session/credential prerequisite (environment activation, tenant token) or because an earlier NNCS/environment check result was unacceptable.

Recommended next experiment:

Compare against a working NPLN title through the same probe. Run Splatoon 3 (already functional against production Nextendo) with the same debug tap (`NEXTENDO_S3_DEBUG_PROXY_IP` now also covers any "npln" host) and record its connection-start behavior: whether it sends HEADERS immediately, which services/methods it calls first, what its client SETTINGS contain, and whether it resolves account/BAAS hosts before its tenant. Differences versus Stardew isolate the client-local prerequisite. Do not retain tokens, payload bodies, certificates, keys, account identifiers, or raw captures.

- **Hypothesis:** A healthy NPLN client (Splatoon 3) sends HEADERS on a freshly opened tenant connection without requiring any server application response, and resolves a token/identity source before connecting; Stardew's divergence from that pattern identifies the missing prerequisite.
- **Smallest method:** Same loopback probe, same sanitized metadata logging, one controlled Splatoon 3 boot to the lobby; compare sanitized flow summaries (DNS order, SETTINGS ids, first frames) between titles.
- **Decision rule:** If Splatoon sends HEADERS where Stardew cancels, the transport is validated and the delta is Stardew's client-local prerequisite; the specific missing step becomes the next experiment target. If Splatoon also cancels pre-HEADERS against the probe, the probe's transport must be improved before any client-side conclusion is drawn.

## Citron Launch Protocol (2026-08-31, family-wide)

- The assistant may launch/kill/restart citron autonomously when either
  (a) no other citron instance is running, or (b) the only running
  instance is this project's own instance — identified by the Stardew Valley
  NSP path on the command line plus this project's `NEXTENDO_*` env
  vars — including closing and relaunching it to test freshly rebuilt
  citron binaries.
- ASK FIRST when any OTHER title's citron instance is running: Among
  Us, Outbound, Fall Guys, CTR:NF, Stardew, MC and PvZ all share this
  machine and the same citron build; a broad kill takes their session
  down.
- Detection: `pgrep -af "^/home/tobagin/REPOS/citron-nextendo/build"`
  and inspect each process's cmdline (NSP path) and environ
  (title-specific `NEXTENDO_*` vars).
- Never broad-`pkill` citron patterns: they match the calling shell
  itself and other titles' instances.

### Kills: target by game, never by binary path (incident 2026-08-31)

A binary-path kill (`pkill -f citron-nextendo/build`) terminated a
running Outbound session on the shared machine. When killing a citron
instance, kill ONLY pids whose cmdline contains THIS project's game
NSP path (check /proc/<pid>/cmdline per pid). When any other title's
instance is running, ask before launching or killing anything — memory
pressure alone (29 GB host) can OOM a foreign session.
