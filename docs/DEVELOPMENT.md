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
    └── DEVELOPMENT.md       # 本文件
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
go build -o /tmp/ar_smoke.exe .
DB_PATH="file:data/autorepair.db?_busy_timeout=5000" PORT=8091 /tmp/ar_smoke.exe
```
- 用 8091（8080=llama、8090=正式容器）
- 測完 `taskkill /F /IM ar_smoke.exe`

---

## 6. 測試流程（每次改動後必做）

1. `go build ./...` — 必須 exit 0
2. `templ generate` — 改了 `.templ` 一定要跑
3. 本地 8091 起 smoke test：
   - `curl` 登入（正確密碼 302、錯誤 401）
   - 每個動到的頁面 curl 200
   - 即時搜尋頁確認 `grep -c "hx-get.*search"` > 0
4. `bash build.sh` — 產 release
5. `docker compose up -d --force-recreate` — 部署驗證

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

## 9. 給其他 Agent 的檢查清單

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
