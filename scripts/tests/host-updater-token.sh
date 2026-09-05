#!/usr/bin/env bash
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${root}/scripts/install-host-updater.sh"

# Only generate and run the helper in a disposable fixture, never install services.
fixture="$(mktemp -d "${TMPDIR:-/tmp}/canvas-updater-test.XXXXXX")"
UPDATER_TOKEN_HELPER="${fixture}/libexec/token-helper"
install_token_helper
"${BASH}" -n "$UPDATER_TOKEN_HELPER"
export CANVAS_UPDATER_TOKEN_FILE="${fixture}/run/token"
export CANVAS_UPDATER_TOKEN=01234567890123456789012345678901
"${BASH}" "$UPDATER_TOKEN_HELPER"
[[ "$(cat "$CANVAS_UPDATER_TOKEN_FILE")" == "$CANVAS_UPDATER_TOKEN" ]]
export CANVAS_UPDATER_TOKEN=abcdefghijklmnopqrstuvwxyz012345
"${BASH}" "$UPDATER_TOKEN_HELPER"
[[ "$(cat "$CANVAS_UPDATER_TOKEN_FILE")" == "$CANVAS_UPDATER_TOKEN" ]]
export CANVAS_UPDATER_TOKEN_FILE="${fixture}/cold-start/token"
"${BASH}" "$UPDATER_TOKEN_HELPER"
[[ "$(cat "$CANVAS_UPDATER_TOKEN_FILE")" == "$CANVAS_UPDATER_TOKEN" ]]
printf 'Token helper creation, rotation and missing-directory recovery passed.\n'

INSTALL_DIR="${fixture}/deployment"
mkdir -p "$INSTALL_DIR"
printf 'CANVAS_IMAGE_TAG=1.2.7\n' > "${INSTALL_DIR}/.env"
printf 'name: open-ai-canvas\n' > "${INSTALL_DIR}/docker-compose.yml"
REQUESTED_INSTALL_DIR="$INSTALL_DIR"
REQUESTED_COMPOSE_FILE=docker-compose.yml
COMPOSE_FILE=docker-compose.yml
REQUESTED_RELEASE_COMPOSE_FILE=docker-compose.1panel.yml
REQUESTED_IMAGE_TAG=1.2.8
read_compose_file
read_release_compose_file
read_image_tag
[[ "$COMPOSE_FILE" == docker-compose.yml ]]
[[ "$RELEASE_COMPOSE_FILE" == docker-compose.1panel.yml ]]
[[ "$RELEASE_TAG" == v1.2.8 ]]
grep -qx 'CANVAS_IMAGE_TAG=1.2.8' "${INSTALL_DIR}/.env"
REQUESTED_RELEASE_COMPOSE_FILE=docker-compose.yml
if (read_release_compose_file) >/dev/null 2>&1; then
    printf 'Unsafe development compose source was accepted.\n' >&2
    exit 1
fi
printf 'Local/Release compose separation and explicit version selection passed.\n'
