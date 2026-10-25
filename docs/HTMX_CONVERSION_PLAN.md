# Inline JS → HTMX 轉換計畫

盤點日期：2026-10-25  
範圍：`web/**/*.templ`、`internal/handler/*.go` 內產生的 legacy HTML、自有 `internal/handler/static/*.js`。  
不重複計入：`htmx.min.js`、`html5-qrcode.min.js`、Templ 產生的 `*_templ.go`、測試與 `.hermes/plans` 文件。  
本文件只有唯讀審計與計畫；本次未修改程式，也未執行 git 操作。

## 結論摘要

- 原始模板共有 **27 個 `<script>` 標籤**：**17 個 inline script**、10 個外部 script 載入；另有 `workorders.go:1231` 的 legacy inline script。
- 自有 authored JS/HTML 共有 **19 個 `fetch()` 呼叫點**（不含 vendor 與產生碼鏡像）：其中頁面/功能 AJAX 主要集中在 invoice OCR、parts/phrases lookup、quick vehicle、workorder legacy add-item；其餘是 scan、photo markup、offline queue、service worker。
- `addEventListener()` 以 source call-site 計，約 **65 處**；其中大多是 scanner、canvas、離線佇列、Service Worker、動態 DOM 與欄位同步，不是 HTMX request。
- 因此「約 40 個 listener」若只算頁面 inline 互動是合理的近似；若把自有 static runtime 與 SW 一併納入，實際數量較高。**不能把所有 listener 都當成待替換 AJAX。**
- 建議第一批只處理搜尋/片段回應與簡單 POST；OCR、canvas、相機、離線佇列、列印、欄位 enable/disable 與多步驟 UI 保留 JS。

分類統計（以功能/區塊，不以每一個 listener 重複計算）：

| 分類 | 數量 | 判定 |
|---|---:|---|
| 🟢 可乾淨轉 HTMX | 5 | 純查詢或表單提交；前提是 endpoint 提供 HTML fragment，並保留既有 CSRF/target |
| 🟡 部分可轉 | 8 | request 可交給 HTMX，但選取、驗證、modal、JSON mapping 或狀態更新仍需 JS |
| 🔴 不宜轉 HTMX | 11 | OCR/vision、canvas、相機、離線/SW、列印、複雜欄位聯動或動態編輯器 |

> 分類是「功能區塊」而非把同一段內的每個 event 算成一項。`fetch()` 現在多回 JSON；若不增加 HTML fragment response，直接把 `hx-get` 指到現有 API 會只把 JSON 塞進 DOM，不能視為完成轉換。

## 逐處盤點

### 🟢 可乾淨轉 HTMX

這些是最適合先做的候選；實作時可保留原 URL，讓 handler 依 `HX-Request` 回傳既有 JSON 或對應 HTML fragment，避免破壞 API client。

| 檔案:行號 | 現況/行為 | API | HTMX 形狀 |
|---|---|---|---|
| `web/pages/workorder.templ:343-399` | 工單新增項目的零件搜尋；輸入後 debounce、取得清單、更新 `#partResults` | `GET /api/parts?q=...` | `hx-get="/api/parts"`、`hx-trigger="keyup changed delay:300ms"`、`hx-target="#partResults"`、`hx-swap="innerHTML"`；endpoint 回傳結果 fragment |
| `web/pages/purchase_order.templ:308-345` | 進貨單庫存零件搜尋；目前已有 hx 屬性但仍以 JS fetch 覆蓋，並在點擊後帶入表單欄位 | `GET /api/parts?q=...` | 統一成單一 HTMX 搜尋 fragment；點選帶值可用 `hx-on`/小型事件 JS，不能保證純 HTMX |
| `internal/handler/workorders.go:1231-1281` | legacy 工單零件搜尋，與新版頁面重複 | `GET /api/parts?q=...` | 與 `workorder.templ` 共用 fragment，移除重複 renderer/JS |
| `web/pages/workorder.templ:403-445` | 技師片語搜尋與結果區更新 | `GET /api/phrases/search?q=...` | `hx-get` + delay + target；片語結果以 fragment 輸出 |
| `web/pages/purchase_order.templ:284-301` | 送出前把 `quantity`/`unit_price_f` 同步到 hidden 欄位 | 無 | 不是 AJAX；若 handler 改讀可見欄位即可移除，否則保留極小 submit JS，不應為 HTMX 硬改 |

