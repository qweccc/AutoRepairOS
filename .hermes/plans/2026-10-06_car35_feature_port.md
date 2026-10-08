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

### 資料盤點修正（2026-10-07，實讀 Data35.mdb 後）

原表是從 32 份 `.rpt` 反推**功能**；補做逐表逐欄盤點後，有 4 處判斷要改。
可重跑：`python scripts/analyze_car35_data.py`（輸出 `data/car35_data_gap.json`）。

| # | 原本寫的 | 實際狀況（非空筆數為證） | 修正 |
|---|---------|------------------------|------|
| 2 | 保險理賠工單「CAR35 有」 | `Maintain Header` 的 `Insurance Flag` / `Company No` / `Name` / `Date` / `Remark` / `Table No` **全部 0/95,440 筆**；保險資訊只出現在 `Remark`，95,440 張工單中僅 50 筆的備註提到保險公司 | CAR35 這塊**設計了但沒在用**。不回填舊工單，只從新工單收結構化資料。車輛保險改用 `Car Insurance List`（2,667 筆，完整） |
| 5 | 應付帳款 `Out`（Carp83） | `Check Out` **0 筆**、`Firm Pay` **0 筆** | 舊系統從未使用應付票據/應付沖銷。這是**全新功能**，不是移植 |
| 6 | 採購單 `List`（Carp84） | `Material Header` 103,916 筆、`Material Detail` 166,315 筆，`Firm No` 10.1 萬筆能對到廠商、料號 16.4 萬筆能對到零件主檔 | 這是**真的進貨單**（不是領料單，明細單價對應 `In Price`）。可回填成採購/入庫歷史，優先度可上調 |
| 8 | 代客保管 `Header.Consign` | `Consign` 欄位**不存在**；`Consign Date` 有 95,396 筆，語意是「交車日期」（`Complete Date` 幾乎相同） | 舊系統**沒有**代客保管功能。純加值，且無歷史資料可遷 |
| 12 | 裝潢分帳 `Header.Decorate` | `Decorate Flag` **0/95,440 筆** | 同 #2，設計了沒用。**移除**此項 |
| 3 | 付款方式拆分 | `Customer Pay` 90,762 筆：現金 83,094 筆、刷卡 7,126 筆、支票 741 筆；金額合計 3.48 億 / 4,417 萬 / 714 萬 | 確認是真的，但**刷卡＋支票不到 8,000 筆**。欄位仍要做，但舊資料回填的價值主要在現金以外的 7,867 筆 |
| — | （新增）收款紀錄本身 | `Customer Pay` 90,762 筆可對到 87,625 張工單 | 應收帳款明細的**歷史資料來源**，不是只有報表格式 |

### 車主對應缺口（最高價值的單一修復）

原本 migration 用**電話**當客戶比對鍵，但 `customers.phone` 有 UNIQUE 限制，
撞號會自動加 `#序號`（例：`07002-232#1`），而 CAR35 的客戶編號被存進
`customers.notes` —— 兩個欄位都不再等於原編號，於是 `migrate_vehicles()`
的客戶比對失敗、整台車被跳過，連帶讓工單/投保/定檢失去車主。

改用**姓名**或 `customers.notes` 的 CAR35 編號重建對應後的實測結果：

| 表 | 現在 | 修好後 | 救回 |
|---|---|---|---|
| `vehicles` | 12,986 / 15,010（86.5%） | 14,986（99.8%） | 2,000 台 |
| `work_orders` | 85,945 / 95,425（90.1%） | 95,362（99.9%） | 9,417 筆 |

`customers` 本身沒有缺口（6,511 個 CAR35 客戶名稱 100% 都在），
`customers` 也沒有同名衝突，所以姓名對應是安全的鍵。
剩下的 63 筆工單車牌是 `'-'`、`'C2'`、`'90'` 這類無效值，不需處理。

其餘查證為真的欄位（可直接遷移）：

