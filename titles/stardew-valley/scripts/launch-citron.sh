#!/usr/bin/env bash
# Launches citron-nextendo for Stardew research against the LOCAL podman
# Nextendo stack (~/REPOS/nextendo-local): account (https://account.tobagin.eu,
# the local Traefik route to the account container — also :8099 direct), Stardew NPLN :18501,
# BAAS JWKS :18448, NAT-check responder UDP :10025/:10125 (nncs profile).
# Every Nintendo hostname is pinned to loopback, so a plain relaunch through
# this script can never reach production by accident. See docs/local-stack.md.
#
# Data isolation: citron portable mode (a `user/` dir in the cwd). The profile
# owns its config/log/cache — so the LOCAL account link never mixes with the
# production link in ~/.config/citron — but shares the default install's nand/
# (firmware + installed 0.20.0 update + saves, 25 GB) and keys/ via symlinks.
# One instance per data dir still applies: never run the default profile's
# citron at the same time (shared nand, and citron truncates its log per start).
#
# Usage: scripts/launch-citron.sh [path to citron-nextendo checkout]
# Env:   STARDEW_CITRON_PROFILE_DIR, STARDEW_NSP, STARDEW_STACK_DIR,
#        STARDEW_SHARED_CITRON_DIR, NEXTENDO_PID, STARDEW_ALLOW_SHARED=1
set -euo pipefail

CITRON_DIR="${1:-$HOME/REPOS/citron-nextendo}"
CITRON_BIN="$CITRON_DIR/build/use-nopgo/bin/citron"
PROFILE_DIR="${STARDEW_CITRON_PROFILE_DIR:-$HOME/.local/share/stardew-nextendo-citron-profile}"
SHARED_CITRON="${STARDEW_SHARED_CITRON_DIR:-$HOME/.local/share/citron}"
STACK_DIR="${STARDEW_STACK_DIR:-$HOME/REPOS/nextendo-local}"
NSP_PATH="${STARDEW_NSP:-/mnt/media/Emulation/roms/switch/Stardew Valley [0100e65002bb8000] [v0].nsp}"
SIGNING_KEY="$STACK_DIR/secrets/baas_signing_key.pem"
# The local account deployment behind the operator's Traefik (valid LE cert, so the
# browser OAuth sign-in is clean, and it matches the stack's NEXTENDO_BASE_URL).
# STARDEW_ACCOUNT_URL=http://127.0.0.1:8099 reaches the same container directly.
ACCOUNT_URL="${STARDEW_ACCOUNT_URL:-https://account.tobagin.eu}"

[[ -x "$CITRON_BIN" ]] || { echo "error: citron binary not found: $CITRON_BIN" >&2; exit 1; }
[[ -r "$SIGNING_KEY" ]] || { echo "error: BAAS signing key not found: $SIGNING_KEY (nextendo-local/README.md, Setup 1)" >&2; exit 1; }
[[ -f "$NSP_PATH" ]] || { echo "error: game not found: $NSP_PATH" >&2; exit 1; }

# Stack preflight (warn only): without these the game fails closed on loopback.
curl -s --max-time 5 -o /dev/null "$ACCOUNT_URL/api/me" ||
    echo "warning: account server not reachable at $ACCOUNT_URL — start the stack: cd $STACK_DIR && podman-compose --profile nncs up -d" >&2
for spec in t:18501:stardew t:18448:baas-jwks u:10025:nncs; do
    IFS=: read -r proto port name <<<"$spec"
    ss -l"$proto"n | grep ":$port " > /dev/null ||
        echo "warning: $name is not listening on $port — start the stack: cd $STACK_DIR && podman-compose --profile nncs up -d" >&2
done