**低風險優先清單：** parts search（新版與 legacy 共用）、phrases search、purchase-order parts lookup。Supplier search 只有在 OCR 流程外獨立成普通選擇/搜尋時才屬此類；目前 invoice OCR 內的 supplier matching 不屬純查詢。

### 🟡 部分可轉

| 檔案:行號 | 現況/行為 | API | 為何不能直接移除 JS |
|---|---|---|---|
| `web/pages/workorder_new.templ:146-190` | 快速建立客戶與車輛，POST 後檢查 JSON、重複車牌 confirm、帶回 vehicle id/label/里程並關閉 modal | `POST /api/quick-vehicle` | POST 可改 `hx-post`，但 duplicate 的高風險確認、錯誤 alert、帶值與 focus 仍需 JS；可先讓 server 回 fragment/status |
| `web/components/invoice_scan.templ:37-82` | 讀檔、Base64、送 invoice OCR、顯示 loading/錯誤、把結果套入表單 | `POST /api/vision/invoice` | request 可用 HTMX，但 FileReader、圖片 payload、結果 mapping 與人工複核流程必須保留 |
| `web/components/invoice_scan.templ:96-134` | OCR 後抓 supplier list，精確/包含比對後選取下拉或顯示未找到 | `GET /api/suppliers` | API lookup 可轉，但比對演算法與「新增供應商」決策仍是 JS |
| `web/components/invoice_scan.templ:136-187` | 找不到 supplier 時快速建立，新增 option、選取並顯示 existing/similar 訊息 | `POST /api/suppliers/quick-create` | 可用 HTMX 更新 supplier note/option fragment；JSON 分支與 UI 狀態仍需保留 |
| `internal/handler/workorders.go:1284-1325` | legacy 新增工單項目 POST，收到 JSON 後 append table row、更新總額、reset form、顯示錯誤 | `POST` 至 `form.action`（XMLHttpRequest） | 可改為回傳 row/total fragment，但目前 response 是 JSON，且 reset/disable/error 邏輯仍需少量 JS |
| `web/pages/workorder.templ:359-399` | 零件搜尋結果點擊後寫入 hidden id、描述、價格；mouseenter/leave 樣式 | 無另一次 API | 查詢可 HTMX；選取欄位 mapping 與 hover 可用事件/少量 JS，非純 HTMX |
| `web/pages/purchase_order.templ:315-345` | 搜尋結果點擊後寫入進貨表單 hidden id/料號/價格 | 無另一次 API | 查詢可 HTMX；選取結果至表單欄位仍需 JS 或伺服器回傳帶有明確事件的 fragment |
| `web/pages/vehicle_list.templ:72-116`、`web/pages/vehicle.templ:59-77,128-146` | 品牌車型選「其他」時切換 name、disabled、required、顯示區塊 | 無 | 這是表單欄位聯動；HTMX 不適合每次 select change round-trip，應保留小型 JS |

### 🔴 不宜轉 HTMX

