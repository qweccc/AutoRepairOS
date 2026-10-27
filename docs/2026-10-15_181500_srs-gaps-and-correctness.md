# SRS 缺口補齊與正確性修復 Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** 補齊 SRS 中 30+ 項未完成需求，修掉 7 項資料正確性／安全缺陷，並補上 SRS 自身的文件缺口。

**Architecture:** 分四層推進——先修正確性與安全（會讓帳務出錯或資料外洩，且是其他功能的基礎），再補 SRS 文件缺口（§6 資料模型、§8.3 Service Layer），然後按業務價值補功能缺口，最後處理 SRS 文件與功能的落差。新增功能一律走「migration → handler/service → templ → 測試」。

**Tech Stack:** Go 1.25 + SQLite(WAL) + Templ + HTMX，單機 exe 與 Docker 雙模式。

---

## 現況事實（已實測確認，非推斷）

| 事實 | 數據 |
|---|---|
| migration 數量 | 28 個，最大編號 025 |
| 直接 DELETE 的檔案數 | 12 個檔、18 處 |
| internal/ 目錄 | cmd/config/cron/db/handler/integration/model/nav/seed/session（**無 service 層**） |
| SRS §6 資料模型 | 只有標題一行，無內容 |
| SRS §8.3 架構圖 | 有「業務邏輯層 (Service Layer)」，實作沒有對應目錄 |
| 用 GET 做的變更操作 | 26 條路由（delete/receive/toggle/run/deactivate/reactivate） |
| session cookie | 有 HttpOnly，**無 SameSite、Secure=false** |

---

## Phase 0：正確性與安全修復（最高優先，會讓帳務出錯）

這 7 項不是功能缺失而是「現有功能會算錯錢或洩漏資料」，必須先修——後續功能都建立在這些流程上。

### Task 0.1: currentUser 檢查 active 欄位（停用員工仍可登入）

**Objective:** 被停用的員工無法再用既有 session 或重新登入。

**Files:**
- Modify: `internal/handler/auth.go:39-52`（currentUser 的 SELECT）
- Modify: `internal/handler/staff.go:406-412`（DeactivateStaff 順手清 session）
- Test: `internal/handler/staff_active_test.go`

**Step 1:** 確認 model.User 有沒有 Active 欄位
```bash
grep -n "Active" internal/model/models.go | head -3
```
若沒有則加上 `Active bool`（migration 014_users_active 已加過 DB 欄位，只需 struct）。

**Step 2:** currentUser 的 SELECT 加 active 並在 =0 時視同帳號不存在
```go
err = db.DB.QueryRow(
    "SELECT id, username, role, name, active FROM users WHERE id = ?", userID,
).Scan(&user.ID, &user.Username, &user.Role, &user.Name, &user.Active)
if err != nil || !user.Active {
    // 帳號已刪除或已停用：清掉 session，讓前端導回登入頁
    session.Delete(token)
    if err == nil {
        err = http.ErrNoCookie   // 停用視同未登入
    }
    return user, err
}
```

**Step 3:** DeactivateStaff 停用後刪掉該使用者的所有 session
```go
// 停用後立刻讓所有已開啟的 session 失效，
// 否則被停用者手上的瀏覽器分頁仍可操作（active=0 只在下次登入時才擋得住）。
session.DeleteByUserID(id)
```
`internal/session/session.go` 加 `DeleteByUserID(userID int64)`，遍歷 sessions.json 刪掉該 user 的 token。

**Step 4:** 測試（三個案例）
- 停用後用原 session cookie 打任一頁 → 302 到 /login
- 停用後用帳密登入 → 401
- 復職後原帳密可登入 → 302

```bash
go test ./internal/handler/ -run TestStaffActive -v
```

**Step 5:** Commit
```bash
git add internal/handler/auth.go internal/handler/staff.go internal/session/session.go internal/model/models.go internal/handler/staff_active_test.go
git commit -m "fix(auth): 停用員工即刻失效，session 與登入雙向檢查 active"
```

---

### Task 0.2: session cookie 加 SameSite（CSRF 防線第一層）

**Objective:** 跨站請求不會帶上 session cookie。

