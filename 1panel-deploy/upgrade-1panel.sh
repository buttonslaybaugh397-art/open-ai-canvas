#!/usr/bin/env bash

set -Eeuo pipefail
umask 077

PROJECT_DIR="${PROJECT_DIR:-$PWD}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.1panel.yml}"
ENV_FILE="${ENV_FILE:-.env}"
PROJECT_NAME="${COMPOSE_PROJECT_NAME:-}"
IMAGE_OWNER="${CANVAS_IMAGE_OWNER:-buttonslaybaugh397-art}"
IMAGE_TAG="${CANVAS_IMAGE_TAG:-}"
WEB_PORT="${CANVAS_HTTP_PORT:-6868}"
HEALTH_URL="${CANVAS_HEALTH_URL:-http://127.0.0.1:${WEB_PORT}}"
EXPECTED_REVISION="${CANVAS_EXPECTED_REVISION:-}"
BACKEND_VOLUME="${CANVAS_BACKEND_VOLUME:-open-ai-canvas_backend-data}"
SECRETS_VOLUME="${CANVAS_SECRETS_VOLUME:-open-ai-canvas_deployment-secrets}"
POSTGRES_VOLUME="${CANVAS_POSTGRES_VOLUME:-open-ai-canvas_postgres-data}"
REDIS_VOLUME="${CANVAS_REDIS_VOLUME:-open-ai-canvas_redis-data}"
BACKUP_CONFIRMED=0
ASSUME_YES=0
DRY_RUN=0

COMPOSE_CMD=()
OVERRIDE_FILE=""

usage() {
    cat <<'USAGE'
Usage:
  sudo bash upgrade-1panel.sh \
    --project-dir /1233/open-ai-canvas-main \
    --compose-file docker-compose.1panel.yml \
    --image-tag 1.5.7.1 \
    --expected-revision eba014162c8c4b80bf1370da9e7c79ebffa8fbf0 \
    --backup-confirmed \
    --yes

Required for a real upgrade:
  --project-dir DIR       Existing 1Panel Compose project directory.
  --image-tag TAG         Published GHCR tag shared by backend and web.
  --backup-confirmed      Confirm that a recoverable data backup exists outside
                          this script. This script never creates a volume backup.

Optional:
  --compose-file FILE     Compose file relative to --project-dir.
                          Default: docker-compose.1panel.yml
  --env-file FILE         Private env file relative to --project-dir.
                          Default: .env; omitted when it does not exist.
  --project-name NAME     Existing Compose project name when it cannot be inferred.
  --image-owner OWNER     GHCR owner. Default: buttonslaybaugh397-art
  --expected-revision SHA Require the pulled images to carry this OCI revision.
  --web-port PORT         Existing Web host port to preserve. Default: 6868.
  --health-url URL        Base URL for the readiness check.
  --backend-volume NAME   Existing backend data volume.
  --secrets-volume NAME   Existing deployment secrets volume.
  --postgres-volume NAME  Existing PostgreSQL data volume.
  --redis-volume NAME     Existing Redis data volume.
  --dry-run               Validate the project and print the upgrade plan only.
  --yes                   Do not ask before stopping Web and Backend.
  -h, --help              Show this help.

The script pulls only migrate/backend/web, runs the target migration, then
recreates Backend and Web. It preserves PostgreSQL, Redis, the existing four
volumes, credentials, and the configured Web port. It never removes volumes
or creates a second Compose project.
USAGE
}

fail() {
    printf 'ERROR: %s\n' "$1" >&2
    exit 1
}

step() {
    printf '\n==> %s\n' "$1"
}

normalize_port() {
    local value="$1"
    [[ "$value" =~ ^[0-9]+$ ]] || fail "Port must be a number from 1 to 65535: $value"
    ((value >= 1 && value <= 65535)) || fail "Port must be a number from 1 to 65535: $value"
}