| 檔案:行號 | 現況/行為 | API/外部副作用 | 原因 |
|---|---|---|---|
| `web/components/common.templ:128-153` + `internal/handler/static/scan.js:10-55` | 行照/車牌拍照、FileReader、vision response 回填空欄 | `POST /api/vision/recognize` | 檔案轉 Base64、OCR mapping、只填空欄、錯誤/相機 UI；HTMX 不能取代客戶端檔案處理 |
| `web/components/common.templ:163-176` + `scan.js:67-105` | 相機條碼掃描、啟停 scanner、掃到後 dispatch input/change | 無（`html5-qrcode`） | 相機串流與第三方 scanner lifecycle，不是 server fragment |
| `web/components/invoice_scan.templ:25-260` | invoice OCR 後動態建立可編輯品項列、數量/單價即時計算、刪除/新增、同步 `items_json` | `POST /api/vision/invoice`、`GET /api/suppliers`、`POST /api/suppliers/quick-create` | 多步驟、人工複核、動態資料結構與金額計算；最多只拆出 request/結果 fragment |
| `web/pages/workorder_new.templ:96-120` | 保險件切換後 enable/disable 多欄位，並同步 hidden insurance company mirror | 無 | `disabled` 影響提交語意；round-trip 會改變即時表單行為與資料安全邏輯 |
| `web/pages/workorder_new.templ:192` | Escape 關閉快速建車 modal；同區 `openQuickModal/closeQuickModal` 管理 focus/reset | 無 | 純客戶端 modal lifecycle；不應用 HTMX 取代 |
| `web/pages/estimates.templ:128-145` | 輸入車牌時在本地 options 比對，更新 vehicle id/提示 | 無 | 本地即時比對與 hidden id 一致性，使用 HTMX 會增加不必要 round-trip |
| `web/pages/stage7_forms.templ:517-531` | 動態新增/刪除套餐明細列 | 無 | DOM builder、表單欄位索引/集合編輯；HTMX 可另做 server-rendered row，但不是乾淨替換 |
| `web/pages/print.templ:20`、`quotation.templ:116`、`receipt.templ:101` | `#print` hash 載入後 `window.print()`；頁面按鈕也直接列印 | 無 | 瀏覽器列印命令是本地副作用，HTMX 不提供替代 |
| `internal/handler/static/photo-markup.js:1-307` | 照片 annotation canvas pointer drawing、重繪、resize、刪除與 walkaround canvas 點位 | `GET/POST /api/photos/:id/annotations`、`POST /api/annotations/:id/delete`、`GET/POST/DELETE /work-orders/:id/walkaround-marks` | canvas 座標、pointer capture、繪圖與即時 overlay 是核心 UX；API 刷新可局部 HTMX 化，但 canvas controller 必須留 |
| `internal/handler/static/offline.js:1-660` | IndexedDB draft/photo queue、online/offline、背景 retry、HTMX afterRequest、SW update banner | 動態 POST queue URL；照片 multipart upload | 離線與重試是全域 runtime；改成 HTMX 會失去離線保證與成功後清草稿語意 |
| `internal/handler/static/sw.js:1-160` | Service Worker install/activate/fetch/cache/offline fallback | `fetch(req)` network-first/cache-first | Service Worker lifecycle 與 cache strategy 不屬頁面 DOM 更新 |

## Listener / inline 行為明細

下表按 source 區塊列出 listener，不把同一區塊內的每個匿名 callback 再重複當成「一個轉換專案」。

| 檔案:行號 | listeners / inline handler | 判定 |
|---|---|---|
| `web/components/common.templ:141,164` | `DOMContentLoaded` 初始化 plate/barcode scanner | 🔴 |
| `web/components/invoice_scan.templ:26,37,39,136,191,203,208,213-214,219,230,257` | 初始化、檔案 change、supplier quick-create、品項編輯/刪除/新增、submit sync | 🔴（OCR/編輯器） |
| `web/pages/workorder.templ:359,379,386-387,394` | parts input、結果 click/hover、外部 click 關閉 | 🟡 |
| `web/pages/workorder.templ:419,431,436-437` | phrases input、結果 click/hover | 🟢 request + 🟡 selection |
| `web/pages/workorder_new.templ:115-116,192` | insurance change sync、Escape modal | 🔴 |
| `web/pages/purchase_order.templ:287,315,332` | submit hidden sync、parts input、結果 click | 🟡 |
| `web/pages/estimates.templ:134` | plate input 本地選車 | 🔴 |
| `web/pages/vehicle*.templ` | `onchange="toggleModelOther()"`：兩個 vehicle page 加列表頁共三份 | 🔴 |
| `web/pages/stage7_forms.templ:517` | inline `onclick` 新增列、動態列內移除 | 🔴 |
| `web/pages/print.templ:20`、`quotation.templ:116`、`receipt.templ:101` | window load → print | 🔴 |
| `internal/handler/workorders.go:1241,1246,1264,1271-1272,1279,1284` | legacy parts search/hover/外部 click/submit JSON 更新 | 🟡 |
| `static/vehicle-search.js:120-178` | input/focus/click/keydown/DOMContentLoaded；本地 vehicle selector | 🔴 |
| `static/scan.js:16-75` | file picker/OCR、barcode scanner click/lifecycle | 🔴 |
| `static/photo-markup.js:129-299` | annotation delete、pointer drawing、submit、image/resize、walkaround click | 🔴 |
| `static/offline.js:201-609` | online/offline、draft change/HTMX/submit、photo upload、SW update、DOMContentLoaded | 🔴 |
| `static/sw.js:45-71` | install/activate/message/fetch lifecycle | 🔴 |

## 分階段計畫

### 階段 0：建立 response contract 與防回歸基線

