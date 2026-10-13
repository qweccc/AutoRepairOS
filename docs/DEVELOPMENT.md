# AutoRepairOS 開發規範

給所有協作 agent 的統一規範。修改本專案前請完整讀過，新增頁面/功能時**照相同模式**做，才能維持一致性與可維護性。

---

## 1. 技術棧（固定，勿擅自更換）

| 層 | 技術 | 版本/備註 |
|---|---|---|
| 語言 | Go | 1.22+，`internal/` 分層 |
| HTML | Templ（`.templ`） | 型別安全，勿再寫字串拼接 HTML |
| 互動 | HTMX | 局部更新、即時搜尋，CDN 載入 |
| 樣式 | 自訂 CSS（深色主題） | 定義在 `components.PageStyle()`，含 responsive |
| DB | SQLite（WAL 模式） | modernc.org/sqlite（純 Go，CGO_ENABLED=0） |
| 遷移 | 編號 SQL | `internal/db/migration/NNN_xxx.up.sql` |
| 密碼 | bcrypt | golang.org/x/crypto/bcrypt |
| 部署 | Docker 多階段 + compose | 單一 `autorepairos` 服務，port 8090 |

**禁止：** 字串拼接 HTML（`strings.Builder` 產 table）、jQuery、前端框架（React/Vue）、任何需要 npm build 的工具鏈。

### 視覺辨識（拍照自動填入）架構

`internal/integration/vision`：分層混合策略，三種辨識方式可切換（`.env` 控制）。

| 模式 | 用途 | 說明 |
|---|---|---|
| `VISION_MODE=cloud` | 外部 AI Vision API | OpenAI 相容 `/v1/chat/completions`（Gemini 等），準確率最高，需連網 |
| `VISION_MODE=local` | 本地 llama-server | Qwen2.5-VL-3B（GGUF），完全離線；`VISION_AUTOSTART=true` 時由後端自動管理程序生命週期 |
| 條碼掃描 | 前端 html5-qrcode | 零件條碼，零伺服器負擔，純前端 |

**local 模式自動開關（VISION_AUTOSTART）生命週期：**
1. 辨識請求進來 → `healthOK()` 探測服務（1s timeout）
2. 沒開 + `VISION_AUTOSTART=true` → `exec.Command("llama-server", "-m <GGUF>", "--mmproj <投影檔>", "--port <從URL取>")` 背景啟動（不彈視窗，`HF_HUB_OFFLINE=1` 不碰網路）
3. 輪詢 `/health` 直到就緒（最多 150 秒；CPU 上 3B 約 10-60 秒）
4. 用完啟動閒置看守：超過 `VISION_IDLE_MINUTES`（預設 10）分鐘沒人用 → 自動 Kill
5. 防孤兒雙保險：正常關閉走 signal handler + `StopVisionClient()`；強制 kill 靠 **Windows Job Object（`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`，父死子亡）**

**離線優先：** `VISION_MODEL_PATH` 有值時直接吃本地 GGUF，不用 `-hf` 規格（`-hf` 開機時會連 HuggingFace 驗證，防火牆/離線環境會卡住）。視覺模型**必須**帶 `VISION_MMPROJ_PATH`（投影檔），否則只當文字模型用。

**跨平台：** `vision_windows.go`（Job Object + HideWindow）與 `vision_unix.go`（stub）以 build tag 分流；動這個目錄務必雙平台 build（`GOOS=linux go build ./...`）。

---

## 2. 目錄結構與職責

```
AutoRepairOS/
├── main.go                  # 路由註冊、中介層（RequireLogin/CounterUp）
├── build.sh                 # Release 建置（exe + docker，版本含日期時間）
├── Dockerfile               # 多階段建置
├── docker-compose.yml       # 服務定義、named volume、web bind-mount
├── .env                     # 機密與環境設定（gitignore，勿 commit）
├── .env.example             # 範本（進版控）
├── release/                 # exe 產物（autorepair_vYYYYMMDD-HHMM.exe）
├── web/
│   ├── components/          # 共用 Templ 元件（layout、common）
│   └── pages/               # 各頁面 Templ（xxx.templ → 自動產生 xxx_templ.go）
├── internal/
│   ├── handler/             # HTTP handler（業務邏輯 + 呼叫 Templ render）
│   ├── model/               # struct 定義（DB 欄位對應）
│   ├── db/                  # DB 連線、migration SQL
│   ├── session/             # session token 管理
│   ├── seed/                # 初始資料（owner 帳號等）
│   ├── cron/                # 排程任務
│   └── integration/         # line、telegram 外部整合
└── docs/
    ├── SRS.md               # 需求規格
    └── DEVELOPMENT.md       # 本文件（含 CSS 設計系統，§9）
```

