[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$composeFile = Join-Path $repoRoot "docker-compose.local.yml"
$dataDir = Join-Path $repoRoot ".local\project-workbench-debug"

foreach ($commandName in @("docker")) {
    if (-not (Get-Command $commandName -ErrorAction SilentlyContinue)) {
        throw "未找到 $commandName，请先安装并启动所需运行时。"
    }
}
if (-not (Test-Path -LiteralPath $composeFile -PathType Leaf)) {
    throw "未找到 Compose 文件：$composeFile"
}

New-Item -ItemType Directory -Force -Path $dataDir | Out-Null

$previousHostRoot = $env:CANVAS_HOST_REPOSITORY_ROOT
$env:CANVAS_HOST_REPOSITORY_ROOT = $repoRoot
Push-Location $repoRoot
try {
    & docker compose -f $composeFile up -d --build
    if ($LASTEXITCODE -ne 0) {
        throw "本地 Compose 服务启动失败。"
    }
    & docker compose -f $composeFile ps
    if ($LASTEXITCODE -ne 0) {
        throw "无法读取本地 Compose 服务状态。"
    }
} finally {
    Pop-Location
    if ($null -eq $previousHostRoot) {
        Remove-Item Env:CANVAS_HOST_REPOSITORY_ROOT -ErrorAction SilentlyContinue
    } else {
        $env:CANVAS_HOST_REPOSITORY_ROOT = $previousHostRoot
    }
}

Write-Host ""
Write-Host "本地服务已启动。" -ForegroundColor Green
Write-Host "前端：http://localhost:3000"
Write-Host "迁移助手：Compose 内网 migration-helper:9714（不发布宿主机端口）"
