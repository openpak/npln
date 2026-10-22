# Switch lobby investigation — 2026-10-12

> **RESOLVED 2026-10-14: the console's network clock, not the server.** The game calls
> `nn::time::StandardNetworkSystemClock::GetCurrentTime` (returned 0) and then `IsAccuracySufficient`, which
> returned **false** on the Switch (read with a debugger at `main+0x3b08b4c`/`+0x3b08b54`); it then raises a
> kind-only error through `main+0x14f44cc` and closes the NPLN connection itself. Harbor's emulated time service
> always answers "sufficient", so Harbor never hit it.
> **Root causes (both fixed, no game patch, no `[time]` stopgap needed):** (1) the community sysmodule `sys-patch`
> on the SD (`[nim] blankcal0crashfix_17.0.0+`) nulled nim's SSL-context callback (`mov x2,xzr` at nim VA 0x19ecd4),
> so nim's HTTPS wrapper failed with curl code 94 before any ClientHello and the clock was never recalibrated;
> disabled with `=0` (emuMMC serves the real CAL0). (2) nim 23.0.x accepts only `{"unixtime":"<ms>"}` from
> `api.hac.lp1.ctest.srv.nintendo.net/v1/time`; nx-baas now answers that (commit 34d7a07, deployed). With both in
> place nim calibrated (no retry burst) and the Switch reached the lobby with no overrides. Every hypothesis below
> (TLS, peer name, SaltyNX, latency regions, UDP replies, account) was rejected; the third-party Splatoon 3
> certificate/peer-name patches were tested on hardware and changed nothing. See the docs repo
> `switch/sys-patch-notes.md`.


The retail Switch reports “A communication error has occurred”; Harbor reaches the lobby.
The current cause is **not yet established**. The earlier loopback latency endpoint defect is
fixed in the running deployment, but hardware still abandons startup before lobby services.

## Evidence

- **Confirmed:** the running Splatoon 3 container uses NPLN v0.7.12, started at
  2026-10-11 19:18:39 UTC. Its STUN host is the production public address, with no latency
  host or port override. The response therefore names that address on UDP 3478.
- **Confirmed:** the Switch's application authentication names version `002a0000`
  (2752512), matching the local v11.3.0 update inventory. Application authentication returns
  HTTP 200. NPLN TLS, HTTP/2 and `IssuePrearrangedUserToken` complete successfully.
- **Confirmed:** at 2026-10-12 10:21:39–40 UTC (11:21 Dublin), the client calls
  `IssuePrearrangedUserToken`, then `ListLatencyMeasurementServers`, then closes the connection
  after 820 ms. There are no schedule, lobby messaging or presence calls on that connection,
  no logged RPC errors, and no HTTP/2 GOAWAY or RST_STREAM.
- **Confirmed, server-side observation:** a UDP endpoint emits five 20-byte requests to
  STUN at 10:21:40.274–40.669 UTC, coinciding with the failed startup. The server emits a
  40-byte reply to each. Attribution to the game's latency measurement is **high confidence**
  from timing and packet shape; this trace does not prove the Switch received or accepted
  the replies. Addresses are replaced with per-run tags in the retained metadata.
- **Confirmed:** three independent UDP Binding Requests from the development machine receive
  successful Binding responses from the advertised public endpoint, with matching transaction
  IDs and source endpoint, in 25–28 ms. This proves external reachability from that machine,
  not reception by the Switch.
- **Confirmed:** the owner reports the retry failed. At 10:29:05 UTC the same NPLN user calls
  only `ValidateToken`, gets a successful response, and closes after 246 ms. Cached retries
  do not repeat the original startup/latency sequence.
- **Historical comparison:** before v0.7.12, hardware closed roughly six seconds after latency
  discovery; Harbor continued into lobby services. After deployment, hardware's initial
  attempt ends in under a second. The old timeout is not sufficient to explain the new failure.
- **Confirmed fresh-launch control:** the owner fully closes and reopens the game and reports
  the same error. At 10:30:50–51 UTC, initial auth and latency discovery complete, and the
  client closes after 856 ms. A coincident UDP endpoint again emits five requests and the server
  emits five replies. A further fresh launch at 10:39:28–29 UTC closes after 899 ms
  at the same boundary. Cached retry state is not a sufficient explanation.
