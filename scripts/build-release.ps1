
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
    $label = Get-Date -Format "yyyy-MM-dd"
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
& go build -trimpath -o $output .
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
if (-not (Test-Path -LiteralPath $settingsFile)) {
    Copy-Item -LiteralPath $settingsExample -Destination $settingsFile
}

Write-Host "建置完成：$output"
Write-Host "必要的 web runtime 資產已複製到：$webOutput"
Write-Host "可編輯設定檔：$settingsFile"
Write-Host "雙擊啟動：$launcherCommand"
