#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_ROOT="$(cd -- "$(dirname -- "$0")/.." && pwd -P)"
REPO_ROOT="${CANVAS_REPO_ROOT:-$SCRIPT_ROOT}"
REPO_ROOT="$(cd -- "$REPO_ROOT" && pwd -P)"
COMPOSE_FILE="$REPO_ROOT/docker-compose.local.yml"
DATA_DIR="$REPO_ROOT/.local/project-workbench-debug"

die() {
    printf '本地服务启动失败：%s\n' "$1" >&2
    exit 1
}

command -v docker >/dev/null 2>&1 || die '未找到 docker，请先安装并启动 Docker。'
[[ -f "$COMPOSE_FILE" ]] || die "未找到 Compose 文件：$COMPOSE_FILE"

mkdir -p "$DATA_DIR"

cd "$REPO_ROOT"
docker compose -f "$COMPOSE_FILE" up -d --build
docker compose -f "$COMPOSE_FILE" ps

printf '\n本地服务已启动。\n'
printf '前端：http://localhost:3000\n'
printf '迁移助手：Compose 内网 migration-helper:9714（不发布宿主机端口）\n'