| 項目 | 證據 |
|------|------|
| 客戶生日 | `Customer.Birthday` 2,205/8,118（27%） |
| 客戶統編 | `Customer.Tax Name` 8,112、`ID No` 3,049 |
| 駕駛人 | `Car.Driver Name` 14,607/15,010（97%） |
| 車輛定檢 | `Car.Start Check Date` 4,902、`Car Check List` 21,176 筆 |
| 技師分工 | `Maintain Detail.Staff No` 231,188/390,425 筆有值，可算技師產值 |
| 零件分類/庫位 | `Material.Class No`、`Store No`（已有 `category` 欄） |
| 廠商完整資料 | `Firm.Tax No` 150、`Fax` 235、`Sales Name` 209 |

### 明確不做

- 總帳會計、稅務申報（SRS 已排除；CAR35 的 Carp9x 是 1999 年代做法，不符台灣現行稅制）
- 分店管理（SRS 為單店，CAR35 的 `Branch` 用不到）
- 郵寄標籤（`Label-1` / `Letter-1`，無實用價值）
- **代客保管**（CAR35 沒有此功能，無資料可遷；要做也是純新功能）
- **裝潢分帳**（`Decorate Flag` 全表 0 筆）

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
- `DeactivateInsuranceCompany` — 停用（active=0，軟刪除；歷史工單透過 insurance_company_id 引用，故不真刪）

權限：`CounterUp`（櫃檯可維護保險公司清單）

**Step 4: 路由註冊**

