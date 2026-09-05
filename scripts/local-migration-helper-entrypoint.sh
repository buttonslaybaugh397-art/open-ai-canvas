#!/bin/sh

set -eu

ROOT="${CANVAS_LOCAL_REPOSITORY_ROOT:-/workspace}"
HELPER_DIR="$ROOT/.local/migration-helper"
TOKEN_FILE="$HELPER_DIR/token"

mkdir -p "$HELPER_DIR"
if [ ! -s "$TOKEN_FILE" ]; then
    umask 077
    token="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
    [ "${#token}" -ge 64 ] || { echo '无法生成本地迁移助手 Token。' >&2; exit 1; }
    printf '%s\n' "$token" > "$TOKEN_FILE"
fi
# The backend container runs as the unprivileged `app` user and needs to read
# this local bearer token to authenticate requests to the helper.
chmod 644 "$TOKEN_FILE" 2>/dev/null || true

exec /usr/local/bin/open-ai-canvas-local-migration-helper \
    -root "$ROOT" \
    -address "${CANVAS_LOCAL_MIGRATION_HELPER_ADDR:-0.0.0.0:9714}" \
    -token-file "$TOKEN_FILE" \
    "$@"