**分工原則：**
- `web/pages/*.templ` 只負責「長什麼樣」，不放業務邏輯
- `internal/handler/*.go` 負責查 DB、組資料、呼叫 Templ `.Render()`
- handler 不可直接產 HTML 字串

---

## 3. 新增一個 CRUD 列表頁的標準流程

以「供應商 suppliers」為範本，照做即可。

### 3.1 Migration（若需要新表/新欄位）

在 `internal/db/migration/` 新增 `NNN_功能名.up.sql`（NNN 遞增，目前到 025）：

```sql
-- NNN: 簡短說明
-- 為什麼需要：（一句話）
ALTER TABLE xxx ADD COLUMN yyy TEXT DEFAULT '';
-- 若為對照表，INSERT OR IGNORE 進 lookup_items
INSERT OR IGNORE INTO lookup_items (category, code, label, sort_order) VALUES
    ('category_name', '01', '標籤', 1);
```

規則：
- 一律 `IF NOT EXISTS` / `INSERT OR IGNORE`（可重跑）
- 新欄位給 `DEFAULT ''` 或 `0`（舊資料不為 NULL）
- 對照資料進 `lookup_items`，勿硬編碼在 Go

### 3.2 Model（struct）

在 `internal/model/models.go` 加 struct，欄位用 `json:"..."` tag：

```go
type Supplier struct {
    ID       int64     `json:"id"`
    Name     string    `json:"name"`
    Active   int       `json:"active"`
    CreatedAt time.Time `json:"created_at"`
}
```

### 3.3 Templ 頁面（`web/pages/xxx.templ`）

```templ
package pages

import (
    "strconv"
    "autorepairos/web/components"
)

type XxxRow struct {  // 列資料結構
    ID   int64
    Name string
}

// ListXxxPage 列表頁（HTMX 即時搜尋）。
templ ListXxxPage(rows []XxxRow, total, page, size int, q string) {
    @components.Layout("頁面標題", "/路徑", "owner", "owner", "AutoRepair OS") {
        <div class="card">
            <h2>列表（共 { strconv.Itoa(total) } 筆）</h2>
            <input type="search" name="q" value={ q }
                placeholder="搜尋..."
                hx-get="/api/xxx/search"
                hx-trigger="keyup changed delay:300ms, search"
                hx-target="#xxx-rows"
                hx-swap="innerHTML"
                style="...">
            <table>
                <tr><th>名稱</th><th>操作</th></tr>
                <tbody id="xxx-rows">
                    for _, r := range rows {
                        @XxxRowContent(r)
                    }
                </tbody>
            </table>
            if len(rows) == 0 {
                <div class="empty">沒有資料</div>
            }
            @components.Pager("/路徑", page, calcPages(total, size), total)
        </div>
    }
}

templ XxxRowContent(r XxxRow) {
    <tr>
        <td>{ r.Name }</td>
        <td><a class="btn btn-s" href={ "/xxx/" + strconv.FormatInt(r.ID, 10) + "/edit" }>編輯</a></td>
    </tr>
}

// XxxSearchRows 即時搜尋回傳的純列（無 Layout）。
templ XxxSearchRows(rows []XxxRow) {
    if len(rows) == 0 {
        <tr><td colspan="2" style="...">查無資料</td></tr>
    }
    for _, r := range rows {
        @XxxRowContent(r)
    }
}
```

**Templ 鐵則（踩過的坑）：**
1. Go 邏輯（`:=`、`if`）在 templ 內**必須用 `{{ }}` 包**，但 `for`、`if...{}` 控制結構不用
2. `onclick="return confirm(...)"` 無法直接用字串 → 改用 `data-confirm="訊息"` 屬性（JS 已統一處理）
3. `if a { } else { }` 在 templ 內縮排敏感，容易壞 → 改用兩個獨立 `if`（`if x { A }` + `if !x { B }`）
4. 危險 HTML 片段用 `@templ.Raw(preEscapedHTML)`，其餘純文字用 `{ value }`（自動跳脫）
5. struct 名稱不可與其他 `.templ` 檔案重複（曾撞 `InsuranceRow`）→ 加前綴如 `VehicleInsuranceRow`
6. 修改 `.templ` 後**一定要跑 `templ generate`** 才會產生 `_templ.go`

