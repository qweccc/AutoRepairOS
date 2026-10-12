$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

function Stop-Deploy {
    param([string]$Message)
    Write-Error $Message
    exit 1
}

function Confirm-Action {
    param([string]$Message)
    $answer = Read-Host "$Message [y/N]"
    return $answer -match '^(y|yes)$'
}

function Read-EnvFile {
    param([string]$Path)
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $Path) {
        if ($line -match '^\s*#' -or $line -notmatch '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$') {
            continue
        }
        $value = $Matches[2]
        if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$Matches[1]] = $value
    }
    return $values
}

$envFile = Join-Path $repoRoot ".env"
if (-not (Test-Path -LiteralPath $envFile)) {
    Copy-Item -LiteralPath (Join-Path $repoRoot ".env.example") -Destination $envFile
    Write-Host "已建立 .env。請填入專屬車廠設定與 DEFAULT_OWNER_PASSWORD，再重新執行 deploy.cmd。"
    exit 2
}

$settings = Read-EnvFile -Path $envFile
$project = [string]$settings["COMPOSE_PROJECT_NAME"]
if ([string]::IsNullOrWhiteSpace($project)) {
    $project = "autorepairos"
}
if ($project -notmatch '^[a-z0-9][a-z0-9_-]*$') {
    Stop-Deploy ".env 的 COMPOSE_PROJECT_NAME 必須是小寫英數字、底線或連字號，且以英數字開頭。"
}

$port = 8090
if ($settings.ContainsKey("HOST_PORT") -and $settings["HOST_PORT"] -ne "") {
    $parsedPort = 0
    if (-not [int]::TryParse([string]$settings["HOST_PORT"], [ref]$parsedPort) -or $parsedPort -lt 1 -or $parsedPort -gt 65535) {
        Stop-Deploy ".env 的 HOST_PORT 必須介於 1 到 65535。"
    }
    $port = $parsedPort
}

$runSeed = ([string]$settings["RUN_SEED"]) -match '^(true|1|yes)$'
$ownerPassword = [string]$settings["DEFAULT_OWNER_PASSWORD"]
if ($runSeed -and ($ownerPassword.Length -lt 12 -or $ownerPassword -match 'CHANGE_ME|Owner001!|password')) {
    Stop-Deploy "首次初始化需在 .env 設定至少 12 字元且非範例值的 DEFAULT_OWNER_PASSWORD。"
}

$dockerCommand = Get-Command docker -ErrorAction SilentlyContinue
if (-not $dockerCommand) {
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
        Stop-Deploy "找不到 Docker 和 winget。請先安裝 Docker Desktop：https://docs.docker.com/desktop/setup/install/windows-install/"
    }
    if (-not (Confirm-Action "尚未安裝 Docker Desktop。要現在透過 winget 安裝嗎？安裝可能需要系統管理員權限與重新啟動")) {
        Stop-Deploy "請先安裝 Docker Desktop，啟用 WSL2 backend，並切換到 Linux containers；完成後重新執行 deploy.cmd。"
    }
    & winget install --id Docker.DockerDesktop --exact --accept-source-agreements --accept-package-agreements
    if ($LASTEXITCODE -ne 0) {
        Stop-Deploy "winget 安裝 Docker Desktop 失敗（exit code $LASTEXITCODE）。請依 Docker 官方指引手動安裝後重試。"
    }
    Write-Host "Docker Desktop 已安裝。請啟動 Docker Desktop、完成首次設定後重新執行 deploy.cmd。"
    exit 2
}

$wslCommand = Get-Command wsl.exe -ErrorAction SilentlyContinue
$wslReady = $false
if ($wslCommand) {
    & wsl.exe --version *> $null
    $wslReady = $LASTEXITCODE -eq 0
}
if (-not $wslReady) {
    if (-not (Confirm-Action "尚未偵測到可用的 WSL2。要執行 wsl --install --no-distribution 嗎？這可能需要系統管理員權限與重新啟動")) {
        Stop-Deploy "請安裝/更新 WSL2 並重新啟動 Windows，再執行 deploy.cmd。"
    }
    & wsl.exe --install --no-distribution
    if ($LASTEXITCODE -ne 0) {
        Stop-Deploy "WSL2 安裝失敗（exit code $LASTEXITCODE）。請以系統管理員 PowerShell 執行 wsl --install，再重試。"
    }
    Write-Host "WSL2 安裝已啟動。若 Windows 要求重新啟動，請先重啟，再執行 deploy.cmd。"
    exit 2
}

