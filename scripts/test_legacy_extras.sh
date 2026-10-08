#!/usr/bin/env bash
# 舊系統補遷移 4 頁驗證
set -u
BASE="${1:-http://localhost:8097}"
CK="$LOCALAPPDATA/Temp/ck_lg.txt"
OUT="$LOCALAPPDATA/Temp/lg_page.html"

rm -f "$CK"
curl -s -c "$CK" -X POST -d "username=owner&password=Owner001!" "$BASE/login" -o /dev/null -w "login=%{http_code}\n"

echo
echo "--- 投保紀錄頁 ---"
curl -s -b "$CK" "$BASE/insurances" -o "$OUT" -w "http=%{http_code}\n"
grep -o '車輛投保紀錄（符合條件 [0-9]* 筆）' "$OUT" | head -1
grep -o '強制險' "$OUT" | head -1
grep -o '泰安產物保險(股)公司' "$OUT" | head -1
printf 'bytes=%s tr=%s\n' "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"

echo
echo "--- 定檢排程頁 ---"
curl -s -b "$CK" "$BASE/inspections" -o "$OUT" -w "http=%{http_code}\n"
grep -o '車輛定檢排程（符合條件 [0-9]* 筆，未完成 [0-9]* 筆）' "$OUT" | head -1
grep -o '待檢驗\|已逾期' "$OUT" | head -1
printf 'bytes=%s tr=%s\n' "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"

echo
echo "--- 只看未完成定檢 ---"
curl -s -b "$CK" "$BASE/inspections?pending=1" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 保養套餐頁 ---"
curl -s -b "$CK" "$BASE/maintenance-templates" -o "$OUT" -w "http=%{http_code}\n"
grep -o '保養套餐範本（[0-9]* 個）' "$OUT" | head -1
grep -o '5千公里定期保養' "$OUT" | head -1

echo
echo "--- 套餐項目明細 ---"
TID=$(curl -s -b "$CK" "$BASE/maintenance-templates" -o "$OUT"; grep -o 'maintenance-templates?id=[0-9]*' "$OUT" | head -1 | grep -o '[0-9]*$')
echo "template id = $TID"
curl -s -b "$CK" "$BASE/maintenance-templates?id=$TID" -o "$OUT" -w "http=%{http_code}\n"
grep -o '項）' "$OUT" | head -1

echo
echo "--- 零用金支出頁 ---"
curl -s -b "$CK" "$BASE/expenses" -o "$OUT" -w "http=%{http_code}\n"
grep -o '零用金／雜支支出（符合條件 [0-9]* 筆）' "$OUT" | head -1
grep -o '單位未確認' "$OUT" | head -1
printf 'bytes=%s tr=%s\n' "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"

echo
echo "--- 導覽列新增項目 ---"
curl -s -b "$CK" "$BASE/" -o "$OUT"
grep -o '投保\|定檢\|保養套餐\|支出' "$OUT" | sort -u
