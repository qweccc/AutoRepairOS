// AutoRepair OS Service Worker
// 快取靜態資源，並在離線時提供應用程式外殼。

const CACHE = 'autorepairos-v3';
const SHELL = [
  '/offline.js',
  '/offline.css'
];

// 會隨版本改變的檔：不做長期快取，一律 network-first（快取只當離線備援）。
// 為什麼需要這份清單：靜態資源原本是 cache-first，命中快取就永不回伺服器驗證，
// 結果改了 app.css 之後，F5（軟重整）仍然拿到舊檔、畫面不會變，只有 Shift+F5
// （繞過 service worker）才看得到新版。車廠人員不會按 Shift+F5，等於功能上線
// 他們看不到。離線優先的前提是不能把「舊版」當成永久答案。
//
// 判斷標準：內容會隨 release 改變的都列進來。純 vendor 函式庫（htmx.min.js、
// html5-qrcode.min.js）版本固定、內容不變，留在 cache-first 沒有問題。
//
// /offline.js、/offline.css 是我們自己維護、會隨 release 改的檔案（它們同時
// 也在 SHELL 被預快取）。只放 SHELL 的話它們會落到最後的 cache-first 分支，
// 修好 offline.js 的 bug 之後既有瀏覽器永遠拿到舊版——而這個檔案正是負責
// 離線佇列的，錯了會靜默丟資料。路徑注意：這兩支掛在根路徑（internal/handler
// /static.go 的 RegisterStatic），不是 /static/ 底下。
//
// /offline.css 目前沒有任何頁面引用（它樣式化的是 body.offline-shell 這個
// 獨立離線頁，實際上沒被產生——SW 的 offlineResponse 用 inline style）。
// 仍留在清單裡：SHELL 本來就會預快取它，列進 network-first 只是避免日後
// 真的接上時踩到同一種 stale 問題。
const NETWORK_FIRST = [
  '/static/app.css',
  '/offline.js',
  '/offline.css',
  '/static/vehicle-search.js',
  '/static/scan.js',
  '/static/photo-markup.js'
];

// 純 vendor 靜態檔：版本固定、內容不變，cache-first 沒有 stale 風險，
// 而且離線時仍要能用（離線區網部署的核心需求）。
const CACHE_FOREVER = [
  '/static/htmx.min.js',
  '/static/html5-qrcode.min.js'
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

// 頁面按下「立即更新」時的通知：讓等待中的新版本立刻接管，
// 不必等所有分頁都關閉（預設行為）。少了這段，提示橫幅按了不會有反應。
self.addEventListener('message', (e) => {
  if (e.data && e.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
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

  // 會隨版本改變的檔（app.css 等）：network-first，離線才退回快取。
  // 這樣改了 CSS 之後一般重整就能看到新版，不必 Shift+F5。
  if (NETWORK_FIRST.includes(url.pathname)) {
    e.respondWith(
      fetch(req)
        .then((res) => {
          if (res.ok && res.type === 'basic') {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put(req, copy));
          }
          return res;
        })
        .catch(() => caches.match(req).then((r) => r || offlineResponse()))
    );
    return;
  }

  // 純 vendor 靜態檔與離線外殼：cache-first（內容固定，離線區網的核心需求）
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
