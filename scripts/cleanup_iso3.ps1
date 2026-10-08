$root = "C:\Users\fr\AutoRepairOS"
$dir = Join-Path $root "data_iso"
if (Test-Path $dir) {
    try { [System.IO.Directory]::Delete($dir, $true); Write-Output "deleted" }
    catch { Write-Output "fail: $($_.Exception.Message)" }
} else { Write-Output "not-exists" }
Write-Output "exists: $(Test-Path $dir)"
