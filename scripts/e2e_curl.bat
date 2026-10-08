@echo off
chcp 65001 >nul
set "ROOT=%~dp0.."
set "COOKIES=%ROOT%scripts\e2e_cookies.txt"
set "OUT=%ROOT%scripts\e2e_curl_run.txt"

if exist "%ROOT%autos.exe" (
  taskkill /f /fi "imagename eq autos.exe" 2>nul
  Start-Sleep 2
  start "" "%ROOT%autos.exe"
)

for /L %%i in (1,1,180) do (
  for /f %%a in ('netstat -aon ^| findstr ":8093" ^| findstr "LISTENING"') do goto :found
  timeout /t 500 >nul
)
:found
echo PORT_UP >> "%OUT%"

echo login >> "%OUT%"
curl.exe -s -c "%COOKIES%" -X POST http://localhost:8093/login ^
  --data "username=owner&password=Owner001!" -o nul
echo login_done >> "%OUT%"

echo list >> "%OUT%"
curl.exe -s -b "%COOKIES%" http://localhost:8093/work-orders > "%ROOT%scripts\e2e_list.html"
echo list_done >> "%OUT%"

echo create >> "%OUT%"
curl.exe -s -b "%COOKIES%" -X POST http://localhost:8093/work-orders ^
  --data "vehicle_id=1&current_mileage=5000&is_insurance=1&insurance_company_id_mirror=1&claim_no=CLM-999&accident_date=2026-10-01&deductible_amount=1500" ^
  -H "Content-Type: application/x-www-form-urlencoded" -o nul
echo create_done >> "%OUT%"

echo receipt >> "%OUT%"
curl.exe -s -b "%COOKIES%" http://localhost:8093/work-orders/receipt > "%ROOT%scripts\e2e_receipt.html"
echo receipt_done >> "%OUT%"
echo ALL_DONE >> "%OUT%"