- **Owner-confirmed account/save controls:** the Switch uses a fresh save. Harbor and Switch
  normally use different OpenPak accounts, but the owner also tested Harbor's account on the
  Switch and observed the same failure. Existing Nintendo save data and the choice of account
  do not explain the platform difference by themselves.

The rotation is current and served from memory, although writing its renewed file fails with
permission denied. The failed connections never request it, so that warning is not evidence
of this lobby failure.

## Next experiment

Fresh-launch controls and a seven-region latency response both failed. The latter produced
35 UDP requests and 35 server replies, but still no lobby RPCs. See the facts-only
[`reference comparison`](reference-comparison-2026-10-12.md) for the test and rejection.

The next evidence needed is the Switch's latency result and the next game startup check.
Server-side success and UDP replies alone cannot identify the rejection inside the game.
Do not change schedules, matchmaking or token claims on the assumption that the last RPC is
necessarily the failing operation.

## Diagnostic reports

A bounded, temporary observer decoded the Switch's prepo MessagePack reports, accepting
integer map keys and extension fields that the adapter's general decoder rejects. It emitted
only field names, type counts, a small event-name allowlist and named numeric results; bodies
and headers were discarded, and no raw reports were stored. The observed `result` values were
`0x19080` (module 128, description 200) in system-report structures. No decisive NPLN error
code was extracted, so these values are not treated as the cause of the lobby failure.

The observer's `/post` response remained HTTP 200 with an empty body, matching the existing
handler. Its route and temporary OpenPak-signed certificate/key were removed after the test.

## Read-only console audit

FTP access on 2026-10-12 confirmed the actual SD-card configuration. No console
files were changed during this audit.

- **Confirmed:** DNS interception and its debug logging are enabled. The default
  and emuMMC host files contain OpenPak redirections; Splatoon 3's Atmosphère
  content directory is empty. An empty content directory does not exclude
  runtime injection by a separate system module.
- **Confirmed:** SaltyNX reports Core `1.5.1-b2` and Splatoon 3 build ID prefix
  `28C4287AEE36F749`. That prefix matches the independently examined local game
  image. Its logs show NX-FPS and ReverseNX injection into Splatoon 3, including
  hooks for `TryWaitSystemEvent`, `WaitSystemEvent`, `InitializeMultiWaitHolder`,
  `LinkMultiWaitHolder` and `WaitAny`. No external plugins or title-specific
  FPSLocker patch were found. **Unconfirmed hypothesis:** these hooks might
  affect startup; their presence alone does not establish a failure.
- **Confirmed:** there is no Splatoon 3 crash report in the inspected directory.
  Of 120 recent error reports examined, 116 name `nn.olsc.EventHandler` or
  `nn.olsc.TransferTask`. All twelve reports already collected by the previous
  investigator contain `2179-3201` from `nn.olsc.EventHandler`, including reports
  near the failed startup attempts. The remaining four application reports
  identify Minecraft or Super Mario Bros. 35, not Splatoon 3. These files do not
  supply a game-side NPLN rejection code.
- **Confirmed:** native LogManager and SD-card logging settings are commented
  out, and `/atmosphere/binlogs` is absent. FTP exposes existing files; it does
  not itself capture the game's execution after latency measurement. The local
  game image imports `nn::diag::detail::PutImpl`, but that does not establish that
  useful NPLN diagnostics will be emitted if native logging is enabled.

### SaltyNX control — rejected as a sufficient fix, restored

**Hypothesis:** SaltyNX injection contributes to the hardware-only startup failure.
Change only `/SaltySD/exceptions.txt` by appending `X0100C2500FC20000`, preserving
the original bytes in a backup. Fully close and reopen Splatoon 3, then attempt
the lobby once. Do not change server responses, credentials, certificates or
the game's save for this control.

