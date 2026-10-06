#!/usr/bin/env bash
# 眷屬分開 + 健保資訊 驗證
set -u
BASE="${1:-http://localhost:8097}"
CK="$LOCALAPPDATA/Temp/ck_st.txt"
OUT="$LOCALAPPDATA/Temp/st_page.html"

rm -f "$CK"
curl -s -c "$CK" -X POST -d "username=owner&password=Owner001!" "$BASE/login" -o /dev/null -w "login=%{http_code}\n"

echo
echo "--- 員工/健保頁 ---"
curl -s -b "$CK" "$BASE/staff" -o "$OUT" -w "http=%{http_code}\n"
grep -o '員工與健保資料' "$OUT" | head -1
grep -o '健保眷屬 [0-9]*' "$OUT" | head -1
grep -o '員工 [0-9]*' "$OUT" | head -1
grep -o '在保中' "$OUT" | head -1
grep -o '身分證號' "$OUT" | head -1
grep -o '加保日' "$OUT" | head -1
grep -o '退保日' "$OUT" | head -1
printf 'bytes=%s tr=%s\n' "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"

echo
echo "--- 只看健保眷屬 ---"
curl -s -b "$CK" "$BASE/staff?kind=dependent" -o "$OUT"
grep -o '符合條件 [0-9]*' "$OUT" | head -1
grep -o '嚴一舒\|王彩秀' "$OUT" | head -1

echo
echo "--- 眷屬依附顯示（應出現主被保險人姓名）---"
curl -s -b "$CK" "$BASE/staff?kind=dependent" -o "$OUT"
grep -o '沈育訓\|陳金生\|林全章\|賴豐泉' "$OUT" | head -1
grep -o '已退保' "$OUT" | head -1

echo
echo "--- 只看員工 ---"
curl -s -b "$CK" "$BASE/staff?kind=employee" -o "$OUT"
grep -o '符合條件 [0-9]*' "$OUT" | head -1

echo
echo "--- 技師佣金頁：眷屬應被排除 ---"
curl -s -b "$CK" "$BASE/technicians/commissions" -o "$OUT" -w "http=%{http_code}\n"
grep -o '部門' "$OUT" | head -1
echo "眷屬姓名是否出現在佣金表（應為空）:"
grep -o '嚴一舒\|謝惠芳\|羅素芳\|陳美枝' "$OUT" | head -1
echo "技師筆數: $(grep -o '<tr>' "$OUT" | wc -l)"

echo
echo "--- 導覽列 ---"
curl -s -b "$CK" "$BASE/" -o "$OUT"
grep -o '員工' "$OUT" | head -1