**Files:**
- Modify: `internal/session/session.go:97-108`（SetCookie）
- Modify: `internal/session/session.go:113-118`（ClearCookie）

**Step 1:** 兩個函式都加 `SameSite`
```go
func SetCookie(w http.ResponseWriter, token string) {
    http.SetCookie(w, &http.Cookie{
        Name:     cookieName,
        Value:    token,
        Path:     "/",
        HttpOnly: true,
        // Lax：同站請求與頂層導覽都帶 cookie（正常使用不受影響），
        // 但跨站的 POST/PUT/DELETE 不帶——這是 CSRF 的主要防線。
        // 不能用 Strict：從 LINE/簡訊點連結進來時 cookie 不會帶，會被踢回登入頁。
        SameSite: http.SameSiteLaxMode,
        Secure:   false, // 本機 HTTP 部署；上線走 TLS 時改 true（.env 加 SWITCH）
        MaxAge:   int(sessionTTL.Seconds()),
    })
}
```
ClearCookie 同樣加 `SameSite: http.SameSiteLaxMode`，否則清除有時不生效。

**Step 2:** 驗證登入流程未壞
```bash
cd release && PORT=8099 ./autorepair_v*.exe &
# 登入 302、未登入頁面 302、錯誤密碼 401
taskkill /F /IM autorepair_v*.exe
```

**Step 3:** Commit
```bash
git commit -am "fix(session): cookie 加 SameSite=Lax，擋掉跨站 POST 帶 cookie"
```

---

### Task 0.3: CSRF token（變更操作全面防護）

**Objective:** 所有 POST/DELETE/PUT 都驗 token，GET 變更路由一併收斂。

**設計取捨：** 26 條 GET 變更路由（如 `/customers/{id}/delete`）一次全改 POST 會動到所有 templ 的連結寫法。分兩步：先加 CSRF token（立刻見效），再逐步把 GET 改 POST（低風險批次作業）。

**Files:**
- Create: `internal/csrf/csrf.go`
- Test: `internal/csrf/csrf_test.go`
- Modify: `internal/session/session.go`（token 存 session）
- Modify: `internal/handler/layout.go`（renderPage 注入 hidden input）
- Modify: `web/components/layout.templ`（templ 頁注入）
- Modify: `internal/handler/authz.go`（加 VerifyCSRF 中介層）

**Step 1: csrf 套件**
```go
package csrf

import (
    "crypto/rand"
    "crypto/subtle"
    "encoding/base64"
)

// Token 產生 32 bytes 隨機值（base64url，不含 padding，可直接放 HTML 屬性）。
func Token() (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return base64.RawURLEncoding.EncodeToString(b), nil
}

// Valid 常數時間比對，避免時序側信道。
func Valid(got, want string) bool {
    if got == "" || want == "" {
        return false
    }
    return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
```

**Step 2:** session 端存 token（沿用既有 sessions.json，不新增儲存）
- `session.CSRFToken(userID)` 回傳該使用者既有的 token，沒有則建
- 登入時產生、登出時清掉

**Step 3: 中介層**
```go
// VerifyCSRF 驗 POST/PUT/PATCH/DELETE 的 csrf_token。
// GET 不驗（GET 不應有副作用；既有的 GET 變更路由由 Task 0.4 逐步收斂）。
func VerifyCSRF(fn http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case "POST", "PUT", "PATCH", "DELETE":
            if !csrf.Valid(r.FormValue("csrf_token"), wantCSRF(r)) {
                http.Error(w, "CSRF 驗證失敗", http.StatusForbidden)
                return
            }
        }
        fn(w, r)
    }
}
```
`main.go` 用一個包裝函式把 VerifyCSRF 套在所有 POST 路由上（比逐條掛省事且不會漏）。

**Step 4:** 兩種頁面都注入 hidden input
- `renderPage` 的 `<body>` 開頭加 `<input type="hidden" name="csrf_token" value="...">`——但表單散落在各處，更穩健的做法是在 `pageStyle` 之後插入一段 JS：頁面上所有 `<form>` 在 submit 前自動補 hidden。這樣不用改 30 個 templ。
- HTMX 的 hx-post 也吃同一套：監聽 `htmx:configRequest` 把 token 加進參數。