The installed version's public
[source](https://github.com/masagrator/SaltyNX/blob/1.5.1-beta2/saltysd_proc/source/main.c#L336)
checks this file on each process attachment and aborts bootstrap for the
`X`-prefixed title. A plain title-ID line only suppresses external plugins in
that version, so it is not an equivalent control. A console reboot is not
required by this code path.

**Decision rule:** verify a new SaltyNX log entry saying bootstrap was excluded
for Splatoon 3, then correlate the fresh connection with the server trace. Reaching
post-latency lobby RPCs and the lobby supports the hypothesis; unchanged failure
with exclusion verified rejects removing SaltyNX as a sufficient fix. Restore
the exact original exception file after the control.

The owner explicitly approved this temporary exclusion. It was applied and
read back successfully at 2026-10-12 11:40 UTC. The original file was verified
against the inspection copy and backed up on the console; the proposed file
differs by exactly the one appended exclusion line. The FTP server refused replacing an existing
file by rename, so the original was retained under an additional backup name
before the verified staged file was renamed into place.

**Result — rejected as a sufficient fix.** SaltyNX logged that Splatoon 3 was
forced into `exceptions.txt` and bootstrap was aborted. At 11:42:57–58 UTC,
the fresh game connection completed initial authentication and latency discovery,
then closed after 887 ms by client EOF, without lobby RPCs. A coincident UDP
endpoint sent five probes and received five server replies. The owner reported
the same communication error. A cached retry closed after 240 ms. After ftpd
was reopened, the exclusion evidence was collected and the original exception
file was restored and verified byte-for-byte. The injection hypothesis does not
justify a permanent console change on this evidence.

### Native log capture — no files generated, configuration restored

Enable only native log collection: `atmosphere.enable_log_manager = u8!0x1`,
`lm.enable_sd_card_logging = u8!0x1`, and
`lm.sd_card_log_output_directory = str!atmosphere/binlogs`. Preserve the original
configuration. Reboot to activate it, fully launch Splatoon 3 and reproduce the
failure once, then reopen ftpd for collection and rollback. This is a capture
experiment, not a proposed compatibility fix. Useful game diagnostics are not
guaranteed. A missing log entry is not evidence that a check succeeded; if the
capture supplies no NPLN result, targeted game instrumentation is still needed.
The owner explicitly approved these additional console changes. The three
settings were applied and read back at 11:46 UTC, after verifying and backing
up the original configuration. The owner confirmed rebooting, reproducing the
failure and reopening ftpd. The 11:47:10–11 UTC initial connection completed
authentication and latency discovery, then closed after 860 ms. Cached retries
closed after 262 and 221 ms. `/atmosphere/binlogs` did not exist on collection,
and no native log files were available. This capture is **inconclusive**: it
supplies no client-side NPLN result and does not establish whether the native
logger started or whether the game emitted a log.

The installed `stratosphere.romfs` does contain the Atmosphère LogManager NSP
under embedded title `0100000000000420`; absence of a loose content folder is
not evidence that the logger binary is missing. The original settings file was
restored and verified byte-for-byte. A subsequent reboot is needed to return
the running logging configuration to its original state. Exact private state
and console backup names are in `native-log-state.json` in the audit directory.

### Standalone debugger — aborted after an attachment conflict, restored

Existing file diagnostics have not identified the check following latency
measurement. The installed Atmosphère source provides a standalone debugger
via `atmosphere.enable_standalone_gdbstub = u8!0x1`, listening on TCP 22225;
that port was closed before the experiment. The owner explicitly approved
temporary debugger activation and attachment; the one-line configuration was
applied and read back at 11:53 UTC, with verified backups. TCP 22225 became
reachable after the owner rebooted. Debugger attachment can inspect game threads and
numeric results at the independently located startup/error paths. Any trace
using breakpoints must account for its effect on timing and cannot by itself
prove that the corresponding unpaused failure has the same cause. Activation
requires a reboot; no game patch or server change is justified by the current
evidence. The private `gdb-state.json` records rollback paths. The prepared
observer checks the matching game's instructions, then uses hardware breakpoints
at the latency-result conversion and startup-error construction paths, recording
only numeric result headers and program counters. These raw fields must be
interpreted against the game image before naming a cause.

**Outcome — no game trace captured.** The launch hook caught the game process,
but GDB attachment returned `E01`; a second attachment attempt also failed.
SaltyNX's retained log confirms it had attached to that same process during
launch. The prior exclusion had already been restored, leaving a debugger
ownership conflict during this experiment. No game instruction checks or
breakpoints were reached. The owner reported the game remained on its launching
screen and a console panic occurred when closing it to reopen ftpd.

Reports at 12:01 UTC record `am` (application manager) taking a data abort at
address `0x40` with result `2168-0002`, followed by `qlaunch` (HOME menu) reporting
`2011-0301` and `hoc:clk` reporting `2345-0048`. This sequence is evidence of a
separate failure during the debugger experiment; it does not identify the
original Splatoon 3 lobby rejection. The precise mechanism of `am`'s crash is
not established by these reports alone.

After the owner rebooted and reopened ftpd, the debugger setting was removed
by restoring and verifying the exact original settings file. Another reboot
is needed to unload the debugger from the running console. All temporary
console-file changes from this investigation are restored; backups and private
reports are retained. Do not reuse a game launch hook with SaltyNX injection
active. Any future attempt needs a controlled debugger setup and explicit
verification that competing game debuggers are excluded first.

### Owner-requested debugger retry with SaltyNX excluded — completed

Before the cleanup reboot, the owner explicitly requested another debugger
attempt with the per-game SaltyNX exclusion. The debugger was still running;
the already-restored system settings were left unchanged. The one-line
exception was reapplied with a new verified backup, and later logs confirmed
bootstrap was excluded on both game launches in this retry.

**Confirmed:** debugger attachment now succeeds. The first launch resumed
normally after the observer could not identify a named `main` module. Module
names reported by this debugger are blank; subsequent address resolution
uses module ranges and checks both expected instruction sequences against
the independently examined matching game image. The first initial connection
at 12:07:04 UTC closed after 778 ms and had five coincident probes/server replies.
It preceded later observer arming, so subsequent cached-token attempts were
not a valid latency trace.

**Confirmed fresh trace:** the observer was armed before resuming the next
game launch at 12:13:08 UTC. At 12:13:16 UTC, a hardware breakpoint hit
`main+0x557194` from `main+0x552064`, on the previously mapped latency-processing
path. The coincident startup connection completed authentication and latency
discovery, emitted five UDP probes with five server replies, then closed after
954 ms. The owner reported the same communication error. The selected
startup-error construction path (`main+0x44f029c`) was not observed on this run.

The captured two-word input header represents a pointer rather than a decoded
result code. **Unconfirmed:** whether the latency result collection is empty,
whether the Switch accepted the UDP replies, and which following check rejects
startup. The next observer needs to inspect the container's numeric fields and
its empty-header relationship, with their layout independently verified; treating
the pointer words as an error would be incorrect. The helper is prepared to
collect those additional fields, but they were not captured in this run.

Read-only snapshots of the previously located networking object showed state
field `+0x28 = 0x1e`, field `+0x150 = 0`, and no inner object in its sampled error
context. Bounded frame-pointer stacks were retained privately, including the
`enl::TaskThread` waiting at a known task-thread path. These post-error snapshots
do not identify a causal failure code or prove that the worker is stuck.

All breakpoints were removed and the debugger detached before the owner closed
the game. The per-game exception file was restored and verified byte-for-byte;
the system settings still match their original bytes. A reboot remains necessary
to unload the already-running debugger. Private retry state, exclusion evidence,
callback observations and snapshots are under the audit directory; no production
server changes were made. The lobby cause remains unconfirmed.

Audit artifacts and the proposed one-line diff are private in
`scratch/spl3/ftp-audit-20261012/`; the earlier collection is in
`scratch/spl3/ftp-20261012/`. Error-report bodies, console identifiers and raw
injection logs remain outside the repository.

## Earlier artifacts

Private research directory: `scratch/spl3/lobby-20261012/` in the workspace. It contains a
metadata-only UDP observer and game disassembly for investigating the startup checks. No packet
payloads, player addresses, account IDs or tokens were written into this report. The temporary
observer and the regional-discovery experiment were both removed; production runs the original
v0.7.12 image again. No third-party implementation or patch bytes were imported.
