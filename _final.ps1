$ErrorActionPreference = "Stop"
$base = "C:\Users\fr\AutoRepairOS"
$go = "C:\Program Files\Go\bin\go.exe"

Write-Output "=== [0] KILL all autrepairos + go run ==="
foreach ($p in (Get-Process -Name autrepairos -ErrorAction SilentlyContinue)) { Stop-Process -Id $p.Id -Force }
foreach ($p in (Get-Process -Name go -ErrorAction SilentlyContinue)) { $cl=($p.CommandLines -join ' '); if ($cl -match 'run\s+') { Stop-Process -Id $p.Id -Force } }
Start-Sleep -Seconds 2

Write-Output "=== [1] DELETE root + data db (fresh) ==="
foreach ($f in @(
  "$base\autorepair.db","$base\autorepair.db-shm","$base\autorepair.db-wal",
  "$base\data\autorepair.db","$base\data\autorepair.db-shm","$base\data\autorepair.db-wal"
)) { if (Test-Path $f) { Remove-Item $f -Force -EA SilentlyContinue; Write-Output "  deleted $f" } }
Start-Sleep -Seconds 2

Write-Output "=== [2] data/ EMPTY before seed? ==="
Get-ChildItem "$base\data" -Force -EA SilentlyContinue | Select-Object Name,Length | Format-Table -AutoSize

Write-Output "=== [3] BUILD binary from project root ==="
Set-Location $base
& $go build -o "$base\autrepairos.exe" . 2>&1
Write-Output "  BUILD EXIT: $LASTEXITCODE"

Write-Output "=== [4] RESEED from project root ==="
& "$base\cmd\seed\seed.exe" 2>&1
Start-Sleep -Seconds 1

Write-Output "=== [5] VERIFY data/autorepair.db hashes ==="
$py = "C:\Users\fr\AppData\Local\hermes\hermes-agent\venv\Scripts\python.exe"
$env:PYTHONIOENCODING = "utf-8"
& $py -c "import sqlite3; c=sqlite3.connect('$base\data\autorepair.db'); print('data db:', [r for r in c.execute('SELECT id,username,substr(password,1,10) FROM users ORDER BY id') if r])" 2>&1

Write-Output "=== [6] START binary from project root PORT=8370 ==="
$env:PORT = "8370"
& "$base\autrepairos.exe" 2>&1 | Out-Null
Start-Sleep -Seconds 6
