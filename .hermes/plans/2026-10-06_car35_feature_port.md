# CAR35 功能移植計畫

> **For Hermes:** 依序實作，每階段完成後跑完整回歸測試。

**Goal:** 把 CAR35（Delphi + Access 車行系統，1997–2014）有而 AutoRepair OS 缺的功能，依營收影響排序逐一移植。

**Architecture:** 每個功能群組 = 一個 migration（新增表/欄位）+ handler + 頁面 + 測試。沿用現有 `internal/handler` / `internal/db/migration` 結構，不改架構。

**Tech Stack:** Go + SQLite WAL + Docker + Tailwind（server-rendered HTML）+ LINE Messaging API

---

## 參考基準

分析標的：`C:\Users\fr\35-20231008`
分析方法：32 份 Crystal Report 反推資料模型，見 `docs/CAR35_COMPARISON.md`

可重跑：`python scripts/analyze_car35.py`、`python scripts/compare_car35.py`

### 現有資料表（13 張）

```
backup_records  customers  inventory_items  notifications  scheduled_tasks
sessions  suppliers  technician_worklogs  users  vehicles
work_order_items  work_order_photos  work_orders
```

---

## 盤點對照總表

| # | CAR35 功能 | CAR35 來源 | 我們的差距 | 階段 |
|---|-----------|-----------|-----------|------|
| 1 | 保險公司主檔 | `Company`（Carp78/92） | **完全沒有** | P0-1 |
| 2 | 保險理賠工單 | `Header.Insurance`（Carp32/77/91） | **完全沒有** | P0-1 |
| 3 | 付款方式拆分 | `Pay.Cash/Check/Card`（Carp13/23/52/92） | **完全沒有** | P0-2 |
| 4 | 應收帳款明細 | `In.Amount/Due/Status`（Carp82） | 只有 `customers.unpaid_amount` 單一數字 | P1-1 |
| 5 | 應付帳款 | `Out.Amount/Due/Status`（Carp83） | 完全沒有 | P1-2 |
| 6 | 採購單 | `List.In/Out/Sequence`（Carp84） | 只有庫存數字，無進貨紀錄 | P1-2 |
| 7 | 廠商完整資料 | `Firm.Tax/Sales/Fax`（Carp72） | 只有名稱/電話/地址 | P1-2 |
| 8 | 代客保管 | `Header.Consign`（Carp51） | 完全沒有 | P2-1 |
| 9 | 客戶生日 | `Customer.Birthday/Age`（Carp81） | 完全沒有 | P2-2 |
| 10 | 車輛年檢 | `Temp.License/Order`（Carp75） | 只有 `inspection_due_date` | P2-3 |
| 11 | 車輛年份查詢 | `Car.Year/Color/Driver/Engine`（Carp31/74） | 有 year/engine，缺 color/driver | P2-4 |
| 12 | 裝潢分帳 | `Header.Decorate`（Carp91） | 完全沒有 | P3 |
| 13 | 員工事資 | `Staff.Start/End/Bank/BB/Birthday`（Carp79） | 只有帳密角色 | P3 |
| 14 | 車型/原廠對照 | `Model` / `OEM`（Carp31/74/75/76） | 完全沒有 | P3 |
| 15 | 零件分類 | `Material.Class` / `Store`（Carp61/73） | 完全沒有 | P3 |

### 明確不做

- 總帳會計、稅務申報（SRS 已排除；CAR35 的 Carp9x 是 1999 年代做法，不符台灣現行稅制）
- 分店管理（SRS 為單店，CAR35 的 `Branch` 用不到）
- 郵寄標籤（`Label-1` / `Letter-1`，無實用價值）

---

## 階段 P0-1：保險公司 + 理賠工單

**為何第一：** 保險理賠佔車行營收 5–7 成。CAR35 用 5 份報表處理這塊。

### Task 1: insurance_companies 表

**Files:**
- Create: `internal/db/migration/004_insurance_companies.up.sql`
- Create: `internal/db/migration/004_insurance_companies.down.sql`

**Step 1: 寫 schema**

```sql
-- 保險公司主檔
-- CAR35 對應：Company 表（Carp78 廠商/公司主檔、Carp92 公司別帳務彙總）
CREATE TABLE IF NOT EXISTS insurance_companies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,            -- 簡稱，如「國泰」「富邦」
    name TEXT NOT NULL,                   -- 公司全名
    tax_id TEXT DEFAULT '',               -- 統一編號
    phone TEXT DEFAULT '',
    contact_person TEXT DEFAULT '',
    address TEXT DEFAULT '',
    claim_phone TEXT DEFAULT '',          -- 客服電話（車行常用）
    notes TEXT DEFAULT '',
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_insurance_companies_active ON insurance_companies(active);
```

