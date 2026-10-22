$ErrorActionPreference = "Stop"
$releaseDir = $PSScriptRoot

function Stop-Launcher {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Red
    Read-Host "按 Enter 關閉此視窗"
    exit 1
}

$settingsPath = Join-Path $releaseDir ".env"
if (-not (Test-Path -LiteralPath $settingsPath)) {
    Stop-Launcher "找不到 .env。請將 .env.example 複製為 .env，填妥設定後再啟動。"
}

$settings = @{}
$applicationKeys = @(
    "PORT",
    "RUN_SEED",
    "DEFAULT_OWNER_USERNAME",
    "DEFAULT_OWNER_PASSWORD",
    "DEFAULT_COUNTER_USERNAME",
    "DEFAULT_COUNTER_PASSWORD",
    "DEFAULT_TECH_USERNAME",
    "DEFAULT_TECH_PASSWORD",
    "APP_NAME",
    "DB_PATH",
    "DB_ENABLE_WAL",
    "LINE_CHANNEL_ID",
    "LINE_CHANNEL_SECRET",
    "LINE_CHANNEL_TOKEN",
    "LINE_BASE_URL",
    "TELEGRAM_BOT_TOKEN",
    "TELEGRAM_CHAT_ID",
    "TELEGRAM_BASE_URL"
)
foreach ($key in $applicationKeys) {
    [Environment]::SetEnvironmentVariable($key, $null, "Process")
}

foreach ($line in Get-Content -LiteralPath $settingsPath -Encoding UTF8) {
    if ($line -match '^\s*(#.*)?$') {
        continue
    }
    if ($line -notmatch '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$') {
        Stop-Launcher "設定檔有無法辨識的行：$line"
    }

    $key = $Matches[1]
    $value = $Matches[2].Trim()
    if ($value.Length -ge 2 -and
        (($value.StartsWith('"') -and $value.EndsWith('"')) -or
         ($value.StartsWith("'") -and $value.EndsWith("'")))) {
        $value = $value.Substring(1, $value.Length - 2)
    }
    $settings[$key] = $value
}

$port = [string]$settings["PORT"]
if ([string]::IsNullOrWhiteSpace($port)) {
    $port = "8080"
}
$parsedPort = 0
if (-not [int]::TryParse($port, [ref]$parsedPort) -or $parsedPort -lt 1 -or $parsedPort -gt 65535) {
    Stop-Launcher ".env 的 PORT 必須是 1 到 65535 之間的數字。"
}

$runSeed = [string]$settings["RUN_SEED"]
if ($runSeed -match '^(?i:true|1)$') {
    $ownerPassword = [string]$settings["DEFAULT_OWNER_PASSWORD"]
    if ($ownerPassword.Length -lt 12 -or $ownerPassword -match 'CHANGE_ME|password') {
        Stop-Launcher "首次初始化請在 .env 設定至少 12 字元且非範例值的 DEFAULT_OWNER_PASSWORD。"
    }
}

$executables = @(Get-ChildItem -LiteralPath $releaseDir -Filter "AutoRepairOS-*.exe" -File |
    Sort-Object LastWriteTime -Descending)
if ($executables.Count -eq 0) {
    Stop-Launcher "找不到 AutoRepairOS-日期或版本.exe；請確認執行檔與 start.cmd 位於同一資料夾。"
}

$requiredWebPage = Join-Path $releaseDir "web\index.html"
if (-not (Test-Path -LiteralPath $requiredWebPage)) {
    Stop-Launcher "找不到 web\index.html；請將完整 release 資料夾一起解壓縮，不要只複製 exe。"
}

foreach ($key in $settings.Keys) {
    [Environment]::SetEnvironmentVariable($key, [string]$settings[$key], "Process")
}
$env:PORT = [string]$parsedPort
$env:RUN_SEED = $runSeed

Set-Location $releaseDir
Write-Host "啟動：$($executables[0].Name)"
Write-Host "網址：http://localhost:$parsedPort/login"
Write-Host "停止服務請按 Ctrl+C。"
& $executables[0].FullName
if ($LASTEXITCODE -ne 0) {
    Stop-Launcher "AutoRepairOS 結束時回報錯誤（exit code $LASTEXITCODE）。請檢查上方訊息及 .env 設定。"
}
