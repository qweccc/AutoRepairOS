// AutoRepair OS Service Worker
// 快取靜態資源，並在離線時提供應用程式外殼。

const CACHE = 'autorepairos-v1';
const SHELL = [
  '/offline.js',
  '/offline.css'
];

self.addEventListener('install', (e) => {
  e.waitUntil(
    caches.open(CACHE)
      .then((c) => Promise.allSettled(SHELL.map((u) => c.add(u))))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(
        keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))
      ))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  const req = e.request;

  // 只處理 GET；API 與照片交由網路直接處理（避免 stale 資料）
  if (req.method !== 'GET') return;

  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/api/')) return;

  // 導覽請求：network-first，離線時回退到快取的登入頁外殼
  if (req.mode === 'navigate') {
    e.respondWith(
      fetch(req)
        .then((res) => {
          const copy = res.clone();
          caches.open(CACHE).then((c) => c.put('/offline-shell', copy));
          return res;
        })
        .catch(() => caches.match('/offline-shell').then((r) => r || offlineResponse()))
    );
    return;
  }

  // 靜態資源：cache-first
  e.respondWith(
    caches.match(req).then((hit) => {
      if (hit) return hit;
      return fetch(req).then((res) => {
        if (res.ok && res.type === 'basic') {
          const copy = res.clone();
          caches.open(CACHE).then((c) => c.put(req, copy));
        }
        return res;
      }).catch(() => offlineResponse());
    })
  );
});

function offlineResponse() {
  return new Response(
    '<!DOCTYPE html><html lang="zh-Hant"><head><meta charset="utf-8">' +
    '<title>離線 - AutoRepair OS</title>' +
    '<style>body{background:#0f172a;color:#e2e8f0;font-family:"Segoe UI","Microsoft JhengHei",sans-serif;' +
    'display:flex;align-items:center;justify-content:center;height:100vh;margin:0;text-align:center}' +
    'h1{color:#38bdf8;font-size:22px}p{color:#94a3b8;font-size:14px}</style></head>' +
    '<body><div><h1>⚠ 離線模式</h1>' +
    '<p>無法連線至伺服器。已啟用的本地資料仍可檢視，' +
    '提交的操作會暫存並於恢復連線後自動上傳。</p></div></body></html>',
    { headers: { 'Content-Type': 'text/html; charset=utf-8' }, status: 503 }
  );
}