```js
// 掛在 app.js（新建）：表單與 HTMX 請求自動帶 csrf_token
document.addEventListener('submit', e => {
  const f = e.target;
  if (f.querySelector('[name=csrf_token]')) return;
  const i = document.createElement('input');
  i.type = 'hidden'; i.name = 'csrf_token'; i.value = window.CSRF_TOKEN;
  f.appendChild(i);
}, true);
document.body.addEventListener('htmx:configRequest', e => {
  e.detail.parameters['csrf_token'] = window.CSRF_TOKEN;
});
```
`window.CSRF_TOKEN` 由頁面注入 `<script>window.CSRF_TOKEN="..."</script>`。

**Step 5:** 測試
- 無 token 的 POST → 403
- 錯 token → 403
- 正確 token → 正常
- GET 不受影響

**Step 6:** Commit

---

### Task 0.4: 採購單收貨與庫存調整改原子操作

**Objective:** 併發請求不會重複入庫或把庫存扣成負數。

**Files:**
- Modify: `internal/handler/purchase_orders.go:377-392`（狀態檢查移進交易）
- Modify: `internal/handler/inventory.go:326-370`（ApplyStockAdjust 條件更新）
- Test: `internal/handler/purchase_receive_test.go`（補併發案例）

**Step 1: 收貨——狀態搶占放進交易內**
問題是 `SELECT status` 在 `Begin()` 之前，兩個併發請求都讀到 draft 就各自入庫。

改成在交易內用條件 UPDATE 搶占（SQLite 單寫者，這招有效）：
```go
tx, err := db.DB.Begin()
if err != nil { fail(w, "進貨失敗"); return }
defer tx.Rollback()

// 在交易內搶占「draft → received」。rowsAffected=0 表示別人已經收過了
// （或狀態本來就不是 draft），這時整筆交易放棄，不會重複入庫。
res, err := tx.Exec(`UPDATE purchase_orders SET status='received', received_at=? 
                      WHERE id=? AND status='draft'`, nowText(), id)
if err != nil { fail(w, "進貨失敗"); return }
n, _ := res.RowsAffected()
if n == 0 {
    fail(w, "此單已進貨或狀態不符")
    return
}
```
原本交易末尾那條 `UPDATE purchase_orders SET status='received'` 刪掉（已提前做）。
品項為空的檢查移到交易內或保留在外（只讀檢查，無併發風險）。

**Step 2: 庫存調整——條件 UPDATE + 重讀驗證**
原本 `SELECT stock_quantity` 在交易外讀、交易內無條件 `+ delta`，併發時兩個請求都通過舊值檢查。

改成條件 UPDATE 直接讓 DB 把關：
```go
// 條件 UPDATE：只有扣完不會變負數才生效。rowsAffected=0 就是庫存不足，
// 這讓檢查與寫入成為同一個原子操作，併發扣庫不會都通過舊值檢查。
res, err := tx.Exec(`UPDATE inventory_items 
                     SET stock_quantity = stock_quantity + ? 
                     WHERE id = ? AND stock_quantity + ? >= 0`, delta, id, delta)
if err != nil { fail(w, "調庫失敗"); return }
n, _ := res.RowsAffected()
if n == 0 { fail(w, "庫存不足"); return }

// 異動紀錄的 before/after 要在 UPDATE 之後重讀，否則併發下數字不準
var after int
if err := tx.QueryRow("SELECT stock_quantity FROM inventory_items WHERE id = ?", id).
    Scan(&after); err != nil { fail(w, "調庫失敗"); return }
if err := recordMovement(tx, id, delta, after-delta, after, reason, "manual", 0, actorID); err != nil {
    fail(w, "寫入異動紀錄失敗"); return
}
```

**Step 3:** 測試補併發案例（用 goroutine 同時打，斷言只有一筆成功、庫存正確）

**Step 4:** Commit

---

### Task 0.5: 庫存搜尋 API 的成本價權限

**Objective:** 非授權角色看不到成本價。

**Files:**
- Modify: `internal/handler/inventory.go:137-170`
- Modify: `main.go`（路由加角色限制）

