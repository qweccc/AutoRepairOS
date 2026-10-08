Add-Type -AssemblyName System.Runtime.Serialization.Formatters
$root = "C:\Users\fr\AutoRepairOS"
$dir = Join-Path $root "data_iso"
Write-Output "exists: $(Test-Path $dir)"
if (-not (Test-Path $dir)) { exit 0 }
for ($i = 0; $i -lt 20; $i++) {
    try { [System.IO.Directory]::Delete($dir, $true); Write-Output "deleted at try $($i+1)"; break }
    catch { Start-Sleep -Seconds 1 }
}
Write-Output "exists: $(Test-Path $dir)"
