#!/usr/bin/env bash

set -Eeuo pipefail

ACTION="${1-}"
REQUESTED_ARCHIVE_PATH="${2-}"
TARGET_ROOT="${3-}"

if [[ "$ACTION" != "export" && "$ACTION" != "import" ]]; then
    printf '用法：%s export|import [迁移包路径] [仓库目录]\n' "$0" >&2
    exit 2
fi

SCRIPT_ROOT="$(cd -- "$(dirname -- "$0")/.." && pwd -P)"
REPO_ROOT="${TARGET_ROOT:-$SCRIPT_ROOT}"
REPO_ROOT="$(cd -- "$REPO_ROOT" && pwd -P)"
COMPOSE_FILE="$REPO_ROOT/docker-compose.local.yml"
COMPOSE_HOST_ROOT="${CANVAS_HOST_REPOSITORY_ROOT:-$REPO_ROOT}"
DATA_DIR="$REPO_ROOT/.local/project-workbench-debug"
MIGRATION_DIR="$REPO_ROOT/.local/migrations"
CONFIG_FILES=(docker-compose.local.yml docker-compose.build.yml Dockerfile nginx.conf VERSION .env)

die() {
    printf '本地迁移失败：%s\n' "$1" >&2
    exit 1
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || die "未找到 $1，请先安装对应运行时。"
}

require_common() {
    require_command docker
    [[ -f "$COMPOSE_FILE" ]] || die "当前目录不是 open-ai-canvas 仓库：$REPO_ROOT"
    if [[ -z "${COMPOSE_PROJECT_NAME:-}" ]]; then
        COMPOSE_PROJECT_NAME="$(docker ps -a --filter label=com.docker.compose.service=backend --format '{{.Label "com.docker.compose.project"}}' | head -n 1)"
        if [[ -n "$COMPOSE_PROJECT_NAME" ]]; then
            export COMPOSE_PROJECT_NAME
        fi
    fi
}

run_compose() {
    (cd -- "$REPO_ROOT" && CANVAS_HOST_REPOSITORY_ROOT="$COMPOSE_HOST_ROOT" docker compose -f "$COMPOSE_FILE" "$@")
}

compose_up() {
    local -a services=()
    if [[ "${CANVAS_LOCAL_MIGRATION_HELPER_CONTAINER:-0}" == "1" ]]; then
        services=(backend web)
    fi
    run_compose up -d --force-recreate --remove-orphans --wait --wait-timeout 600 "${services[@]}"
}

running_services() {
    run_compose ps --status running -q backend web
}

sha256_file() {
    sha256sum "$1" | awk '{print $1}'
}

file_size() {
    stat -c '%s' "$1" 2>/dev/null || stat -f '%z' "$1"
}

