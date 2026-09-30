$ErrorActionPreference = "Stop"
$base = "C:\Users\fr\AutoRepairOS"
$bash = "C:\Users\fr\AppData\Local\hermes\git\bin\bash.exe"
$out = "$base\_login_out.txt"

Write-Output "=== port 8150 ==="
try { $sc=0; Invoke-WebRequest -Uri "http://127.0.0.1:8150/" -TimeoutSec 3 -UseBasicParsing | Out-Null; Write-Output "UP status=$sc" } catch { Write-Output "DOWN" }

Write-Output "=== RUN login_test.sh via bash (bash does redirect) ==="
$psi = Start-Process -FilePath $bash -ArgumentList $base + "\_login_test.sh" -Wait -PassThru
Write-Output "  exit=$($psi.ExitCode)"

Write-Output "=== DONE ==="
