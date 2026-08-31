#!/usr/bin/env bash

set -Eeuo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/open-ai-canvas}"
REQUESTED_REPOSITORY="${CANVAS_UPDATER_REPOSITORY:-${REPOSITORY:-}}"
REPOSITORY="${REQUESTED_REPOSITORY:-buttonslaybaugh397-art/open-ai-canvas}"
REQUESTED_COMPOSE_FILE="${CANVAS_UPDATER_COMPOSE_FILE:-}"
COMPOSE_FILE="${REQUESTED_COMPOSE_FILE:-docker-compose.deploy.yml}"
REQUESTED_SOCKET_DIR="${CANVAS_UPDATER_SOCKET_DIR:-}"
SOCKET_DIR="${REQUESTED_SOCKET_DIR:-/run/open-ai-canvas-updater}"
UPDATER_BIN="/usr/local/bin/open-ai-canvas-host-updater"
UPDATER_ENV="/etc/open-ai-canvas-updater.env"
UPDATER_SERVICE="/etc/systemd/system/open-ai-canvas-updater.service"

fail() {
    printf 'Host Updater 安装失败：%s\n' "$1" >&2
    exit 1
}

require_root() {
    [[ "${EUID}" -eq 0 ]] || fail "请使用 sudo 运行"
    [[ "$(uname -s)" == "Linux" ]] || fail "仅支持 Linux 服务器"
    command -v systemctl >/dev/null 2>&1 || fail "服务器必须使用 systemd"
    command -v curl >/dev/null 2>&1 || fail "缺少 curl"
    command -v sha256sum >/dev/null 2>&1 || fail "缺少 sha256sum"
    command -v openssl >/dev/null 2>&1 || fail "缺少 openssl"
    [[ -f "${INSTALL_DIR}/.env" ]] || fail "未找到 ${INSTALL_DIR}/.env"
}

read_compose_file() {
    local configured
    configured="$(sed -n 's/^CANVAS_UPDATER_COMPOSE_FILE=//p' "${INSTALL_DIR}/.env" | tail -n 1)"
    if [[ -z "$REQUESTED_COMPOSE_FILE" && -n "$configured" ]]; then
        COMPOSE_FILE="$configured"
    fi
    [[ "$COMPOSE_FILE" =~ ^[A-Za-z0-9._-]+\.ya?ml$ ]] || fail "CANVAS_UPDATER_COMPOSE_FILE 必须是安装目录中的 YAML 文件名"
    [[ -f "${INSTALL_DIR}/${COMPOSE_FILE}" ]] || fail "未找到 ${INSTALL_DIR}/${COMPOSE_FILE}"
}

read_socket_dir() {
    local configured
    configured="$(sed -n 's/^CANVAS_UPDATER_SOCKET_DIR=//p' "${INSTALL_DIR}/.env" | tail -n 1)"
    if [[ -z "$REQUESTED_SOCKET_DIR" && -n "$configured" ]]; then
        SOCKET_DIR="$configured"
    fi
    [[ "$SOCKET_DIR" =~ ^/[A-Za-z0-9._/-]+$ && "$SOCKET_DIR" != *..* ]] || fail "CANVAS_UPDATER_SOCKET_DIR 必须是无空格的绝对路径"
}

read_image_tag() {
    local configured
    configured="$(sed -n 's/^CANVAS_IMAGE_TAG=//p' "${INSTALL_DIR}/.env" | tail -n 1)"
    [[ -n "$configured" && "$configured" != "latest" ]] || fail "请先把 CANVAS_IMAGE_TAG 固定为已发布版本"
    if [[ "$configured" == v* ]]; then
        RELEASE_TAG="$configured"
    else
        RELEASE_TAG="v${configured}"
    fi
}

