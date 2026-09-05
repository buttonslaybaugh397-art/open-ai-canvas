[CmdletBinding()]
param(
    [string]$TargetRoot
)

$ErrorActionPreference = "Stop"

$repoRoot = if ([string]::IsNullOrWhiteSpace($TargetRoot)) {
    (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
} else {
    [IO.Path]::GetFullPath($TargetRoot)
}
$backendDir = Join-Path $repoRoot "backend"
$helperDir = Join-Path $repoRoot ".local\migration-helper"
$tokenFile = Join-Path $helperDir "token"
$binary = Join-Path $helperDir "open-ai-canvas-local-migration-helper.exe"
$stdoutLog = Join-Path $helperDir "helper.stdout.log"
$stderrLog = Join-Path $helperDir "helper.stderr.log"
$sourceFiles = @(
    (Join-Path $backendDir "cmd\local-migration-helper\main.go"),
    (Join-Path $backendDir "internal\hostupdate\types.go")
)

foreach ($commandName in @("go", "docker")) {
    if (-not (Get-Command $commandName -ErrorAction SilentlyContinue)) {
        throw "未找到 $commandName，无法启动本地迁移助手。"
    }
}

New-Item -ItemType Directory -Force -Path $helperDir | Out-Null

if (-not (Test-Path -LiteralPath $tokenFile -PathType Leaf)) {
    $bytes = New-Object byte[] 32
    [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    $token = ([BitConverter]::ToString($bytes)).Replace("-", "").ToLowerInvariant()
    [IO.File]::WriteAllText($tokenFile, $token + [Environment]::NewLine, [Text.Encoding]::ASCII)
}

$existing = Get-NetTCPConnection -LocalPort 9714 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
if ($existing) {
    $process = Get-Process -Id $existing.OwningProcess -ErrorAction SilentlyContinue
    if ($process -and $process.Path -eq $binary) {
        Write-Host "本地迁移助手已在运行（PID $($process.Id)）。" -ForegroundColor Cyan
        exit 0
    }
    throw "端口 9714 已被 $($process.ProcessName) 占用，无法启动本地迁移助手。"
}

$rebuild = -not (Test-Path -LiteralPath $binary -PathType Leaf)
if (-not $rebuild) {
    $binaryTime = (Get-Item -LiteralPath $binary).LastWriteTimeUtc
    $rebuild = @($sourceFiles | Where-Object { (Get-Item -LiteralPath $_).LastWriteTimeUtc -gt $binaryTime }).Count -gt 0
}
if ($rebuild) {
    Push-Location $backendDir
    try {
        & go build -o $binary ./cmd/local-migration-helper
        if ($LASTEXITCODE -ne 0) { throw "本地迁移助手构建失败。" }
    } finally {
        Pop-Location
    }
}

$process = Start-Process -FilePath $binary -ArgumentList @("-root", $repoRoot, "-address", "0.0.0.0:9714", "-token-file", $tokenFile) -WindowStyle Hidden -PassThru -RedirectStandardOutput $stdoutLog -RedirectStandardError $stderrLog
Start-Sleep -Milliseconds 750
if ($process.HasExited) {
    $detail = if (Test-Path -LiteralPath $stderrLog) { Get-Content -LiteralPath $stderrLog -Tail 30 -Raw } else { "未产生日志" }
    throw "本地迁移助手启动失败：$detail"
}
Write-Host "本地迁移助手已启动（PID $($process.Id)）。" -ForegroundColor Green