**Step 2: 驗證 migration 可跑**

```bash
cd C:/Users/fr/AutoRepairOS
export PATH="$PATH:/c/Program Files/Go/bin"
go build ./... && echo BUILD_OK
```

**Step 3: 啟動容器確認 migration 成功**

```bash
export PATH="$PATH:/c/Users/fr/AppData/Local/Programs/DockerDesktop/resources/bin"
docker compose up -d --build --force-recreate
sleep 10
docker logs autorepairos-autorepairos-1 2>&1 | grep -i migration | tail -3
```

**Step 4: Commit**

```bash
git add internal/db/migration/004_*.sql
git commit -m "feat(db): 新增 insurance_companies 保險公司主檔"
```

---

### Task 2: work_orders 加保險欄位

**Files:**
- Create: `internal/db/migration/005_work_orders_insurance.up.sql`
- Create: `internal/db/migration/005_work_orders_insurance.down.sql`

**Step 1: 寫 schema**

```sql
-- 工單加保險理賠欄位
-- CAR35 對應：Header.Insurance（Carp32 完整估價單、Carp77 保險公司車輛、Carp91 保險/稅務拆分）
ALTER TABLE work_orders ADD COLUMN insurance_company_id INTEGER REFERENCES insurance_companies(id);
ALTER TABLE work_orders ADD COLUMN claim_no TEXT DEFAULT '';          -- 保險理賠編號
ALTER TABLE work_orders ADD COLUMN is_insurance INTEGER NOT NULL DEFAULT 0;  -- 1=保險件 0=自費件
ALTER TABLE work_orders ADD COLUMN accident_date TEXT DEFAULT '';      -- 事故日期
ALTER TABLE work_orders ADD COLUMN deductible_amount REAL NOT NULL DEFAULT 0;  -- 折舊/自負額

CREATE INDEX IF NOT EXISTS idx_work_orders_insurance ON work_orders(insurance_company_id);
CREATE INDEX IF NOT EXISTS idx_work_orders_is_insurance ON work_orders(is_insurance);
```

**Step 2: 確認 migration 框架支援 ALTER TABLE ADD COLUMN**

檢查 `internal/db/migrate.go` 是否有 idempotency 保護（重複執行會報錯）。若無，需加 `columnExists()` 檢查。

**Step 3: 驗證 + Commit**

```bash
go build ./... && echo BUILD_OK
docker compose up -d --build --force-recreate && sleep 10
curl -s -m 8 -o /dev/null -w "login=%{http_code}\n" http://localhost:8090/login
git add internal/db/migration/005_*.sql
git commit -m "feat(db): work_orders 加保險理賠欄位（company/claim_no/accident_date/deductible）"
```

---

### Task 3: 保險公司 CRUD handler + 頁面

**Files:**
- Create: `internal/handler/insurance.go`
- Modify: `main.go`（路由註冊）
- Create: `internal/handler/insurance_test.go`

**Step 1: 寫失敗測試**

```go
// internal/handler/insurance_test.go
package handler

import "testing"

func TestInsuranceCompanyCRUD(t *testing.T) {
    // 用現有測試 fixture 模式建立 srv
    // 1. GET /insurance-companies → 200，內容含「新增保險公司」
    // 2. POST /insurance-companies 建立 → 302，DB 有列
    // 3. GET 編輯頁 → 200
    // 4. POST 編輯 → 欄位已更新
    // 5. tech 角色 → 403
}
```

**Step 2: 跑測試確認失敗**

```bash
go test ./internal/handler/ -run TestInsuranceCompanyCRUD -v -count=1
```
Expected: FAIL（handler 未定義）

**Step 3: 實作 handler**

`internal/handler/insurance.go` 提供：
- `ListInsuranceCompanies(w, r, user)` — 列表 + 搜尋
- `CreateInsuranceCompany` — 建立（code 唯一性檢查）
- `EditInsuranceCompanyForm` / `UpdateInsuranceCompany`
- `DeleteInsuranceCompany` — 軟刪除（active=0）

權限：`CounterUp`（櫃檯可維護保險公司清單）

**Step 4: 路由註冊**

```go
mux.HandleFunc("GET /insurance-companies", handler.AllStaff(handler.ListInsuranceCompanies))
mux.HandleFunc("POST /insurance-companies", handler.CounterUp(handler.CreateInsuranceCompany))
mux.HandleFunc("GET /insurance-companies/{id}/edit", handler.CounterUp(handler.EditInsuranceCompanyForm))
mux.HandleFunc("POST /insurance-companies/{id}", handler.CounterUp(handler.UpdateInsuranceCompany))
mux.HandleFunc("GET /insurance-companies/{id}/delete", handler.OwnerOnly(handler.DeleteInsuranceCompany))
```

