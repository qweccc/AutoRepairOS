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

## 暫不納入的候選項目

- 個人化儀表板
- 全域搜尋
EOF
git add docs/ROADMAP.md && git commit -m "docs: 建立改善 Roadmap"