```go
mux.HandleFunc("GET /insurance-companies", handler.AllStaff(handler.ListInsuranceCompanies))
mux.HandleFunc("POST /insurance-companies", handler.CounterUp(handler.CreateInsuranceCompany))
mux.HandleFunc("GET /insurance-companies/{id}/edit", handler.CounterUpPage(handler.EditInsuranceCompanyForm))
mux.HandleFunc("POST /insurance-companies/{id}", handler.CounterUp(handler.UpdateInsuranceCompany))
mux.HandleFunc("GET /insurance-companies/{id}/delete", handler.OwnerOnly(handler.DeactivateInsuranceCompany))
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

---

## Phase 2 提案（2026-10-08，大C 實測後）

> 本節由大C 依 Hermes 上報的 4 個 commit + 實際 DB 筆數實測後整理。
> 目的：確認 Phase 1 完成狀態，並提出 Phase 2 優先序。

### 一、Phase 1 完成狀態（大C 實測）

| 驗證項 | 結果 | 備註 |
|---|---|---|
| `go build ./...` | ✅ BUILD_OK | go1.27.1 |
| `go vet ./...` | ✅ 通過 | |
| `go test ./internal/...` | ✅ handler/line/cron ok | |
| test_auth | ✅ 27/27 | Hermes 計畫書寫 10/10（測試腳本已擴充） |
| test_workflow | ✅ 27/27 | |
| test_status_flow | ✅ 27/27 | |
| test_phase2 | ✅ 27/27 | |
| test_phase3 | ✅ 27/27 | |

**注意：** Hermes 計畫書的「10/10、13/13、25/25、27/27」是舊數字；測試腳本後來擴充成各 27 項，但**全部通過**，回歸狀態是好的。

### 二、已確認的 4 個 commit（Hermes 上報）

| commit | 內容 | 狀態 |
|---|---|---|
| `0d0b1eb` | 四頁歷史明細（投保/定檢/保養套餐/支出） | ✅ 已提交，四頁有資料 |
| `14f960d` | 工單列表技師/零件篩選 + 零件欄 | ✅ 已提交，未指派 62,097 筆可篩 |
| `66e929f` | 首頁數字卡可點、技師下拉過濾、按鈕改名 | ✅ 已提交 |
| `0f7dbb4` | 計畫檔執行進度與簽名 | ✅ 已提交 |

### 三、DB 事實對照（大C 實讀 data_container_autorepair.db）

| Hermes 說 | 大C 實測 | 判斷 |
|---|---|---|
| insurances 不存在 | `vehicle_insurances` 2,438 筆 | ✅ 正確（表名是 vehicle_insurances） |
| inspections 不存在 | `vehicle_inspections` 19,918 筆 | ✅ 正確 |
| maintenance_templates | 25 筆 | ✅ 正確 |
| expenses | 3,390 筆（全在 1999–2003）| ✅ 正確，近 10 年 0 筆 |
| 「40k 筆歷史」 | 85,825 張工單 | 數字不同但同量級 |
| 5 張業務表全缺 | 只有 customer_pay 真的缺 | ✅ 正確 |
| MaxOpenConns(1) | — | ⚠️ 此建議會 nested query 死鎖，勿採 |
| Data35.mdb 可 access-parser 完整解析 | — | ✅ Hermes 實測過 |

### 四、關鍵發現：`customer_pay` 收款沖銷未實作

Hermes 說要做「`customer_pay` 收款沖銷 + `/receivables`」，但**實際 DB 四檔（data / live / live2 / snapshot）都沒有 `customer_pay` 表**：

- `receivables`、`receivable_payments`、`payables`、`purchase_orders`、`work_order_consign`、`customer_pay`：**全部 MISSING**
- `supplier` 表也不存在（只有 suppliers 在 live DB，data DB 可能沒遷移）

結論：**收款沖銷 + 應收/應付/採購這整組 P1-1 / P1-2 都還沒動**，是 Phase 2 的正式起點。

### 五、Phase 2 優先序建議

依實際使用頻率 + 資料價值，建議順序（非原計畫順序）：

| 順位 | 項目 | 規模 | 說明 |
|---|---|---|---|
| **0** | 修車主對應 | 小 | 不改畫面讓車輛 86.5%→99.8%、工單 90.1%→99.9%。原 Phase 1 沒做 |
| **1** | 收款沖銷 + `/receivables` | 中 | 90,762 筆歷史；解鎖付款方式與應收明細。但 `customer_pay` 表目前不存在，需先建表 |
| **2** | 進貨單回填 | 中 | 10.4 萬張單；`inventory_movements` 目前 0 筆 |
| **3** | 技師分工回填 | 小 | 231,188 筆未遷；算技師產值 |
| **4** | 客戶主檔補欄位 | 小 | 發票抬頭 99.9%、連絡人 98.4% |
| **5** | 車輛補欄位 | 小 | 駕駛人 97.3%、顏色 78%、車種 63.8% |
| **6** | 供應商 CRUD（只讀→可編修）| 小 | 695 家 |
| **7** | 投保／定檢補「標記完成」| 小 | 21,176 筆定檢只有 1 筆回填完成 |
| **8** | 報表死連結清理 + 出險報表 | 中 | 需先有保險件資料 |

### 六、Phase 2 執行建議

1. **先做「修車主對應」（順位 0）**—— 規模小、回報高，且是其他遷移的基礎。
2. **收款沖銷需先建 `customer_pay` 表**—— 目前不存在，不能直接接頁面。
3. **P1-2 應付帳款 / 採購單** 是全新功能（舊系統空表或設計了沒用），優先度可往後排。
4. **代客保管 / 裝潢分帳** 明確不做（舊系統無此功能 / 全表 0 筆）。

---

# 執行進度（2026-10-07 更新）

> 本節由 Hermes 維護。每個 Task 完成後更新，並附 commit hash 與驗證結果。

## 已完成

| Task | 內容 | commit | 驗證 |
|---|---|---|---|
| **P0-1 T1–T2** | `insurance_companies` 表 + `work_orders` 保險欄位 | `b57a56b` | migration 004/005 已套用 |
| **P0-1 T3** | 保險公司 CRUD + 頁面 | `9a04a5a` | `/insurance-companies` HTTP 200，16 筆 |
| **P0-1 T4** | 開立工單整合保險欄位 | `78b597c` | 工單表單含件別／保險公司／理賠編號 |
| **盤點修正** | 逐表逐欄資料盤點，修正 4 處功能誤判 | `a87e60a` `74bb051` `778dc2f` | `scripts/analyze_car35_data.py` 可重跑 |
| **畫面級對照** | 截圖欄位 → 我們的欄位 + 非空筆數 | `8528435` | `docs/CAR35_SCREEN_GAP.md` |
| **使用頻率** | 依實際使用量重排優先序 | `b267ced` | `docs/CAR35_USAGE_FREQUENCY.md` |
| **UX 檢視** | 實跑量測順手度 | `b267ced` | `docs/UX_REVIEW.md`、`scripts/ux_probe.py` |
| **UX 修復** | 首頁數字卡可點、技師下拉過濾行政、按鈕改名、排除測試資料 | `66e929f` | 下拉 61→52 項；6 張卡有 href |
| **舊系統補遷移** | 車輛投保／定檢／保養套餐／零用金四頁（大C 產出，Hermes 整理提交） | `0d0b1eb` | 導覽 15 項，四頁有資料 |
| **工單查詢** | 列表技師／零件篩選 + 零件欄 | `14f960d` | 未指派 62,097 筆可篩；零件「雨刷」3,061 筆 |

**回歸狀態**（每次提交前都跑）：
```
go build ./... && go vet ./...            ✅
go test ./internal/... -count=1           ✅
test_auth 10/10  test_workflow 10/10      ✅
test_status_flow 13/13                    ✅
test_phase2 25/25  test_phase3 27/27      ✅
```

## 未完成（依實際使用頻率重排，非原計畫順序）

| 順位 | 項目 | 為什麼這個順序 | 規模 |
|---|---|---|---|
| **0** | **修車主對應**（改 `migrate_vehicles` 比對鍵） | 不動畫面就讓車輛 86.5%→99.8%、工單 90.1%→99.9% | 小 |
| 1 | `customer_pay` 收款沖銷 + `/receivables` | 90,762 筆歷史；一張頁面解鎖付款方式與應收明細 | 中 |
| 2 | 進貨單（`Material Header/Detail`）+ 回填 `inventory_movements` | 10.4 萬張單；`inventory_movements` 目前 0 筆 | 中 |
| 3 | 技師分工回填（`Maintain Detail.Staff No`） | 231,188 筆未遷；`technician_worklogs` 只有 32 筆 | 小 |
| 4 | 客戶主檔補欄位 | 發票抬頭 99.9%、連絡人 98.4% 都有值 | 小 |
| 5 | 車輛補欄位 | 駕駛人 97.3%、顏色 78%、車種 63.8% | 小 |
| 6 | 供應商 CRUD（只讀 → 可編修） | 695 家 | 小 |
| 7 | 投保／定檢補「標記完成」 | 舊系統 21,176 筆定檢只有 1 筆回填完成 | 小 |
| 8 | 報表死連結清理 + 出險報表 | 需先有保險件資料 | 中 |

### 明確往後排（有使用頻率為證）

| 項目 | 證據 |
|---|---|
| 收入／支出費用登錄 | 3,390 筆全在 1999–2003，**近 10 年 0 筆** |
| 應付帳款／應付票據 | `Firm Pay`、`Check Out` **空表** → 全新功能非移植 |
| 零件車種／庫位／來往廠商 | 19 / 10 / 34 筆（0.1–0.3%） |
| 客戶的應收帳款科目／票據科目 | **0 / 0 / 1 筆** |
| 代客保管 | 舊系統**無此功能**（`Consign` 欄位不存在） |
| 裝潢分帳 | `Decorate Flag` 0/95,440 筆 |

### 原計畫有誤、已修正的判斷

| 原計畫寫的 | 實測 |
|---|---|
| 保險理賠工單 P0「CAR35 有」 | 保險欄位**全表 0 筆**，設計了但沒在用 |
| 代客保管 P2-1 | 舊系統**沒有**此功能 |
| 應付帳款 P1-2 是「移植」 | 舊系統空表 → 是**全新功能** |
| 採購單 P1-2 | 查證是**真的進貨單**（可對到廠商與料價）→ 可上調 |
| 付款方式拆分 | 確認是真的，但刷卡＋支票不到 8,000 筆 |

---

## 簽名

| 角色 | 負責範圍 | 時間 |
|---|---|---|
| **Hermes** | 資料盤點、UX 量測與修復、工單查詢、計畫維護、舊系統補遷移的整理提交 | 2026-10-07 |
| **大C（助理）** | 四頁歷史明細（投保／定檢／保養套餐／支出）、17 模組盤點、car35-plan 文件 | 2026-10-07 |
| **@大c** | 平行開發中（LINE webhook、報表、authz 等未提交變更） | — |

> 服務現況：`http://localhost:8090`（Docker，`docker compose -p autorepairos`）
> 帳號 `owner` / `Owner001!`｜DB：named volume `autorepairos_autorepairos-data`
> 可重跑：`scripts/analyze_car35_data.py`、`scripts/ux_probe.py`