### 3.4 Handler（`internal/handler/xxx.go`）

```go
func ListXxx(w http.ResponseWriter, r *http.Request, user model.User) {
    // 1. 解析查詢參數（q/page/size）
    // 2. 組 SQL where + args
    // 3. countRows 取 total
    // 4. Query 取 rows，scan 進 []model.Xxx
    // 5. 轉成 []pages.XxxRow
    // 6. pages.ListXxxPage(...).Render(r.Context(), w)
}

// SearchXxxAPI 即時搜尋 API（HTMX 用）。
func SearchXxxAPI(w http.ResponseWriter, r *http.Request) {
    // 1. 取 q
    // 2. Query WHERE ... LIKE ? LIMIT 50
    // 3. 轉 []pages.XxxRow
    // 4. pages.XxxSearchRows(...).Render(r.Context(), w)  // 純列，無 Layout
}
```

**Handler 鐵則：**
- 新增 handler 要在檔案頂部 `import` 加 `"autorepairos/web/pages"`
- 回傳 HTML 一律 `.Render(r.Context(), w)`，不用 `w.Write([]byte(...))`
- 錯誤提示給使用者看的用 Templ 元件或既有 `fail()`/`redirectMsg()`

### 3.5 路由（`main.go`）

```go
// 頁面
mux.HandleFunc("GET /xxx", handler.RequireLogin(handler.ListXxx))
// 即時搜尋 API
mux.HandleFunc("GET /api/xxx/search", handler.RequireLogin(handler.SearchXxxAPI))
```

**權限中介層：**
- `RequireLogin` — 需登入
- `CounterUp` — 櫃檯以上（寫入操作，如開工單、收款）
- 不加中介層 — 公開（僅 `/login`、`/webhook/line` 等）

---

## 4. HTMX 即時搜尋標準模式

**前端（templ）：**
```html
<input type="search" name="q"
    hx-get="/api/xxx/search"
    hx-trigger="keyup changed delay:300ms, search"
    hx-target="#xxx-rows"
    hx-swap="innerHTML">
```

- `delay:300ms` — 輸入停止 300ms 才送（防抖）
- `hx-target` 指向 `<tbody id="xxx-rows">` — 只換表格 body，不整頁重整
- API 回傳**純 `<tr>` 片段**（`XxxSearchRows`），不是完整 HTML 頁

**已完成的 16 個即時搜尋：**
`/api/{customers,inventory,work-orders,vehicles,lookup,insurance,suppliers,appointments,subcontracts,equipment,staff,receivables,insurances,inspections,expenses}/search`

**不需要即時搜尋的頁面：**
- 編輯/新增表單（form-only）
- 管理面板（scheduled-tasks，資料量小 + 操作按鈕）
- 圖表頁（analytics、reports）
- 工單子頁（worklogs，單一工單資料量小）

---

## 5. Release 規格

### 版本號
`vYYYYMMDD-HHMM`（日期 + 時間，24 小時制）。一天可 build 多版不覆蓋。
範例：`v20261013-1338`

### 建置
```bash
cd AutoRepairOS && bash build.sh
```

產出：
- exe：`release/autorepair_vYYYYMMDD-HHMM.exe`（~22 MB，Windows 單機版）
- **`release/web/` 自動同步最新 `web/`**（`start.ps1` 開頭會檢查 `web\index.html` 存在才啟動，少了會直接報錯；不同步 → 車廠跑舊前端）
- docker image：`autorepairos-autorepairos:vYYYYMMDD-HHMM`（~48 MB）

### 部署
```bash
docker compose up -d --force-recreate   # 用最新 build 的 image
```

- 服務名：`autorepairos`，對外 port **8090**（8080 保留給本機 llama-server）
- DB 在 named volume `autorepairos-data`（**非** host bind-mount，避免 Windows SQLite disk I/O error 4618）
- `web/` 用 bind-mount `./web:/app/web`（改前端兩邊立即生效）

### 登入帳號（seed）
`.env` 控制：
```
RUN_SEED=true
DEFAULT_OWNER_USERNAME=owner
DEFAULT_OWNER_PASSWORD=<在 .env 設定>
```
seed 只在空 DB 時建立。既有 volume 改 .env 密碼不會生效，需刪 volume 重建：
```bash
docker compose down
docker volume rm autorepairos_autorepairos-data
docker compose up -d
```