**Step 5: 跑測試確認通過**

```bash
go test ./internal/handler/ -run TestInsuranceCompanyCRUD -v -count=1
```
Expected: PASS

**Step 6: 側邊欄加入口 + Commit**

在 `renderPage` 的導覽加「保險公司」，然後：
```bash
git add internal/handler/insurance.go internal/handler/insurance_test.go main.go
git commit -m "feat(insurance): 保險公司 CRUD 與頁面"
```

---

### Task 4: 工單頁整合保險欄位

**Files:**
- Modify: `internal/handler/workorder_new.go`（開立工單表單）
- Modify: `internal/handler/workorders.go`（工單詳情顯示）
- Modify: `internal/handler/phase2_checkout_test.go`（既有測試要加新欄位）

**Step 1: 開立工單表單加保險區塊**

在 `workorder_new.go` 的表單加：
```html
<label>件別<select name="is_insurance">
  <option value="0">自費</option>
  <option value="1">保險理賠</option>
</select></label>
<label>保險公司<select name="insurance_company_id">...</select></label>
<label>理賠編號<input name="claim_no"></label>
<label>事故日期<input name="date" name="accident_date"></label>
```

**Step 2: 工單詳情顯示保險資訊**

在 `workorders.go` 的 `ShowWorkOrder` 加一列：
```go
if wo.IsInsurance {
    body += `<div class="pill p-waitparts">保險理賠</div> 保險公司：X / 理賠編號：Y`
}
```

**Step 3: CreateWorkOrder handler 讀新欄位**

在 INSERT 加 5 個欄位。注意：**欄位數必須與 Scan 目標數一致**（這是本專案反覆踩到的坑，見 `go-sqlite-app-debugging` skill）。

**Step 4: 跑回歸**

```bash
go build ./... && go vet ./... && go test ./internal/... -count=1
python -u scripts/test_workflow.py   # 期望 10/10
python -u scripts/test_status_flow.py  # 期望 13/13
```

**Step 5: Commit**

```bash
git add internal/handler/workorder_new.go internal/handler/workorders.go
git commit -m "feat(work-order): 開立工單整合保險理賠欄位"
```

---

### Task 5: 報表區分保險件與自費件

**Files:**
- Modify: `internal/handler/reports.go`
- Create: `scripts/test_reports_insurance.py`

**Step 1: 加報表區塊**

在營收報表加：
```
保險件營收：$X（佔比 Y%）
自費件營收：$Z
保險公司別彙總表
```

**Step 2: 測試**

```python
# scripts/test_reports_insurance.py
# 建立一張保險件、一張自費件，驗證報表分別統計
```

**Step 3: 回歸 + Commit**

```bash
python -u scripts/test_reports_insurance.py
git add internal/handler/reports.go scripts/test_reports_insurance.py
git commit -m "feat(reports): 營收報表區分保險件與自費件"
```

---

### Task 6: LINE 推播保險估價單

**Files:**
- Modify: `internal/integration/line/templates.go`
- Create: `internal/handler/estimate_notify.go`

**Step 1: 加 Flex Message 模板**

在 `templates.go` 加 `EstimateFlexMessage(wo)`：
```
🔧 AutoRepair OS 估價單
車牌：ABC-1234
保險公司：國泰
估價金額：$12,000
自負額：$3,000

[同意維修] [再議]
```

**Step 2: 加「送LINE 估價單」按鈕**

在工單詳情頁（`approval_status = pending` 時）加按鈕 → POST `/work-orders/{id}/send-estimate`

**Step 3: 測試**

```bash
go test ./internal/integration/line/ -count=1
```

**Step 4: Commit**

```bash
git commit -m "feat(line): 保險估價單 Flex Message 與推播"
```

---

### P0-1 完成檢查

```bash
export PATH="$PATH:/c/Program Files/Go/bin"
go build ./... && go vet ./... && go test ./internal/... -count=1
python -u scripts/test_auth.py          # 10/10
python -u scripts/test_workflow.py      # 10/10
python -u scripts/test_status_flow.py   # 13/13
python -u scripts/test_phase2.py        # 25/25
python -u scripts/test_phase3.py        # 27/27
```

**全部通過才算完成。**

---

## 階段 P0-2：付款方式拆分

**為何第二：** 沒有支付方式就無法對帳、分析資金流。CAR35 在 4 份報表都拆 Cash/Check/Card。

