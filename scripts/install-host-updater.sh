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
REQUESTED_UPDATER_RELEASE_TAG="${CANVAS_UPDATER_RELEASE_TAG:-}"
REQUESTED_SOCKET_DIR="${CANVAS_UPDATER_SOCKET_DIR:-}"
SOCKET_DIR="${REQUESTED_SOCKET_DIR:-/run/open-ai-canvas-updater}"
UPDATER_BIN="/usr/local/bin/open-ai-canvas-host-updater"
UPDATER_TOKEN_HELPER="/usr/local/libexec/open-ai-canvas-updater-token"
UPDATER_ENV="/etc/open-ai-canvas-updater.env"
UPDATER_SERVICE="/etc/systemd/system/open-ai-canvas-updater.service"
DETECTED_IMAGE_TAG=""
DETECTED_IMAGE_OWNER=""
STAGED_UPDATER=""
INSTALL_TEMP_FILES=()
RESUME_UPDATER=false

cleanup_install() {
    local file
    for file in "${INSTALL_TEMP_FILES[@]}"; do [[ ! -f "$file" ]] || rm -f -- "$file"; done
    if [[ "$RESUME_UPDATER" == true ]]; then
        systemctl start open-ai-canvas-updater.service || true
    fi
}

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
    command -v flock >/dev/null 2>&1 || fail "缺少 flock（util-linux）"
    docker compose version >/dev/null 2>&1 || fail "缺少 Docker Compose"
}

resolve_install_dir() {
    if [[ -n "$REQUESTED_INSTALL_DIR" ]]; then
        return
    fi
    local candidate
    for candidate in "$COMPOSE_FILE" compose.yaml compose.yml docker-compose.yml docker-compose.yaml docker-compose.1panel.yml docker-compose.deploy.yml; do
        if [[ -f "${PWD}/${candidate}" ]]; then INSTALL_DIR="${PWD}"; return; fi
    done
}

existing_env_value() {
    [[ -f "${INSTALL_DIR}/.env" ]] || return 0
    sed -n "s/^${1}=//p" "${INSTALL_DIR}/.env" | tail -n 1 | tr -d '\r' | sed 's/^"//;s/"$//'
}