### 本地測試（不動容器）
```bash
go build -o "$LOCALAPPDATA/Temp/ar_smoke.exe" .
DB_PATH="file:data/autorepair.db?_busy_timeout=5000" PORT=8099 "$LOCALAPPDATA/Temp/ar_smoke.exe"
```
- **測試一律用 8099**（8080=本機 ornith llama、8090=正式容器、8091=sqlite-web、8092=視覺模型 Qwen）
- **測完一定關掉**：`taskkill /F /IM ar_smoke.exe`
- 若測視覺辨識（VISION_AUTOSTART=true），app 會自動在 8092 起 llama-server；
  測完殺 app 時 Job Object 會連帶清掉它（強制 kill 也不會留孤兒）
- 視覺辨識的冷啟動請求會等模型載入（約 10-60 秒），curl 要給 -m 200 以上

---

## 6. 測試流程（每次改動後必做）

1. `go build ./...` — 必須 exit 0
2. `GOOS=linux go build ./...` — 動到 `internal/integration/vision` 時必做（Docker 是 Linux build）
3. `templ generate` — 改了 `.templ` 一定要跑
4. 本地 8099 起 smoke test：
   - `curl` 登入（正確密碼 302、錯誤 401）
   - 每個動到的頁面 curl 200
   - 即時搜尋頁確認 `grep -c "hx-get.*search"` > 0
   - **長請求寫檔再讀**：冷啟動/辨識這類慢請求，curl 輸出要用 `> file` 重定向，
     直接接 `| tail` 在 Windows bash 會 exit 23（寫入錯誤）
5. `taskkill /F /IM ar_smoke.exe` — 測完關掉（視覺服務會被 Job Object 連帶清理）
6. `bash build.sh` — 產 release
7. `docker compose up -d --force-recreate` — 部署驗證

**密碼/機密：** 絕不寫進程式碼、絕不 commit。用 `.env`（已 gitignore），`.env.example` 放範本。

---

## 7. 命名慣例

| 類型 | 慣例 | 範例 |
|---|---|---|
| Templ 頁面 | `ListXxxPage`、`EditXxxPage`、`ShowXxxPage` | `ListSuppliersPage` |
| 列結構 | `XxxRow` | `SupplierRow` |
| 即時搜尋元件 | `XxxSearchRows` | `SupplierSearchRows` |
| 搜尋 API | `SearchXxxAPI` | `SearchSuppliersAPI` |
| Migration | `NNN_功能_snake.up.sql` | `022_vehicle_registration_fields.up.sql` |
| DB 表 | 複數 snake_case | `inventory_items`、`work_orders` |
| Route | kebab-case | `/insurance-companies`、`/work-orders` |
| API route | `/api/複數/search` | `/api/suppliers/search` |

---

## 8. 常見錯誤與修法

| 錯誤 | 原因 | 修法 |
|---|---|---|
| `templ parsing error: expected nodes` | `if/else` 縮排壞了 | 拆成兩個獨立 `if`，或用 patch 逐行修 |
| `XxxRow redeclared` | struct 名稱跨檔重複 | 加前綴（`VehicleXxxRow`） |
| `undefined: pages` | handler 沒 import | 加 `"autorepairos/web/pages"` |
| `undefined: pageNo/countRows` | lint 單檔看不到同 package | 忽略 lint，直接 `go build ./...` |
| 容器登入 401 | volume 是舊 DB，沒 seed | 刪 volume 重建（見 §5） |
| Docker build 失敗 `web/pages not in std` | Dockerfile build stage 沒 COPY web/ | Dockerfile 已修，勿移除該行 |
| 頁面 404 | binary 舊 | `docker compose up -d --force-recreate` |
| SQLite disk I/O 4618 | Windows bind-mount | 用 named volume（已是現況） |

---

## 9. CSS 設計系統

全站樣式集中在 **`web/static/app.css`**（單一深色主題）。改樣式請改這裡，**勿在個別 templ 內重寫 `<style>`**（login 頁的少量額外樣式除外，僅覆寫表單版面）。

> 靜態檔實際由 `internal/handler/static/app.css` embed 進 binary（`//go:embed`）。
> 兩處內容相同，修改時**兩邊都要同步**（或改完 `cp web/static/app.css internal/handler/static/app.css`）。

### 9.1 色彩（深色主題）

