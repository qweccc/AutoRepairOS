
param(
    [string]$Version
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "找不到 Go。請先安裝 Go 1.25 或更新版本，再重新執行。"
}

if ([string]::IsNullOrWhiteSpace($Version)) {
    # 預設版本號含日期與時間：同一天可能建置多次（例如修完 bug 重跑），
    # 只有日期會讓後者覆蓋前者，無法區別「這個 exe 是幾點建的」。
    $label = Get-Date -Format "yyyy-MM-dd_HHmmss"
} else {
    $label = $Version.Trim()
    if ($label -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') {
        throw "版本名稱只能使用英數字、點、底線及連字號，且必須以英數字開頭。"
    }
}

$releaseDir = Join-Path $repoRoot "release"
New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null
$output = Join-Path $releaseDir "AutoRepairOS-$label.exe"
$webOutput = Join-Path $releaseDir "web"
$webSource = Join-Path $repoRoot "web"
if (-not (Test-Path -LiteralPath (Join-Path $webSource "index.html"))) {
    throw "找不到 web\index.html。release\web 只是建置時複製出去的產物，不是來源；請在 web\ 維護前端檔案後重新建置（不可沿用 release\web 的舊檔，否則會打包過期內容）。"
}

Write-Host "正在建置 Windows 執行檔：$output"
Write-Host "版本標籤：$label"
$stamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
$rev = (git rev-parse --short HEAD 2>$null)
if (-not $rev) { $rev = "unknown" }
Write-Host "建置時間：$stamp"
Write-Host "Git 修訂：$rev"
# 注入建置時間與 git 修訂，讓啟動 log 能顯示跑的是哪一版。
# buildTime 含空格，必須用單引號包住整個 -X 值，否則 go 會把它拆成兩個
# 參數而讓 linker 報 usage 錯誤。git commit 是 hex 不需引號。
& go build -trimpath -ldflags "-X 'main.buildTime=$stamp' -X main.gitCommit=$rev" -o $output .
if ($LASTEXITCODE -ne 0) {
    throw "Go 建置失敗（exit code $LASTEXITCODE）。"
}

New-Item -ItemType Directory -Force -Path $webOutput | Out-Null
if ($webSource -ne $webOutput) {
    Copy-Item -Path (Join-Path $webSource "*") -Destination $webOutput -Recurse -Force
}

$launcherScript = Join-Path $releaseDir "start.ps1"
$launcherCommand = Join-Path $releaseDir "start.cmd"
Copy-Item -LiteralPath (Join-Path $PSScriptRoot "release-start.ps1") -Destination $launcherScript -Force
Copy-Item -LiteralPath (Join-Path $PSScriptRoot "release-start.cmd") -Destination $launcherCommand -Force

$settingsFile = Join-Path $releaseDir ".env"
$settingsExample = Join-Path $releaseDir ".env.example"
Copy-Item -LiteralPath (Join-Path $PSScriptRoot "release.env.example") -Destination $settingsExample -Force
# 只在不存在時才建立 .env：這個檔是給部署者填機密的，每次建置都覆蓋會把
# 人家填好的密碼沖掉。
#
# 注意：也因此 release/.env 可能是很久以前複製的，其中的非機密設定
# （如 DB_ENABLE_WAL）會停留在當時的預設值。改預設值時要一併更新
# scripts/release.env.example 與既有的 release/.env，否則 release 版會
# 靜默跑在舊預設上 —— 這個坑在把 DB_ENABLE_WAL 預設改為 true 時就踩到了。
if (-not (Test-Path -LiteralPath $settingsFile)) {
    Copy-Item -LiteralPath $settingsExample -Destination $settingsFile
}

Write-Host "建置完成：$output"
Write-Host "必要的 web runtime 資產已複製到：$webOutput"
Write-Host "可編輯設定檔：$settingsFile"
Write-Host "雙擊啟動：$launcherCommand"
