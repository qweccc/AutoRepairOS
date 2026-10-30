# AutoRepairOS 改善 Roadmap

## 現況盤點

| 模組 | 狀態 | 缺口 |
|------|------|------|
| WorkOrder 總金額 | 部分完成 | TotalAmount 與明細可能不一致 |
| 庫存扣除 | 部分完成 | 需 SQL 層級條件與 RowsAffected 檢查 |
| 軟刪除 | 部分完成 | User/Supplier 缺 deleted_at，搜尋未統一過濾 |
| PartSummary | 完成 | 需觸發更新機制 |
| Passbook 快照 | 完成 | 需確保歷史金額不隨價格變動 |
| Inventory 搜尋效能 | 待驗證 | 需加 index |

## 已確認的產品與架構決策

- Service Layer 採漸進遷移
- WorkOrder 總金額以明細加總為準，避免備份欄位
- 庫存扣除必須在 SQL 層級條件化

## 任務清單

### Phase 0: 安全與資料一致性

#### [x] T0.1 WorkOrder 總金額一致性 (SRS FR-WO)
- 在狀態轉換（In Progress → Paid）時，強制 Sum(WorkOrderItem.Subtotal) 並更新 TotalAmount
- 涉及檔案：internal/handler/workorder.go, internal/service/work_order.go
- 驗收：結帳金額與明細加總一致

