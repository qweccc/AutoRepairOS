# 技師流程前端痛點 + 健保進出編修

> 產出：2026-10-07 ｜ 產出者：大C（助理）
> 來源：手讀 `internal/handler/` 各頁渲染 + `main.go` 路由盤點 + `data/autorepair.db` 欄位
> 用途：整理技師用哪裡不順、健保進出缺編修，供 hermes 排優先級
> 狀態：**待 hermes 確認優先級**

---

## 一、技師流程前端不順（5 個痛點）

### 痛點 1：工時筆只能「新增 / 刪除」，無法修改
- 頁：`/work-orders/{id}/worklogs`（`ListTechnicianWorklogs`）
- 每行只有「刪除」按鈕，表單只有「新增工時紀錄」。
- 錯記工時／時薪／日期 → 只能整筆刪掉重加，歷史紀錄斷續、不專業。
- 建議：加「編輯」按鈕（限未結帳工單），改動時重算 `labor_amount`、`commission_amount`。
- **重算邏輯（現成）**：`AddTechnicianWorklog` 已寫好 `labor := hours * hourlyRate`、`commission := labor * commRate / 100`，且 `techWorklogCols` 已 `COALESCE` 兜底。改動 = 把 `Add` 改成 `UPDATE` + 重算這兩欄即可。
- **限制現況**：`requireOrderMutable(woID)` 擋結帳後補登 → 編輯也要放同一道門（已結帳工單連編輯都擋，合理）。

### 痛點 2：工時頁看不到工單狀態與金額對照
- 表頭只有「日期 / 技師 / 工時 / 時薪 / 工資 / 佣金率 / 佣金 / 備註」。
- 看不到這筆工時對應的工單：狀態（待辦／結帳／已結帳）、工單總額、保險件別。
- 結帳後補工時會被後端擋（`requireOrderMutable`），但頁面上沒明顯提示「此工單已結帳，工時鎖定」。
- 建議：工時表加一列「工單狀態／總額」，或把工單標題帶到每行。

### 痛點 3：補舊工單時日期預設成 today
- 表單 `work_date` 預設 `today()`，但技師補登的是幾個月前的工單。
- 每次要手動改日期，容易漏改。
- 建議：工單頁把「記工時」入口帶入該工單 `created_at` 日期，或工時頁顯示「建議日期 = 工單建立日」。

### 痛點 4：技師沒有「我的工單」入口
- 工單列表 `/work-orders` 對技師顯示**全部**工單，沒有「分配給我的工單」。
- 要記工時得先翻列表、找對車、再點 `/worklogs`，多一道手。
- 建議：`/work-orders` 列表對技師角色加「我的工單」篩選，或加 `/work-orders/mine`。

### 痛點 5：技師下拉選單沒擋離職者
- `technicianOptions()` 擋了行政 E、眷屬 G、廠商 H，但**沒擋離職**（註解已承認）。
- 離職技師仍會出現在選單 → 櫃檯可能把工時記到已離職的人名下。
- 建議：`users` 加「在職」欄位（或改動 `staff_kind`），或先擋 `insurance_end` 有值者。

> 結論：**痛點 1、2 是最痛**（工時改不了、看不明白），排最優先。

---

## 二、健保進出：完全缺編修（users 表的投保資料）

### 現況
- 頁：`/staff`（`ListStaff`）——**只有 GET，只讀**。
- `main.go` 只有 `GET /staff`，**沒有** `/staff/{id}/edit`、`POST /staff`、`DELETE`。
- 表頭：姓名、編號、身分、部門、依附、身分證號、生日、加保日、退保日 —— **全部不能改**。

### 問題
- 健保資料（身分證號、加保日、退保日）是人事要長期維護的，但遷移進來後**永遠無法編輯**。
- 退保日有值 = 已退保（前端會紅字標示），但一旦填錯、或員工退保後復保，都沒地方改。
- 身分（staff_kind：員工/眷屬）也是只讀，遷移時判錯的眷屬擋不掉。

### 需要做的
照 `insurance_companies` 範例（軟刪除）改寫 `users` 健保相關欄位的 CRUD：

| 欄位 | 現況 | 建議 |
|---|---|---|
| `national_id` 身分證號 | 只讀 | 可編輯（限員工，不讓改眷屬冒用） |
| `insurance_start` 加保日 | 只讀 | 可編輯 |
| `insurance_end` 退保日 | 只讀 | 可編輯；有值=已退保 |
| `staff_kind` 身分 | 只讀 | 員工/眷屬/廠商 三選一，限員工改 |
| `staff_branch` 部門 | 只讀 | 部門下拉選（A~H） |
| `birthday` 生日 | 只讀 | 可編輯 |