normalize_tag() {
    [[ "$IMAGE_TAG" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] ||
        fail "--image-tag is not a valid Docker image tag"
}

normalize_owner() {
    [[ "$IMAGE_OWNER" =~ ^[a-z0-9][a-z0-9.-]*(/[a-z0-9][a-z0-9._-]*)?$ ]] ||
        fail "--image-owner is not a valid GHCR owner"
}

confirm() {
    ((DRY_RUN == 1 || ASSUME_YES == 1)) && return
    [[ -t 0 ]] || fail "Non-interactive execution requires --yes."
    local answer
    read -r -p "$1 [y/N] " answer
    [[ "$answer" == "y" || "$answer" == "Y" ]] || fail "Cancelled."
}

parse_args() {
    while (($# > 0)); do
        case "$1" in
            --project-dir)
                [[ $# -ge 2 ]] || fail "--project-dir requires a value"
                PROJECT_DIR="$2"
                shift 2
                ;;
            --compose-file)
                [[ $# -ge 2 ]] || fail "--compose-file requires a value"
                COMPOSE_FILE="$2"
                shift 2
                ;;
            --env-file)
                [[ $# -ge 2 ]] || fail "--env-file requires a value"
                ENV_FILE="$2"
                shift 2
                ;;
            --project-name)
                [[ $# -ge 2 ]] || fail "--project-name requires a value"
                PROJECT_NAME="$2"
                shift 2
                ;;
            --image-owner)
                [[ $# -ge 2 ]] || fail "--image-owner requires a value"
                IMAGE_OWNER="$2"
                shift 2
                ;;
            --image-tag)
                [[ $# -ge 2 ]] || fail "--image-tag requires a value"
                IMAGE_TAG="$2"
                shift 2
                ;;
            --expected-revision)
                [[ $# -ge 2 ]] || fail "--expected-revision requires a value"
                EXPECTED_REVISION="$2"
                shift 2
                ;;
            --web-port)
                [[ $# -ge 2 ]] || fail "--web-port requires a value"
                WEB_PORT="$2"
                shift 2
                ;;
            --health-url)
                [[ $# -ge 2 ]] || fail "--health-url requires a value"
                HEALTH_URL="$2"
                shift 2
                ;;
            --backend-volume)
                [[ $# -ge 2 ]] || fail "--backend-volume requires a value"
                BACKEND_VOLUME="$2"
                shift 2
                ;;
            --secrets-volume)
                [[ $# -ge 2 ]] || fail "--secrets-volume requires a value"
                SECRETS_VOLUME="$2"
                shift 2
                ;;
            --postgres-volume)
                [[ $# -ge 2 ]] || fail "--postgres-volume requires a value"
                POSTGRES_VOLUME="$2"
                shift 2
                ;;
            --redis-volume)
                [[ $# -ge 2 ]] || fail "--redis-volume requires a value"
                REDIS_VOLUME="$2"
                shift 2
                ;;
            --backup-confirmed)
                BACKUP_CONFIRMED=1
                shift
                ;;
            --dry-run)
                DRY_RUN=1
                shift
                ;;
            --yes)
                ASSUME_YES=1
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                fail "Unknown argument: $1"
                ;;
        esac
    done
}

cleanup() {
    if [[ -n "$OVERRIDE_FILE" && -f "$OVERRIDE_FILE" ]]; then
        rm -f -- "$OVERRIDE_FILE"
    fi
}
trap cleanup EXIT

service_id() {
    local service="$1"
    local id
    id="$("${COMPOSE_CMD[@]}" ps -q "$service" | head -n 1 | tr -d '\r')"
    [[ -n "$id" ]] || fail "Running Compose service not found: $service"
    printf '%s\n' "$id"
}

volume_mounts() {
    docker inspect -f '{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}' "$1"
}

require_volume() {
    local service="$1"
    local id="$2"
    local expected="$3"
    local mounts
    mounts="$(volume_mounts "$id")"
    grep -Fxq "$expected" <<<"$mounts" ||
        fail "$service is not mounted with the expected existing volume"
}

container_image() {
    docker inspect -f '{{.Config.Image}}' "$1"
}

container_revision() {
    docker inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$1" 2>/dev/null || true
}

image_revision() {
    docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$1" 2>/dev/null || true
}

assert_running_healthy() {
    local service="$1"
    local id="$2"
    local status health
    status="$(docker inspect -f '{{.State.Status}}' "$id")"
    health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$id")"
    [[ "$status" == "running" ]] || fail "$service is not running after upgrade"
    [[ "$health" == "healthy" ]] || fail "$service is not healthy after upgrade"
}

write_override() {
    local backend_image="ghcr.io/${IMAGE_OWNER}/open-ai-canvas-backend:${IMAGE_TAG}"
    local web_image="ghcr.io/${IMAGE_OWNER}/open-ai-canvas-web:${IMAGE_TAG}"
    OVERRIDE_FILE="$(mktemp "${PROJECT_DIR%/}/.open-ai-canvas-upgrade.XXXXXX.yml")"
    cat >"$OVERRIDE_FILE" <<EOF
services:
  migrate:
    image: ${backend_image}
    pull_policy: always
  backend:
    image: ${backend_image}
    pull_policy: always
  web:
    image: ${web_image}
    pull_policy: always
EOF
}

main() {
    parse_args "$@"

    [[ "$(id -u)" == "0" ]] || fail "Run as root so Docker and the 1Panel project are unambiguous."
    [[ "$(uname -s)" == "Linux" ]] || fail "This upgrade script targets a Linux 1Panel host."
    [[ -n "$IMAGE_TAG" ]] || fail "--image-tag is required; use a published tag, not an uncommitted source revision."
    normalize_tag
    normalize_owner
    normalize_port "$WEB_PORT"
    [[ "$EXPECTED_REVISION" == "" || "$EXPECTED_REVISION" =~ ^[0-9a-f]{40}$ ]] ||
        fail "--expected-revision must be a 40-character lowercase commit SHA"

    if ((DRY_RUN == 0 && BACKUP_CONFIRMED == 0)); then
        fail "A verified recoverable data backup is required; pass --backup-confirmed after checking it."
    fi

    command -v docker >/dev/null 2>&1 || fail "docker is required"
    docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required"
    command -v curl >/dev/null 2>&1 || fail "curl is required for the readiness check"

    PROJECT_DIR="$(cd "$PROJECT_DIR" 2>/dev/null && pwd -P)" || fail "Project directory does not exist"
    COMPOSE_PATH="$PROJECT_DIR/$COMPOSE_FILE"
    [[ -f "$COMPOSE_PATH" ]] || fail "Compose file not found: $COMPOSE_PATH"
    if [[ "$ENV_FILE" == /* ]]; then
        ENV_PATH="$ENV_FILE"
    else
        ENV_PATH="$PROJECT_DIR/$ENV_FILE"
    fi

    COMPOSE_CMD=(docker compose --project-directory "$PROJECT_DIR")
    [[ -f "$ENV_PATH" ]] && COMPOSE_CMD+=(--env-file "$ENV_PATH")
    COMPOSE_CMD+=(-f "$COMPOSE_PATH")
    [[ -n "$PROJECT_NAME" ]] && COMPOSE_CMD+=(--project-name "$PROJECT_NAME")

    step "Validate the existing 1Panel Compose project"
    "${COMPOSE_CMD[@]}" config --quiet

    POSTGRES_ID="$(service_id postgres)"
    REDIS_ID="$(service_id redis)"
    BACKEND_ID="$(service_id backend)"
    WEB_ID="$(service_id web)"

    require_volume postgres "$POSTGRES_ID" "$POSTGRES_VOLUME"
    require_volume postgres "$POSTGRES_ID" "$SECRETS_VOLUME"
    require_volume redis "$REDIS_ID" "$REDIS_VOLUME"
    require_volume backend "$BACKEND_ID" "$BACKEND_VOLUME"
    require_volume backend "$BACKEND_ID" "$SECRETS_VOLUME"

    current_binding="$(docker port "$WEB_ID" 3000/tcp 2>/dev/null | head -n 1 || true)"
    [[ "$current_binding" == *":${WEB_PORT}"* ]] ||
        fail "Web is not currently bound to the preserved port ${WEB_PORT}"

    write_override
    UPGRADE_CMD=("${COMPOSE_CMD[@]}" -f "$OVERRIDE_FILE")

    step "Preflight summary"
    printf 'Project: %s\nCompose: %s\nImage tag: %s\nWeb port: %s\n' \
        "$PROJECT_DIR" "$COMPOSE_PATH" "$IMAGE_TAG" "$WEB_PORT"
    printf 'Existing images: backend=%s web=%s\n' "$(container_image "$BACKEND_ID")" "$(container_image "$WEB_ID")"
    printf 'Target images: backend=ghcr.io/%s/open-ai-canvas-backend:%s web=ghcr.io/%s/open-ai-canvas-web:%s\n' \
        "$IMAGE_OWNER" "$IMAGE_TAG" "$IMAGE_OWNER" "$IMAGE_TAG"
    printf 'Volumes verified: backend-data, deployment-secrets, postgres-data, redis-data\n'
    ((DRY_RUN == 1)) && { printf '\nDry run complete. No images, containers, services, or volumes were changed.\n'; return; }

    step "Pull the published target images"
    "${UPGRADE_CMD[@]}" pull migrate backend web
    if [[ -n "$EXPECTED_REVISION" ]]; then
        target_backend="ghcr.io/${IMAGE_OWNER}/open-ai-canvas-backend:${IMAGE_TAG}"
        target_web="ghcr.io/${IMAGE_OWNER}/open-ai-canvas-web:${IMAGE_TAG}"
        [[ "$(image_revision "$target_backend")" == "$EXPECTED_REVISION" ]] || fail "Pulled backend image revision does not match --expected-revision"
        [[ "$(image_revision "$target_web")" == "$EXPECTED_REVISION" ]] || fail "Pulled web image revision does not match --expected-revision"
    fi

    confirm "Stop only Web and Backend, run the target database migration, then recreate them?"

    step "Stop Web and Backend while preserving PostgreSQL and Redis"
    "${UPGRADE_CMD[@]}" stop web backend

    step "Run and verify the target database migration"
    "${UPGRADE_CMD[@]}" up --no-deps --force-recreate --exit-code-from migrate migrate

    step "Start the target Backend and Web images"
    "${UPGRADE_CMD[@]}" up -d --no-deps --force-recreate --wait --wait-timeout 600 backend web

    BACKEND_ID="$(service_id backend)"
    WEB_ID="$(service_id web)"
    assert_running_healthy backend "$BACKEND_ID"
    assert_running_healthy web "$WEB_ID"
    [[ "$(docker port "$WEB_ID" 3000/tcp 2>/dev/null | head -n 1)" == *":${WEB_PORT}"* ]] ||
        fail "Web no longer exposes the preserved port ${WEB_PORT}"
    [[ "$(container_image "$BACKEND_ID")" == "ghcr.io/${IMAGE_OWNER}/open-ai-canvas-backend:${IMAGE_TAG}" ]] ||
        fail "Running backend image is not the requested target tag"
    [[ "$(container_image "$WEB_ID")" == "ghcr.io/${IMAGE_OWNER}/open-ai-canvas-web:${IMAGE_TAG}" ]] ||
        fail "Running web image is not the requested target tag"

    step "Verify the external application readiness endpoint"
    curl -fsS --max-time 30 "${HEALTH_URL%/}/api/health/ready" >/dev/null

    printf '\nUpgrade complete.\n'
    printf 'Project: %s\nImage tag: %s\nWeb port: %s\n' "$PROJECT_DIR" "$IMAGE_TAG" "$WEB_PORT"
    printf 'PostgreSQL, Redis, backend data, deployment secrets, and Redis data volumes were preserved.\n'
}

main "$@"
