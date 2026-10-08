# AutoRepairOS 開單 / 修車流程優化計畫（櫃檯 + 技師視角）

> 產出：2026-10-08 ｜ 產出者：大C（助理）
> 來源：實讀 `internal/handler/workorders.go`、`worklogs.go`、`staff.go`、`receivables.go`
>        + `main.go` 路由盤點 + `data/autorepair.db` 欄位對照
> 用途：以「**開單人員**（櫃檯）＋ **技師**」真正操作順序，盤點流程斷點 → 給 hermes 排優先級
> 範圍：只動「**已建好的工單**」的編輯／狀態／顯示，不動遷移邏輯
> 狀態：待 hermes 排優先級

---

## 一、現況事實（2026-10-08 實測，非推測）

### 1.1 工單狀態流（`UpdateWorkOrderStatus` + `statusOptions`）
`main.go` 路由：
```
GET  /work-orders/{id}          → ShowWorkOrder（看工單）
POST /work-orders/{id}/status   → UpdateWorkOrderStatus（改狀態）
POST /work-orders/{id}/approval → UpdateWorkOrderApproval（核准）
POST /work-orders/{id}/sop      → ToggleWorkOrderSOP（切換 SOP）
POST /work-orders/{id}/items    → AddWorkOrderItem（加零件）
GET  /work-orders/{id}/items/{item_id}/delete → 刪零件
```
狀態流（`statusOptions`）：**pending → in_progress → waiting_parts → completed → paid**。
- `requireOrderMutable(woID)` 擋「**已結帳（paid）**」後再補登工時/零件 —— **合理，保留**。

### 1.2 工時頁（`ListTechnicianWorklogs`）— **痛點集中處**
- 表頭 9 欄：日期／技師／工時／時薪／工資／佣金率／佣金／備註／**操作**。
- **「操作」只有「刪除」**，沒有編輯。錯記工時／時薪 → 只能刪重加，歷史斷續。
- **表單 `work_date` 預設成 `today()`** —— 補舊工單要手改日期。
- **看不到工單狀態／總額／保險件別**，工時表跟工單脫節。
- **加總公式現成**：`labor = hours×hourlyRate`、`commission = labor×commRate/100`，
  `AddTechnicianWorklog` 已寫好，編輯 = 把 INSERT 改成 UPDATE + 重算即可。

### 1.3 健保／員工頁（`ListStaff`）— **完全只讀**
- 只有 `GET /staff`，**沒有** `/staff/{id}/edit`、`POST /staff`、DELETE。
- 表頭：姓名／編號／身分／部門／依附／身分證號／生日／加保日／退保日 —— **全不可改**。
- 退保日有值 = 已退保（紅字標示），但填錯／復保都沒地方改。

### 1.4 收款頁（`receivables.go`）
- 路由：`GET /receivables`、`GET /receivables/new`、`POST /receivables`、`GET /receivables/{id}/void`。
- 收款沖銷模組已建（`d74f194`），但**沒有「編輯／沖銷」按鈕**（只有 void 單號），
  收錯金額只能 void 重開，體驗生硬。

---

## 二、痛點清單（按操作視角排序）

| # | 痛點 | 角色 | 嚴重度 | 現況 |
|---|---|---|---|---|
| A | 工時改不了，錯記只能刪重加 | 技師 | 🔴 最痛 | 工時頁只有刪除 |
| B | 工時頁看不到工單狀態／總額 | 技師 | 🔴 很痛 | 表頭無工單資訊 |
| C | 補舊工單時日期預設成 today | 技師 | 🟠 痛 | `work_date=today()` |
| D | 健保／員工資料完全不可編輯 | 櫃檯/HR | 🔴 最痛 | `/staff` 只讀 |
| E | 技師沒有「我的工單」入口 | 技師 | 🟠 痛 | 列表只有全店工單 |
| F | 離職技師仍出現在指派選單 | 櫃檯 | 🟡 中 | `technicianOptions()` 未擋離職 |
| G | 收款沖銷收錯只能 void 重開 | 櫃檯 | 🟡 中 | 無編輯／部分沖銷 |
| H | 工單狀態流對技師不透明 | 技師 | 🟡 中 | 看不到「待我核准／待料」 |