read_compose_file() {
    local configured candidate
    configured="$(existing_env_value CANVAS_UPDATER_COMPOSE_FILE)"
    if [[ -z "$REQUESTED_COMPOSE_FILE" && -n "$configured" ]]; then
        COMPOSE_FILE="$configured"
    elif [[ -z "$REQUESTED_COMPOSE_FILE" ]]; then
        local candidates=()
        for candidate in compose.yaml compose.yml docker-compose.yml docker-compose.yaml docker-compose.1panel.yml docker-compose.deploy.yml; do
            [[ ! -f "${INSTALL_DIR}/${candidate}" ]] || candidates+=("$candidate")
        done
        [[ ${#candidates[@]} -eq 1 ]] || fail "发现零个或多个编排文件，请用 CANVAS_UPDATER_COMPOSE_FILE 指定现有文件，不要复制或改名"
        COMPOSE_FILE="${candidates[0]}"
    fi
    [[ "$COMPOSE_FILE" =~ ^[A-Za-z0-9._-]+\.ya?ml$ ]] || fail "CANVAS_UPDATER_COMPOSE_FILE 必须是安装目录中的 YAML 文件名"
    [[ -f "${INSTALL_DIR}/${COMPOSE_FILE}" ]] || fail "未找到 ${INSTALL_DIR}/${COMPOSE_FILE}"
}

compose_command() {
    local args=(docker compose --project-directory "$INSTALL_DIR")
    [[ ! -f "${INSTALL_DIR}/.env" ]] || args+=(--env-file "${INSTALL_DIR}/.env")
    "${args[@]}" -f "${INSTALL_DIR}/${COMPOSE_FILE}" "$@"
}

read_deployment_images() {
    local images image owner tag kind seen_backend=false seen_web=false
    images="$(compose_command config --images 2>/dev/null)" || fail "无法解析编排镜像，请先在服务器检查 docker compose config"
    while IFS= read -r image; do
        if [[ "$image" =~ ^ghcr\.io/([A-Za-z0-9_.-]+)/open-ai-canvas-(backend|web):([A-Za-z0-9_][A-Za-z0-9_.-]*)$ ]]; then
            owner="${BASH_REMATCH[1]}"; kind="${BASH_REMATCH[2]}"; tag="${BASH_REMATCH[3]}"
            [[ -z "$DETECTED_IMAGE_TAG" || ( "$DETECTED_IMAGE_TAG" == "$tag" && "$DETECTED_IMAGE_OWNER" == "$owner" ) ]] || fail "前后端镜像的仓库或版本不一致"
            DETECTED_IMAGE_TAG="$tag"; DETECTED_IMAGE_OWNER="$owner"
            if [[ "$kind" == backend ]]; then seen_backend=true; else seen_web=true; fi
        fi
    done <<< "$images"
    [[ "$seen_backend" == true && "$seen_web" == true ]] || fail "编排需要包含 GHCR 的影策 backend/web 镜像"
}

read_release_compose_file() {
    local configured volumes
    configured="$(existing_env_value CANVAS_UPDATER_RELEASE_COMPOSE_FILE)"
    if [[ -z "$REQUESTED_RELEASE_COMPOSE_FILE" && -z "$configured" && "$COMPOSE_FILE" != docker-compose.1panel.yml && "$COMPOSE_FILE" != docker-compose.deploy.yml ]]; then
        volumes="$(compose_command config --volumes 2>/dev/null)" || fail "无法识别编排数据卷"
        if grep -qx deployment-secrets <<< "$volumes"; then configured=docker-compose.1panel.yml; else configured=docker-compose.deploy.yml; fi
    fi
    RELEASE_COMPOSE_FILE="${REQUESTED_RELEASE_COMPOSE_FILE:-${configured:-$COMPOSE_FILE}}"
    case "$RELEASE_COMPOSE_FILE" in
        docker-compose.1panel.yml|docker-compose.deploy.yml) ;;
        *) fail "请用 CANVAS_UPDATER_RELEASE_COMPOSE_FILE 指定 docker-compose.1panel.yml 或 docker-compose.deploy.yml，避免下载本地开发编排" ;;
    esac
}

read_image_tag() {
    local configured
    configured="${REQUESTED_IMAGE_TAG:-${DETECTED_IMAGE_TAG:-$(existing_env_value CANVAS_IMAGE_TAG)}}"
    if [[ -z "$configured" || "$configured" == "latest" ]]; then
        local release_url release_tag
        release_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPOSITORY}/releases/latest" || true)"
        release_tag="${release_url##*/}"
        [[ "$release_tag" == v* && "${release_tag#v}" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || fail "无法解析 ${REPOSITORY} 的最新 Release，请先将 CANVAS_IMAGE_TAG 固定为已发布版本"
        RELEASE_TAG="$release_tag"
        printf 'CANVAS_IMAGE_TAG=latest，已固定到最新 Release %s\n' "$RELEASE_TAG"
        return
    fi
    configured="${configured#v}"
    [[ "$configured" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || fail "CANVAS_IMAGE_TAG 不是有效的 Docker 镜像标签"
    RELEASE_TAG="v${configured}"
}

read_repository() {
    local configured
    configured="$(existing_env_value CANVAS_UPDATER_REPOSITORY)"
    configured="${configured:-${DETECTED_IMAGE_OWNER}/open-ai-canvas}"
    if [[ -z "$REQUESTED_REPOSITORY" && -n "$configured" ]]; then
        REPOSITORY="$configured"
    fi
    [[ "$REPOSITORY" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail "CANVAS_UPDATER_REPOSITORY 必须是 owner/repository"
}

read_updater_release_tag() {
    UPDATER_RELEASE_TAG="$REQUESTED_UPDATER_RELEASE_TAG"
    if [[ -z "$UPDATER_RELEASE_TAG" ]]; then
        local release_url
        release_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPOSITORY}/releases/latest")" || fail "无法读取 Host Updater 最新 Release"
        UPDATER_RELEASE_TAG="${release_url##*/}"
    fi
    [[ "$UPDATER_RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9_.-]+)?$ ]] || fail "CANVAS_UPDATER_RELEASE_TAG 必须是有效 Release 版本"
}

install_binary() {
    local arch asset temporary checksum_file expected
    case "$(uname -m)" in
        x86_64|amd64) arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) fail "不支持的 CPU 架构：$(uname -m)" ;;
    esac
    asset="open-ai-canvas-host-updater-linux-${arch}"
    install -d -m 0755 "$(dirname "$UPDATER_BIN")"
    temporary="$(mktemp "$(dirname "$UPDATER_BIN")/.canvas-updater-download.XXXXXX")"
    checksum_file="$(mktemp)"
    INSTALL_TEMP_FILES+=("$temporary" "$checksum_file")
    curl -fsSL "https://github.com/${REPOSITORY}/releases/download/${UPDATER_RELEASE_TAG}/${asset}" -o "$temporary"
    curl -fsSL "https://github.com/${REPOSITORY}/releases/download/${UPDATER_RELEASE_TAG}/SHA256SUMS" -o "$checksum_file"
    expected="$(awk -v asset="$asset" '$2 == asset { print $1 }' "$checksum_file")"
    [[ "$expected" =~ ^[a-f0-9]{64}$ ]] || fail "Release 校验清单缺少 ${asset}"
    printf '%s  %s\n' "$expected" "$temporary" | sha256sum -c - >/dev/null || fail "Host Updater SHA-256 校验失败"
    chmod 0755 "$temporary"
    STAGED_UPDATER="$temporary"
    rm -f "$checksum_file"
}

configure_deployment() {
    local capabilities temporary_state output key value
    temporary_state="$(mktemp -d)"
    capabilities="$(CANVAS_UPDATER_TOKEN='' CANVAS_UPDATER_STATE_DIR="$temporary_state/state" CANVAS_UPDATER_BACKUP_DIR="$temporary_state/backups" "$STAGED_UPDATER" capabilities 2>/dev/null || true)"
    rmdir "$temporary_state/state" "$temporary_state/backups" "$temporary_state" 2>/dev/null || true
    [[ "$capabilities" == compose-config-v1 ]] || fail "该 Release 的 Host Updater 不支持自动编排配置，请使用包含 compose-config-v1 的新版 Release"
    # Check before stopping, then configure checks again with the service stopped.
    "$STAGED_UPDATER" check-install || fail "存在正在执行或未处理的更新/迁移，未停止服务"
    if systemctl is-active --quiet open-ai-canvas-updater.service; then
        RESUME_UPDATER=true
        systemctl stop open-ai-canvas-updater.service
    fi
    output="$(CANVAS_UPDATER_INSTALL_DIR="$INSTALL_DIR" CANVAS_UPDATER_COMPOSE_FILE="$COMPOSE_FILE" \
        CANVAS_UPDATER_REPOSITORY="$REPOSITORY" CANVAS_UPDATER_RELEASE_COMPOSE_FILE="$RELEASE_COMPOSE_FILE" \
        CANVAS_UPDATER_IMAGE_TAG="${RELEASE_TAG#v}" CANVAS_UPDATER_SOCKET_DIR="$REQUESTED_SOCKET_DIR" \
        "$STAGED_UPDATER" configure)" || fail "自动配置失败，未启动或重建业务服务"
    while IFS='=' read -r key value; do
        case "$key" in SOCKET_DIR) SOCKET_DIR="$value" ;; *) fail "自动配置返回了未知字段" ;; esac
    done <<< "$output"
    [[ "$SOCKET_DIR" =~ ^/[A-Za-z0-9._/-]+$ && "$SOCKET_DIR" != / && "$SOCKET_DIR" != *..* ]] || fail "自动配置返回的 Socket 目录无效"
    local staged_binary
    staged_binary="$(mktemp "${UPDATER_BIN}.XXXXXX")"
    INSTALL_TEMP_FILES+=("$staged_binary")
    install -m 0755 "$STAGED_UPDATER" "$staged_binary"
    mv -f "$staged_binary" "$UPDATER_BIN"
    rm -f "$STAGED_UPDATER"
    STAGED_UPDATER=""
}

ensure_token() {
    local token temporary_token token_file
    token="$(existing_env_value CANVAS_UPDATER_TOKEN)"
    if [[ -z "$token" && -s "${SOCKET_DIR}/token" ]]; then token="$(cat "${SOCKET_DIR}/token")"; fi
    if [[ -z "$token" ]]; then
        token="$(openssl rand -hex 32)"
    fi
    [[ "$token" =~ ^[A-Za-z0-9._~+/=-]{32,}$ ]] || fail "CANVAS_UPDATER_TOKEN 必须至少 32 位，且不能包含空白、引号或控制字符"
    install -d -m 0755 "$SOCKET_DIR"
    umask 077
    token_file="${SOCKET_DIR}/token"
    temporary_token="$(mktemp "${SOCKET_DIR}/.token.XXXXXX")"
    printf '%s\n' "$token" > "$temporary_token"
    chmod 0444 "$temporary_token"
    mv -f "$temporary_token" "$token_file"
    printf 'CANVAS_UPDATER_TOKEN=%s\nCANVAS_UPDATER_TOKEN_FILE=%s/token\nCANVAS_UPDATER_REPOSITORY=%s\nCANVAS_UPDATER_INSTALL_DIR=%s\nCANVAS_UPDATER_BACKUP_DIR=%s/backups\nCANVAS_UPDATER_COMPOSE_FILE=%s\nCANVAS_UPDATER_SOCKET=%s/updater.sock\n' "$token" "$SOCKET_DIR" "$REPOSITORY" "$INSTALL_DIR" "$INSTALL_DIR" "$COMPOSE_FILE" "$SOCKET_DIR" > "$UPDATER_ENV"
    printf 'CANVAS_UPDATER_RELEASE_COMPOSE_FILE=%s\n' "$RELEASE_COMPOSE_FILE" >> "$UPDATER_ENV"
    printf 'CANVAS_UPDATER_CONFIG_SOURCE=compose\n' >> "$UPDATER_ENV"
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
    RESUME_UPDATER=false
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
    local attempt
    for attempt in {1..30}; do
        if compose_command exec -T backend sh -ec \
            'test -r /run/open-ai-canvas-updater/token && test -S /run/open-ai-canvas-updater/updater.sock' >/dev/null 2>&1; then
            return
        fi
        sleep 1
    done
    fail "backend 容器未看到 Host Updater Token 或 Unix Socket，请确认 1Panel 使用的编排文件是 ${COMPOSE_FILE}"
}

recreate_backend() {
    printf '正在拉取 backend/web 镜像并重建容器，使更新器 Token 与 Socket 挂载生效...\n'
    compose_command pull backend web || fail "backend/web 镜像拉取失败，请检查镜像仓库权限和 CANVAS_IMAGE_TAG"
    compose_command up -d --force-recreate backend web || fail "backend/web 容器重建失败，请检查 1Panel 当前使用的 Compose 文件是否为 ${COMPOSE_FILE}"
    verify_backend_mount
}

main() {
    require_root
    exec 9>/run/open-ai-canvas-updater-install.lock
    flock -n 9 || fail "另一个安装进程正在运行"
    trap cleanup_install EXIT
    resolve_install_dir
    [[ "$INSTALL_DIR" =~ ^/[A-Za-z0-9._/-]+$ && "$INSTALL_DIR" != / && "$INSTALL_DIR" != *..* ]] || fail "INSTALL_DIR 必须是无空格的独立绝对目录"
    read_compose_file
    read_deployment_images
    read_release_compose_file
    read_repository
    read_image_tag
    read_updater_release_tag
    printf '将备份原编排、自动配置 Socket 挂载并重建 backend/web；此配置备份不是完整数据备份，请先保留业务数据恢复点。\n'
    printf '应用版本：%s；Host Updater 版本：%s（独立选择，不自动升级已固定的应用版本）。\n' "$RELEASE_TAG" "$UPDATER_RELEASE_TAG"
    install_binary
    configure_deployment
    ensure_token
    install_token_helper
    install_service
    verify_updater
    recreate_backend
    printf 'Host Updater 已安装，更新仓库：%s，Compose：%s，Socket：%s/updater.sock\n' "$REPOSITORY" "$COMPOSE_FILE" "$SOCKET_DIR"
    printf 'Token 文件：%s/token\n' "$SOCKET_DIR"
    printf '宿主机认证检查与 backend 挂载检查已通过。\n'
    printf '无需手工挂载二进制或填写 .env；后续业务配置直接维护原编排。\n'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
