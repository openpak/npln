# Core Keeper online flow — 2026-10-12

The local Switch base and installed update identify application
`010070201D1BA000`, update 1.3.0.3/content 2424832. Native method mapping used
the update's NSO and IL2CPP v31 metadata. Binaries, method maps, decompilation,
screenshots and runtime logs remain in private `scratch/core-keeper/`; no
proprietary code or capture payloads are included here.

## Static observations

| Method | Native address | Observation |
| --- | --- | --- |
| `NetworkingManager.get_impl` | `0xf3d1110` | Selects the secondary implementation when `UseSecondaryNetworking` is set; otherwise prefers the main implementation. |
| `NetworkingManager.NetImplInit` | `0xf3d1c80` | Constructs and initializes PlayFab Party as the secondary implementation. Both implementations participate in initialization, so transport selection alone cannot establish that PlayFab is optional. |
| `SwitchCommunicationHandler.CreateP2pPlugin` | `0xf3c2100` | Constructs Nintendo's NPLN QUIC peer plugin. |
| `SwitchSessionManager.SetDefaultMatchMakingConfig` | `0xf412130` | Sets the IL2CPP string literal `ckMatchmakingConfigs`. |
| `SwitchSessionManager.CreateGameSessionAsync` | `0xf414380` | Native NPLN session creation path; async state machine at `0xf4197d0`. |
| `SwitchSessionManager.JoinGameSessionAsync` | mapped in private artifacts | Native NPLN session join path; async state machine at `0xf41a000`. |

Nintendo PlayFab authentication and PlayFab Party code are also present. Their
presence does not establish a mandatory PlayFab login in the selected Switch
hosting path. No literal tenant was found in the static sweep.

## Runtime observations

Harbor only was used for the runtime test. The existing Duck Game console was
reused first; its active firmware user was unlinked. Existing linked test
consoles were then discovered, and `sweep-oth-host` was reused. Harbor logged
successful console sign-in with `linked: true` before launching Core Keeper.

At 16:33:23 UTC, the title requested
`POST 94845.playfabapi.com/MultiplayerServer/ListPartyQosServers`, user agent
`PlayFabParty/1.0`. The production PlayFab placeholder returned 503. The game
continued to the main menu. At 16:34:01 UTC, **Start Online Game** resolved and
connected to `t-b0546eeb-lp1.lp1.t.npln.srv.nintendo.net`, offering
`grpc-exp` and `h2`, then requested the NPLN Auth service. The tenant had no
dedicated route and the generic fallback returned 404. A communication error
2321-4224 subsequently appeared; this first attempt therefore does not test
the dedicated NPLN implementation.

The dedicated service started at 16:37:25 UTC on tenant port 21130 and session
port 24570. `/health` identifies `core-keeper`; an external TLS probe verifies
the leaf under the existing OpenPak CA and negotiates `h2`.

A fresh Harbor launch after deployment successfully completed
`Auth/IssuePrearrangedUserToken` at 16:46:36 UTC and started Friends/Presence
streams. The client reached world/character selection while the startup
PlayFab QoS request still received 503. The owner reported that it was working.
No GameSession creation or second-device join was recorded before the client
exited. Its log shows an orderly shutdown at process-relative 225 seconds,
configuration writes and pipeline-cache serialization. No recent systemd
core dump or kernel OOM event was found. The shutdown initiator is unknown;
another Harbor instance was subsequently observed running Castle Crashers.
The Core Keeper server remained active and healthy.

### Physical-console authentication error

The owner subsequently reported an authentication error on the physical console.
At 16:58:07 UTC its NPLN `IssuePrearrangedUserToken` completed successfully and
Friends/Presence streams continued. At 16:59:37 UTC the game made five
`POST /Client/LoginWithNintendoServiceAccount` calls to
`94845.playfabapi.com`; every call received the existing PlayFab placeholder's
503 response. No GameSession creation call was recorded during this attempt.

The endpoint name is redacted as a per-process tag in production logs. It was
identified without reading any token or request body: a bodyless HEAD probe to
that known SDK endpoint produced the identical tag in the same server process.
A probe to `LoginWithNintendoSwitchDeviceId` produced a different tag. Native
`NintendoPlayFabAuthentication.Login` also has a five-attempt retry limit.