---

## 三、優化計畫（每個痛點的改寫範圍）

### A. 工時筆加「編輯」（`worklogs.go`）
- 每行加「編輯」按鈕（限**未結帳**工單，與刪除共用 `requireOrderMutable` 門）。
- 路由：`POST /work-orders/{id}/worklogs/{log_id}`（更新）。
- 改寫：`AddTechnicianWorklog` 拆成「新增／更新」兩路，更新時重算 `labor_amount`、
  `commission_amount`（公式現成）。
- **UI 細節**：編輯表單預設回填 `hours / hourly_rate / commission_rate / work_date`，
  改動即時顯示重算後的工資／佣金（`labor`、`commission`），讓技師一眼確認金額。

### B. 工時表加「工單狀態／總額／保險件別」
- `ListTechnicianWorklogs` 每行加一列「工單狀態」，JOIN `work_orders` 取 `status`。
- 表頭加「工單總額」＋「保險件別」，讓技師知道這筆工時對應的是哪個工單、是否已保險。
- 結帳後補工時被擋 → 頁首加提示「**此工單已結帳，工時已鎖定**」。

### C. 補舊工單日期預設成工單建立日
- 工時表單 `work_date` 預設值改成該工單 `wo.created_at`（日），
  或工時頁表頭加「**建議日期 = 工單建立日**」提示。
- 避免技師補登時漏改日期。

### D. 健保／員工 CRUD（`staff.go`）— 優先最高
- 路由：`GET /staff/{id}/edit`（表單）+ `POST /staff/{id}`（儲存）。
- 改寫：照 `insurance.go` 範例（軟刪除）：
  - `EditStaffForm`：`scanStaff` 帶回現值填充表單
  - `UpdateStaff`：`UPDATE users SET national_id=?, insurance_start=?, insurance_end=?,
    staff_kind=?, staff_branch=?, birthday=?, updated_at=?, WHERE id=?`
  - `SoftDeleteStaff`：`UPDATE users SET active=0 WHERE id=?`（舊工單外鍵引用，不真刪）
- **欄位可編輯性**（限員工，不讓改眷屬冒用）：
  - `national_id`（身分證號）— 可編輯
  - `insurance_start`（加保日）— 可編輯
  - `insurance_end`（退保日）— 可編輯；有值 = 已退保
  - `staff_kind`（身分：員工/眷屬/廠商）— 三選一下拉，限員工改
  - `staff_branch`（部門：A~H）— 部門下拉選
  - `birthday`（生日）— 可編輯
- **前端**：`/staff` 表每行加「編輯」按鈕（限員工）→ 跳 `/staff/{id}/edit` → 保存後重導 `/staff`。
- **權限**：健保編輯限櫃檯／管理員；員工只能改自己。

### E. 工單列表對技師加「我的工單」
- `/work-orders` 列表對技師角色加「我的工單」篩選（`WHERE technician_id = 登入者`）
  或加 `/work-orders/mine` 頁。
- 減少技師翻全店列表找對車、再點 `/worklogs` 的多一道手。

### F. 擋離職技師（離職標記）
- **現況**：`users` 表**沒有** `active/deleted/is_active` 欄位（只有 12 欄）→ 目前無法標記離職。
- **做法**：先 `ALTER TABLE users ADD COLUMN active INTEGER DEFAULT 1`，再：
  - `DeactivateStaff`：`UPDATE users SET active=0 WHERE id=?`（離職／退休）
  - `reactivateStaff`：`UPDATE users SET active=1 WHERE id=?`（復職）
  - `technicianOptions()` 改查 `WHERE active=1`，**離職技師不再出現在指派選單**。
  - `/staff` 頁表頭加「**狀態**」欄：`active=1` 顯示「在職」，`active=0` 顯示「離職／已停用」（置灰）。
- **62 技師 + 40 眷屬**，其中 `insurance_end` 已 47 筆有值（約 12 人已退保）—— 退保時間可對到離職。
- **權限**：停用／復職限櫃檯／管理員；員工只能改自己。

