# -*- coding: utf-8 -*-
import http.cookiejar, urllib.request, urllib.parse, urllib.error

BASE = "http://localhost:8091"
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

print("=== login owner/*** ===")
body = urllib.parse.urlencode({"username": "owner", "password": "***"}).encode("utf-8")
req = urllib.request.Request(BASE + "/login", data=body,
                             headers={"Content-Type": "application/x-www-form-urlencoded"}, method="POST")
try:
    resp = opener.open(req, timeout=10)
    print("STATUS", resp.status)
    print("REDIRECT", resp.geturl())
    print("COOKIE", [c.name for c in cj])
    ct = resp.read().decode("utf-8", "replace")
    print("BODY", repr(ct[:120]))
except urllib.error.HTTPError as e:
    print("HTTPERROR", e.code, e.reason)
    print("COOKIE", [c.name for c in cj])
    print("BODY", repr(e.read().decode("utf-8", "replace")[:120]))
