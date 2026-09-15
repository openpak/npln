#!/usr/bin/env bash
# Stardew's Ryujinx launcher — a thin wrapper over the family shared launchers
# (workspace tools/launch-ryujinx-{host,joiner}.sh) with Stardew's NSP + NPLN route baked in.
#
# Two SHARED profiles, not one per game (2026-09-02): host = ~/ryujinx-instances/host,
# joiner = ~/ryujinx-instances/joiner, reused across every title. Sign host into the local
# `host` account and joiner into `joiner` (until those exist, `stardewhost`/`stardewjoin` — see
# docs/shared/ryujinx-isolation.md). Runbook: docs/local-stack.md.
#
# Usage: scripts/launch-ryujinx.sh [--joiner] [--menu]
#   --joiner   use the shared joiner profile (default: host)
#   --menu     main window only — sign in from the Nextendo menu, then load the game
# Env: STARDEW_NSP, NEXTENDO_ALLOW_SHARED=1 (launch alongside another instance, ask first),
#      RYUJINX_DATA_DIR (override the profile dir), plus anything the shared launcher honours.
set -euo pipefail

HERE="$(dirname "$(readlink -f "$0")")"
SHARED="$HERE/../../../../../tools"
NSP_PATH="${STARDEW_NSP:-/mnt/media/Emulation/roms/switch/Stardew Valley [0100e65002bb8000] [v0].nsp}"
ROUTE="t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net=127.0.0.1:${STARDEW_NPLN_PORT:-21010}"   # Stardew NPLN tenant -> this server (Openpak/ports.md)

LAUNCHER="launch-ryujinx-host.sh"; ARGS=()
for a in "$@"; do case "$a" in
    --joiner) LAUNCHER="launch-ryujinx-joiner.sh";;
    --menu)   ARGS+=(--menu);;
    *)        echo "unknown flag: $a (use --joiner and/or --menu)" >&2; exit 64;;
esac; done

# Backward-compat: the old STARDEW_ALLOW_SHARED maps to the shared launcher's NEXTENDO_ALLOW_SHARED.
[[ "${STARDEW_ALLOW_SHARED:-0}" == "1" ]] && export NEXTENDO_ALLOW_SHARED=1

[[ -x "$SHARED/$LAUNCHER" ]] || { echo "error: shared launcher missing: $SHARED/$LAUNCHER" >&2; exit 1; }
exec env NEXTENDO_ROUTE="$ROUTE" "$SHARED/$LAUNCHER" ${ARGS[@]+"${ARGS[@]}"} "$NSP_PATH"