Conclusion: the startup PlayFab Party QoS failure is tolerated, but the later
Nintendo service-account login is a blocker in the reported console flow.
Successful NPLN login alone does not establish multiplayer compatibility.
The owner clarified the failure occurs after the intro, loading and “Please
wait”, followed by “Failed to authenticate”, before choosing Host or Join.
That startup sequence matches the failed PlayFab login. No numeric code was
supplied.
An implemented PlayFab login path and subsequent hosting/two-device testing
are required. No fabricated success response, game patch or upstream Microsoft
login was used.

The implementation requirements and unresolved Party transport dependency
are recorded in [playfab-requirements.md](playfab-requirements.md).

## Deployment

The Quadlet is `~/.config/containers/systemd/openpak-core-keeper.container` on
`<prod-host>`. It mounts the arm64 binary from
`~/openpak/experiments/core-keeper-20261012/nplnd`; SHA-256:
`0ec0c03367bc43a7bb27c11fd8c3ea7507b5f6a281d0810cba4b6af5ea463193`.
The original Traefik configuration is backed up as `dynamic.before.yml` in
that directory. Do not restore that whole file over subsequent title changes.
Rollback removes only the `core-keeper` TCP router/service, stops and removes
its Quadlet and removes its exact 24570 firewall rule. Existing titles were
not restarted. Certificates and private keys remain on the host.

Validation: full Go tests and arm64 build passed. Production health, public
tenant/session TLS verification and HTTP/2 negotiation passed. Harbor
authentication and friends/presence streaming were observed; hosted sessions
and second-device joins remain unverified.

## Harbor join / invitation diagnosis — 2026-10-12 21:45 UTC

The owner reports that hosting is accessible, but a hosted game does not appear
in Harbor and invitations are not sent. The inspected production NPLN journal
from 18:00 through 21:44 UTC contains authentication and Friends/Presence
streams, with two friends returned, but no GameSession creation, query or join
RPC and no Gamesync activity. This does not yet verify a published online world.

PlayFab Nintendo login and entity exchange returned 200 at 21:41:03 UTC.
Party QoS still returned 503, and Party telemetry reported error details 4333,
4202/result 4 and 4266/result 11. These failures are a candidate blocker; their
causal relationship to the missing world has not been established.

The mapped retail Switch `StartFriendInvitation` method at `0xf4141b0` is an
empty return in the existing private decompilation. This particular method
cannot send an invitation; other UI/OS invitation paths remain unverified.
No discovery filter, title option or production service was changed on this
evidence. Next: establish that the host enters an online world, identify the
host device and whether the joining device uses a distinct OpenPak account,
then correlate one fresh host/search attempt with both service journals.

The owner clarified that the physical Switch hosts a playable world and the
in-game menu lists friends. Selecting `<user-a>`, then confirming send, has no
visible effect. Through 21:54 UTC the inspected nx-baas invitation logs contain
Harbor inbox GETs returning 200, but no POST to invitation groups or other
invitation creation endpoint. NPLN still records no session creation or hosting
presence update. Loading friends is verified separately from invitation sending.

Further static analysis finds a real Nintendo send path in
`SwitchNetworking.SendSessionInvitations` (`0xf3ccf10`) and its callback
(`0xf3cf900`). `SwitchPlatform.SendInvitation` (`0xf3f8b10`) instead only
invokes its success callback, with no send operation. The earlier empty
`SwitchSessionManager.StartFriendInvitation` is therefore insufficient to
identify the active UI path. The confirmation-to-send dispatch remains under
investigation; no production fix is claimed.

The confirmation callbacks and UI update were also inspected in private
artifacts. This analysis does not yet prove which invitation path executes
on the physical console. Through the inspected 21:50–21:56 UTC window, no
invitation creation or NPLN GameSession/hosting-presence update was recorded.
The next discriminating evidence is a console-side trace of the confirmed
send action and online session start, including the native call's result.
Do not infer that friend-list retrieval proves successful invite submission,
or label the empty platform method as the cause without observing its dispatch.