**做法：** `main.go` 的 `/api/inventory/search` 目前是 `RequireLogin`（任何登入者都可）。改成只有 owner 能拿到完整欄位，其他人拿精簡版（無 cost_price）。

```go
// SearchInventoryAPI 的成本價只給 owner。
// 技師等角色用同一個 API 搜尋零件時不該看到成本（庫存頁面本來就有這層限制，
// API 繞過它等於沒有）。
if user.Role != "owner" {
    // 回傳不含 cost_price 的列（前端表格本來就不顯示給非 owner）
}
```
需要把 `SearchInventoryAPI` 的簽名改成吃 `model.User`（與 `AllStaff` 包法一致），路由改 `handler.AllStaff(handler.SearchInventoryAPI)`。

**驗證：** owner 打有 cost_price；tech 打沒有。

---

### Task 0.6: 收款時呼叫結帳 service，不再直接改工單狀態

**Objective:** 收款自動完成結帳該做的一切（扣庫存、釋預留、更新客戶總額），而不是只改一個 status 欄位。

**決策：方案 B——收款時自動觸發結帳流程。** 結帳邏輯抽成 `service.CheckoutService`（Task 1.2），工單完工與收款兩個路徑都呼叫它，行為一致。

**Files:**
- Create: `internal/service/checkout.go`（Task 1.2 的產出，此任務使用）
- Modify: `internal/handler/receivables.go:449-454`
- Modify: `internal/handler/workorders.go`（完工路徑改呼叫同一 service）
- Test: `internal/service/checkout_test.go`、`internal/handler/receivables_test.go`

**Step 1:** 先把結帳流程現況摸清楚（這決定要搬哪些邏輯）
```bash
grep -n "status.*paid\|status.*completed\|reserved\|消費總額\|total_spent" internal/handler/workorders.go | head -20
```

**Step 2:** 結帳流程搬進 service（一個交易內做完）
```go
func (s *CheckoutService) Checkout(workOrderID, actorID int64) error {
    tx, err := s.db.Begin()
    if err != nil { return err }
    defer tx.Rollback()

    // 1. 搶占狀態（同 Task 0.4 的條件 UPDATE 手法，併發安全）
    res, err := tx.Exec(`UPDATE work_orders SET status='paid',
        updated_at=datetime('now','localtime')
        WHERE id=? AND status<>'paid'`, workOrderID)
    if err != nil { return err }
    n, _ := res.RowsAffected()
    if n == 0 { return nil }   // 已結帳過，視為成功（idempotent）

    // 2. 扣庫存 + 釋放預留量（把原本 workorders.go 完工時做的搬進來）
    // 3. 更新客戶消費總額
    // 4. 寫異動紀錄

    return tx.Commit()
}
```

**Step 3:** 收款 handler 改用 service
```go
// 收款完成 → 觸發結帳（扣庫存、釋預留、更新客戶總額）。
// 原本這裡只 UPDATE status='paid'，跳過結帳該做的事，導致付款狀態與
// 庫存、客戶帳目不一致。結帳邏輯抽成 service，完工路徑也呼叫同一份。
if woID > 0 {
    if err := service.NewCheckoutService(db.DB).Checkout(woID, actor); err != nil {
        // 結帳失敗不可讓收款紀錄孤立存在：整筆撤回
        log.Printf("收款後結帳失敗（工單 #%d）: %v", woID, err)
        redirectMsg(w, r, "/receivables/new", "收款已記錄但結帳失敗，請至工單手動結帳")
        return
    }
}
```
> 注意：收款寫入與結帳應該在**同一個交易**裡。若難以一次到位，退而求其次是上面的
> 「結帳失敗要明確告知並可手動補救」，不可假裝成功。

**Step 4:** 測試
- 收款後工單 status=paid、庫存已扣、客戶 total_spent 已更新
- 併發兩次收款只有一次真正結帳（idempotent）
- 重複結帳不會重複扣庫存

---

### Task 0.7: 軟刪除取代 18 處 DELETE

**Objective:** 符合 §8.7「重要資料與工單明細不得實體刪除」。