# --- Emulator Launch Protocol (docs/shared/emulator-launch-protocol.md):
# --- never launch alongside another title's instance without an explicit override.
others=()
for p in $(pgrep -x citron 2>/dev/null); do
    p_cwd=$(readlink -f "/proc/$p/cwd" 2>/dev/null) || continue
    if [[ "$p_cwd" == "$PROFILE_DIR" ]]; then
        echo "error: this profile's citron is already running (PID $p) — one instance per data dir." >&2
        exit 3
    fi
    others+=("PID $p cwd=$p_cwd $(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null | cut -c1-80)")
done
if (( ${#others[@]} > 0 )) && [[ "${STARDEW_ALLOW_SHARED:-0}" != "1" ]]; then
    echo "error: other citron instances are running:" >&2
    printf '  %s\n' "${others[@]}" >&2
    echo "Ask before sharing the machine, or set STARDEW_ALLOW_SHARED=1 to launch alongside them deliberately." >&2
    exit 2
fi

if [[ ! -d "$PROFILE_DIR/user" ]]; then
    mkdir -p "$PROFILE_DIR/user"
    ln -s "$SHARED_CITRON/nand" "$PROFILE_DIR/user/nand"
    ln -s "$SHARED_CITRON/keys" "$PROFILE_DIR/user/keys"
    echo "note: created isolated citron profile $PROFILE_DIR (nand/ and keys/ shared with $SHARED_CITRON via symlinks)" >&2
fi

# citron's GetConfiguredIp() prefers the profile SETTING over NEXTENDO_SERVER_IP /
# NEXTENDO_NAT_IP, and a fresh profile defaults both to the production IPs — the env
# vars alone are silently ignored. Pin the settings (Qt `\default=false` marker or
# the value is discarded) before every launch, and fail closed if that didn't take.
INI="$PROFILE_DIR/user/config/qt-config.ini"
mkdir -p "$(dirname "$INI")"
if [[ ! -f "$INI" ]]; then
    printf '[Services]\nenable_nextendo\\default=false\nenable_nextendo=true\nnextendo_server_ip\\default=false\nnextendo_server_ip=127.0.0.1\nnextendo_nat_ip\\default=false\nnextendo_nat_ip=127.0.0.1\n' > "$INI"
else
    sed -i -E \
        -e 's/^(enable_nextendo)\\default=.*/\1\\default=false/' \
        -e 's/^enable_nextendo=.*/enable_nextendo=true/' \
        -e 's/^(nextendo_server_ip|nextendo_nat_ip)\\default=.*/\1\\default=false/' \
        -e 's/^(nextendo_server_ip|nextendo_nat_ip)=.*/\1=127.0.0.1/' "$INI"
fi
for k in nextendo_server_ip nextendo_nat_ip; do
    grep -q "^$k=127.0.0.1\$" "$INI" || { echo "error: could not pin $k=127.0.0.1 in $INI — refusing to launch (would reach production)" >&2; exit 1; }
done

cd "$PROFILE_DIR"

# NEXTENDO_SERVER_IP/NAT_IP pin every remaining Nintendo host to loopback; the
# S3 debug tap moves the NPLN tenant (any "npln" host) to the local server;
# the JWKS port remap serves the id_token's jku fetch from the local baas-jwks.
# The signing key must be the SAME pem baas-jwks publishes (secrets/ of the stack).
exec env \
    NEXTENDO_ENABLE=1 \
    NEXTENDO_API="$ACCOUNT_URL" \
    NEXTENDO_API_TRUSTED_SUFFIX=tobagin.eu \
    NEXTENDO_SERVER_IP=127.0.0.1 \
    NEXTENDO_NAT_IP=127.0.0.1 \
    NEXTENDO_S3_DEBUG_PROXY_IP=127.0.0.1 \
    NEXTENDO_S3_DEBUG_PROXY_PORT=18501 \
    NEXTENDO_BAAS_JWKS_PORT=18448 \
    NEXTENDO_BAAS_SIGNING_KEY="$(<"$SIGNING_KEY")" \
    NEXTENDO_PID="${NEXTENDO_PID:-1800000005}" \
    "$CITRON_BIN" "$NSP_PATH"