### Actual selected transport and QoS fix

At 22:08 and 22:10 UTC, guarded read-only inspection of physical Core Keeper
PID 192 found `NetworkingManager` with `secondary_selected=1`, a null primary
implementation and `PlayFabPartyNetworking` as its secondary implementation.
The cross-play preference was 0. The initial Nintendo-only entry observers
had no hits during the owner's host/invite attempt; this alone was not proof
of any specific failure, and the retained state supplies the transport evidence.
The observers were removed and the game detached. Later debugger capability
requests timed out before attachment; no later trace is claimed.

The owner confirms selecting a friend and confirming send in the in-game UI.
Successful friend retrieval is separate from submitting an invitation.
The absence of NPLN GameSession RPCs must not be treated as proof that the
selected Party path uses those RPCs. Inspect its session-wrapper/presence path
and actual request sequence before assigning the discovery failure to NPLN.

The PlayFab QoS dependency was implemented and deployed with a real UDP
beacon. After the owner added cloud ingress for UDP 3075, its public response
was verified at 22:28 UTC. See the
[PlayFab deployment record](../../../../playfab/docs/deployment.md). Party
allocation/relay and end-to-end host/invitation/join are still unverified.

### Fresh test with working public QoS

After a full console-game restart, the 22:33 UTC attempt returned 200 for
`ListPartyQosServers` and `LoginWithNintendoServiceAccount`. The beacon's served
probe count increased from two independent checks to six, confirming actual
device probes. The owner confirmed sending another invitation. No Party
allocation request or invitation submission was observed in that window.
Guarded read-only inspection at 22:37 UTC found the active wrapper's manager to
be `SwitchPlayFabPartySessionManager`, with its current session pointer null.
The debugger detached cleanly. This establishes missing session state; it does
not yet identify why session creation failed. A fresh observer set targets the
selected Party host, connection callbacks and invitation dispatch.

### Observer validation

The first Party observer window ended at 22:42 UTC before the owner's reply;
no conclusion can be drawn about a later action. The second window had no
hardware-breakpoint hits, including the confirmation callback. A guarded
software breakpoint at `NetworkingManager.Update` immediately hit MainThread
at 22:55 UTC, confirming the main image and selected Party implementation.
All observers were removed and the game detached. Hardware-observer silence
is therefore not reliable evidence that those callbacks did not execute.
The invitation/session observers were rearmed using software breakpoints,
with cleanup restoring the game instructions on deletion/detachment.

### Confirmed invitation gate on the physical console

At 23:07:36 UTC the corrected software observer captured
`PlayFabPartyNetworking.SendSessionInvitations` on MainThread, with one selected
friend. The active `SwitchPlayFabPartySessionManager` had a null current session.
Its inspected send implementation checks this state before the native invitation
call. This explains why this attempt submitted no invitation; it does not yet
identify the cause of missing session creation. All observers were removed and
the game detached cleanly.

Subsequent read-only inspection found `NintendoPlayFabAuthentication` with
`_authenticated` false, no local entity key and no PlayFab user ID. The SDK had
no local user or network handle. Both `_connectSessionTask` and `_stopSessionTask`
were retained with flags `0x02000400` (the latter was initially mislabeled as a
login task in private diagnostics). The wrapper reports IsInSession/IsHost true,
so those flags alone cannot establish successful network/session creation.
The 22:33 HTTP login returning 200 is not proof of completed game-side login.
Investigation now targets the login completion callbacks and initial network
creation after a fresh process launch.

### Offline protocol audit, 2026-10-13

At the owner's request, expanded static analysis through the managed HTTP
parsers, host/authentication coroutine, native RequestParty models and
allocation validator, descriptor encoding/CRC and Switch presence/invite
submission. The [protocol flow and remaining implementation](../../../../playfab/docs/core-keeper-flow.md)
records findings and explicitly separates established constraints from runtime
unknowns. The new `internal/party` allocation models/validator pass the PlayFab
race tests and vet; they do not implement or advertise a relay. No additional
console interaction or production mutation was performed for this audit.