**Files:**
- Create: `internal/db/migration/026_soft_delete.up.sql`
- Modify: 12 個 handler 檔的 delete 函式
- Test: 各刪除函式補「刪除後仍可查詢、列表不再顯示」的案例

**Step 1: migration 加 deleted_at**
```sql
-- 026: 軟刪除。SRS §8.7 要求重要資料與工單明細不得實體刪除
-- （車廠事後要查「這台車上次來是什麼時候」、「這張單誰刪的」）。
-- 做法：各表加 deleted_at，刪除改成設時間戳；列表查詢一律加 IS NULL 過濾。
ALTER TABLE customers       ADD COLUMN deleted_at TEXT;
ALTER TABLE vehicles        ADD COLUMN deleted_at TEXT;
ALTER TABLE work_orders     ADD COLUMN deleted_at TEXT;
ALTER TABLE work_order_items ADD COLUMN deleted_at TEXT;
ALTER TABLE appointments    ADD COLUMN deleted_at TEXT;
ALTER TABLE equipment       ADD COLUMN deleted_at TEXT;
ALTER TABLE equipment_maintenance ADD COLUMN deleted_at TEXT;
ALTER TABLE inventory_items ADD COLUMN deleted_at TEXT;
ALTER TABLE vehicle_insurances ADD COLUMN deleted_at TEXT;
ALTER TABLE vehicle_inspections ADD COLUMN deleted_at TEXT;
ALTER TABLE work_order_photos ADD COLUMN deleted_at TEXT;
ALTER TABLE worklogs        ADD COLUMN deleted_at TEXT;
ALTER TABLE purchase_orders ADD COLUMN deleted_at TEXT;
ALTER TABLE purchase_order_items ADD COLUMN deleted_at TEXT;
ALTER TABLE subcontracts    ADD COLUMN deleted_at TEXT;
ALTER TABLE maintenance_templates ADD COLUMN deleted_at TEXT;
```

**Step 2:** 抽一個共用 helper 避免 18 處各寫一遍（DRY）
```go
// internal/db/softdelete.go
// SoftDelete 把實體刪除改成設 deleted_at。
// table 只接受本專案白名單內的常數，不接受外部輸入（避免 SQL injection）。
func SoftDelete(table, idCol string, id int64) (bool, error) {
    allowed := map[string]bool{
        "customers": true, "vehicles": true, "work_orders": true, /* ... */
    }
    if !allowed[table] {
        return false, fmt.Errorf("table not allowed: %s", table)
    }
    q := fmt.Sprintf("UPDATE %s SET deleted_at = datetime('now','localtime') WHERE %s = ? AND deleted_at IS NULL", table, idCol)
    res, err := DB.Exec(q, id)
    if err != nil { return false, err }
    n, _ := res.RowsAffected()
    return n > 0, nil
}
```

**Step 3:** 每個 delete 函式從 `DELETE FROM x WHERE id=?` 改成 `db.SoftDelete("x", "id", id)`。

**Step 4:** 所有列表 SQL 加 `AND x.deleted_at IS NULL`。這是容易漏的一環，用 grep 驗證：
```bash
grep -rn "FROM customers" --include=*.go internal/handler/ | grep -v deleted_at
```
每一條都該出現 deleted_at（或確認是給軟刪除資料用的特殊查詢）。

**Step 5:** 測試：刪除後 (a) 列表不再出現 (b) DB 裡資料還在 (c) 重複刪除回「找不到」。

---

## Phase 1：補 SRS 文件缺口

### Task 1.1: §6 資料模型概要

**Objective:** 讓 SRS 的資料模型章節有實際內容，且與 migration 一致。

**Files:**
- Modify: `docs/SRS.md` §6

**做法：** 從 migration 反推（權威來源是 `internal/db/migration/*.up.sql` + `internal/model/models.go`），產出：
- 每張表的用途一句話
- 主鍵、外鍵、關鍵欄位
- 關聯圖（文字描述或 mermaid）

```bash
# 產生表格清單當底稿
grep -h "CREATE TABLE" internal/db/migration/*.up.sql
```

驗證一致性：文件提到的每張表都真的存在，反過來每個 migration 的表都寫進文件。