- 確認 `GET /api/parts`、`GET /api/phrases/search` 現有 JSON client 不被破壞；新增 `HX-Request` HTML fragment 分支或新的 fragment endpoint。
- 為每個 target 記錄目前 DOM 結構、空結果文字、debounce、排序、CSRF/header 與成功後欄位值。
- 風險：若直接把 JSON response 套 `hx-target`，會顯示原始 JSON；若共用 handler 未分流，現有 API 測試/其他 client 會回歸。
- 驗收：現有 handler tests 綠；非 HTMX request response 完全不變；瀏覽器手動確認空/多筆/特殊字元。

### 階段 1：parts / suppliers / phrases search

- 🟢 `workorder.templ` parts search、`workorders.go` legacy parts search、`workorder.templ` phrases search。
- 🟢 `purchase_order.templ` parts lookup request；🟡 保留選取後寫入表單的最小 JS。
- 🟡 supplier list 只先抽成可重用 fragment；不要把 invoice OCR 的 supplier matching 整段改成 HTMX。
- 風險：新版與 legacy 兩份 UI 漂移、JSON/HTML response、`part_number`/庫位/庫存/價格 escape、結果選取仍需欄位 mapping。
- 驗收：同一 API、同一 query/debounce/排序；點選後 hidden id、描述、價格、料號與送出 payload 逐欄一致；CSRF 不變。

### 階段 2：簡單 form POST / 工單項目 fragment

- 🟢 盤點可直接回 HTML 的 simple POST forms（既有 `hx-post` 表單優先）。
- 🟡 `internal/handler/workorders.go:1284` 新增項目改為回 row + total fragment；保留錯誤訊息、表單 reset、按鈕 loading 的最小 JS。
- 🟡 `workorder_new` quick vehicle 先只讓 server 回成功/錯誤 fragment，再保留 duplicate confirmation 與帶回 vehicle id 的 JS。
- 風險：JSON response callers、追加列與 total 的 atomicity、表單 reset 時 hidden/CSRF、重複車牌不可默默選錯車。
- 驗收：相同 POST URL、欄位、CSRF；成功後 table/total/表單狀態相同；錯誤與 duplicate confirmation 相同；離線 draft 行為不變。

### 階段 3：局部狀態與共用片段

- 將 invoice supplier note/option 更新、photo annotation list refresh 評估為 HTMX fragment；canvas drawing、OCR FileReader、items editor 保留 JS。
- 把重複的 parts result markup 由 server fragment 統一，避免 `innerHTML` 拼接未 escape。
- 保留 insurance mirror、vehicle model「其他」、estimate 本地 vehicle match、stage7 dynamic rows。
- 風險：partial replacement 會重綁 listener；HTMX swap 後 `photo-markup.js`/scanner 初始化時機；表單欄位 name/disabled semantics。
- 驗收：HTMX swap 前後 listener 不重複；欄位 enable/disable 與原本一致；所有 dynamic fragment 的文字與 attribute escape 測試通過。

### 階段 4：明確保留 JS，縮小 inline surface

- 將可重用且非 HTMX 的邏輯移到既有 static module：insurance sync、model-other、print helper、簡單 modal；不改成框架。
- 不改寫 `offline.js`、`sw.js`、`photo-markup.js`、`scan.js` 的核心流程，只補測試與初始化邊界。
- 風險：把「減少 inline script」誤當成「移除 JS」；外部化時改變載入順序或 `DOMContentLoaded` 行為。
- 驗收：功能行為、API、CSRF、offline retry、print hash 完全一致；無新增 jQuery/framework/npm build。

## 建議順序

1. 先做 **phrases/parts search fragment**（含 `workorder.go` legacy 重複點），因為範圍小、可量測、最接近 HTMX。
2. 接著做 **purchase order parts lookup**，但保留結果選取的最小 JS。
3. 再處理 **簡單 POST / 工單新增項目**，先建立 HTML fragment response contract，再移除 fetch。
4. 最後才評估 quick vehicle、supplier quick-create、invoice OCR 周邊；OCR 本體、insurance mirror、vehicle selector、canvas/photo、offline/SW、print **不要為了 HTMX 指標硬轉**。

若無法證明 API response、CSRF header、更新 target、空結果、錯誤訊息與送出 payload 完全一致，該項目應停留在 🟡，而不是宣稱 🟢。