### Task 7: 付款方式列舉 + 欄位

**Files:**
- Create: `internal/db/migration/006_payment_method.up.sql`
- Create: `internal/handler/checkout_payment.go`

```sql
ALTER TABLE work_orders ADD COLUMN payment_method TEXT DEFAULT '';  -- cash/check/card/transfer/receivable
ALTER TABLE work_orders ADD COLUMN paid_amount REAL NOT NULL DEFAULT 0;
ALTER TABLE work_orders ADD COLUMN paid_at TEXT DEFAULT '';
ALTER TABLE work_orders ADD COLUMN invoice_no TEXT DEFAULT '';  -- 發票號碼
```

### Task 8: 結帳頁付款方式選擇

在 `receipt.go` 的結帳表單加付款方式下拉 + 實收金額 + 發票號碼。

### Task 9: 報表依付款方式分析

營收報表加「現金 $X / 支票 $Y / 信用卡 $Z / 轉帳 $W」 breakdown。

### Task 10: 對帳報表

新增「實收 vs 應收」對照（對應 CAR35 的 `Pay` vs `Real`）。

---

## 階段 P1-1：應收帳款明細

**為何：** 車行分期付款常見，目前只有單一數字無法追蹤。

### Task 11: receivable 表

```sql
-- 應收帳款明細
-- CAR35 對應：In 表（Carp82：In.Amount / In.Due / In.Status）
CREATE TABLE IF NOT EXISTS receivables (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER REFERENCES work_orders(id),
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    amount REAL NOT NULL,
    due_date TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',   -- open / partial / settled / overdue / void
    settled_amount REAL NOT NULL DEFAULT 0,
    notes TEXT DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS receivable_payments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    receivable_id INTEGER NOT NULL REFERENCES receivables(id),
    amount REAL NOT NULL,
    method TEXT DEFAULT 'cash',
    paid_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    received_by INTEGER REFERENCES users(id)
);
```

### Task 12: 應收帳款頁面 + 分期收款

- 列表：未結清金額、到期日、狀態
- 分期收款：可多次部分收款
- 逾期提醒：接入排程器

### Task 13: 逾期提醒排程

新增排程任務 `receivable_overdue_scan`，每日掃描到期未收的應收款，產出通知。

---

## 階段 P1-2：採購與廠商往來

### Task 14: suppliers 補欄位

```sql
-- CAR35 對應：Firm 表（Carp72：Firm.Tax / Sales / Fax）
ALTER TABLE suppliers ADD COLUMN code TEXT DEFAULT '';
ALTER TABLE suppliers ADD COLUMN tax_id TEXT DEFAULT '';
ALTER TABLE suppliers ADD COLUMN fax TEXT DEFAULT '';
ALTER TABLE suppliers ADD COLUMN contact_email TEXT DEFAULT '';
ALTER TABLE suppliers ADD COLUMN payment_terms TEXT DEFAULT '';  -- 帳期，如「月結 30 天」
```

### Task 15: 採購單 + 入庫

```sql
CREATE TABLE IF NOT EXISTS purchase_orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    supplier_id INTEGER NOT NULL REFERENCES suppliers(id),
    order_no TEXT NOT NULL UNIQUE,
    ordered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL DEFAULT 'ordered',  -- ordered / partial / received / cancelled
    total_amount REAL NOT NULL DEFAULT 0,
    notes TEXT DEFAULT '',
    created_by INTEGER REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS purchase_order_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    purchase_order_id INTEGER NOT NULL REFERENCES purchase_orders(id),
    inventory_item_id INTEGER REFERENCES inventory_items(id),
    description TEXT NOT NULL,
    quantity REAL NOT NULL DEFAULT 1,
    unit_cost REAL NOT NULL DEFAULT 0,
    received_quantity REAL NOT NULL DEFAULT 0
);
```

入庫時自動更新 `inventory_items.stock_quantity` 與加權平均成本。

### Task 16: 應付帳款