### Task 1.2: 建立 internal/service/ 骨架（對應決策 1）

**Objective:** 讓實作開始符合 SRS §8.3 的架構圖（Service Layer），並作為 Phase 2 新功能的落點。

**Files:**
- Create: `internal/service/service.go`（骨架與共用慣例說明）
- Create: `internal/service/checkout.go`（第一個 service，由 Task 0.6 使用）
- Test: `internal/service/checkout_test.go`

**設計原則（這決定分層有沒有價值）：**
```go
package service

// CheckoutService 工單結帳。
// 為什麼用依賴注入而不是吃全域 db.DB：
// 單元測試可以餵 :memory: DB 驗證扣庫存邏輯，不必起整個 HTTP 服務；
// 而且這是「真解耦」與「只是把程式碼搬家」的分界線。
type CheckoutService struct {
    db *sql.DB
}

func NewCheckoutService(db *sql.DB) *CheckoutService {
    return &CheckoutService{db: db}
}

// Checkout 結帳一筆工單：扣庫存、釋放預留量、更新客戶消費總額。
// 全部在同一個交易內，中途失敗整筆撤回——這是 Task 0.4 那類
// 「非原子操作」的正解，也是為什麼它值得抽成 service（兩個消費者都要正確）。
func (s *CheckoutService) Checkout(workOrderID, actorID int64) error {
    tx, err := s.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()
    // ... 扣庫存 / 釋預留 / 更新總額 / 標 paid
    return tx.Commit()
}
```

**漸進式原則（重要）：**
- 只把「有兩個以上消費者」的邏輯抽進 service。目前已確認：結帳（工單完工 + 收款）
- Phase 2 的新功能（庫存扣減、SOP、AP 帳款…）**一開始就寫在 service**
- 舊 handler 不為了架構一致而單獨重構，只有本來就要改的（Task 0.4、0.6）順便搬

**驗證：**
```bash
go test ./internal/service/ -v
go build ./...
```

---

## Phase 2：功能缺口（按業務價值排序）

以下每項都依 DRY 原則先找現成可重用者，避免重造。

### Task 2.1: 通知失敗自動重試（FR-NOTI-04）— 最小成本高價值

**現況：** 外送失敗標 failed，處理器只撈 pending，恢復連線不會重試。

**Files:** `internal/cron/cron.go`（processPendingNotifications / markFailed）

**做法：** 撈取條件改成 `pending OR (failed AND retry_count < N AND 上次重試超過 M 分鐘)`，並在 `internal/model/models.go` 確認 notifications 表有 retry_count；沒有就加 migration 027。

上限避免無限重試（例：5 次後停止，並在排程頁顯示）。

### Task 2.2: 保養／驗車／保險到期提醒（FR-NOTI-02、FR-INS-03）

**依賴：** 保險到期日欄位（Task 2.5）

**Files:** `internal/cron/cron.go` 加掃描、migration 加需要的日期欄位

### Task 2.3: 預約日曆視圖 + 前日提醒（FR-APP-01、FR-APP-05）

**Files:** `web/pages/appointment.templ`、`internal/handler/appointments.go`、`internal/cron/cron.go`

### Task 2.4: 轉帳支付方式 + 結帳自動建收款（FR-FIN-01、FR-FIN-02）

**依賴：** Task 0.6（收款／結帳分流）——必須先做，否則會建立在錯誤的流程上。

### Task 2.5: 保險保單號碼／到期日／理賠金額（FR-INS-01~03）

**Files:** migration 加 `policy_number`、`policy_expiry`、`claim_amount`、`claim_note`

### Task 2.6: 逐項 SOP 清單與完工控制（FR-WO-08）

**Files:** migration 建 `work_order_sop_items`、`internal/handler/workorders.go`、`web/pages/workorder.templ`

### Task 2.7: 技師片語庫 + 語音輸入相容性（FR-WO-07）

**重要範圍修正：語音辨識不做，片語庫才做。**

語音輸入不用我們整合——手機／電腦的**輸入法**就有麥克風按鈕（Android Gboard、
iOS 聽寫、Windows 語音輸入），它在輸入法層級把語音轉成文字後直接送進 `<textarea>`，
**完全不經過我們的伺服器，離線也能用**。我們要做的只有兩件事：

