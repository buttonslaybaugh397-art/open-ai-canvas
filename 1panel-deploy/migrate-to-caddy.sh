#!/usr/bin/env bash

set -Eeuo pipefail
umask 077

PROJECT_DIR="${PROJECT_DIR:-$PWD}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
ENV_FILE="${ENV_FILE:-.env}"
PROJECT_NAME="${COMPOSE_PROJECT_NAME:-}"
DOMAIN="${CANVAS_DOMAIN:-}"
OLD_HTTP_PORT="${CANVAS_OLD_HTTP_PORT:-}"
NEW_HTTP_PORT="${CANVAS_NEW_HTTP_PORT:-3000}"
CADDYFILE_PATH="${CADDYFILE_PATH:-/etc/caddy/Caddyfile}"
CADDY_BIN="${CADDY_BIN:-caddy}"
CADDY_SERVICE="${CADDY_SERVICE:-caddy}"
TAKEOVER_SERVICE="${TAKEOVER_SERVICE:-}"
TAKEOVER_CONTAINER="${TAKEOVER_CONTAINER:-}"
VERIFY_URL="${VERIFY_URL:-}"
BACKUP_ROOT="${BACKUP_ROOT:-/var/backups/open-ai-canvas-caddy}"
ASSUME_YES=0
DRY_RUN=0
SKIP_PUBLIC_CHECK=0
ALLOW_CADDYFILE_REPLACE=0

BACKUP_DIR=""
BRIDGE_FILE=""
ENV_BACKUP=""
CADDYFILE_BACKUP=""
CADDYFILE_WAS_PRESENT=0
CADDYFILE_INSTALLED=0
WEB_CHANGED=0
ENV_CHANGED=0
CADDY_INITIAL_ACTIVE=0
TAKEOVER_STOPPED=0
ROLLBACK_IN_PROGRESS=0
SUCCESS=0

usage() {
    cat <<'USAGE'
Usage:
  sudo bash migrate-to-caddy.sh \
    --project-dir /opt/1panel/compose/open-ai-canvas \
    --domain canvas.example.com \
    --takeover-service openresty \
    --yes

Required:
  --project-dir DIR       Existing 1Panel Compose project directory.
  --domain HOST           Public HTTPS host, without http:// or a path.

Optional:
  --compose-file FILE     Compose file relative to --project-dir (default: docker-compose.yml).
  --env-file FILE         Environment file relative to --project-dir (default: .env).
  --project-name NAME     Existing Compose project name if it cannot be inferred.
  --old-port PORT         Existing Web host port; otherwise detected from the running Web container.
  --new-port PORT         New loopback Web port (default: 3000).
  --caddyfile FILE        Host Caddyfile (default: /etc/caddy/Caddyfile).
  --caddy-bin FILE        Caddy executable (default: caddy).
  --caddy-service NAME    systemd Caddy service (default: caddy).
  --takeover-service NAME systemd service currently owning 80/443, such as openresty.
  --takeover-container ID Docker container currently owning 80/443.
  --verify-url URL        Public URL to check; default: https://<domain>.
  --backup-root DIR       Backup root (default: /var/backups/open-ai-canvas-caddy).
  --replace-caddyfile     Allow replacing a non-empty existing Caddyfile after backing it up.
  --skip-public-check     Skip the public HTTPS smoke check.
  --dry-run               Validate and print the plan without changing services or files.
  --yes                   Do not ask before switching the gateway and recreating Web.
  -h, --help              Show this help.

The script never runs "docker compose down -v", removes volumes, or creates a second
PostgreSQL service. It changes only the existing Web binding and the host Caddy service.
USAGE
}

fail() {
    printf 'ERROR: %s\n' "$1" >&2
    exit 1
}

step() {
    printf '\n==> %s\n' "$1"
}

print_command() {
    printf '+'
    printf ' %q' "$@"
    printf '\n'
}

run_command() {
    print_command "$@"
    if ((DRY_RUN == 0)); then
        "$@"
    fi
}

