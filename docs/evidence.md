# Evidence: npln

## Connectivity rollout

All 18 deployed title services use the standalone OpenPak connectivity service for
ICE credential allocation as of 2026-10-13. The adapter verifies the caller's bearer,
registers its generation independently of IP, and preserves NPLN's ICE response
fields/cache conventions. Each title has its own API token. Infrastructure probes
pass; post-rollout game testing starts with Stardew Valley. Deployment and guarded
rollback: [connectivity rollout](../../../../../openpak/connectivity/docs/npln-deployment-2026-10-13.md).