1. **片語庫**（這才是真正省打字的部分）：技師常用描述存成可點選的片語，
   點一下整段插入輸入框。車廠使用者多不擅長打字，這個價值最高。
2. **確認輸入框可用**：textarea 不要有攔貼、長度上限等問題。

若整合 Web Speech API 反而會有反效果——它要連 Google 伺服器，離線區網部署下
只會出現「按了沒反應」的按鈕，比沒有更糟。

**Files:**
- Create: `internal/db/migration/0XX_tech_phrases.up.sql`
- Modify: `internal/handler/worklogs.go`（片語 CRUD）
- Modify: `web/pages/workorder.templ`（片語選擇 UI）
- Test: `internal/handler/tech_phrase_test.go`

### Task 2.8: 車身示意圖標記舊傷（FR-VH-06）

**Files:** `web/pages/vehicle.templ`、`internal/handler/photos.go`

### Task 2.9: 獨立估價單草稿 + 轉正式工單（FR-WO-01）

### Task 2.10: 車主數位 Passbook（FR-VH-05）

### Task 2.11: 客戶一鍵同意／拒絕核准（FR-WO-06）

**現況：** 核准按鈕指向內部收據頁（客戶看不到），LINE Flex 未接通。

### Task 2.12: AP 帳款與 Aging（FR-FIN-05）

**Files:** `internal/handler/print_docs.go`、`purchase_orders.go`

### Task 2.13: 毛利計入工資與外包成本（FR-FIN-06）

**現況：** 分析頁把非零件成本標為「未記錄」。需要把 worklogs（工資）與 subcontracts（外包）接進損益計算。

### Task 2.14: 保固成本統計（FR-FIN-07）

### Task 2.15: 報表匯出 CSV/Excel/PDF（FR-FIN-08、FR-PRINT-07）

**現況：** 只有經營分析有 CSV。

**做法：** 抽一個共用的 export helper（DRY），各報表呼叫，不要每個報表各寫一份 CSV 邏輯。

### Task 2.16: 經營分析圖表與維度分組（FR-ANA-01）

**注意：** 離線部署不能用 CDN 圖表庫，要用本地 JS（跟 htmx.min.js 同樣做法）。

### Task 2.17: 技師效率指標（FR-ANA-02）

### Task 2.18: 方案代碼毛利分析（FR-ANA-06）

### Task 2.19: （已取消）SMS Gateway（FR-CRM-04）

**決策：取消。** 使用者決定不做簡訊，通知只走 LINE。客戶資料的 SMS 偏好選項
改為隱藏或標示「暫不提供」，避免櫃檯選了卻沒人送。`internal/cron/cron.go`
的 SMS 分支一併移除（若有的話）。

### Task 2.20: 完工結算卡（FR-NOTI-03）

### Task 2.21: 生日關懷（FR-NOTI-03）

### Task 2.22: 照片納入備份 + 離線佇列存檔（§7.2）

**現況：** 備份只備 DB；前端離線佇列不存檔案內容。

### Task 2.23: 條碼掃描接上庫存表單（FR-INV-05）

**現況：** scan.js 有掃描函式，庫存表單沒接。

---

## Phase 3：效能驗證（§7.1）

**現況：** 沒有任何基準測試。

**做法：**
1. 寫一個灌 10,000 筆的測試資料腳本（`tools/seed_bench.go`，可重複執行、帶 `--reset`）
2. 用 `curl -w "%{time_total}"` 量常用列表頁（工單、庫存、客戶）
3. 低於 300ms 就記錄數據；超過就加 index（`CREATE INDEX` 放 migration）
4. 通知佇列灌 1,000 筆量處理時間

驗收數據寫進 `docs/DEVELOPMENT.md` §6，之後改動可回歸比對。

---

## 已決策事項（使用者回覆 2026-10-15）