absolute_archive_path() {
    local archive="$1"
    if [[ -z "$archive" ]]; then
        mkdir -p "$MIGRATION_DIR"
        archive="$MIGRATION_DIR/open-ai-canvas-migration-$(date -u +%Y%m%d-%H%M%S).zip"
    elif [[ "$archive" != /* ]]; then
        archive="$REPO_ROOT/$archive"
    fi
    mkdir -p "$(dirname -- "$archive")"
    printf '%s\n' "$archive"
}

image_names() {
    run_compose config --images | awk 'NF && !seen[$0]++ { print }'
}

copy_config_to_archive() {
    local work="$1" relative source destination
    mkdir -p "$work/service-config"
    for relative in "${CONFIG_FILES[@]}"; do
        source="$REPO_ROOT/$relative"
        if [[ ! -f "$source" ]]; then
            [[ "$relative" == ".env" ]] && die "缺少 $source，请先配置本地服务环境。"
            continue
        fi
        destination="$work/service-config/$relative"
        mkdir -p "$(dirname -- "$destination")"
        cp -a "$source" "$destination"
    done
}

write_manifest() {
    local work="$1" version="$2"
    python3 - "$work" "$version" <<'PY'
import hashlib
import json
import sys
from pathlib import Path

root = Path(sys.argv[1]).resolve()
version = sys.argv[2].strip() or "local"
files = []
for path in sorted(root.rglob("*")):
    if not path.is_file() or path.name == "manifest.json" or path.is_symlink():
        continue
    hasher = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            hasher.update(chunk)
    files.append({
        "path": path.relative_to(root).as_posix(),
        "size": path.stat().st_size,
        "sha256": hasher.hexdigest(),
    })
manifest = {
    "schemaVersion": 1,
    "createdAt": __import__("datetime").datetime.now(__import__("datetime").timezone.utc).isoformat(),
    "version": version,
    "databaseDriver": "sqlite",
    "composeFile": "docker-compose.local.yml",
    "files": files,
}
(root / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, separators=(",", ":")) + "\n", encoding="utf-8")
PY
}

validate_manifest() {
    local root="$1"
    python3 - "$root" <<'PY'
import hashlib
import json
import os
import re
import sys
from pathlib import Path, PurePosixPath

root = Path(sys.argv[1]).resolve()

def fail(message):
    raise SystemExit(message)

def safe_name(value):
    return isinstance(value, str) and value and "\\" not in value and not value.startswith("/") and ":" not in value and ".." not in PurePosixPath(value).parts

manifest_path = root / "manifest.json"
if not manifest_path.is_file() or manifest_path.is_symlink():
    fail("迁移包缺少 manifest.json。")
try:
    manifest = json.loads(manifest_path.read_text(encoding="utf-8-sig"))
except Exception as exc:
    fail(f"解析迁移包清单失败：{exc}")
if manifest.get("schemaVersion") != 1:
    fail("迁移包 schemaVersion 不受支持。")
if not str(manifest.get("version", "")).strip():
    fail("迁移包缺少来源版本。")
if manifest.get("databaseDriver") != "sqlite":
    fail("迁移包必须使用 SQLite。")
items = manifest.get("files")
if not isinstance(items, list) or not items:
    fail("迁移包清单为空。")

listed = {}
for item in items:
    if not isinstance(item, dict):
        fail("迁移包清单包含无效文件。")
    name = item.get("path")
    if name == "manifest.json" or not safe_name(name) or name in listed:
        fail(f"迁移包清单包含无效或重复路径：{name}")
    size = item.get("size")
    digest = item.get("sha256")
    if not isinstance(size, int) or size < 0 or not isinstance(digest, str) or not re.fullmatch(r"[0-9a-fA-F]{64}", digest):
        fail(f"迁移包清单包含无效文件：{name}")
    path = root / PurePosixPath(name)
    if path.is_symlink() or not path.is_file():
        fail(f"迁移包文件缺失：{name}")
    if path.stat().st_size != size:
        fail(f"迁移包文件大小不匹配：{name}")
    hasher = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            hasher.update(chunk)
    if hasher.hexdigest().lower() != digest.lower():
        fail(f"迁移包 SHA-256 校验失败：{name}")
    listed[name] = True

for required in ("images.tar", "service-config/.env"):
    if required not in listed:
        fail(f"迁移包缺少 {required}。")
if not (root / "data" / "project-workbench-debug").is_dir():
    fail("迁移包缺少 SQLite 数据目录。")

actual = set()
for current, directories, files in os.walk(root, followlinks=False):
    for directory in directories:
        if Path(current, directory).is_symlink():
            fail("迁移包包含符号链接。")
    for filename in files:
        path = Path(current, filename)
        if path.is_symlink():
            fail("迁移包包含符号链接。")
        relative = path.relative_to(root).as_posix()
        if relative != "manifest.json":
            actual.add(relative)
if actual != set(listed):
    fail("迁移包文件清单不完整。")
PY
}

assert_safe_zip_entries() {
    local archive="$1" entry
    require_command unzip
    while IFS= read -r entry; do
        [[ -n "$entry" ]] || continue
        [[ "$entry" != /* && "$entry" != *$'\r'* && "$entry" != *\\* && "$entry" != *:* ]] || die "迁移包包含不安全路径：$entry"
        [[ "$entry" != ".." && "$entry" != ../* && "$entry" != */../* && "$entry" != */.. ]] || die "迁移包包含不安全路径：$entry"
    done < <(unzip -Z1 "$archive")
}

export_migration() {
    local archive work version image
    local was_running=0
    local -a images
    require_common
    require_command zip
    require_command sha256sum
    require_command python3
    [[ -d "$DATA_DIR" ]] || die "本地数据目录不存在：$DATA_DIR"
    run_compose config --quiet
    images=()
    while IFS= read -r image; do
        [[ -n "$image" ]] && images+=("$image")
    done < <(image_names)
    [[ "${#images[@]}" -gt 0 ]] || die "当前 Compose 未找到本地服务镜像。"
    for image in "${images[@]}"; do
        docker image inspect "$image" >/dev/null 2>&1 || die "本地镜像不存在：$image，请先执行 docker compose -f docker-compose.local.yml up -d --build。"
    done

    archive="$(absolute_archive_path "$REQUESTED_ARCHIVE_PATH")"
    work="$(mktemp -d "${TMPDIR:-/tmp}/open-ai-canvas-migration.XXXXXX")"
    cleanup_export() {
        if [[ "$was_running" -eq 1 ]]; then
            compose_up >/dev/null || true
        fi
        rm -rf -- "$work"
    }
    trap cleanup_export EXIT

    if [[ -n "$(running_services)" ]]; then
        was_running=1
        run_compose stop backend web
    fi
    copy_config_to_archive "$work"
    mkdir -p "$work/data/project-workbench-debug"
    cp -a "$DATA_DIR/." "$work/data/project-workbench-debug/"
    printf '正在导出 Docker 服务镜像：%s\n' "${images[*]}"
    docker image save -o "$work/images.tar" "${images[@]}" || die "Docker 镜像导出失败。"
    version=local
    if [[ -f "$REPO_ROOT/VERSION" ]]; then
        version="$(tr -d '\r\n' < "$REPO_ROOT/VERSION")"
        [[ -n "$version" ]] || version=local
    fi
    write_manifest "$work" "$version"
    validate_manifest "$work"
    rm -f -- "$archive"
    (cd -- "$work" && zip -q -r "$archive" .)
    printf '%s  %s\n' "$(sha256_file "$archive")" "$(basename -- "$archive")" > "$archive.sha256"
    printf '迁移包已生成：%s\n' "$archive"
    printf 'SHA-256：%s\n' "$(sha256_file "$archive")"
}

