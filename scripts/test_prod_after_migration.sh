#!/usr/bin/env bash
# 正式服務（8090）遷移後驗證：資料筆數、分頁、履歷、負庫存補正
set -u
BASE="${1:-http://localhost:8090}"
CK="$LOCALAPPDATA/Temp/ck_prod.txt"
OUT="$LOCALAPPDATA/Temp/prod_page.html"

rm -f "$CK"
curl -s -c "$CK" -X POST -d "username=owner&password=Owner001!" "$BASE/login" -o /dev/null -w "login=%{http_code}\n"

echo
echo "--- 各頁面狀態與大小 ---"
for u in / /customers /vehicles /work-orders /inventory /insurance-companies /inventory/negative /inventory/movements /reports; do
  code=$(curl -s -b "$CK" -o "$OUT" -w "%{http_code}" "$BASE$u")
  printf '%-24s http=%s  %8s bytes\n' "$u" "$code" "$(wc -c < "$OUT")"
done

echo
echo "--- 遷移資料筆數（頁面顯示）---"
curl -s -b "$CK" "$BASE/customers" -o "$OUT";        printf '客戶:     %s\n' "$(grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1)"
curl -s -b "$CK" "$BASE/vehicles" -o "$OUT";         printf '車輛:     %s\n' "$(grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1)"
curl -s -b "$CK" "$BASE/work-orders" -o "$OUT";      printf '工單:     %s\n' "$(grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1)"
curl -s -b "$CK" "$BASE/inventory" -o "$OUT";        printf '零件:     %s\n' "$(grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1)"
curl -s -b "$CK" "$BASE/insurance-companies" -o "$OUT"; printf '保險公司: %s\n' "$(grep -o '符合條件 [0-9]* 家' "$OUT" | head -1)"

echo
echo "--- 分頁 ---"
curl -s -b "$CK" "$BASE/work-orders" -o "$OUT"; grep -o '第 [0-9]* / [0-9]* 頁，共 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 負庫存補正 ---"
curl -s -b "$CK" "$BASE/inventory/negative" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1
grep -o '缺口總和' "$OUT" | head -1

echo
echo "--- 車輛履歷（真實舊資料車牌）---"
curl -s -b "$CK" "$BASE/vehicles?q=0006-FP" -o "$OUT"
VID=$(grep -o 'vehicles/[0-9]*' "$OUT" | grep -v edit | grep -v delete | head -1 | grep -o '[0-9]*$')
echo "vehicle id = $VID"
curl -s -b "$CK" -o "$OUT" -w "http=%{http_code}\n" "$BASE/vehicles/$VID"
grep -o '維修次數\|累計消費\|歷年更換項目' "$OUT" | sort -u

echo
echo "--- 舊資料日期是否正確顯示（不再空白）---"
curl -s -b "$CK" "$BASE/work-orders" -o "$OUT"
grep -oE '20[0-9]{2}-[0-9]{2}-[0-9]{2}' "$OUT" | head -3
