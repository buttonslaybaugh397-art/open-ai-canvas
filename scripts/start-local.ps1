[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$backendDir = Join-Path $repoRoot "backend"
$webDir = Join-Path $repoRoot "web"
$dataDir = Join-Path $repoRoot ".local\project-workbench-debug"
$configuredCacheDir = [string]$env:CANVAS_PROJECT_CACHE_DIR
if ($null -eq $configuredCacheDir) {
    $configuredCacheDir = ""
}
$configuredCacheDir = [Environment]::ExpandEnvironmentVariables($configuredCacheDir).Trim()
$projectCacheDir = if ([string]::IsNullOrWhiteSpace($configuredCacheDir)) {
    "D:\open-ai-canvas-cache"
} elseif ([IO.Path]::IsPathRooted($configuredCacheDir)) {
    $configuredCacheDir
} else {
    Join-Path $repoRoot $configuredCacheDir
}
$projectCacheDir = [IO.Path]::GetFullPath($projectCacheDir)
$goBuildCache = Join-Path $projectCacheDir "go-build"
$goModuleCache = Join-Path $projectCacheDir "go-mod"
$bunCacheDir = Join-Path $projectCacheDir "bun-cache"
$nodeModulesDir = Join-Path $webDir "node_modules"
$nodeModulesCacheDir = Join-Path $projectCacheDir "web-node-modules"

foreach ($commandName in @("go", "bun")) {
    if (-not (Get-Command $commandName -ErrorAction SilentlyContinue)) {
        throw "未找到 $commandName，请先安装项目要求的运行时。"
    }
}

foreach ($directory in @($dataDir, $projectCacheDir, $goBuildCache, $goModuleCache, $bunCacheDir)) {
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
}

& (Join-Path $PSScriptRoot "start-local-migration-helper.ps1") -TargetRoot $repoRoot

$nodeModulesItem = Get-Item -LiteralPath $nodeModulesDir -Force -ErrorAction SilentlyContinue
if ($null -eq $nodeModulesItem) {
    New-Item -ItemType Directory -Force -Path $nodeModulesCacheDir | Out-Null
    New-Item -ItemType Junction -Path $nodeModulesDir -Target $nodeModulesCacheDir | Out-Null
} elseif (($nodeModulesItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0) {
    if (Test-Path -LiteralPath $nodeModulesCacheDir) {
        $cacheEntries = @(Get-ChildItem -LiteralPath $nodeModulesCacheDir -Force -ErrorAction SilentlyContinue)
        if ($cacheEntries.Count -gt 0) {
            throw "外置 node_modules 缓存目录已有内容，但 web/node_modules 仍是本地目录，请先人工确认后再迁移：$nodeModulesCacheDir"
        }
        [IO.Directory]::Delete($nodeModulesCacheDir, $false)
    }
    Move-Item -LiteralPath $nodeModulesDir -Destination $nodeModulesCacheDir
    New-Item -ItemType Junction -Path $nodeModulesDir -Target $nodeModulesCacheDir | Out-Null
}

$viteBinary = @(
    (Join-Path $nodeModulesDir ".bin\vite"),
    (Join-Path $nodeModulesDir ".bin\vite.exe"),
    (Join-Path $nodeModulesDir ".bin\vite.bunx")
) | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
if ($null -eq $viteBinary) {
    Write-Host "web/node_modules 不存在，正在执行 bun install --frozen-lockfile..." -ForegroundColor Yellow
    Push-Location $webDir
    try {
        & bun install --frozen-lockfile --cache-dir $bunCacheDir
        if ($LASTEXITCODE -ne 0) {
            throw "bun install 失败，无法启动前端。"
        }
    } finally {
        Pop-Location
    }
}

function Test-ListeningPort([int]$Port) {
    try {
        return $null -ne (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction Stop | Select-Object -First 1)
    } catch {
        return $false
    }
}

foreach ($port in @(3000, 8080)) {
    if (Test-ListeningPort $port) {
        throw "端口 $port 已被占用，请先关闭占用进程后重试。"
    }
}

$powerShellPath = (Get-Command pwsh -ErrorAction SilentlyContinue).Source
if (-not $powerShellPath) {
    $powerShellPath = (Get-Command powershell.exe -ErrorAction SilentlyContinue).Source
}
if (-not $powerShellPath) {
    throw "未找到 PowerShell，无法打开前后端独立窗口。"
}

function ConvertTo-PowerShellLiteral([string]$Value) {
    return "'" + $Value.Replace("'", "''") + "'"
}

$backendDirLiteral = ConvertTo-PowerShellLiteral $backendDir
$webDirLiteral = ConvertTo-PowerShellLiteral $webDir
$dataDirLiteral = ConvertTo-PowerShellLiteral $dataDir

$backendCommand = @"
`$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $backendDirLiteral
`$env:CANVAS_BACKEND_ADDR = '127.0.0.1:8080'
`$env:CANVAS_BACKEND_DATA_DIR = $dataDirLiteral
`$env:GOCACHE = $(ConvertTo-PowerShellLiteral $goBuildCache)
`$env:GOMODCACHE = $(ConvertTo-PowerShellLiteral $goModuleCache)
Write-Host '影策后端：http://127.0.0.1:8080' -ForegroundColor Cyan
go run ./cmd/server
"@

$webCommand = @"
`$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $webDirLiteral
`$env:VITE_API_PROXY_TARGET = 'http://127.0.0.1:8080'
Write-Host '影策前端：http://localhost:3000' -ForegroundColor Cyan
bun run dev
"@

$backendProcess = Start-Process -FilePath $powerShellPath -WindowStyle Normal -WorkingDirectory $backendDir -PassThru -ArgumentList @("-NoLogo", "-NoExit", "-NoProfile", "-Command", $backendCommand)
$webProcess = Start-Process -FilePath $powerShellPath -WindowStyle Normal -WorkingDirectory $webDir -PassThru -ArgumentList @("-NoLogo", "-NoExit", "-NoProfile", "-Command", $webCommand)

Write-Host "已打开前后端开发窗口。" -ForegroundColor Green
Write-Host "后端窗口 PID: $($backendProcess.Id)；前端窗口 PID: $($webProcess.Id)"
Write-Host "访问 http://localhost:3000；分别在两个窗口按 Ctrl+C 停止服务。"
