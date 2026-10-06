#!/usr/bin/env bash
# AutoRepair OS UX 驗證：分頁、車輛履歷、開工單頁大小
set -u
BASE="${1:-http://localhost:8099}"
CK="$LOCALAPPDATA/Temp/ck_ux.txt"
OUT="$LOCALAPPDATA/Temp/ux_page.html"

rm -f "$CK"
curl -s -c "$CK" -X POST -d "username=owner&password=Owner001!" "$BASE/login" -o /dev/null -w "login=%{http_code}\n"

echo "--- 列表頁：大小 / 列數 / 載入時間 ---"
for u in /customers /vehicles /work-orders /inventory /insurance-companies /work-orders/new; do
  t=$(curl -s -b "$CK" -o "$OUT" -w "%{time_total}" "$BASE$u")
  printf '%-24s %8ss  %9s bytes  %4s tr\n' "$u" "$t" "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"
done

echo
echo "--- 工單分頁資訊 ---"
curl -s -b "$CK" "$BASE/work-orders" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1
grep -o '第 [0-9]* / [0-9]* 頁，共 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 工單第 2 頁（確認換頁有效）---"
curl -s -b "$CK" "$BASE/work-orders?page=2" -o "$OUT"
grep -o '第 [0-9]* / [0-9]* 頁' "$OUT" | head -1

echo
echo "--- 工單日期篩選 ---"
curl -s -b "$CK" "$BASE/work-orders?from=2016-01-01&to=2016-12-31" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 車輛履歷頁 ---"
VID=$(curl -s -b "$CK" "$BASE/vehicles" -o "$OUT"; grep -o '/vehicles/[0-9]*' "$OUT" | grep -v edit | grep -v delete | head -1 | grep -o '[0-9]*')
echo "vehicle id = $VID"
curl -s -b "$CK" "$BASE/vehicles/$VID" -o "$OUT" -w "http=%{http_code}\n"
grep -o '維修次數\|累計消費\|未結金額\|最近里程\|歷年更換項目' "$OUT" | sort -u
printf 'bytes=%s tr=%s\n' "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"

echo
echo "--- 車輛搜尋（車牌）---"
curl -s -b "$CK" "$BASE/vehicles?q=3107" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 客戶搜尋（中文，需 percent-encode）---"
curl -s -b "$CK" "$BASE/customers?q=%E4%B9%99%E7%99%BC" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 工單搜尋（中文車主）---"
curl -s -b "$CK" "$BASE/work-orders?q=%E4%B8%AD%E8%8F%AF%E9%9B%BB%E4%BF%A1" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 每頁筆數切換（per=200）---"
curl -s -b "$CK" "$BASE/work-orders?per=200" -o "$OUT"
printf 'bytes=%s tr=%s\n' "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"
