#!/usr/bin/env bash

set -Eeuo pipefail

REQUESTED_INSTALL_DIR="${INSTALL_DIR:-}"
INSTALL_DIR="${REQUESTED_INSTALL_DIR:-/opt/open-ai-canvas}"
REQUESTED_REPOSITORY="${CANVAS_UPDATER_REPOSITORY:-${REPOSITORY:-}}"
REPOSITORY="${REQUESTED_REPOSITORY:-buttonslaybaugh397-art/open-ai-canvas}"
REQUESTED_COMPOSE_FILE="${CANVAS_UPDATER_COMPOSE_FILE:-}"
COMPOSE_FILE="${REQUESTED_COMPOSE_FILE:-docker-compose.deploy.yml}"
REQUESTED_RELEASE_COMPOSE_FILE="${CANVAS_UPDATER_RELEASE_COMPOSE_FILE:-}"
RELEASE_COMPOSE_FILE=""
REQUESTED_IMAGE_TAG="${CANVAS_IMAGE_TAG:-}"
REQUESTED_SOCKET_DIR="${CANVAS_UPDATER_SOCKET_DIR:-}"
SOCKET_DIR="${REQUESTED_SOCKET_DIR:-/run/open-ai-canvas-updater}"
UPDATER_BIN="/usr/local/bin/open-ai-canvas-host-updater"
UPDATER_TOKEN_HELPER="/usr/local/libexec/open-ai-canvas-updater-token"
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
    command -v docker >/dev/null 2>&1 || fail "缺少 Docker"
    docker compose version >/dev/null 2>&1 || fail "缺少 Docker Compose"
}

