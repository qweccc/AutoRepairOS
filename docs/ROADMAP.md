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

#### [ ] T0.1 WorkOrder 總金額一致性 (SRS FR-WO)
- 在狀態轉換（In Progress → Paid）時，強制 Sum(WorkOrderItem.Subtotal) 並更新 TotalAmount
- 涉及檔案：internal/handler/workorder.go, internal/service/work_order.go
- 驗收：結帳金額與明細加總一致

#### [ ] T0.2 庫存扣除競態條件 (SRS FR-INV)
- SQL: UPDATE inventory SET stock = stock - ? WHERE id = ? AND stock >= ?
- 檢查 RowsAffected，為 0 報錯
- 涉及檔案：internal/handler/inventory.go, internal/db/*.sql
- 驗收：並發扣除不產生負庫存

#### [ ] T0.3 軟刪除統一 (SRS FR-CUST/VEH)
- User/Supplier/Customer/Vehicle/Inventory 加入 deleted_at
- Search API 過濾 deleted_at IS NULL
- 涉及檔案：internal/db/migration/*.sql, internal/handler/*.go
- 驗收：刪除後不出現在搜尋結果

### Phase 1: 優化

#### [ ] T1.1 PartSummary 自動更新
- WorkOrderItem 增刪時觸發 UpdateSummary
- 涉及檔案：internal/service/work_order.go
- 驗收：PartSummary 與明細一致

#### [ ] T1.2 Passbook 快照
- 確保 Subtotal/Tax 為結帳時快照
- 涉及檔案：internal/handler/passbook.go
- 驗收：歷史記錄不隨價格變動

#### [ ] T1.3 Inventory 索引
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