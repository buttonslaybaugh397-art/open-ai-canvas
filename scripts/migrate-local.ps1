[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet("export", "import")]
    [string]$Action,

    [Parameter(Position = 1)]
    [string]$ArchivePath,

    [string]$TargetRoot
)

# Windows PowerShell 5.1 requires a UTF-8 BOM; the file is normalized below.
$ErrorActionPreference = "Stop"

$scriptRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$repoRoot = if ([string]::IsNullOrWhiteSpace($TargetRoot)) { $scriptRoot } else { [IO.Path]::GetFullPath($TargetRoot) }
$composeHostRoot = if ([string]::IsNullOrWhiteSpace($env:CANVAS_HOST_REPOSITORY_ROOT)) { $repoRoot } else { $env:CANVAS_HOST_REPOSITORY_ROOT }
$composeFile = Join-Path $repoRoot "docker-compose.local.yml"
$dataDir = Join-Path $repoRoot ".local\project-workbench-debug"
$migrationDir = Join-Path $repoRoot ".local\migrations"
$configFiles = @(
    "docker-compose.local.yml",
    "docker-compose.build.yml",
    "Dockerfile",
    "nginx.conf",
    "VERSION",
    ".env"
)

function Write-Info([string]$Message) {
    Write-Host $Message -ForegroundColor Cyan
}

function Assert-Command([string]$Name) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "未找到 $Name，请先安装并启动对应运行时。"
    }
}

function Invoke-DockerCompose([string[]]$Arguments) {
    $previousHostRoot = $env:CANVAS_HOST_REPOSITORY_ROOT
    $env:CANVAS_HOST_REPOSITORY_ROOT = $composeHostRoot
    Push-Location $repoRoot
    try {
        & docker compose @Arguments
        if ($LASTEXITCODE -ne 0) {
            throw "Docker Compose 命令失败：docker compose $($Arguments -join ' ')"
        }
    } finally {
        Pop-Location
        if ($null -eq $previousHostRoot) {
            Remove-Item Env:CANVAS_HOST_REPOSITORY_ROOT -ErrorAction SilentlyContinue
        } else {
            $env:CANVAS_HOST_REPOSITORY_ROOT = $previousHostRoot
        }
    }
}

function Invoke-DockerComposeUp() {
    $arguments = @('-f', $composeFile, 'up', '-d', '--force-recreate', '--remove-orphans', '--wait', '--wait-timeout', '600')
    if ($env:CANVAS_LOCAL_MIGRATION_HELPER_CONTAINER -eq '1') {
        $arguments += @('backend', 'web')
    }
    Invoke-DockerCompose $arguments
}

function Get-ImageNames() {
    $names = @(
        Select-String -Path $composeFile -Pattern '^\s*image:\s*(\S+)' | ForEach-Object { $_.Matches[0].Groups[1].Value }
    ) | Where-Object { $_ -and $_ -notmatch '^oven/bun:' } | Select-Object -Unique
    if ($names.Count -eq 0) {
        throw "没有从 $composeFile 找到本地服务镜像。"
    }
    return @($names)
}

function Get-RelativeArchivePath([string]$Root, [string]$Path) {
    $relative = [IO.Path]::GetRelativePath($Root, $Path).Replace('\', '/')
    if ([IO.Path]::IsPathRooted($relative) -or $relative -eq '..' -or $relative.StartsWith('../')) {
        throw "迁移归档路径越界：$Path"
    }
    return $relative
}

function Assert-SafeArchivePath([string]$Path) {
    $normalized = $Path.Replace('\', '/')
    if ([IO.Path]::IsPathRooted($Path) -or $normalized -eq '..' -or $normalized.StartsWith('../') -or $normalized.Contains('/../')) {
        throw "迁移归档包含不安全路径：$Path"
    }
}

function Get-ManifestFiles([string]$Root) {
    return @(
        Get-ChildItem -LiteralPath $Root -File -Recurse -Force | ForEach-Object {
            $relative = Get-RelativeArchivePath $Root $_.FullName
            $hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
            [PSCustomObject]@{ path = $relative; size = $_.Length; sha256 = $hash }
        }
    )
}

function Test-LocalServiceReady() {
    $deadline = (Get-Date).AddMinutes(2)
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:3000/api/health/ready" -TimeoutSec 10
            if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 300) {
                return $true
            }
        } catch {}
        Start-Sleep -Seconds 2
    }
    return $false
}

