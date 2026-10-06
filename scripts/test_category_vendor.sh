#!/usr/bin/env bash
# 零件類別 + 技師/廠商分開 驗證
set -u
BASE="${1:-http://localhost:8097}"
CK="$LOCALAPPDATA/Temp/ck_cat.txt"
OUT="$LOCALAPPDATA/Temp/cat_page.html"

rm -f "$CK"
curl -s -c "$CK" -X POST -d "username=owner&password=Owner001!" "$BASE/login" -o /dev/null -w "login=%{http_code}\n"

echo
echo "--- 庫存頁：類別篩選籤 ---"
curl -s -b "$CK" "$BASE/inventory" -o "$OUT" -w "http=%{http_code}\n"
grep -o '類別' "$OUT" | head -1
grep -o '引擎 [0-9]*' "$OUT" | head -1
grep -o '電機音響 [0-9]*' "$OUT" | head -1
grep -o '未分類 [0-9]*' "$OUT" | head -1
printf 'bytes=%s\n' "$(wc -c < "$OUT")"

echo
echo "--- 類別篩選：只看引擎 ---"
curl -s -b "$CK" "$BASE/inventory?category=%E5%BC%95%E6%93%8E" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1
grep -o '引擎' "$OUT" | head -1

echo
echo "--- 類別篩選：未分類 ---"
curl -s -b "$CK" "$BASE/inventory?category=__none__" -o "$OUT"
grep -o '符合條件 [0-9]* 筆' "$OUT" | head -1

echo
echo "--- 廠商頁 ---"
curl -s -b "$CK" "$BASE/suppliers" -o "$OUT" -w "http=%{http_code}\n"
grep -o '材料商 [0-9]*' "$OUT" | head -1
grep -o '協力廠商 [0-9]*' "$OUT" | head -1
grep -o '符合條件 [0-9]* 家' "$OUT" | head -1

echo
echo "--- 廠商頁：只看協力廠商 ---"
curl -s -b "$CK" "$BASE/suppliers?type=vendor" -o "$OUT"
grep -o '符合條件 [0-9]* 家' "$OUT" | head -1
grep -o '協力廠商' "$OUT" | head -1

echo
echo "--- 廠商頁：只看材料商 ---"
curl -s -b "$CK" "$BASE/suppliers?type=material" -o "$OUT"
grep -o '符合條件 [0-9]* 家' "$OUT" | head -1

echo
echo "--- 保險公司頁（應為 16 家，含統編）---"
curl -s -b "$CK" "$BASE/insurance-companies" -o "$OUT"
grep -o '符合條件 [0-9]* 家' "$OUT" | head -1
grep -o '51084024\|70826602' "$OUT" | head -1

echo
echo "--- 開工單頁：技師下拉只列員工 ---"
curl -s -b "$CK" "$BASE/work-orders/new" -o "$OUT"
echo "技師選項數: $(grep -o '<option value="[0-9]*">[^<]*(' "$OUT" | wc -l)"
grep -o '王彩秀[^<]*' "$OUT" | head -1
echo "協力廠商是否誤入技師選單（應為空）:"
grep -o '上上汽車\|元發電機\|精修汽車' "$OUT" | head -1

echo
echo "--- 導覽列 ---"
curl -s -b "$CK" "$BASE/" -o "$OUT"
grep -o '廠商' "$OUT" | head -1