- 路由：`GET /staff/{id}/edit`（表單）+ `POST /staff/{id}`（儲存）。
- 刪除/停用：員工由 HR 管理，建議**停用（active=0）**而非真刪（舊工單外鍵引用）。
- 權限：健保編輯限櫃檯/管理員；員工只能改自己。

### 實作範本（照 insurance_companies 範例）
- `EditInsuranceCompanyForm`：`FormValue("code/name/tax_id/...`) + 帶回現值填充表單
- `UpdateInsuranceCompany`：`db.DB.Exec(UPDATE ... SET ..., id)` + 返回工單頁
- `softDelete`：`UPDATE ... SET active=0 WHERE id=?`（員工停用同理）
- `scanInsurance` / `insuranceCols`：`COALESCE` 兜底空值（users 欄位同理）
- 表單欄位：`national_id / insurance_start / insurance_end / staff_kind / staff_branch / birthday`
- 前端：`staff.go` 現有的 staff 頁加「編輯」按鈕 → 跳 `/staff/{id}/edit` → 保存後重導 `/staff`

> 結論：**健保進出（加保/退保日）是這次要優先補的**，因為 `/staff` 完全不能用。

---

## 三、對 17-module-editability.md 的修正

把 #7 員工技師從「只讀對照」改成「部分可編修」：

| # | 模組 | 頁面對 | 路由（增/改/刪） | 現況 |
|---|---|---|---|---|
| 7 | 員工健保 | /staff | ❌ 僅 List（**需補 edit/create**） | ⚠️ 缺增刪，健保欄位不可改 |

---

## 四、建議優先順序

1. **P0.5：健保進出編修**（`/staff` 完全不可用，最痛）—— 照保險公司範例補 CRUD
2. **P1：工時頁加「編輯」** —— 錯記只能刪重加的痛
3. **P1：工時頁顯示工單狀態／金額** —— 讓技師一眼看懂
4. **P2：工單列表對技師加「我的工單」** —— 減少翻列表
5. **P2：擋離職技師** —— 需先加在職欄位，排最後

---

## 五、實作分解（每個痛點的改寫範圍）

### P0.5 健保編修（`internal/handler/staff.go`）
- 新增 `EditStaffForm(w, r, user)`：`scanStaff` 帶回現值填充表單
- 新增 `UpdateStaff(w, r)`：`db.DB.Exec(UPDATE users SET national_id=?, insurance_start=?, insurance_end=?, staff_kind=?, staff_branch=?, birthday=?, updated_at=..., WHERE id=?)` + 重導 `/staff`
- 新增 `SoftDeleteStaff`：`UPDATE users SET active=0 WHERE id=?`（舊工單外鍵引用，不真刪）
- `staff.go` 頁加「編輯」按鈕（限可編輯者）→ 跳 `/staff/{id}/edit`
- 路由 `main.go`：`GET /staff/{id}/edit` + `POST /staff/{id}`
- 權限：健保編輯限櫃檯/管理員；員工只能改自己

### P1 工時頁加編輯（`internal/handler/worklogs.go`）
- 表單每行加「編輯」按鈕（限未結帳工單）→ 跳 `/work-orders/{id}/worklogs/{log_id}/edit`
- 新增 `EditTechnicianWorklog`：`UPDATE technician_worklogs SET hours=?, hourly_rate=?, labor_amount=?, commission_rate=?, commission_amount=?, notes=? WHERE id=?`（`labor_amount=hours*hourly_rate`、`commission_amount=labor*commRate/100` 重算）
- 現成：`AddTechnicianWorklog` 已寫好重算公式 + `techWorklogCols` 已 `COALESCE` 兜底

### P1 工時頁顯示狀態/金額
- `ListTechnicianWorklogs` 表每行加一列「工單狀態／總額」（JOIN work_orders 取 status + total_amount）
- 結帳後補工時被 `requireOrderMutable` 擋 → 頁首加提示「此工單已結帳，工時已鎖定」

### P2 我的工單
- `/work-orders` 列表對技師角色加「我的工單」篩選（`WHERE technician_id = 登入者`）或 `/work-orders/mine`

### P2 擋離職技師
- `technicianOptions()` 查詢加擋離職（需先加 `users.is_active` 欄位，或改動 `staff_kind`）
- 低優先：先擋 `insurance_end` 有值者（已退保者）
