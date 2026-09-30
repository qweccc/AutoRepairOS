$ErrorActionPreference = "Stop"
$base = "C:\Users\fr\AutoRepairOS"
$bash = "C:\Users\fr\AppData\Local\hermes\git\bin\bash.exe"
$psi = Start-Process -FilePath $bash -ArgumentList @("-c", "bash '" + $base + "\_ls.sh'") -Wait -PassThru
Write-Output "=== exit=$($psi.ExitCode) ==="