$composeArgs = @("compose", "--env-file", $envFile, "-p", $project)
$dockerType = & docker info --format '{{.OSType}}' 2>$null
if ($LASTEXITCODE -ne 0) {
    $desktopPaths = @(
        (Join-Path $env:ProgramFiles "Docker\Docker\Docker Desktop.exe"),
        (Join-Path $env:LOCALAPPDATA "Programs\DockerDesktop\Docker Desktop.exe")
    )
    $desktopPath = $desktopPaths | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    if ($desktopPath) {
        Write-Host "正在啟動 Docker Desktop..."
        Start-Process -FilePath $desktopPath
    } else {
        Stop-Deploy "Docker daemon 未啟動。請開啟 Docker Desktop 並確認使用 WSL2 backend，再重試。"
    }

    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Seconds 2
        $dockerType = & docker info --format '{{.OSType}}' 2>$null
        if ($LASTEXITCODE -eq 0) {
            break
        }
    }
    if ($LASTEXITCODE -ne 0) {
        Stop-Deploy "Docker Desktop 尚未就緒。請確認 Docker Desktop/WSL2 已啟動，再重試。"
    }
}

if (($dockerType | Out-String).Trim() -ne "linux") {
    Stop-Deploy "目前 Docker 使用 Windows containers。請在 Docker Desktop 選擇「Switch to Linux containers」後重新執行。"
}

& docker @composeArgs version *> $null
if ($LASTEXITCODE -ne 0) {
    Stop-Deploy "找不到 Docker Compose v2。請更新 Docker Desktop 後重試。"
}

Write-Host "正在建置 AutoRepairOS..."
& docker @composeArgs build autorepairos
if ($LASTEXITCODE -ne 0) {
    Stop-Deploy "Docker 映像建置失敗；現有服務尚未停止。請檢查上方錯誤。"
}

$runningContainer = & docker @composeArgs ps --status running -q autorepairos
$volumeName = "${project}_autorepairos-data"
$volumeExists = $false
& docker volume inspect $volumeName *> $null
$volumeExists = $LASTEXITCODE -eq 0

if ($runningContainer) {
    & docker @composeArgs stop autorepairos
    if ($LASTEXITCODE -ne 0) {
        Stop-Deploy "停止服務以備份資料失敗；未繼續部署。"
    }
}

$backupFile = $null
if ($volumeExists) {
    $backupDir = Join-Path $repoRoot "data\backups"
    New-Item -ItemType Directory -Force -Path $backupDir | Out-Null
    $timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
    $backupName = "${project}_$timestamp.db"
    $backupFile = Join-Path $backupDir $backupName
    & docker run --rm --mount "type=volume,source=$volumeName,target=/data,readonly" --mount "type=bind,source=$backupDir,target=/backup" alpine:3.21 sh -c "if [ -f /data/autorepair.db ]; then cp /data/autorepair.db /backup/$backupName; else echo NO_DATABASE; fi"
    if ($LASTEXITCODE -ne 0) {
        if ($runningContainer) {
            & docker @composeArgs start autorepairos *> $null
        }
        Stop-Deploy "部署前備份資料失敗；已嘗試恢復原服務。請勿手動刪除 Docker volume。"
    }
    if (-not (Test-Path -LiteralPath $backupFile)) {
        $backupFile = $null
    }
}

Write-Host "正在啟動服務..."
& docker @composeArgs up -d --no-build
if ($LASTEXITCODE -ne 0) {
    if ($runningContainer) {
        & docker @composeArgs start autorepairos *> $null
    }
    Stop-Deploy "Docker Compose 啟動失敗。資料 volume 未刪除；請檢查 logs 並使用備份復原。"
}

$url = "http://127.0.0.1:$port/login"
$healthy = $false
for ($i = 0; $i -lt 30; $i++) {
    try {
        $response = Invoke-WebRequest -Uri $url -TimeoutSec 4 -UseBasicParsing
        if ($response.StatusCode -eq 200) {
            $healthy = $true
            break
        }
    } catch {
        Start-Sleep -Seconds 2
    }
}

if (-not $healthy) {
    & docker @composeArgs logs --tail 80 autorepairos
    $backupHint = if ($backupFile) { "部署前備份：$backupFile" } else { "本次未發現既有資料庫可備份。" }
    Stop-Deploy "服務健康檢查失敗。$backupHint 請勿執行 down -v；修復後再試。"
}

Write-Host "AutoRepairOS 已就緒：$url"
if ($backupFile) {
    Write-Host "部署前資料備份：$backupFile"
}
if ($runSeed) {
    Write-Host "首次登入後請將 .env 的 RUN_SEED 設為 false 並清空初始化密碼，再次執行 deploy.cmd。"
}
