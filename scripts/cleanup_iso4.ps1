$root = "C:\Users\fr\AutoRepairOS"
$dir = Join-Path $root "data_iso"
Write-Output "exists: $(Test-Path $dir)"
if (-not (Test-Path $dir)) { exit 0 }
# retry delete a few times
for ($i = 0; $i -lt 10; $i++) {
    try { [System.IO.Directory]::Delete($dir, $true); Write-Output "deleted at try $($i+1)"; break }
    catch { Start-Sleep -Seconds 1; Write-Output "try $($i+1) fail" }
}
Write-Output "exists: $(Test-Path $dir)"
