#!/usr/bin/env bash
# 負庫存補正介面驗證：列表、篩選、單筆補正、異動紀錄
set -u
BASE="${1:-http://localhost:8099}"
CK="$LOCALAPPDATA/Temp/ck_neg.txt"
OUT="$LOCALAPPDATA/Temp/neg_page.html"

rm -f "$CK"
curl -s -c "$CK" -X POST -d "username=owner&password=Owner001!" "$BASE/login" -o /dev/null -w "login=%{http_code}\n"

echo "--- 負庫存列表 ---"
curl -s -b "$CK" -o "$OUT" -w "http=%{http_code} time=%{time_total}\n" "$BASE/inventory/negative"
grep -o '負庫存料號' "$OUT" | head -1
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1
grep -o '缺口總和' "$OUT" | head -1
printf 'bytes=%s tr=%s\n' "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"

echo
echo "--- 只列有售價 ---"
curl -s -b "$CK" "$BASE/inventory/negative?sort=sellable" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 分頁第 2 頁 ---"
curl -s -b "$CK" "$BASE/inventory/negative?page=2" -o "$OUT"
grep -o '第 [0-9]* / [0-9]* 頁' "$OUT" | head -1

echo
echo "--- 庫存頁負庫存提示橫幅 ---"
curl -s -b "$CK" "$BASE/inventory" -o "$OUT"
grep -o '個料號庫存為負數' "$OUT" | head -1
grep -o '前往補正' "$OUT" | head -1

echo
echo "--- 單筆補正表單 ---"
# 取第一個負庫存料號 id
ITEM=$(curl -s -b "$CK" "$BASE/inventory/negative" -o "$OUT"; grep -o 'inventory/negative?id=[0-9]*' "$OUT" | head -1 | grep -o '[0-9]*$')
echo "item id = $ITEM"
curl -s -b "$CK" -o "$OUT" -w "http=%{http_code}\n" "$BASE/inventory/negative?id=$ITEM"
grep -o '設為 0（歸零，不補貨）\|設為安全庫存\|設為指定數量' "$OUT" | sort -u

echo
echo "--- 異動紀錄頁（補正前）---"
curl -s -b "$CK" "$BASE/inventory/movements" -o "$OUT"
grep -o '庫存異動紀錄（符合條件 [0-9]* 筆）' "$OUT" | head -1

echo
echo "--- 執行補正：設為 0 ---"
curl -s -b "$CK" -o /dev/null -w "fix_http=%{http_code}\n" \
  -X POST -d "item_id=$ITEM&mode=zero&reason=測試補正" "$BASE/inventory/negative/fix"

echo
echo "--- 異動紀錄頁（補正後，應出現 legacy_fix）---"
curl -s -b "$CK" "$BASE/inventory/movements?source=legacy_fix" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1
grep -o '舊資料補正' "$OUT" | head -1
grep -o '測試補正' "$OUT" | head -1