read_repository() {
    local configured
    configured="$(sed -n 's/^CANVAS_UPDATER_REPOSITORY=//p' "${INSTALL_DIR}/.env" | tail -n 1)"
    if [[ -z "$REQUESTED_REPOSITORY" && -n "$configured" ]]; then
        REPOSITORY="$configured"
    fi
    [[ "$REPOSITORY" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail "CANVAS_UPDATER_REPOSITORY 必须是 owner/repository"
}

upsert_env_value() {
    local key="$1" value="$2" temporary
    temporary="$(mktemp "${INSTALL_DIR}/.env.XXXXXX")"
    awk -v key="$key" -v value="$value" '
        BEGIN { updated=0 }
        index($0, key "=") == 1 { print key "=" value; updated=1; next }
        { print }
        END { if (!updated) print key "=" value }
    ' "${INSTALL_DIR}/.env" > "$temporary"
    chmod --reference="${INSTALL_DIR}/.env" "$temporary"
    mv "$temporary" "${INSTALL_DIR}/.env"
}

install_binary() {
    local arch asset temporary checksum_file expected
    case "$(uname -m)" in
        x86_64|amd64) arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) fail "不支持的 CPU 架构：$(uname -m)" ;;
    esac
    asset="open-ai-canvas-host-updater-linux-${arch}"
    temporary="$(mktemp)"
    checksum_file="$(mktemp)"
    curl -fsSL "https://github.com/${REPOSITORY}/releases/download/${RELEASE_TAG}/${asset}" -o "$temporary"
    curl -fsSL "https://github.com/${REPOSITORY}/releases/download/${RELEASE_TAG}/SHA256SUMS" -o "$checksum_file"
    expected="$(awk -v asset="$asset" '$2 == asset { print $1 }' "$checksum_file")"
    [[ "$expected" =~ ^[a-f0-9]{64}$ ]] || fail "Release 校验清单缺少 ${asset}"
    printf '%s  %s\n' "$expected" "$temporary" | sha256sum -c - >/dev/null || fail "Host Updater SHA-256 校验失败"
    install -m 0755 "$temporary" "$UPDATER_BIN"
    rm -f "$temporary" "$checksum_file"
}

ensure_token() {
    local token
    token="$(sed -n 's/^CANVAS_UPDATER_TOKEN=//p' "${INSTALL_DIR}/.env" | tail -n 1)"
    if [[ -z "$token" ]]; then
        token="$(openssl rand -hex 32)"
    fi
    [[ ${#token} -ge 32 ]] || fail "CANVAS_UPDATER_TOKEN 长度不足"
    upsert_env_value CANVAS_UPDATER_TOKEN "$token"
    upsert_env_value CANVAS_IMAGE_OWNER "${REPOSITORY%%/*}"
    upsert_env_value CANVAS_UPDATER_REPOSITORY "$REPOSITORY"
    upsert_env_value CANVAS_UPDATER_COMPOSE_FILE "$COMPOSE_FILE"
    upsert_env_value CANVAS_UPDATER_SOCKET_DIR "$SOCKET_DIR"
    umask 077
    printf 'CANVAS_UPDATER_TOKEN=%s\nCANVAS_UPDATER_REPOSITORY=%s\nCANVAS_UPDATER_INSTALL_DIR=%s\nCANVAS_UPDATER_COMPOSE_FILE=%s\nCANVAS_UPDATER_SOCKET=%s/updater.sock\n' "$token" "$REPOSITORY" "$INSTALL_DIR" "$COMPOSE_FILE" "$SOCKET_DIR" > "$UPDATER_ENV"
}

install_service() {
    local temporary_service
    install -d -m 0755 "$SOCKET_DIR"
    install -d -m 0700 /var/lib/open-ai-canvas-updater "${INSTALL_DIR}/backups"
    temporary_service="$(mktemp)"
    printf '%s\n' \
        '[Unit]' \
        'Description=Open AI Canvas Host Updater' \
        'After=docker.service network-online.target' \
        'Requires=docker.service' \
        'Wants=network-online.target' \
        '' \
        '[Service]' \
        'Type=simple' \
        "EnvironmentFile=${UPDATER_ENV}" \
        "ExecStart=${UPDATER_BIN}" \
        'Restart=on-failure' \
        'RestartSec=5s' \
        'NoNewPrivileges=true' \
        'PrivateTmp=true' \
        'ProtectHome=true' \
        'ProtectSystem=full' \
        "ReadWritePaths=${INSTALL_DIR} /var/lib/open-ai-canvas-updater ${SOCKET_DIR} /usr/local/bin" \
        '' \
        '[Install]' \
        'WantedBy=multi-user.target' > "$temporary_service"
    install -m 0644 "$temporary_service" "$UPDATER_SERVICE"
    rm -f "$temporary_service"
    systemctl daemon-reload
    systemctl enable --now open-ai-canvas-updater.service
    systemctl restart open-ai-canvas-updater.service
}

main() {
    require_root
    read_compose_file
    read_socket_dir
    read_image_tag
    read_repository
    install_binary
    ensure_token
    install_service
    printf 'Host Updater 已安装，更新仓库：%s，Compose：%s，Socket：%s/updater.sock\n' "$REPOSITORY" "$COMPOSE_FILE" "$SOCKET_DIR"
    printf '请重建 backend 容器，使 Token 与 Socket 挂载生效。\n'
}

main "$@"