resolve_install_dir() {
    if [[ -n "$REQUESTED_INSTALL_DIR" ]]; then
        return
    fi
    if [[ -f "${PWD}/.env" && ( -f "${PWD}/${COMPOSE_FILE}" || -f "${PWD}/docker-compose.1panel.yml" || -f "${PWD}/docker-compose.deploy.yml" ) ]]; then
        INSTALL_DIR="${PWD}"
    fi
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

read_release_compose_file() {
    local configured
    configured="$(sed -n 's/^CANVAS_UPDATER_RELEASE_COMPOSE_FILE=//p' "${INSTALL_DIR}/.env" | tail -n 1)"
    RELEASE_COMPOSE_FILE="${REQUESTED_RELEASE_COMPOSE_FILE:-${configured:-$COMPOSE_FILE}}"
    case "$RELEASE_COMPOSE_FILE" in
        docker-compose.1panel.yml|docker-compose.deploy.yml) ;;
        *) fail "请用 CANVAS_UPDATER_RELEASE_COMPOSE_FILE 指定 docker-compose.1panel.yml 或 docker-compose.deploy.yml，避免下载本地开发编排" ;;
    esac
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
    configured="${REQUESTED_IMAGE_TAG:-$configured}"
    if [[ -z "$configured" || "$configured" == "latest" ]]; then
        local release_url release_tag
        release_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPOSITORY}/releases/latest" || true)"
        release_tag="${release_url##*/}"
        [[ "$release_tag" == v* && "${release_tag#v}" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || fail "无法解析 ${REPOSITORY} 的最新 Release，请先将 CANVAS_IMAGE_TAG 固定为已发布版本"
        RELEASE_TAG="$release_tag"
        upsert_env_value CANVAS_IMAGE_TAG "${RELEASE_TAG#v}"
        printf 'CANVAS_IMAGE_TAG=latest，已固定到最新 Release %s\n' "$RELEASE_TAG"
        return
    fi
    configured="${configured#v}"
    [[ "$configured" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || fail "CANVAS_IMAGE_TAG 不是有效的 Docker 镜像标签"
    RELEASE_TAG="v${configured}"
    if [[ "$(sed -n 's/^CANVAS_IMAGE_TAG=//p' "${INSTALL_DIR}/.env" | tail -n 1)" != "$configured" ]]; then
        upsert_env_value CANVAS_IMAGE_TAG "$configured"
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
    local token temporary_token token_file
    token="$(sed -n 's/^CANVAS_UPDATER_TOKEN=//p' "${INSTALL_DIR}/.env" | tail -n 1)"
    if [[ -z "$token" ]]; then
        token="$(openssl rand -hex 32)"
    fi
    [[ ${#token} -ge 32 ]] || fail "CANVAS_UPDATER_TOKEN 长度不足"
    upsert_env_value CANVAS_UPDATER_TOKEN "$token"
    upsert_env_value CANVAS_IMAGE_OWNER "${REPOSITORY%%/*}"
    upsert_env_value CANVAS_UPDATER_REPOSITORY "$REPOSITORY"
    upsert_env_value CANVAS_UPDATER_COMPOSE_FILE "$COMPOSE_FILE"
    upsert_env_value CANVAS_UPDATER_RELEASE_COMPOSE_FILE "$RELEASE_COMPOSE_FILE"
    upsert_env_value CANVAS_UPDATER_SOCKET_DIR "$SOCKET_DIR"
    upsert_env_value CANVAS_UPDATER_TOKEN_FILE "/run/open-ai-canvas-updater/token"
    install -d -m 0755 "$SOCKET_DIR"
    umask 077
    token_file="${SOCKET_DIR}/token"
    temporary_token="$(mktemp "${SOCKET_DIR}/.token.XXXXXX")"
    printf '%s\n' "$token" > "$temporary_token"
    chmod 0444 "$temporary_token"
    mv -f "$temporary_token" "$token_file"
    printf 'CANVAS_UPDATER_TOKEN=%s\nCANVAS_UPDATER_TOKEN_FILE=%s/token\nCANVAS_UPDATER_REPOSITORY=%s\nCANVAS_UPDATER_INSTALL_DIR=%s\nCANVAS_UPDATER_BACKUP_DIR=%s/backups\nCANVAS_UPDATER_COMPOSE_FILE=%s\nCANVAS_UPDATER_SOCKET=%s/updater.sock\n' "$token" "$SOCKET_DIR" "$REPOSITORY" "$INSTALL_DIR" "$INSTALL_DIR" "$COMPOSE_FILE" "$SOCKET_DIR" > "$UPDATER_ENV"
    printf 'CANVAS_UPDATER_RELEASE_COMPOSE_FILE=%s\n' "$RELEASE_COMPOSE_FILE" >> "$UPDATER_ENV"
    chmod 0600 "$UPDATER_ENV"
}

install_token_helper() {
    local temporary
    install -d -m 0755 "$(dirname "$UPDATER_TOKEN_HELPER")"
    temporary="$(mktemp)"
    printf '%s\n' \
        '#!/bin/sh' \
        'set -eu' \
        ': "${CANVAS_UPDATER_TOKEN:?CANVAS_UPDATER_TOKEN is required}"' \
        ': "${CANVAS_UPDATER_TOKEN_FILE:?CANVAS_UPDATER_TOKEN_FILE is required}"' \
        'directory="$(dirname "$CANVAS_UPDATER_TOKEN_FILE")"' \
        'install -d -m 0755 "$directory"' \
        'umask 077' \
        'temporary="$(mktemp "${CANVAS_UPDATER_TOKEN_FILE}.tmp.XXXXXX")"' \
        'trap '\''rm -f "$temporary"'\'' EXIT HUP INT TERM' \
        'printf "%s\n" "$CANVAS_UPDATER_TOKEN" > "$temporary"' \
        'chmod 0444 "$temporary"' \
        'mv -f "$temporary" "$CANVAS_UPDATER_TOKEN_FILE"' \
        'trap - EXIT HUP INT TERM' > "$temporary"
    install -m 0755 "$temporary" "$UPDATER_TOKEN_HELPER"
    rm -f "$temporary"
}

install_service() {
    local temporary_service
    install -d -m 0755 "$SOCKET_DIR"
    install -d -m 0700 /var/lib/open-ai-canvas-updater "${INSTALL_DIR}/backups"
    temporary_service="$(mktemp)"
    # /run can be absent after reboot; allow ExecStartPre to recreate the directory.
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
        "ExecStartPre=${UPDATER_TOKEN_HELPER}" \
        "ExecStart=${UPDATER_BIN}" \
        'Restart=on-failure' \
        'RestartSec=5s' \
        'NoNewPrivileges=true' \
        'PrivateTmp=true' \
        'ProtectHome=true' \
        'ProtectSystem=full' \
        "ReadWritePaths=${INSTALL_DIR} /var/lib/open-ai-canvas-updater -${SOCKET_DIR} /usr/local/bin" \
        '' \
        '[Install]' \
        'WantedBy=multi-user.target' > "$temporary_service"
    install -m 0644 "$temporary_service" "$UPDATER_SERVICE"
    rm -f "$temporary_service"
    systemctl daemon-reload
    systemctl enable --now open-ai-canvas-updater.service
    systemctl restart open-ai-canvas-updater.service
}

verify_updater() {
    local token socket_path attempt
    socket_path="${SOCKET_DIR}/updater.sock"
    token="$(cat "${SOCKET_DIR}/token" 2>/dev/null || true)"
    [[ -s "${SOCKET_DIR}/token" ]] || fail "Host Updater Token 文件不存在：${SOCKET_DIR}/token"
    [[ ${#token} -ge 32 ]] || fail "Host Updater Token 文件内容无效：${SOCKET_DIR}/token"
    for attempt in {1..30}; do
        if systemctl is-active --quiet open-ai-canvas-updater.service && [[ -S "$socket_path" ]] && \
            printf 'Authorization: Bearer %s\n' "$token" | \
            curl --fail --silent --max-time 3 --noproxy '*' --unix-socket "$socket_path" \
                --header @- \
                http://localhost/v1/status >/dev/null 2>&1; then
            return
        fi
        sleep 1
    done
    if ! systemctl is-active --quiet open-ai-canvas-updater.service; then
        fail "Host Updater systemd 服务未运行，请执行 journalctl -u open-ai-canvas-updater -n 100 --no-pager"
    fi
    if [[ ! -S "$socket_path" ]]; then
        fail "Host Updater Unix Socket 不存在：${socket_path}"
    fi
    fail "Host Updater Unix Socket 无法完成认证请求，请检查 Token 是否与 backend 编排一致"
}

verify_backend_mount() {
    local compose=(docker compose --env-file "${INSTALL_DIR}/.env" -f "${INSTALL_DIR}/${COMPOSE_FILE}")
    local attempt
    for attempt in {1..30}; do
        if "${compose[@]}" exec -T backend sh -ec \
            'test -r /run/open-ai-canvas-updater/token && test -S /run/open-ai-canvas-updater/updater.sock' >/dev/null 2>&1; then
            return
        fi
        sleep 1
    done
    fail "backend 容器未看到 Host Updater Token 或 Unix Socket，请确认 1Panel 使用的编排文件是 ${COMPOSE_FILE}"
}

recreate_backend() {
    printf '正在拉取 backend/web 镜像并重建容器，使更新器 Token 与 Socket 挂载生效...\n'
    local compose=(docker compose --env-file "${INSTALL_DIR}/.env" -f "${INSTALL_DIR}/${COMPOSE_FILE}")
    "${compose[@]}" pull backend web || fail "backend/web 镜像拉取失败，请检查镜像仓库权限和 CANVAS_IMAGE_TAG"
    "${compose[@]}" up -d --force-recreate backend web || fail "backend/web 容器重建失败，请检查 1Panel 当前使用的 Compose 文件是否为 ${COMPOSE_FILE}"
    verify_backend_mount
}

main() {
    require_root
    resolve_install_dir
    [[ -f "${INSTALL_DIR}/.env" ]] || fail "未找到 ${INSTALL_DIR}/.env，请在 Compose 所在目录执行，或设置 INSTALL_DIR"
    read_compose_file
    read_release_compose_file
    read_socket_dir
    read_repository
    read_image_tag
    install_binary
    ensure_token
    install_token_helper
    install_service
    verify_updater
    recreate_backend
    printf 'Host Updater 已安装，更新仓库：%s，Compose：%s，Socket：%s/updater.sock\n' "$REPOSITORY" "$COMPOSE_FILE" "$SOCKET_DIR"
    printf 'Token 文件：%s/token\n' "$SOCKET_DIR"
    printf '宿主机认证检查与 backend 挂载检查已通过。\n'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
