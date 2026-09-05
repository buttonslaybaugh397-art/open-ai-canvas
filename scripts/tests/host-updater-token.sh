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
# Version discovery must not mutate deployment files before download/validation.
grep -qx 'CANVAS_IMAGE_TAG=1.2.7' "${INSTALL_DIR}/.env"
REQUESTED_RELEASE_COMPOSE_FILE=docker-compose.yml
if (read_release_compose_file) >/dev/null 2>&1; then
    printf 'Unsafe development compose source was accepted.\n' >&2
    exit 1
fi
printf 'Local/Release compose separation and explicit version selection passed.\n'

INSTALL_DIR="${fixture}/inline-deployment"
mkdir -p "$INSTALL_DIR"
printf 'name: existing-project\n' > "${INSTALL_DIR}/docker-compose.yml"
REQUESTED_COMPOSE_FILE=""
REQUESTED_RELEASE_COMPOSE_FILE=""
REQUESTED_IMAGE_TAG=""
REQUESTED_REPOSITORY=""
COMPOSE_FILE=docker-compose.deploy.yml
compose_command() {
    case "$*" in
        'config --images') printf '%s\n' ghcr.io/test-owner/open-ai-canvas-backend:1.2.9 ghcr.io/test-owner/open-ai-canvas-web:1.2.9 postgres:17-alpine redis:7.4-alpine ;;
        'config --volumes') printf '%s\n' backend-data postgres-data redis-data deployment-secrets ;;
        *) return 1 ;;
    esac
}
read_compose_file
read_deployment_images
read_release_compose_file
read_repository
read_image_tag
[[ "$COMPOSE_FILE" == docker-compose.yml ]]
[[ "$RELEASE_COMPOSE_FILE" == docker-compose.1panel.yml ]]
[[ "$REPOSITORY" == test-owner/open-ai-canvas ]]
[[ "$RELEASE_TAG" == v1.2.9 ]]
[[ ! -e "${INSTALL_DIR}/.env" ]]
printf 'services: {}\n' > "${INSTALL_DIR}/compose.yaml"
if (read_compose_file) >/dev/null 2>&1; then
    printf 'Ambiguous compose files were accepted.\n' >&2
    exit 1
fi
printf 'No-env discovery, image/repository inference and ambiguity checks passed.\n'

DETECTED_IMAGE_TAG=1.2.8
REQUESTED_IMAGE_TAG=""
REQUESTED_UPDATER_RELEASE_TAG=""
curl() { printf 'https://github.com/test-owner/open-ai-canvas/releases/tag/v1.2.9'; }
read_image_tag
read_updater_release_tag
[[ "$RELEASE_TAG" == v1.2.8 && "$UPDATER_RELEASE_TAG" == v1.2.9 ]]
REQUESTED_UPDATER_RELEASE_TAG=v1.2.10
read_updater_release_tag
[[ "$RELEASE_TAG" == v1.2.8 && "$UPDATER_RELEASE_TAG" == v1.2.10 ]]
REQUESTED_UPDATER_RELEASE_TAG='../../invalid'
if (read_updater_release_tag) >/dev/null 2>&1; then
    printf 'Invalid updater Release was accepted.\n' >&2
    exit 1
fi
printf 'Updater bootstrap is independent from the installed application version.\n'