| # | 議題 | 決策 |
|---|---|---|
| 1 | §8.3 Service Layer | **方案 B：真的抽 service 層**（使用者要分層） |
| 2 | 語音輸入 FR-WO-07 | **不做語音辨識**，改用輸入法麥克風 + 做技師片語庫 |
| 3 | SMS Gateway FR-CRM-04 | **取消**，通知只走 LINE |
| 4 | 收款 vs 結帳 | **方案 B：收款時自動觸發結帳流程**（抽成共用 service，兩邊呼叫） |
| 5 | Phase 2 範圍 | **全做** |

### 關於決策 1（Service Layer）的困難與對策

使用者要求分層。先講清楚困難在哪，才不會做出一層沒有價值的間接層：

**困難 1：切割點沒有客觀標準。** 以 `ReceivePurchaseOrder` 為例，它現在同時做
HTTP 解析、DB 交易、庫存計算、建零件。要切成 handler／service 兩段，但「讀
FormValue」跟「決定扣多少庫存」之間那條線畫在哪，每個人切法不同。

**困難 2：改動面 = 30 個 handler 全動。** 風險高、review 負擔大，而收益只有在
「有第二個消費者」時才出現。目前只有 HTTP 一個前端。

**困難 3（真正的坑）：SQLite 連線是全域的。** `internal/db` 用 package 層級的
`var DB *sql.DB`，service 層要嘛繼續吃這個全域（那分層只是把程式碼搬家，
沒有真正解耦），要嘛改成依賴注入（每個 service 結構帶 `*sql.DB`，handler 建構時
傳入）——後者才是真解耦，但要動所有 service 的簽名與所有 handler 的呼叫點。

**對策：漸進式，不一次全翻。**
1. **先立骨架**：建 `internal/service/`，只放「確實有兩個以上消費者」的邏輯。
   目前已確認的案例：結帳流程（工單完工 與 收款 都要觸發，正是決策 4 要的）。
2. **service 一律依賴注入**：`type CheckoutService struct{ db *sql.DB }`，
   不吃全域 `db.DB`。這樣單元測試可以餵 `:memory:` DB，也才有真正的解耦。
3. **新增邏輯一律進 service**：Phase 2 的新功能（庫存扣減、SOP、AP 帳款…）
   一開始就寫在 service，不再堆進 handler。
4. **舊 handler 不動**，除非剛好要改那段（如 Task 0.4、0.6 本來就要重寫，
   順便抽進 service）。不為了「架構一致」而單獨重構沒有 bug 的程式碼。

這樣分層是真的（新邏輯都分層、共用邏輯都分層），但不會為此把 30 個檔全翻一遍。
若日後出現第二個前端（App/API），再把舊 handler 逐步搬進去。

> 對應地，SRS §8.3 架構圖**保持不變**（本來就畫了 Service Layer），改的是實作去符合它。

---

## 驗收方式（每階段共用）

```bash
# 每個 task 完成後
go build ./...                    # 必須 exit 0
GOOS=linux go build ./...         # 動到 vision 時必做
go test ./...                     # 全套件 ok
templ generate                    # 改了 .templ 一定要跑
bash build.sh                     # 產 release（舊版自動歸檔）
# docker 部署驗證四件套：login 200、登入 302、動到的頁面 200、sqlite-web 200
```

**新增 migration 一定要從 log 確認真的套用**（named volume 的 DB 會跨版本留存，migration 沒跑會靜默失敗）：
```bash
docker logs autorepairos-autorepairos-1 2>&1 | grep -i "026\|027"
```

---

## 風險

- **Task 0.7 軟刪除是改動面最大的一項**（18 處 DELETE + 所有列表 SQL 都要加過濾）。漏一處過濾的症狀是「刪掉的資料又出現在列表」，極難察覺。建議逐檔改、逐檔 grep 驗證，不要一次全改。
- **Task 0.3 CSRF** 若 token 注入方式有漏洞，會讓所有 POST 表單 403（功能全壞）。先做 SameSite（Task 0.2）就有基本防護，CSRF token 可以穩紮穩打慢慢推。
- **Phase 2 多項互相依賴**（2.4 依 0.6、2.2 依 2.5），順序錯會返工。
- **離線限制**：圖表庫、語音辨識、CDN 一律不能用，每個新功能都要問「斷網時還能不能用」。