confirm() {
    if ((DRY_RUN == 1)); then
        return
    fi
    if ((ASSUME_YES == 1)); then
        return
    fi
    if [[ ! -t 0 ]]; then
        fail "Non-interactive execution requires --yes."
    fi
    local answer
    read -r -p "$1 [y/N] " answer
    [[ "$answer" == "y" || "$answer" == "Y" ]] || fail "Cancelled."
}

normalize_port() {
    local value="$1"
    [[ "$value" =~ ^[0-9]+$ ]] || fail "Port must be a number from 1 to 65535: $value"
    ((value >= 1 && value <= 65535)) || fail "Port must be a number from 1 to 65535: $value"
}

normalize_domain() {
    [[ "$DOMAIN" != *"://"* && "$DOMAIN" != */* && "$DOMAIN" != *" "* && "$DOMAIN" != *$'\t'* ]] ||
        fail "--domain must be a host name, not a URL or path."
    [[ "$DOMAIN" =~ ^[A-Za-z0-9][A-Za-z0-9.:-]*[A-Za-z0-9]$ ]] ||
        fail "--domain contains unsupported characters: $DOMAIN"
}

absolute_path() {
    local base="$1"
    local value="$2"
    if [[ "$value" == /* ]]; then
        printf '%s\n' "$value"
    else
        printf '%s/%s\n' "$base" "$value"
    fi
}

read_env_value() {
    local key="$1"
    awk -v key="$key" '
        $0 ~ "^[[:space:]]*" key "=" {
            sub(/^[^=]*=/, "")
            gsub(/^["'\'']|["'\'']$/, "")
            print
            exit
        }
    ' "$ENV_PATH"
}

port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -ltnH | awk -v port=":$port" '$4 ~ port "$" { found = 1 } END { exit(found ? 0 : 1) }'
        return
    fi
    if command -v lsof >/dev/null 2>&1; then
        lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
        return
    fi
    fail "Neither ss nor lsof is available to check host ports."
}

service_container_id() {
    local service="$1"
    local id
    id="$("${COMPOSE_CMD[@]}" ps -q "$service" | head -n 1 | tr -d '\r')"
    [[ -n "$id" ]] || fail "Running Compose service not found: $service"
    [[ "$(docker inspect -f '{{.State.Status}}' "$id")" == "running" ]] ||
        fail "Compose service is not running: $service"
    printf '%s\n' "$id"
}

write_caddyfile() {
    local target="$1"
    cat >"$target" <<'CADDY'
__CANVAS_DOMAIN__ {
    @canvas_stream path_regexp canvas_stream ^/api/(tasks/[^/]+/text-events|agent/runs/[^/]+/events|ai/system/[^/]+/(responses|chat/completions|models/[^/]+:streamGenerateContent))$

    handle @canvas_stream {
        reverse_proxy 127.0.0.1:__CANVAS_PORT__ {
            flush_interval -1
        }
    }

    handle {
        reverse_proxy 127.0.0.1:__CANVAS_PORT__
    }
}
CADDY
    sed -i \
        -e "s|__CANVAS_DOMAIN__|$DOMAIN|g" \
        -e "s|__CANVAS_PORT__|$NEW_HTTP_PORT|g" \
        "$target"
}

backup_file() {
    local source="$1"
    local target="$2"
    if [[ -e "$source" || -L "$source" ]]; then
        cp -p "$source" "$target"
    fi
}

restore_gateway() {
    if ((CADDYFILE_INSTALLED == 1)); then
        if ((CADDYFILE_WAS_PRESENT == 1)); then
            cp -p "$CADDYFILE_BACKUP" "$CADDYFILE_PATH"
        else
            rm -f "$CADDYFILE_PATH"
        fi
    fi

    if ((CADDY_INITIAL_ACTIVE == 1)); then
        systemctl reload "$CADDY_SERVICE" >/dev/null 2>&1 || systemctl restart "$CADDY_SERVICE" >/dev/null 2>&1
    else
        systemctl stop "$CADDY_SERVICE" >/dev/null 2>&1 || true
    fi

    if ((TAKEOVER_STOPPED == 1)); then
        if [[ -n "$TAKEOVER_SERVICE" ]]; then
            systemctl start "$TAKEOVER_SERVICE" >/dev/null 2>&1 || true
        elif [[ -n "$TAKEOVER_CONTAINER" ]]; then
            docker start "$TAKEOVER_CONTAINER" >/dev/null 2>&1 || true
        fi
    fi
}

rollback() {
    if ((ROLLBACK_IN_PROGRESS == 1 || SUCCESS == 1 || DRY_RUN == 1)); then
        return
    fi
    ROLLBACK_IN_PROGRESS=1
    printf '\nMigration failed. Attempting rollback from %s\n' "$BACKUP_DIR" >&2
    set +e

    if ((ENV_CHANGED == 1)) && [[ -f "$ENV_BACKUP" ]]; then
        cp -p "$ENV_BACKUP" "$ENV_PATH"
    fi

    if ((WEB_CHANGED == 1)); then
        "${COMPOSE_CMD[@]}" up -d --no-deps --force-recreate --pull never web >/dev/null 2>&1
    fi

    if ((CADDYFILE_INSTALLED == 1 || TAKEOVER_STOPPED == 1)); then
        restore_gateway
    fi
    set -e
}

on_exit() {
    local status=$?
    trap - EXIT
    if ((status != 0)); then
        rollback
    fi
    exit "$status"
}

parse_args() {
    while (($# > 0)); do
        case "$1" in
            --project-dir)
                [[ $# -ge 2 ]] || fail "Missing value for --project-dir."
                PROJECT_DIR="$2"
                shift 2
                ;;
            --compose-file)
                [[ $# -ge 2 ]] || fail "Missing value for --compose-file."
                COMPOSE_FILE="$2"
                shift 2
                ;;
            --env-file)
                [[ $# -ge 2 ]] || fail "Missing value for --env-file."
                ENV_FILE="$2"
                shift 2
                ;;
            --project-name)
                [[ $# -ge 2 ]] || fail "Missing value for --project-name."
                PROJECT_NAME="$2"
                shift 2
                ;;
            --domain)
                [[ $# -ge 2 ]] || fail "Missing value for --domain."
                DOMAIN="$2"
                shift 2
                ;;
            --old-port)
                [[ $# -ge 2 ]] || fail "Missing value for --old-port."
                OLD_HTTP_PORT="$2"
                shift 2
                ;;
            --new-port)
                [[ $# -ge 2 ]] || fail "Missing value for --new-port."
                NEW_HTTP_PORT="$2"
                shift 2
                ;;
            --caddyfile)
                [[ $# -ge 2 ]] || fail "Missing value for --caddyfile."
                CADDYFILE_PATH="$2"
                shift 2
                ;;
            --caddy-bin)
                [[ $# -ge 2 ]] || fail "Missing value for --caddy-bin."
                CADDY_BIN="$2"
                shift 2
                ;;
            --caddy-service)
                [[ $# -ge 2 ]] || fail "Missing value for --caddy-service."
                CADDY_SERVICE="$2"
                shift 2
                ;;
            --takeover-service)
                [[ $# -ge 2 ]] || fail "Missing value for --takeover-service."
                TAKEOVER_SERVICE="$2"
                shift 2
                ;;
            --takeover-container)
                [[ $# -ge 2 ]] || fail "Missing value for --takeover-container."
                TAKEOVER_CONTAINER="$2"
                shift 2
                ;;
            --verify-url)
                [[ $# -ge 2 ]] || fail "Missing value for --verify-url."
                VERIFY_URL="$2"
                shift 2
                ;;
            --backup-root)
                [[ $# -ge 2 ]] || fail "Missing value for --backup-root."
                BACKUP_ROOT="$2"
                shift 2
                ;;
            --replace-caddyfile)
                ALLOW_CADDYFILE_REPLACE=1
                shift
                ;;
            --skip-public-check)
                SKIP_PUBLIC_CHECK=1
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

main() {
    parse_args "$@"

    [[ "$(id -u)" == "0" ]] || fail "Run as root so the script can update Caddy and systemd."
    [[ "$(uname -s)" == "Linux" ]] || fail "This migration script targets a Linux 1Panel host."
    [[ -n "$DOMAIN" ]] || fail "--domain is required."
    normalize_domain
    normalize_port "$NEW_HTTP_PORT"
    [[ -z "$OLD_HTTP_PORT" ]] || normalize_port "$OLD_HTTP_PORT"
    [[ -z "$TAKEOVER_SERVICE" || -z "$TAKEOVER_CONTAINER" ]] ||
        fail "Use either --takeover-service or --takeover-container, not both."

    command -v docker >/dev/null 2>&1 || fail "docker is required."
    docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required."
    command -v "$CADDY_BIN" >/dev/null 2>&1 || fail "Caddy is not installed or --caddy-bin is invalid: $CADDY_BIN"
    command -v systemctl >/dev/null 2>&1 || fail "systemctl is required to manage the host Caddy service."

    PROJECT_DIR="$(cd "$PROJECT_DIR" 2>/dev/null && pwd -P)" ||
        fail "Project directory does not exist: $PROJECT_DIR"
    COMPOSE_PATH="$(absolute_path "$PROJECT_DIR" "$COMPOSE_FILE")"
    ENV_PATH="$(absolute_path "$PROJECT_DIR" "$ENV_FILE")"
    [[ -f "$COMPOSE_PATH" ]] || fail "Compose file not found: $COMPOSE_PATH"
    [[ -f "$ENV_PATH" ]] || fail "Environment file not found: $ENV_PATH"
    [[ "$CADDYFILE_PATH" == /* ]] || CADDYFILE_PATH="$(absolute_path "$PROJECT_DIR" "$CADDYFILE_PATH")"
    [[ "$BACKUP_ROOT" == /* ]] || BACKUP_ROOT="$(absolute_path "$PROJECT_DIR" "$BACKUP_ROOT")"

    COMPOSE_CMD=(docker compose --project-directory "$PROJECT_DIR" --env-file "$ENV_PATH" -f "$COMPOSE_PATH")
    if [[ -n "$PROJECT_NAME" ]]; then
        COMPOSE_CMD+=(--project-name "$PROJECT_NAME")
    fi

    step "Validate the existing 1Panel Compose project"
    "${COMPOSE_CMD[@]}" config --quiet

    step "Locate the running application without creating a second project"
    POSTGRES_ID="$(service_container_id postgres)"
    REDIS_ID="$(service_container_id redis)"
    BACKEND_ID="$(service_container_id backend)"
    WEB_ID="$(service_container_id web)"
    : "$POSTGRES_ID" "$REDIS_ID" "$BACKEND_ID"

    if [[ -z "$OLD_HTTP_PORT" ]]; then
        current_binding="$(docker port "$WEB_ID" 3000/tcp 2>/dev/null | head -n 1 || true)"
        if [[ -n "$current_binding" ]]; then
            OLD_HTTP_PORT="${current_binding##*:}"
        else
            OLD_HTTP_PORT="$(read_env_value CANVAS_HTTP_PORT || true)"
        fi
    fi
    [[ -n "$OLD_HTTP_PORT" ]] || fail "Could not detect the existing Web host port; pass --old-port."
    normalize_port "$OLD_HTTP_PORT"
    [[ "$OLD_HTTP_PORT" != "$NEW_HTTP_PORT" ]] ||
        printf 'Existing Web port is already %s; the bridge recreate will be skipped.\n' "$NEW_HTTP_PORT"

    if [[ "$OLD_HTTP_PORT" != "$NEW_HTTP_PORT" ]] && port_in_use "$NEW_HTTP_PORT"; then
        fail "New Web port is already in use: $NEW_HTTP_PORT"
    fi

    timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
    BACKUP_DIR="$BACKUP_ROOT/$timestamp"
    mkdir -p "$BACKUP_DIR"
    chmod 700 "$BACKUP_DIR"
    ENV_BACKUP="$BACKUP_DIR/environment.env"
    cp -p "$ENV_PATH" "$ENV_BACKUP"
    backup_file "$COMPOSE_PATH" "$BACKUP_DIR/compose.yml"
    if [[ -e "$CADDYFILE_PATH" || -L "$CADDYFILE_PATH" ]]; then
        CADDYFILE_WAS_PRESENT=1
        CADDYFILE_BACKUP="$BACKUP_DIR/Caddyfile.previous"
        backup_file "$CADDYFILE_PATH" "$CADDYFILE_BACKUP"
        if [[ -s "$CADDYFILE_PATH" && "$ALLOW_CADDYFILE_REPLACE" != "1" ]]; then
            fail "Existing Caddyfile is non-empty. Pass --replace-caddyfile only when it is dedicated to this application."
        fi
    fi
    cat >"$BACKUP_DIR/metadata.txt" <<EOF
created_at_utc=$timestamp
project_dir=$PROJECT_DIR
compose_file=$COMPOSE_PATH
env_file=$ENV_PATH
domain=$DOMAIN
old_http_port=$OLD_HTTP_PORT
new_http_port=$NEW_HTTP_PORT
EOF

    BRIDGE_FILE="$BACKUP_DIR/caddy-bridge.override.yml"
    cat >"$BRIDGE_FILE" <<EOF
services:
  web:
    ports:
      - "127.0.0.1:${NEW_HTTP_PORT}:3000"
EOF
    BRIDGE_COMPOSE_CMD=("${COMPOSE_CMD[@]}" -f "$BRIDGE_FILE")

    step "Prepare and validate the Caddy configuration"
    CADDY_STAGED="$BACKUP_DIR/Caddyfile.new"
    write_caddyfile "$CADDY_STAGED"
    "$CADDY_BIN" validate --config "$CADDY_STAGED" --adapter caddyfile

    if systemctl is-active --quiet "$CADDY_SERVICE"; then
        CADDY_INITIAL_ACTIVE=1
    fi

    if ((CADDY_INITIAL_ACTIVE == 0)); then
        if port_in_use 80 || port_in_use 443; then
            [[ -n "$TAKEOVER_SERVICE" || -n "$TAKEOVER_CONTAINER" ]] ||
                fail "80/443 are occupied. Pass --takeover-service or --takeover-container explicitly."
            if [[ -n "$TAKEOVER_SERVICE" ]]; then
                systemctl is-active --quiet "$TAKEOVER_SERVICE" ||
                    fail "Takeover service is not active: $TAKEOVER_SERVICE"
            else
                docker inspect "$TAKEOVER_CONTAINER" >/dev/null 2>&1 ||
                    fail "Takeover container was not found: $TAKEOVER_CONTAINER"
                [[ "$(docker inspect -f '{{.State.Status}}' "$TAKEOVER_CONTAINER")" == "running" ]] ||
                    fail "Takeover container is not running: $TAKEOVER_CONTAINER"
            fi
        fi
    fi

    step "Confirm the gateway handover"
    printf 'Project: %s\nOld Web port: %s\nNew Web port: %s\nCaddyfile: %s\nBackup: %s\n' \
        "$PROJECT_DIR" "$OLD_HTTP_PORT" "$NEW_HTTP_PORT" "$CADDYFILE_PATH" "$BACKUP_DIR"
    if [[ -n "$TAKEOVER_SERVICE" ]]; then
        printf 'Gateway service to stop: %s\n' "$TAKEOVER_SERVICE"
    elif [[ -n "$TAKEOVER_CONTAINER" ]]; then
        printf 'Gateway container to stop: %s\n' "$TAKEOVER_CONTAINER"
    fi
    confirm "Apply the migration now?"

    if ((DRY_RUN == 1)); then
        printf '\nDry run complete. No existing project files, containers, or services were changed.\n'
        printf 'Plan and backup artifacts: %s\n' "$BACKUP_DIR"
        SUCCESS=1
        return
    fi

    step "Add the loopback bridge port without stopping Backend or databases"
    if [[ "$OLD_HTTP_PORT" != "$NEW_HTTP_PORT" ]]; then
        "${BRIDGE_COMPOSE_CMD[@]}" config --quiet
        "${BRIDGE_COMPOSE_CMD[@]}" up -d --no-deps --force-recreate --pull never web
        WEB_CHANGED=1
    fi
    curl -fsS --max-time 20 "http://127.0.0.1:${NEW_HTTP_PORT}/" >/dev/null

    step "Install and activate Caddy"
    mkdir -p "$(dirname "$CADDYFILE_PATH")"
    install -m 0644 "$CADDY_STAGED" "$CADDYFILE_PATH"
    CADDYFILE_INSTALLED=1
    if ((CADDY_INITIAL_ACTIVE == 1)); then
        systemctl reload "$CADDY_SERVICE"
    else
        if [[ -n "$TAKEOVER_SERVICE" ]]; then
            systemctl stop "$TAKEOVER_SERVICE"
            TAKEOVER_STOPPED=1
        elif [[ -n "$TAKEOVER_CONTAINER" ]]; then
            docker stop "$TAKEOVER_CONTAINER" >/dev/null
            TAKEOVER_STOPPED=1
        fi
        systemctl enable --now "$CADDY_SERVICE"
    fi
    systemctl is-active --quiet "$CADDY_SERVICE" || fail "Caddy service is not active."

    if ((SKIP_PUBLIC_CHECK == 0)); then
        if [[ -z "$VERIFY_URL" ]]; then
            VERIFY_URL="https://${DOMAIN}"
        fi
        step "Verify the public HTTPS route"
        curl -fsS --max-time 30 "${VERIFY_URL%/}/api/health/ready" >/dev/null
    else
        printf 'Public HTTPS smoke check skipped by request.\n'
    fi

    step "Finalize Web on the new port"
    if [[ "$OLD_HTTP_PORT" != "$NEW_HTTP_PORT" ]]; then
        temporary_env="${ENV_PATH}.caddy-migration.$$"
        awk -v value="$NEW_HTTP_PORT" '
            BEGIN { updated = 0 }
            /^[[:space:]]*CANVAS_HTTP_PORT=/ {
                print "CANVAS_HTTP_PORT=" value
                updated = 1
                next
            }
            { print }
            END {
                if (!updated) print "CANVAS_HTTP_PORT=" value
            }
        ' "$ENV_PATH" >"$temporary_env"
        chmod --reference="$ENV_PATH" "$temporary_env" 2>/dev/null || chmod 600 "$temporary_env"
        mv "$temporary_env" "$ENV_PATH"
        ENV_CHANGED=1

        "${COMPOSE_CMD[@]}" config --quiet
        "${COMPOSE_CMD[@]}" up -d --no-deps --force-recreate --pull never web
        WEB_ID="$("${COMPOSE_CMD[@]}" ps -q web | head -n 1 | tr -d '\r')"
        final_binding="$(docker port "$WEB_ID" 3000/tcp 2>/dev/null | head -n 1 || true)"
        [[ "$final_binding" == *":${NEW_HTTP_PORT}"* ]] ||
            fail "Final Web container is not bound to port ${NEW_HTTP_PORT}."
        [[ "$final_binding" != *":${OLD_HTTP_PORT}"* ]] ||
            fail "Final Web container still exposes the legacy port ${OLD_HTTP_PORT}."
        curl -fsS --max-time 20 "http://127.0.0.1:${NEW_HTTP_PORT}/" >/dev/null
    fi

    SUCCESS=1
    printf '\nMigration complete.\n'
    printf 'Public URL: https://%s\n' "$DOMAIN"
    printf 'Web loopback port: 127.0.0.1:%s\n' "$NEW_HTTP_PORT"
    printf 'Backup: %s\n' "$BACKUP_DIR"
    printf 'No PostgreSQL, Redis, backend data, or secrets volume was removed.\n'
}

trap on_exit EXIT
main "$@"