backup_config() {
    local root="$1" backup="$2" relative source destination
    mkdir -p "$backup/service-config"
    for relative in "${CONFIG_FILES[@]}"; do
        source="$root/$relative"
        if [[ -f "$source" ]]; then
            destination="$backup/service-config/$relative"
            mkdir -p "$(dirname -- "$destination")"
            cp -a "$source" "$destination"
        fi
    done
}

restore_config() {
    local extract="$1" root="$2" relative source destination
    for relative in "${CONFIG_FILES[@]}"; do
        source="$extract/service-config/$relative"
        [[ -f "$source" ]] || continue
        destination="$root/$relative"
        mkdir -p "$(dirname -- "$destination")"
        cp -a "$source" "$destination"
    done
}

rollback_import() {
    local status="$1" relative source destination
    trap - EXIT
    set +e
    if [[ "${import_success:-0}" -eq 1 ]]; then
        rm -rf -- "${extract:-}"
        exit "$status"
    fi
    if [[ "${services_stopped:-0}" -eq 1 ]]; then
        run_compose stop backend web >/dev/null 2>&1
    fi
    if [[ "${data_touched:-0}" -eq 1 && -d "$DATA_DIR" ]]; then
        rm -rf -- "$DATA_DIR"
    fi
    if [[ "${data_moved:-0}" -eq 1 && -d "${backup_root:-}/project-workbench-debug" ]]; then
        mkdir -p "$(dirname -- "$DATA_DIR")"
        mv -- "${backup_root:-}/project-workbench-debug" "$DATA_DIR"
    fi
    if [[ "${config_touched:-0}" -eq 1 ]]; then
        for relative in "${CONFIG_FILES[@]}"; do
            source="${backup_root:-}/service-config/$relative"
            destination="$REPO_ROOT/$relative"
            if [[ -f "$source" ]]; then
                mkdir -p "$(dirname -- "$destination")"
                cp -a "$source" "$destination"
            else
                rm -f -- "$destination"
            fi
        done
    fi
    if [[ "${services_stopped:-0}" -eq 1 ]]; then
        compose_up >/dev/null 2>&1
    fi
    rm -rf -- "${extract:-}"
    exit "$status"
}

import_migration() {
    local archive extract
    require_common
    require_command unzip
    require_command sha256sum
    require_command python3
    [[ -n "$REQUESTED_ARCHIVE_PATH" ]] || die "import 必须提供迁移包路径。"
    archive="$(absolute_archive_path "$REQUESTED_ARCHIVE_PATH")"
    [[ -f "$archive" ]] || die "迁移包不存在：$archive"
    unzip -tq "$archive" >/dev/null || die "迁移包 ZIP 校验失败。"
    assert_safe_zip_entries "$archive"
    extract="$(mktemp -d "${TMPDIR:-/tmp}/open-ai-canvas-restore.XXXXXX")"
    backup_root="$REPO_ROOT/.local/migrations/before-import-$(date -u +%Y%m%d-%H%M%S)"
    mkdir -p "$backup_root"
    services_stopped=0
    data_moved=0
    data_touched=0
    config_touched=0
    import_success=0
    trap 'status=$?; rollback_import "$status"' EXIT

    unzip -q "$archive" -d "$extract"
    validate_manifest "$extract"
    if [[ -n "$(running_services)" ]]; then
        services_stopped=1
        run_compose stop backend web
    fi
    if [[ -d "$DATA_DIR" ]]; then
        mv -- "$DATA_DIR" "$backup_root/project-workbench-debug"
        data_moved=1
    fi
    backup_config "$REPO_ROOT" "$backup_root"
    config_touched=1
    restore_config "$extract" "$REPO_ROOT"
    mkdir -p "$(dirname -- "$DATA_DIR")"
    mkdir -p "$DATA_DIR"
    data_touched=1
    cp -a "$extract/data/project-workbench-debug/." "$DATA_DIR/"
    printf '正在导入 Docker 服务镜像...\n'
    docker load -i "$extract/images.tar" || die "Docker 镜像导入失败。"
    run_compose config --quiet
    compose_up
    import_success=1
    rm -rf -- "$extract"
    printf '迁移恢复完成，数据和本地服务已启动。\n'
    printf '恢复前数据备份：%s\n' "$backup_root"
}

if [[ "$ACTION" == "export" ]]; then
    export_migration
else
    import_migration
fi