| 用途 | 色碼 |
|---|---|
| 背景 | `#0f172a` |
| 卡片/Header 底 | `#1e293b` |
| 邊框/分隔 | `#334155` |
| 主色（連結、強調、主鈕） | `#38bdf8` |
| 文字主 | `#e2e8f0` |
| 文字次/標籤 | `#94a3b8` |
| 文字弱/佔位 | `#64748b` |
| hover 列底色 | `#263449` |

### 9.2 類別速查

**版面**

| 類別 | 用途 |
|---|---|
| `.card` | 內容卡片（每頁主要區塊） |
| `.wrap` | 登入頁置中容器 |
| `.full` | 表單內跨整行的欄位（`grid-column: 1/-1`） |

**按鈕 `.btn` + 修飾**

| 類別 | 用途 | 顏色 |
|---|---|---|
| `.btn-p` | 主要動作（新增、開工單） | 藍 |
| `.btn-s` | 次要動作（編輯、取消、搜尋） | 灰 |
| `.btn-d` | 危險（刪除、停用） | 紅 |
| `.btn-o` | 警告（調庫、停用切換） | 橘 |

**狀態標籤 `.pill` + 色彩**

| 類別 | 用途 | 顏色 |
|---|---|---|
| `.p-completed` | 完成/啟用/成功 | 綠 |
| `.p-progress` | 進行中/分類標籤 | 藍 |
| `.p-pending` | 停用/取消 | 黃 |
| `.p-waitparts` | 等待/待檢驗 | 橘 |
| `.p-apprno` | 逾期/失敗/錯誤 | 紅 |
| `.p-cancelled` | 已取消/中性 | 灰 |

**表單 `.frm`**
- `.frm` 用 CSS Grid，預設**兩欄**（手機自動單欄）
- 內部 `label` 為「上下」排版（文字在上、輸入框在下）
- 需跨整行的欄位加 `.full`
- 輸入元件：`input`/`select`/`textarea` 已統一深色樣式

**表格**
- `table`/`th`/`td` 已設深色樣式，`tr:hover` 高亮
- 數字右對齊：該 `<td>` 加 `.num`

**其他**

| 類別 | 用途 |
|---|---|
| `.stat` | 統計數字區塊（大數字 + 小標籤） |
| `.msg` + `.msg-ok`/`.msg-err` | 提示訊息（成功綠/錯誤紅） |
| `.empty` | 空狀態提示（置中灰字） |
| `.badge` | Header 角色標籤 |
| `code` | 等寬字（料號、單號） |

### 9.3 響應式斷點

| 斷點 | 調整 |
|---|---|
| `≤768px` | nav 字體縮小、main/card padding 縮小、表格字體 12px、stat 全寬、表單單欄 |
| `≤480px` | nav 更窄、字體 11px |

手機操作以「點選」為主，故按鈕與表格都保留足夠點擊面積。

### 9.4 HTMX 函式庫

`web/static/htmx.min.js`（本地，非 CDN）— 離線區網部署也能用。
在 `components.Layout` 與 `login.templ` 的 `<head>` 以 `<script src="/static/htmx.min.js">` 載入。

**為什麼本地不用 CDN：** 車行是離線區網部署，CDN 斷線互動就全掛。已下載固定版本（2.0.4）內建。

### 9.5 新增頁面該怎麼用

1. 包在 `@components.Layout(...)` 裡 → 自動有 header/nav/app.css/htmx
2. 內容用 `.card` 區塊
3. 列表用 `<table>` + 數字欄加 `.num`
4. 操作鈕依語意選 `.btn-p`/`.btn-s`/`.btn-d`/`.btn-o`
5. 狀態用 `.pill` + 對應色彩類別
6. 表單用 `.frm`，跨整行欄位加 `.full`
7. 需要新樣式 → 加進 `app.css` 並更新本節，勿內嵌 `<style>`

---

## 10. 給其他 Agent 的檢查清單

新增/修改頁面前確認：
- [ ] 讀過本文件 §1-§8
- [ ] 用 Templ，不用字串拼接 HTML
- [ ] 列表頁加 HTMX 即時搜尋（照 §4 模式）
- [ ] handler 用 `.Render()` 不用 `w.Write`
- [ ] 新表/欄位走 migration，不用硬編碼
- [ ] 對照資料進 `lookup_items`
- [ ] 機密進 `.env`，不進程式碼/commit
- [ ] 改完跑 §6 測試流程
- [ ] commit 訊息說清楚「做了什麼 + 為什麼」

有疑問或發現規範過時，更新本文件而非繞過它。
