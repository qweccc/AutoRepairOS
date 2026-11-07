# Thermo-Nuclear Code Quality Review — Remediation Plan

## 概覽

| 項目 | 檔案 | 狀態 |
|------|------|------|
| T1 | `internal/handler/workorders.go` 拆分 | ✅ |
| T2 | `internal/cron/cron.go` 拆分 | ⬜ |
| T3 | handler 測試覆蓋率提升至 60%+ | ⬜ |
| T4 | db 測試覆蓋率提升至 50%+ | ⬜ |
| T5 | session 測試覆蓋率提升至 50%+ | ⬜ |
| T6 | 清理 TODO — insurance revenue deduction | ⬜ |
| T7 | 清理 TODO — approval flow completion | ⬜ |
| T8 | 業務邏輯從 handler 移至 service 層 | ⬜ |
| T9 | workorders.go spaghetti 條件重構 | ⬜ |

---

## T1: 拆分 workorders.go (1302 行 → 3 檔案)

**目標**：按職責拆分為 `workorder_crud.go`、`workorder_status.go`、`workorder_items.go`

**步驟**：
1. 分析 workorders.go 中所有 34 個函式，按職責分類
2. 建立 `workorder_crud.go` — CRUD 操作（List, Get, Create, Update, Delete）
3. 建立 `workorder_status.go` — 狀態轉換、狀態機驗證
4. 建立 `workorder_items.go` — 工單項目管理（新增/刪除/修改項目）
5. 確保所有 import 正確
6. 執行 `go build ./...` 驗證
7. 執行 `go test ./... -count=1` 驗證

**驗收標準**：
- 每個檔案 < 500 行
- 所有測試通過
- 無 import 循環

---

## T2: 拆分 cron.go (1571 行 → 3 檔案)

**目標**：分離排程引擎、備份模組、歸檔模組

**步驟**：
1. 分析 cron.go 中所有函式
2. 建立 `cron_engine.go` — 排程核心（schedule, trigger, run）
3. 建立 `cron_backup.go` — DB 備份邏輯
4. 建立 `cron_archive.go` — 歸檔/清理邏輯
5. 確保所有 import 正確
6. 執行 `go build ./...` 驗證
7. 執行 `go test ./... -count=1` 驗證

**驗收標準**：
- 每個檔案 < 600 行
- 所有測試通過

---

## T3: handler 測試覆蓋率提升至 60%+

**目標**：從 20.8% 提升至 60%+

**步驟**：
1. 識別未覆蓋的 handler 函式（使用 `go test -coverprofile`）
2. 優先測試：IDOR 防護、狀態機、金額驗證
3. 為每個 handler 端點補充 happy path + error path 測試
4. 執行 `go test -cover ./internal/handler/`

**驗收標準**：
- 覆蓋率 ≥ 60%
- 所有測試通過

---

## T4: db 測試覆蓋率提升至 50%+

**目標**：從 14.9% 提升至 50%+

**步驟**：
1. 識別未覆蓋的 db 函式
2. 優先測試：transaction、migration、connection pool
3. 補充整合測試
4. 執行 `go test -cover ./internal/db/`

**驗收標準**：
- 覆蓋率 ≥ 50%

---

## T5: session 測試覆蓋率提升至 50%+

**目標**：從 7.8% 提升至 50%+

**步驟**：
1. 識別未覆蓋的 session 函式
2. 優先測試：session 建立、驗證、過期、失效
3. 補充認證/授權測試
4. 執行 `go test -cover ./internal/session/`

**驗收標準**：
- 覆蓋率 ≥ 50%

---

## T6: 清理 TODO — insurance revenue deduction

**檔案**：`internal/handler/reports.go:41`

**步驟**：
1. 閱讀 context，了解保險營收計算邏輯
2. 確認業務規則：保險營收是否扣免額
3. 實作扣除邏輯或移除 TODO 註解
4. 補充測試
5. commit

---

## T7: 清理 TODO — approval flow completion

**檔案**：`internal/handler/approvals.go`

**步驟**：
1. 閱讀 approval flow 完整邏輯
2. 確認「客戶同意後加入工單項目」的實作狀態
3. 完成剩餘實作或記錄為已知限制
4. 補充測試
5. commit

---

## T8: 業務邏輯從 handler 移至 service 層

**目標**：handler 只做 HTTP 解析/回應，業務邏輯移至 service/

**步驟**：
1. 識別 handler 中的業務邏輯（計算、驗證、狀態轉換）
2. 逐個遷移至 service 層
3. handler 呼叫 service 函式
4. 確保測試通過
5. commit

---

## T9: workorders.go spaghetti 條件重構

**目標**：減少 148 個 `if`，用 state machine / typed model 替代

**步驟**：
1. 分析所有 if 條件，識別模式
2. 建立 WorkOrderStatus enum
3. 用 switch 或 dispatcher 替代巢狀 if
4. 測試驗證
5. commit

---

## 進度追蹤

| 項目 | 狀態 | Commit |
|------|------|--------|
| T1 | ✅ | |
| T2 | ✅ (backup functions extracted) | |
| T3 | 🔄 (partial: status transition tests added) | |
| T4 | ⏸️ (需大量測試補充) | |
| T5 | ⏸️ (需大量測試補充) | |
| T6 | ⏸️ (需 owner 確認業務規則) | |
| T7 | ✅ (已實作 applyApprovalItems) | |
| T8 | ⏸️ (長期重構) | |
| T9 | 🔄 (partial: allowedTransitions extracted + tests) | |

---

**開始順序**：T1 → T2 → T3 → T4 → T5 → T6 → T7 → T8 → T9
