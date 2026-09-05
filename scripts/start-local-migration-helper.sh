#!/usr/bin/env bash

set -Eeuo pipefail

TARGET_ROOT="${1-}"
SCRIPT_ROOT="$(cd -- "$(dirname -- "$0")/.." && pwd -P)"
REPO_ROOT="${TARGET_ROOT:-$SCRIPT_ROOT}"
REPO_ROOT="$(cd -- "$REPO_ROOT" && pwd -P)"
BACKEND_DIR="$REPO_ROOT/backend"
HELPER_DIR="$REPO_ROOT/.local/migration-helper"
TOKEN_FILE="$HELPER_DIR/token"
BINARY="$HELPER_DIR/open-ai-canvas-local-migration-helper"
STDOUT_LOG="$HELPER_DIR/helper.stdout.log"
STDERR_LOG="$HELPER_DIR/helper.stderr.log"

die() {
    printf '本地迁移助手启动失败：%s\n' "$1" >&2
    exit 1
}

command -v go >/dev/null 2>&1 || die '未找到 go。'
command -v docker >/dev/null 2>&1 || die '未找到 docker，请先安装并启动 Docker。'
[[ -d "$BACKEND_DIR" ]] || die "未找到 backend 目录：$BACKEND_DIR"

mkdir -p "$HELPER_DIR"
if [[ ! -f "$TOKEN_FILE" ]]; then
    umask 077
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 32 > "$TOKEN_FILE"
    else
        od -An -N32 -tx1 /dev/urandom | tr -d ' \n' > "$TOKEN_FILE"
    fi
    printf '\n' >> "$TOKEN_FILE"
fi
chmod 600 "$TOKEN_FILE"

if [[ -f "$HELPER_DIR/helper.pid" ]]; then
    old_pid="$(cat "$HELPER_DIR/helper.pid" 2>/dev/null || true)"
    if [[ "$old_pid" =~ ^[0-9]+$ ]] && kill -0 "$old_pid" 2>/dev/null; then
        printf '本地迁移助手已在运行（PID %s）。\n' "$old_pid"
        exit 0
    fi
    rm -f "$HELPER_DIR/helper.pid"
fi

if command -v ss >/dev/null 2>&1 && ss -ltn '( sport = :9714 )' | tail -n +2 | grep -q .; then
    die '端口 9714 已被其他进程占用。'
fi

needs_rebuild=0
if [[ ! -x "$BINARY" ]]; then
    needs_rebuild=1
elif find "$BACKEND_DIR/cmd/local-migration-helper" "$BACKEND_DIR/internal/hostupdate" -type f -name '*.go' -newer "$BINARY" -print -quit | grep -q .; then
    needs_rebuild=1
fi
if [[ "$needs_rebuild" -eq 1 ]]; then
    (cd "$BACKEND_DIR" && go build -o "$BINARY" ./cmd/local-migration-helper) || die 'Go 迁移助手构建失败。'
fi

nohup "$BINARY" -root "$REPO_ROOT" -address '0.0.0.0:9714' -token-file "$TOKEN_FILE" > "$STDOUT_LOG" 2> "$STDERR_LOG" < /dev/null &
pid=$!
printf '%s\n' "$pid" > "$HELPER_DIR/helper.pid"
sleep 1
if ! kill -0 "$pid" 2>/dev/null; then
    detail="$(tail -n 30 "$STDERR_LOG" 2>/dev/null || true)"
    rm -f "$HELPER_DIR/helper.pid"
    die "$detail"
fi
printf '本地迁移助手已启动（PID %s）。\n' "$pid"
