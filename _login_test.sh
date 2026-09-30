#!/usr/bin/env bash
set -u
URL="http://127.0.0.1:8150"
UO="owner"; PO="Owner001!"
UC="counter"; PC="Counter001!"
UT="tech"; PT="Tech001!"
pass=0; fail=0

check() { if [ "$1" = "OK" ]; then echo "  PASS: $2"; pass=$((pass+1)); else echo "  FAIL: $2"; fail=$((fail+1)); fi; }

echo "=== [1] GET /login ==="
code=$(curl -s -o /dev/null -w "%{http_code}" $URL/login)
check "$([ "$code" = "200" ] && echo OK || echo FAIL)" "GET /login -> $code"

echo "=== [2] owner login ($UO / $PO) ==="
curl -s -c cookies.txt -D resp.txt -X POST $URL/login -d "username=$UO&password=$PO" -o body.txt
resp=$(head -n 1 resp.txt); cookie=$(grep -i 'set-cookie' resp.txt | tr -d '\r')
echo "  body: $(cat body.txt)"
echo "  resp: $resp"
echo "  cookie: $cookie"
owner=$(curl -s $URL/api/me -b cookies.txt)
echo "  api/me: $owner"
[ "$resp" = "302 Found" ] && check OK "owner login -> 302" || check FAIL "owner login -> $resp"
echo "$owner" | grep -q '"role":"owner"' && check OK "owner role=owner" || check FAIL "owner role check"

echo "=== [3] counter login ($UC / $PC) ==="
curl -s -c c2.txt -X POST $URL/login -d "username=$UC&password=$PC" -o /dev/null
cc=$(curl -s $URL/api/me -b c2.txt)
echo "  api/me: $cc"
[ "$cc" = '{"id":2,"name":"櫃檯人員","role":"counter","username":"counter"}' ] && check OK "counter role=counter" || check FAIL "counter role check: $cc"

echo "=== [4] tech login ($UT / $PT) ==="
curl -s -c c3.txt -X POST $URL/login -d "username=$UT&password=$PT" -o /dev/null
ct=$(curl -s $URL/api/me -b c3.txt)
echo "  api/me: $ct"
[ "$ct" = '{"id":3,"name":"技師","role":"technician","username":"tech"}' ] && check OK "tech role=technician" || check FAIL "tech role check: $ct"

echo "=== [5] wrong password (expect 401) ==="
w=$(curl -s -X POST $URL/login -d "username=$UO&password=wrong")
echo "  resp: $w"
[ "$w" = "帳號或密碼錯誤" ] && check OK "wrong pw -> 401" || check FAIL "wrong pw -> $w"

echo "=== [6] no cookie (expect 401) ==="
n=$(curl -s $URL/api/me)
echo "  api/me: $n"
[ "$n" = '{"error":"未登入"}' ] && check OK "no cookie -> 401" || check FAIL "no cookie -> $n"

echo "=== [7] /logout (no session) ==="
lo=$(curl -s -o /dev/null -w "%{http_code}" $URL/logout)
echo "  /logout -> $lo"

echo "=== SUMMARY: $pass passed, $fail failed ==="