```sql
-- CAR35 對應：Out 表（Carp83：Out.Amount / Out.Due / Out.Status）
CREATE TABLE IF NOT EXISTS payables (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    supplier_id INTEGER NOT NULL REFERENCES suppliers(id),
    purchase_order_id INTEGER REFERENCES purchase_orders(id),
    amount REAL NOT NULL,
    due_date TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    settled_amount REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## 階段 P2：加值功能

| Task | 功能 | CAR35 來源 | 說明 |
|------|------|-----------|------|
| 17 | 代客保管 | `Header.Consign`（Carp51） | `work_order_consign` 表，收據要列出 |
| 18 | 客戶生日關懷 | `Customer.Birthday`（Carp81） | `customers.birthday` + 排程送 LINE 祝福 |
| 19 | 車輛年檢提醒 | `Temp.License`（Carp75） | 擴充 `vehicles` 的年檢到期欄位 |
| 20 | 車輛年份/顏色/駕駛 | `Car.Year/Color/Driver`（Carp74） | `vehicles` 加 `color` / `driver_phone` |
| 21 | LINE 生日行銷 | （現代加值） | 生日月客戶名單，可群發 |

### Task 17: 代客保管

```sql
CREATE TABLE IF NOT EXISTS work_order_consign (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_order_id INTEGER NOT NULL REFERENCES work_orders(id),
    description TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    returned_at TEXT DEFAULT '',
    notes TEXT DEFAULT ''
);
```

收據頁要列出保管物品，結帳後標記已歸還。

---

## 階段 P3：低優先

| Task | 功能 | 理由不做 |
|------|------|---------|
| 22 | 員工事資（生日、銀行、到離職日） | 需要時再加，SRS 只要求工時佣金 |
| 23 | 車型/原廠對照表（Model/OEM） | 對單一車行效益低，可改用簡單文字欄位 |
| 24 | 零件分類與倉庫（Class/Store） | 單店不需要多倉庫 |
| 25 | 裝潢分帳（Decorate） | 車行若無裝潢業務可跳過 |

---

## 風險與注意事項

### 1. Migration 框架的 idempotency

`ALTER TABLE ADD COLUMN` 重複執行會報錯。實作前先確認 `internal/db/migrate.go` 的機制：

```bash
grep -n "schema_migrations\|user_version\|columnExists" internal/db/migrate.go
```

若無保護，需在 Task 2 前先補。

### 2. Scan 欄位數必須對齊

本專案反覆踩到的坑：`SELECT` 欄位數 ≠ `Scan` 目標數時，錯誤被 `continue` 吞掉 → 「查得到但一筆都沒處理」。

加欄位後，`workOrderCols`（`workorders.go:14`）與 `scanWorkOrder`（`:20`）必須同步更新，且 SELECT 的欄位順序要一致。

見 `go-sqlite-app-debugging` skill。

### 3. 其他 agent 平行開發

同一 repo 有其他 agent 在工作。動工前：

```bash
git status --short
git log --oneline -3
```

提交前再檢查一次，避免覆蓋他人變更。

### 4. 部署指令

```bash
docker compose up -d --build --force-recreate
```

`--force-recreate` 不可省（否則沿用舊 container）。

### 5. LINE 額度

免費方案每月 200 則推播。保險估價單 + 保養提醒 + 完工通知若超過，需升級。

---

## 驗證方式（每個 Task 都做）

```bash
# 1. 編譯 + 靜態檢查
export PATH="$PATH:/c/Program Files/Go/bin"
go build ./... && go vet ./... && echo OK

# 2. Go 單元測試
go test ./internal/... -count=1

# 3. 部署
export PATH="$PATH:/c/Users/fr/AppData/Local/Programs/DockerDesktop/resources/bin"
docker compose up -d --build --force-recreate
sleep 10

# 4. HTTP 回歸（期望值）
python -u scripts/test_auth.py          # 10/10
python -u scripts/test_workflow.py      # 10/10
python -u scripts/test_status_flow.py   # 13/13
python -u scripts/test_phase2.py        # 25/25
python -u scripts/test_phase3.py        # 27/27
```

---

## 時程估算

| 階段 | Task 數 | 預估 | 產出 |
|------|--------|------|------|
| P0-1 保險 | 6 | 3–4 天 | 保險公司管理 + 理賠工單 + 報表 + LINE 估價單 |
| P0-2 付款 | 4 | 1–2 天 | 付款方式 + 對帳報表 |
| P1-1 應收 | 3 | 1–2 天 | 分期收款 + 逾期提醒 |
| P1-2 採購 | 3 | 2–3 天 | 採購單 + 入庫 + 應付帳款 |
| P2 加值 | 5 | 2 天 | 代客保管 / 生日 / 年檢 |
| P3 低優先 | 4 | 視需求 | 可跳過 |

**P0 + P1 約 8–11 天**，涵蓋 CAR35 90% 的實用功能。

---

## 執行方式

```bash
# 從 P0-1 Task 1 開始，逐個 Task 實作
# 每個 Task 完成後 commit，不要批次提交
```

建議用 `subagent-driven-development` 逐 Task 派 subagent，每個 Task 兩階段審查（spec compliance → code quality）。