function Assert-Manifest([string]$Root) {
    $manifestPath = Join-Path $Root "manifest.json"
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
        throw "迁移包缺少 manifest.json。"
    }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($manifest.schemaVersion -ne 1) {
        throw "不支持的迁移包版本：$($manifest.schemaVersion)"
    }
    if ([string]::IsNullOrWhiteSpace([string]$manifest.version)) {
        throw "迁移包缺少来源版本。"
    }
    if ([string]$manifest.databaseDriver -ne "sqlite") {
        throw "本地迁移包必须使用 SQLite，实际驱动：$($manifest.databaseDriver)"
    }
    if (-not $manifest.files) {
        throw "迁移包清单为空。"
    }
    foreach ($item in @($manifest.files)) {
        Assert-SafeArchivePath ([string]$item.path)
        $filePath = Join-Path $Root ([string]$item.path)
        if (-not (Test-Path -LiteralPath $filePath -PathType Leaf)) {
            throw "迁移包文件缺失：$($item.path)"
        }
        $actual = (Get-FileHash -LiteralPath $filePath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne ([string]$item.sha256).ToLowerInvariant()) {
            throw "迁移包校验失败：$($item.path)"
        }
    }
    return $manifest
}

function Copy-ConfigToArchive([string]$ConfigRoot) {
    $configDir = Join-Path $ConfigRoot "service-config"
    New-Item -ItemType Directory -Force -Path $configDir | Out-Null
    foreach ($relative in $configFiles) {
        $source = Join-Path $repoRoot $relative
        if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
            if ($relative -eq ".env") {
                throw "缺少 $source。请先配置本地服务环境。"
            }
            continue
        }
        $destination = Join-Path $configDir $relative
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $destination) | Out-Null
        Copy-Item -LiteralPath $source -Destination $destination -Force
    }
}

function Restore-ConfigFromArchive([string]$ExtractRoot, [string]$BackupRoot) {
    $configDir = Join-Path $ExtractRoot "service-config"
    if (-not (Test-Path -LiteralPath $configDir -PathType Container)) {
        throw "迁移包缺少 service-config。"
    }
    $backupConfig = Join-Path $BackupRoot "service-config"
    New-Item -ItemType Directory -Force -Path $backupConfig | Out-Null
    $existing = @($configFiles | Where-Object { Test-Path -LiteralPath (Join-Path $repoRoot $_) -PathType Leaf })
    $existing | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $backupConfig ".existing.json") -Encoding UTF8
    foreach ($relative in $configFiles) {
        $source = Join-Path $configDir $relative
        if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { continue }
        $destination = Join-Path $repoRoot $relative
        if (Test-Path -LiteralPath $destination -PathType Leaf) {
            $saved = Join-Path $backupConfig $relative
            New-Item -ItemType Directory -Force -Path (Split-Path -Parent $saved) | Out-Null
            Copy-Item -LiteralPath $destination -Destination $saved -Force
        }
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $destination) | Out-Null
        Copy-Item -LiteralPath $source -Destination $destination -Force
    }
}

function Restore-ConfigFromBackup([string]$BackupRoot) {
    $backupConfig = Join-Path $BackupRoot "service-config"
    if (-not (Test-Path -LiteralPath $backupConfig -PathType Container)) { return }
    $existingPath = Join-Path $backupConfig ".existing.json"
    $existing = if (Test-Path -LiteralPath $existingPath -PathType Leaf) {
        @((Get-Content -LiteralPath $existingPath -Raw | ConvertFrom-Json) | ForEach-Object { [string]$_ })
    } else {
        @($configFiles | Where-Object { Test-Path -LiteralPath (Join-Path $backupConfig $_) -PathType Leaf })
    }
    foreach ($relative in $configFiles) {
        $source = Join-Path $backupConfig $relative
        $destination = Join-Path $repoRoot $relative
        if (Test-Path -LiteralPath $source -PathType Leaf) {
            New-Item -ItemType Directory -Force -Path (Split-Path -Parent $destination) | Out-Null
            Copy-Item -LiteralPath $source -Destination $destination -Force
        } elseif ($relative -notin $existing -and (Test-Path -LiteralPath $destination -PathType Leaf)) {
            Remove-Item -LiteralPath $destination -Force
        }
    }
}

