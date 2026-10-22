# Core Keeper: missing PlayFab requirements

Research date: 2026-10-12. Switch application `010070201D1BA000`, build
1.3.0.3/content 2424832, PlayFab title `94845`.

The physical-console failure occurs after skipping the intro, during loading
and “Please wait”, followed by “Failed to authenticate”, before Host/Join
selection. NPLN authentication succeeded; the subsequent PlayFab Nintendo
login received five HTTP 503 responses. This is a separate authentication
dependency. The NPLN server is deployed, but this document specifies work
that was **not implemented or deployed at research time**.

Implementation update: the standalone [PlayFab service](../../../../playfab/README.md)
now implements the first-stage login and entity-token exchange, with nx-baas
identity verification, application binding and persistent credentials. It was
deployed at 17:56 UTC; [validation and deployment evidence](../../../../playfab/docs/deployment.md)
are tracked separately. Physical-console
acceptance and the conditional Party stage below remain necessary.

Physical Switch retest at 18:29 UTC: login and `GetEntityToken` both returned
200. Party then sent telemetry events and repeated its QoS lookup (503).
The owner saw only an “OK”/A-button prompt and returned to the main menu;
there was no failure text, and no NPLN GameSession creation was observed.
The authentication dependency is now served, but online entry is unresolved.
The subsequent QoS implementation now passes public and device UDP probes.
The [current protocol audit](../../../../playfab/docs/core-keeper-flow.md)
supersedes the initial conditional-Party assumptions below. It establishes the
selected Party path, exact allocation connection constraints, and the missing
platform session at invite dispatch. Relay transport remains unimplemented.

## First implementation: PlayFab-compatible authentication

Add a small PlayFab service under `servers/playfab`, with Core Keeper as its
first explicitly supported title. Keep Switch token verification in nx-baas;
the service can use its existing authenticated internal identity API.
nx-baas should proxy the supported host to that service before its generic
request-body logger. Unsupported PlayFab titles retain the current 503.
Do not weaken the existing account or ban checks.

### Required login endpoint

`POST https://94845.playfabapi.com/Client/LoginWithNintendoServiceAccount`

The native request supplies `TitleId`, `IdentityToken` and `CreateAccount`.
Microsoft's [login contract](https://learn.microsoft.com/en-us/rest/api/playfab/client/authentication/login-with-nintendo-service-account?view=playfab-rest)
defines the request and result fields. The request body contains an identity
credential; parse it with a bounded body reader and do not log it.

Validate the OpenPak-issued Nintendo service-account token through nx-baas's
`/internal/switch/identity` using `id_token`, not a client-provided player ID.
The existing `pidForIDToken` checks signature, token type, issuer and expiry.
Resolve its linked account and apply existing locks/bans. Also bind host,
body `TitleId=94845` and the verified token's application claim to Core Keeper.
A decoded, unsigned JWT payload is not proof of identity.

Return the SDK's successful JSON envelope with:

| Field under `data` | Requirement |
| --- | --- |
| `PlayFabId` | Stable local player identifier mapped to the proven account. |
| `SessionTicket` | Issued credential bound to that account and PlayFab title, with expiry. |
| `EntityToken.Entity.Id` | Stable local entity identifier. |
| `EntityToken.Entity.Type` | `title_player_account`. |
| `EntityToken.EntityToken` | Issued entity credential with matching subject and title scope. |
| `EntityToken.TokenExpiration` | Actual token expiry, UTC. |
| `NewlyCreated` | Reflect whether the local title-player mapping was newly created. |

Use the standard `code: 200`, `status: "OK"`, `data: {...}` wrapper, not an
empty 200 response. Return any additional requested information according
to its contract; do not invent inventory, purchases or friends.

Native evidence: `NintendoPlayFabAuthentication.Login` async state machine
at `0xf389fd0` reads `LoginResult.PlayFabId` (offset `0x50`) and
`LoginResult.EntityToken.Entity` (`0x28`, then `0x10`), stores its local entity
key and sets its authenticated flag. EntityToken must therefore be non-null.
Its request dispatcher at `0xf389cf0` calls the Nintendo service-account SDK
API at `0xe554a20`; the five-attempt limit matches the console requests.
These are observations, not copied implementation code.

