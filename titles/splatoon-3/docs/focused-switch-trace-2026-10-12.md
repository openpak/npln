# Focused Switch trace — 2026-10-12

The owner confirmed a full emuMMC reboot after the temporary configuration install
at 17:47 UTC and a fresh Splatoon 3 launch. Two hardware breakpoints were armed
at 17:57:58 UTC, after the live game's instruction guards matched the local image:

- `main+0x5b7734`: registered UDP handler's returned consumed bit.
- `main+0x557194`, restricted to return address `main+0x552064`: latency result container.

Between 17:58:06 and 17:58:11, **35 handler completions returned consumed=true;
none returned false**. At 17:58:11, the container's field at `input+0x18` was **7**
and its first pointer did not equal its empty-container sentinel (`input+8`). The
conversion's second explicit argument was zero. This is a nonempty result container;
the zero argument must not be mistaken for a measured native success status.

The helper automatically removed both breakpoints and detached at 17:58:11.341 UTC.
The owner subsequently confirmed the same lobby error. This rejects dropped UDP
dispatch and an empty latency container as explanations **for this traced attempt**.
It corrects the earlier conversation's unsupported “zero consumed” statement.
Debugger stops add timing overhead; this run does not measure normal RTT or prove
that later result evaluation succeeds. Individual result values were not captured.

The production connection trace correlates this run with authentication and the
seven-candidate latency RPC at 17:58:06, followed by a client EOF at 17:58:11
after 5.292 seconds. No later lobby RPC appears. The owner's subsequent retry at
17:58:48 used only `ValidateToken` and closed after 228 milliseconds.

Read-only retained-state inspection at 17:59:12 found network object field `+0x28`
equal to `0x1e`, field `+0x150` zero, and an outer error-context pointer with a null
inner pointer. The helper detached immediately after its bounded inspection. These
are raw state observations, not a decoded native error. No NPLN worker threads
remained in the inspected process.

## Next observation

Disassembly shows `main+0x44de7d8` checks the copied latency result status at `[sp]`.
On zero, it copies the result entries and sets completion at object `+0x1d48`.
The other result consumer at `main+0x329529c` also checks status, then result count
at `[sp+0x68]`, before evaluating regional RTT values. Neither consumer was observed
by the first trace. A later native-error path is `main+0x45036a8`, which reads an
`nn::Result` at `x2`; another is `main+0x44ee798`. These differ from the previously
unproductive `main+0x44f029c` observer. Capture actual status and error inputs before
changing server behavior or attributing failure to latency selection.

The private helper now also supports `$openpak_error_trace=1`, arming only
`main+0x45036a8` and `main+0x44ee798`. It records a single 32-bit raw result at
`x2`, caller offset, and classification arguments, then automatically detaches.
Both instruction guards match the local image and syntax validation passes.
An attempted attachment to the previous PID 148 at 18:02:11 failed because that
process was no longer available; no error-input breakpoint was installed or hit.
This mode remains prepared, not runtime-verified. Recheck current console settings
and SaltyNX exclusion before using a fresh launch hook.

## Artifacts and restoration

Private artifacts are under `scratch/spl3/ftp-audit-20261012/`:
`focused-debug-observations.jsonl`, `focused-retained-state.json`,
`startup-debug.py` (GDB `$openpak_focused_trace=1`), and `extended-trace-state.json`.

After the owner reopened FTPD, `extended-console-config.py restore` completed
successfully. `/SaltySD/exceptions.txt` and
`/atmosphere/config/system_settings.ini` are restored and verified byte-for-byte
against their original backups. `extended-trace-state.json` records
`installed: false`. Both debugger attachments are detached. A full reboot is still
needed to unload the running standalone debugger. Do not run another game launch
hook with the restored SaltyNX injection configuration.

The owner subsequently completed that reboot, and the debugger port was confirmed
closed. After FTPD was reopened, the same guarded temporary settings were reapplied
and verified for the second, error-input trace. **Current console files are therefore
temporarily modified again; reboot and capture are pending.** The state file now
records `installed: true` with fresh backups. Restore with the same guarded helper
after detachment and FTPD recovery.

The running Splatoon server binary hash was verified as
`ff62cb63aa7b4357ba571e0461991d2a4e43ac4981e048ce42ef150788bd844e`.
Its existing binary override remains present. Its recorded service start at 17:56:16
predates this trace; this session did not restart or modify production services.
The passive UDP monitor saw unrelated concurrent traffic and is not used to claim
a console-attributed server probe count. Client-side consumed counts above are
direct observations from the guarded matching game process.

## Second trace and state watchpoint

At 18:37:20 the two error-input observers armed on a fresh matching game launch.
The owner reproduced the failure; neither observer hit. GDB was interrupted,
both breakpoints removed, and the game detached at 18:38:22. Server logs show
authentication and seven-candidate discovery at 18:37:28, client EOF after 1.044 s,
then a cached ValidateToken retry closing after 235 ms. No raw error was captured.

A hardware write watchpoint on the retained network object's `+0x28` captured
states 30→34→27→28→30 on the owner's next retry (18:39:47–51), then detached.
Resolving the actual runtime member-function table identified state 28's update
as `main+0x15060e4`, which polls operation 2 via `main+0x1509a84` and enters 30
when complete. These are cleanup transitions; they do not identify the original
startup rejection. The prepared constructor observers and cleanup watchpoint
are not evidence that latency evaluation succeeds.

The owner proposed invalid latency metadata/results as the cause. This remains
plausible despite received replies and a nonempty container. After the owner
closed the game, a fresh-launch hook was armed at 18:43:13 with
`$openpak_latency_eval=1`: `main+0x557194` records bounded region/latency entries,
and `main+0x44de7d8` records the copied result status/count and detaches. Runtime
capture remains pending. Raw latency values are not assigned units without
confirming their construction. Temporary console settings remain installed.