function Get-RunningServiceNames() {
    $ids = @(Invoke-DockerCompose @('-f', $composeFile, 'ps', '--status', 'running', '-q', 'backend', 'web'))
    return @($ids | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}

function Start-ServicesIfNeeded([bool]$WasRunning) {
    if (-not $WasRunning) { return }
    Write-Info "正在恢复本地服务..."
    Invoke-DockerComposeUp
}

function Export-Migration([string]$RequestedArchivePath) {
    Assert-Command "docker"
    if (-not (Test-Path -LiteralPath $composeFile -PathType Leaf)) { throw "当前目录不是 open-ai-canvas 仓库：$repoRoot" }
    if (-not (Test-Path -LiteralPath $dataDir -PathType Container)) { throw "本地数据目录不存在：$dataDir" }
    Invoke-DockerCompose @('-f', $composeFile, 'config', '--quiet')
    $images = Get-ImageNames
    foreach ($image in $images) {
        & docker image inspect $image *> $null
        if ($LASTEXITCODE -ne 0) { throw "本地镜像不存在：$image。请先执行 docker compose -f docker-compose.local.yml up -d --build。" }
    }

    $archive = if ([string]::IsNullOrWhiteSpace($RequestedArchivePath)) {
        New-Item -ItemType Directory -Force -Path $migrationDir | Out-Null
        Join-Path $migrationDir ("open-ai-canvas-migration-{0}.zip" -f (Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmmss'))
    } else { [IO.Path]::GetFullPath($RequestedArchivePath) }
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $archive) | Out-Null
    $work = Join-Path ([IO.Path]::GetTempPath()) ("open-ai-canvas-migration-{0}" -f [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $work | Out-Null
    $wasRunning = (Get-RunningServiceNames).Count -gt 0
    try {
        if ($wasRunning) { Invoke-DockerCompose @('-f', $composeFile, 'stop', 'backend', 'web') }
        Copy-ConfigToArchive $work
        New-Item -ItemType Directory -Force -Path (Join-Path $work "data") | Out-Null
        Copy-Item -LiteralPath $dataDir -Destination (Join-Path $work "data\project-workbench-debug") -Recurse -Force
        Write-Info "正在导出 Docker 服务镜像：$($images -join ', ')"
        & docker image save -o (Join-Path $work "images.tar") @images
        if ($LASTEXITCODE -ne 0) { throw "Docker 镜像导出失败。" }
        $manifest = [ordered]@{
            schemaVersion = 1
            createdAt = (Get-Date).ToUniversalTime().ToString('o')
            version = if (Test-Path -LiteralPath (Join-Path $repoRoot "VERSION") -PathType Leaf) { (Get-Content -LiteralPath (Join-Path $repoRoot "VERSION") -Raw).Trim() } else { "local" }
            databaseDriver = "sqlite"
            composeFile = "docker-compose.local.yml"
            images = @($images)
            files = @(Get-ManifestFiles $work)
        }
        $manifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $work "manifest.json") -Encoding UTF8
        # manifest.json itself is intentionally written last and is not included in its own hash list.
        $files = @(Get-ManifestFiles $work)
        $manifest.files = $files | Where-Object { $_.path -ne "manifest.json" }
        $manifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $work "manifest.json") -Encoding UTF8
        if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        [IO.Compression.ZipFile]::CreateFromDirectory($work, $archive, [IO.Compression.CompressionLevel]::Optimal, $false)
        $archiveHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
        Set-Content -LiteralPath ($archive + ".sha256") -Value "$archiveHash  $([IO.Path]::GetFileName($archive))" -Encoding ASCII
        Write-Host "迁移包已生成：$archive" -ForegroundColor Green
        Write-Host "SHA-256：$archiveHash"
    } finally {
        try { Start-ServicesIfNeeded $wasRunning } finally { Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue }
    }
}

function Import-Migration([string]$RequestedArchivePath) {
    Assert-Command "docker"
    if ([string]::IsNullOrWhiteSpace($RequestedArchivePath)) { throw "import 必须提供迁移包路径。" }
    $archive = [IO.Path]::GetFullPath($RequestedArchivePath)
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) { throw "迁移包不存在：$archive" }
    if (-not (Test-Path -LiteralPath $composeFile -PathType Leaf)) { throw "目标目录不是 open-ai-canvas 仓库：$repoRoot" }
    $extract = Join-Path ([IO.Path]::GetTempPath()) ("open-ai-canvas-restore-{0}" -f [guid]::NewGuid().ToString('N'))
    $backupRoot = Join-Path $repoRoot (".local\migrations\before-import-{0}" -f (Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmmss'))
    New-Item -ItemType Directory -Force -Path $extract | Out-Null
    $wasRunning = (Get-RunningServiceNames).Count -gt 0
    $dataMoved = $false
    $configTouched = $false
    $dataTouched = $false
    $servicesStopped = $false
    $servicesStarted = $false
    try {
        Expand-Archive -LiteralPath $archive -DestinationPath $extract -Force
        $manifest = Assert-Manifest $extract
        $sourceData = Join-Path $extract "data\project-workbench-debug"
        $sourceImages = Join-Path $extract "images.tar"
        if (-not (Test-Path -LiteralPath $sourceData -PathType Container)) { throw "迁移包缺少 SQLite 数据目录。" }
        if (-not (Test-Path -LiteralPath $sourceImages -PathType Leaf)) { throw "迁移包缺少 Docker 镜像归档。" }
        if ($wasRunning) {
            $servicesStopped = $true
            Invoke-DockerCompose @('-f', $composeFile, 'stop', 'backend', 'web')
        }
        New-Item -ItemType Directory -Force -Path $backupRoot | Out-Null
        if (Test-Path -LiteralPath $dataDir -PathType Container) {
            Move-Item -LiteralPath $dataDir -Destination (Join-Path $backupRoot "project-workbench-debug")
            $dataMoved = $true
        }
        $configTouched = $true
        Restore-ConfigFromArchive $extract $backupRoot
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $dataDir) | Out-Null
        $dataTouched = $true
        Copy-Item -LiteralPath $sourceData -Destination $dataDir -Recurse -Force
        Write-Info "正在导入 Docker 服务镜像..."
        & docker load -i $sourceImages
        if ($LASTEXITCODE -ne 0) { throw "Docker 镜像导入失败。" }
        Invoke-DockerCompose @('-f', $composeFile, 'config', '--quiet')
        Invoke-DockerComposeUp
        $servicesStarted = $true
        if (-not (Test-LocalServiceReady)) { throw "恢复后的本地服务未能通过健康检查。" }
        Write-Host "迁移恢复完成，数据和本地服务已启动。" -ForegroundColor Green
        Write-Host "恢复前数据备份：$backupRoot"
    } catch {
        Write-Host "迁移恢复失败，正在尝试恢复导入前状态..." -ForegroundColor Yellow
        if ($servicesStopped) { try { Invoke-DockerCompose @('-f', $composeFile, 'stop', 'backend', 'web') } catch {} }
        if ($dataTouched -and (Test-Path -LiteralPath $dataDir -PathType Container)) { Remove-Item -LiteralPath $dataDir -Recurse -Force }
        if ($dataMoved -and (Test-Path -LiteralPath (Join-Path $backupRoot "project-workbench-debug") -PathType Container)) {
            Move-Item -LiteralPath (Join-Path $backupRoot "project-workbench-debug") -Destination $dataDir
        }
        if ($configTouched) { Restore-ConfigFromBackup $backupRoot }
        if ($servicesStopped) {
            Start-ServicesIfNeeded $true
            $servicesStarted = $true
        }
        throw
    } finally {
        if ($servicesStopped -and -not $servicesStarted) {
            Start-ServicesIfNeeded $true
        }
        Remove-Item -LiteralPath $extract -Recurse -Force -ErrorAction SilentlyContinue
    }
}

if ($Action -eq "export") {
    Export-Migration $ArchivePath
} else {
    Import-Migration $ArchivePath
}