### F1. 退保時間（健保進出編修）
- 照 D 節健保 CRUD，`insurance_end`（退保日）與 `insurance_start`（加保日）都做成**可編輯**。
- 頁首顯示「**在保中**」統計：`employee AND active=1 AND insurance_start<>'' AND insurance_end=''`。
- 退保日有值 → 紅字標示「已退保」；**復保**時可清空 `insurance_end`（走 `reactivateStaff`）。
- 62 技師 + 40 眷屬，47 筆有退保日（約 12 人已退保）。

### G. 收款沖銷可編輯／部分沖銷
- 現況只有 `void`（整單沖銷）。建議加：
  - `POST /receivables/{id}/adjust`：調整金額／部分沖銷（收錯可直接改）。
- 改寫：`receivables.go` 現有的收款記錄加「編輯／調整」按鈕。

### H. 工單狀態對技師更透明
- `ShowWorkOrder` 頁對技師顯示「**狀態進度條**」（pending→in_progress→waiting_parts→
  completed→paid），讓技師知道這工單卡在誰那。
- 列表對技師加「**待我處理**」篩選（狀態 = in_progress / waiting_parts）。

---

## 四、優先順序建議

| 順位 | 痛點 | 預估工作量 | 理由 |
|---|---|---|---|
| **P0.5** | D 健保／員工 CRUD | 中 | `/staff` 完全不可用，人事最痛 |
| **P1** | A 工時編輯 | 小 | 錯記刪重加的痛，改寫簡單 |
| **P1** | B 工時顯示狀態/總額 | 小 | 讓技師一眼看懂，表頭加列即可 |
| **P2** | C 工時日期預設 | 極小 | 一行改成 `wo.created_at` |
| **P2** | E 我的工單 | 中 | 需權限判斷，減少技師翻列表 |
| **P2** | H 狀態進度條 | 中 | 提升技師體驗，需 CSS 進度條 |
| **P3** | F 擋離職技師（離職標記） | 小 | 需先 `ALTER TABLE users ADD active`；`technicianOptions` 改查 `active=1` |
| **P3** | F1 退保時間（健保進出編修） | 中 | `insurance_end/start` 可編輯；47 筆退保日需對照離職 |

---

## 五、對既有計畫檔的修正

| 檔 | 原標記 | 新標記 |
|---|---|---|
| `technician-flow.md` | P0.5 健保編修＋離職標記＋退保時間 / P1 工時編輯 / P1 工時顯示狀態 / P2 我的工單 / P2 擋離職 / P3 退保時間 | ✅ 本檔正式化（含改寫範圍 + 路由 + 權限） |

---

## 六、實作分解（按角色操作順）

### 櫃檯開單人員流程（理想）
```
1. 新建工單 /work-orders/new → 填車主／車牌／保險件別 → 建
2. 改狀態 /work-orders/{id}/status → pending → in_progress
3. 加零件 /work-orders/{id}/items → 加零件明細（自動算單價）
4. 核准 /work-orders/{id}/approval → 核准
5. 收錢 /receivables → 收銷這工單（建議加「編輯／部分沖銷」）
6. 狀態 → completed → paid（結帳後鎖定工時/零件）
```

### 技師流程（理想）
```
1. 工單列表 /work-orders → 看「我的工單」/「待我處理」
2. 進工單 /work-orders/{id} → 看狀態進度條
3. 記工時 /work-orders/{id}/worklogs → 日期預設=工單建立日，回填現值可編輯
4. 重算工資/佣金即時顯示 → 確認金額無誤
5. 刪錯 /work-orders/{id}/worklogs/{log_id}/delete
```

---

## 七、驗證命令

```python
# 工單狀態流
import sqlite3
c = sqlite3.connect('data/autorepair.db')
print("status流:", c.execute("SELECT COUNT(DISTINCT status) FROM work_orders").fetchone()[0], "種狀態")
print("worklogs 筆數:", c.execute("SELECT COUNT(*) FROM technician_worklogs").fetchone()[0])
print("staff 筆數:", c.execute("SELECT COUNT(*) FROM users WHERE role='technician'").fetchone()[0])
```

> 服務現況：`http://localhost:8090`（Docker）｜帳號 `owner` / `Owner001!`
> 關鍵現況：**`customer_pay` 收款頁已建但無編輯；`/staff` 完全只讀；工時頁只有刪除。**
