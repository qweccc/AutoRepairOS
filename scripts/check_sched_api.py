import urllib.request, urllib.parse, http.cookiejar, re

BASE="http://localhost:8090"
cj=http.cookiejar.CookieJar()
op=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

def pf(path,data):
    b=urllib.parse.urlencode(data).encode("utf-8")
    r=urllib.request.Request(BASE+path,method="POST",data=b)
    r.add_header("Content-Type","application/x-www-form-urlencoded; charset=utf-8")
    try:
        resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace"),resp.geturl()
    except urllib.error.HTTPError as e:
        return e.code,e.read().decode("utf-8","replace"),e.url

def get(path):
    r=urllib.request.Request(BASE+path); resp=op.open(r,timeout=8); return resp.status,resp.read().decode("utf-8","replace"),resp.geturl()

st,b,u=pf("/login",{"username":"owner","password":"Owner001!"})
st,b,u=get("/scheduled-tasks")

# 任務表：抓每個 tr 內的粗體名 + 識別碼 + 頻率 + 狀態 + 上次 + 下次 + 次數
print("--- 排程任務狀態 ---")
for row in re.findall(r'<tr>(.*?)</tr>', b, re.S):
    if '<td><b>' not in row or 'pill' not in row:
        continue
    name=re.search(r'<td><b>(.*?)</b>', row, re.S)
    key=re.search(r'<code>(.*?)</code>', row, re.S)
    rate=re.search(r'<td>([^<]*分鐘[^<]*)</td>', row, re.S)
    last=re.search(r'<td>([^<]*)</td>', row, re.S)
    nextrun=re.search(r'<td>([^<]*)</td>', row, re.S)
    count=re.search(r'<td class="num">(\d+)</td>', row, re.S)
    if name:
        print("  %-12s | %-20s | %s | 上次:%s | 下次:%s | 次數:%s" % (
            name.group(1).strip(), key.group(1).strip() if key else "",
            rate.group(1).strip() if rate else "",
            last.group(1).strip() if last else "",
            nextrun.group(1).strip() if nextrun else "",
            count.group(1) if count else ""))

print("\n--- 通知紀錄 (recent) ---")
for row in re.finditer(r'<tr><td>(\d+)</td>', b):
    start=row.start()
    seg=b[start:start+400]
    kind=re.search(r'保養到期|完工通知|估價核准|LINE|SMS|內部|不通知|待處理|已送達|已取消|失敗', seg)
    status=re.search(r'<span class="pill [^"]*">([^<]*)</span>', seg)
    print("  #%s | 狀態:%s" % (row.group(1), (kind.group(0)+" ["+status.group(1)+"]") if kind else "?"))

print("\n--- 備份紀錄 ---")
for row in re.finditer(r'<tr><td>(\d{4}-\d{2}-\d{2})</td>', b):
    start=row.start()
    seg=b[start:start+300]
    path=re.search(r'font-size:11\.5px">([^<]*)</td>', seg)
    size=re.search(r'<td class="num">(\d+)</td>', seg)
    photo=re.search(r'<td class="num">(\d+)</td>', seg)
    stt=re.search(r'<span class="pill [^"]*">([^<]*)</span>', seg)
    print("  %s | %s | DB:%sKB | 照片:%s | %s" % (
        row.group(1), path.group(1).strip() if path else "?",
        size.group(1) if size else "?", photo.group(1) if photo else "?",
        stt.group(1) if stt else "?"))