The public [Unity authentication context](https://github.com/PlayFab/UnitySDK/blob/master/ExampleTestProject/Assets/PlayFabSDK/Shared/Public/PlayFabAuthenticationContext.cs)
also considers client/entity login established only when its respective
ticket/token is nonempty. Issue real OpenPak credentials and verify them
on subsequent calls; shared placeholder tokens are not an authentication
implementation. Keep mappings and signing material persistent across restarts.

### Token exchange/refresh

Implement `POST /Authentication/GetEntityToken` against a valid session ticket
or entity token, with the same stable entity and a renewed expiry. Microsoft's
[entity-token contract](https://learn.microsoft.com/en-us/rest/api/playfab/authentication/authentication/get-entity-token?view=playfab-rest)
documents the exchange. This endpoint has not yet been observed in Core
Keeper's runtime because login currently fails first; it is the next SDK
dependency to prepare, not a claim that the console already called it.

Keep client tickets and entity tokens distinct in audience/type, reject wrong
title, expired or invalid credentials, and preserve account bans on refresh.

## Existing infrastructure can be reused

- The signed Switch network profile already redirects `.playfabapi.com`.
- The TLS fallback already serves a trusted certificate for
  `94845.playfabapi.com`; the failed login reaches the handler over TLS.
- NPLN tenant `t-b0546eeb-lp1`, its service, CA certificate and direct session
  port 24570 are already installed.
- nx-baas already verifies the NSA token and resolves OpenPak account identity.

The missing piece is HTTP API behavior and issued credentials, rather than a
new DNS ceiling or replacement console account.

## Party networking: conditional second stage

The startup `POST /MultiplayerServer/ListPartyQosServers` already occurs and
gets 503. That failure is tolerated through NPLN login and menu selection.
Do not advertise dummy QoS addresses or claim that an empty list is supported
without testing it. Returning meaningful QoS data requires a corresponding
endpoint; the [public QoS contract](https://learn.microsoft.com/en-us/rest/api/playfab/multiplayer/multiplayer-server/list-party-qos-servers?view=playfab-rest)
describes the list, not the native Party relay protocol.

Core Keeper includes Nintendo NPLN/QUIC transport and a secondary PlayFab
Party implementation. Static mapping alone does not establish which later
Party operations remain mandatory after successful authentication. The
examined `PlayFabPartySessionWrapper.HandleAuthenticationSuccess` calls
`_SetMuted`; that call is not evidence that it creates a Party network.

The SDK has `GetEntityTokenCompleted -> _CreateLocalUser -> PartyCreateLocalUser`
and subsequent create/join network paths. Microsoft's
[Party quickstart](https://learn.microsoft.com/en-us/xbox/playfab/multiplayer/networking/quickstart)
explains the separate local-user and network lifecycle. If Core Keeper
actually selects that transport, login JSON will not supply its network
creation, descriptors, invitations, authenticated peers or relay service.
That would be a larger Party networking project. It is not yet demonstrated
as necessary for Switch-to-Switch NPLN play.

## Acceptance and next observation

1. Test login shape, stable identity, title binding and ticket/entity exchange;
   reject tampered, expired, wrong-title and banned-account credentials.
2. Restart Core Keeper on the physical console and pass the startup “Please
   wait” screen without “Failed to authenticate”. Record the first subsequent
   API path and result, never its credential body.
3. Host one world. Confirm GameSession creation and Gamesync on NPLN, and check
   whether any Party create/join path or new backend hostname appears.
4. Join from a second physical machine/account. Only that establishes working
   multiplayer and whether Party relay support can remain out of scope.

No successful PlayFab response was installed during this research, no game
was patched, and no credentials were sent to Microsoft. Bounded native
analysis and private artifacts are in `scratch/core-keeper/`.
