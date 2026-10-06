#!/usr/bin/env bash
# 下次保養：日期與里程分開 驗證
set -u
BASE="${1:-http://localhost:8097}"
CK="$LOCALAPPDATA/Temp/ck_m.txt"
OUT="$LOCALAPPDATA/Temp/m_page.html"

rm -f "$CK"
curl -s -c "$CK" -X POST -d "username=owner&password=Owner001!" "$BASE/login" -o /dev/null -w "login=%{http_code}\n"

echo
echo "--- 車輛列表：保養分兩欄 ---"
curl -s -b "$CK" "$BASE/vehicles" -o "$OUT" -w "http=%{http_code}\n"
grep -o '下次保養日' "$OUT" | head -1
grep -o '下次保養里程' "$OUT" | head -1
printf 'bytes=%s tr=%s\n' "$(wc -c < "$OUT")" "$(grep -o '<tr>' "$OUT" | wc -l)"

echo
echo "--- 逾期標示（應出現「逾期」或「已超過」）---"
grep -o '逾期[0-9]*天' "$OUT" | head -3
grep -o '已超過 [0-9]*' "$OUT" | head -3

echo
echo "--- 車輛編輯表單：保養間隔欄位 ---"
VID=$(curl -s -b "$CK" "$BASE/vehicles" -o "$OUT"; grep -o 'vehicles/[0-9]*' "$OUT" | grep -v edit | grep -v delete | head -1 | grep -o '[0-9]*$')
echo "vehicle id = $VID"
curl -s -b "$CK" "$BASE/vehicles/$VID/edit" -o "$OUT"
grep -o '最後保養日\|最後保養里程\|保養間隔（月）\|保養間隔（公里）' "$OUT" | sort -u

echo
echo "--- 車輛履歷：下次保養分成依日期/依里程 ---"
curl -s -b "$CK" "$BASE/vehicles/$VID" -o "$OUT" -w "http=%{http_code}\n"
grep -o '依日期\|依里程\|兩者先到者為準\|最後保養\|保養間隔' "$OUT" | sort -u

echo
echo "--- 新增車輛表單 ---"
curl -s -b "$CK" "$BASE/vehicles" -o "$OUT"
grep -o '保養間隔（月）' "$OUT" | head -1