#### [x] T0.2 庫存扣除競態條件 (SRS FR-INV)
- SQL: UPDATE inventory SET stock = stock - ? WHERE id = ? AND stock >= ?
- 檢查 RowsAffected，為 0 報錯
- 涉及檔案：internal/handler/inventory.go, internal/db/*.sql
- 驗收：並發扣除不產生負庫存

#### [x] T0.3 軟刪除統一 (SRS FR-CUST/VEH)
- User/Supplier/Customer/Vehicle/Inventory 加入 deleted_at
- Search API 過濾 deleted_at IS NULL
- 涉及檔案：internal/db/migration/*.sql, internal/handler/*.go
- 驗收：刪除後不出現在搜尋結果

### Phase 1: 優化

#### [x] T1.1 PartSummary 自動更新
- WorkOrderItem 增刪時觸發 UpdateSummary
- 涉及檔案：internal/service/work_order.go
- 驗收：PartSummary 與明細一致

#### [x] T1.2 Passbook 快照
- 確保 Subtotal/Tax 為結帳時快照
- 涉及檔案：internal/handler/passbook.go
- 驗收：歷史記錄不隨價格變動

#### [x] T1.3 Inventory 索引
- 建立 part_number, bin_location, category index
- 涉及檔案：internal/db/migration/*.sql
- 驗收：搜尋效能提升

## 依賴與建議執行順序

- T0.1 → T1.1
- T0.2 獨立
- T0.3 獨立

## 每個任務的完成條件

- go build ./... 通過
- go test ./... 通過
- 無回歸

## Vision 模組改善

### [ ] T2.1 Context 超時檢查
- 確認 vision.Client 使用 http.NewRequestWithContext 並設定逾時
- 涉及檔案：internal/integration/vision/client.go
- 驗收：超時時 Go Routine 不堆積

### [ ] T2.2 圖片大小限制
- 檢查 Base64 長度並限制最大 10MB
- 涉及檔案：internal/handler/vision.go
- 驗收：超過限制回 413

### [ ] T2.3 SupplierOptions 搜尋過濾
- 加入 search 參數，避免每次都撈 300 筆
- 涉及檔案：internal/handler/vision.go, web/pages/*.templ
- 驗收：關鍵字過濾正常

### [ ] T2.4 共用錯誤處理封裝
- 封裝「解析圖片 → 呼叫辨識 → 回傳結果」Helper
- 涉及檔案：internal/handler/vision.go
- 驗收：減少重複程式碼

### [ ] T2.5 辨識進度回報（未來）
- 考慮 WebSocket 或 polling
- 涉及檔案：web/static/*.js
- 驗收：使用者感知進度

## Access 匯入工具修正（外部 review，2026-10-29 已逐條對程式碼驗證）

工具：`unpublished/analysis-and-tests/tools/`（`full_import_access.py` 完整匯入、`sync_access_incremental.py` 增量同步、共用 `access_import_common.py`、核心 `migrate_access.py`）。
五項均已讀實際程式碼確認屬實，非僅照單全收。修正核心目標：**讓同步具備真正冪等性**（來源主鍵 UPDATE／新增 INSERT），修正前不要用增量工具正式同步帳務資料。

**修正進度（2026-10-29）**：T3.1 / T3.2 / T3.3 / T3.5 / T3.6 已修並用 `test_import_idempotency.py`（5 則）驗證；T3.4 未做（見該條）。

#### [x] T3.1 增量同步以「整列 hash」判新 → 來源資料修改後被當新資料重複插入 (高)
- 屬實。`access_import_common.py:filter_new_rows` 以 `(table_name, row_hash)` 判新，`row_hash` 是整列 `sha256`（`row_hash()`）；來源任一欄位修改 → hash 變 → 不在 `legacy_import_state` → 當新資料 INSERT，舊列保留 → 重複。state 只記已插入 hash、從無 UPDATE 路徑。
- ✅ 已修（2026-10-29）：`filter_new_rows` 改為以「來源主鍵」（`No`/`Sequence No`/`Name`）對照 **DB 既有 `legacy_no`** 判新，已存在即略過，從根上消除重複；無主鍵或 DB 無該欄位才退回 hash 法。
- 涉及檔案：`access_import_common.py`（`filter_new_rows` / `existing_db_keys` / `table_key_col`）。
- 驗收：修改來源一列後重跑，DB 內該主鍵只有一筆。→ `test_modified_existing_row_is_not_duplicated` 通過。

#### [x] T3.2 增量同步中途失敗，重跑重複寫入 (高)
- 屬實。`sync_access_incremental.py` 中 `save_state()` 在 `migrator.run()` **之後**才執行；run 中途失敗則 state 未更新 → 重跑時 `filter_new_rows` 把已成功列再判為新 → 重複。且同步模式以 `migrator._guard_append_only = lambda: False` 關閉了「整批已存在」保護，run 內無逐列去重兜底。
- ✅ 已修（2026-10-29）：去重權威從 `legacy_import_state`（會在崩潰時遺失）改為 **DB 既有 `legacy_no`**——即使 state 一行沒寫回，重跑仍照 DB 已有主鍵略過已入列，與執行次序無關。
- 涉及檔案：`access_import_common.py:existing_db_keys`（T3.1 同一處）。
- 驗收：模擬 run 中途失敗後重跑，DB 無重複。→ `test_crash_rerun_uses_db_keys_not_state`（state 為空仍正確）通過。

#### [x] T3.3 `--reset` 未清除 receivables → 完整重匯後收款仍是舊資料且被跳過 (高)
- 屬實。`migrate_access.py:reset()`（L1862）清 work_order_items/work_orders/vehicles/customers/suppliers/inventory/vehicle_insurances/vehicle_inspections/templates/expenses/legacy users/insurance_companies，**沒有 `DELETE FROM receivables`**；重匯後舊收款保留，且其 hash 已在 state 中 → 增量同步跳過 → 收款停在舊值。
- ✅ 已修（2026-10-29）：`reset()` 補 `DELETE FROM receivables` 與 `DELETE FROM technician_worklogs`（後者重匯也會翻倍）。
- 涉及檔案：`migrate_access.py:reset()`。
- 驗收：完整重匯後查 `receivables` 全表為新匯入值。→ `test_reset_clears_receivables_and_worklogs` 通過（實測 receivables -2、worklogs -2、ACME-001 保留、legacy 使用者清除）。

#### [x] T3.4 工單 Insurance Name 讀取但未寫入保險公司關聯 (中)
- 屬實。`migrate_access.py` 工單區塊（L1219-1210）讀 `ins_name = clean_str(col(t,'Insurance Name',i))`，但 INSERT 時 `insurance_company_id` 寫死 `None`（L1235），`ins_name` 成為未使用變數 → 保險工單 `insurance_company_id` 全 NULL → 依保險公司查詢／統計失靈。
- ✅ 已修（2026-10-29）：新增 `resolve_insurance_company(ins_name)`——以 `insurance_companies` 的 `name`/`code` 正規化（去空白）建對照，先精確比對、miss 用「最長包含」備援，比不到回 None（不硬猜）。工單 INSERT 改用它填 `insurance_company_id`，並印命中／比不到計數。
- 涉及檔案：`migrate_access.py`（`_insurance_company_map` / `resolve_insurance_company` / `migrate_work_orders`）。
- 驗收：有保險名的工單 `insurance_company_id` 非 NULL 且指向正確公司；統計頁可查出。→ `InsuranceCompanyResolveTest`（精確/簡稱/空白/未知）+ `WorkOrderInsuranceInsertTest`（端到端真的寫入）通過。

#### [x] T3.5 `full_import_access.py --dry-run` 仍修改 schema，非真正唯讀 (中)
- 屬實。`full_import_access.py` 與 `sync_access_incremental.py` 都在 dry-run 判定**之前**呼叫 `ensure_sync_schema_if_needed(conn)`，其內 `ALTER TABLE work_orders ADD COLUMN` 與 `CREATE TABLE legacy_import_state` 無論 dry-run 都會執行 → dry-run 會動到 schema。
- ✅ 已修（2026-10-29）：兩支都把 `ensure_sync_schema_if_needed` 移入 `if not args.dry_run:`；sync 的唯讀驗證改 try/except，缺 schema 時報清楚訊息而非擅自建立。
- 涉及檔案：`full_import_access.py`、`sync_access_incremental.py`。
- 驗收：對乾淨 DB 跑 `--dry-run` 後 DB 檔不變、無新欄位／新表。

### 建議補的測試（review 末條建議）
- [x] T3.6 補 SQLite fixture 測試覆蓋（2026-10-29）：新增 `unpublished/analysis-and-tests/tools/test_import_idempotency.py`（11 則，`python -m unittest test_import_idempotency` 全綠）。涵蓋重跑冪等、中途失敗重續（state 為空仍正確）、來源資料修改不重複、完整重置清收款、保險公司名稱解析與工單端到端寫入。已用「換回舊 hash 法 → 3 則變紅」確認測試有鑑別力。