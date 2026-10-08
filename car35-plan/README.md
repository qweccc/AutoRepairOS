# car35 功能擴充 — 計畫草案

> 來源文件：AutoRepair OS SRS（Google Doc export，已存於 `/tmp/autorepairOS.md`）
> 狀態：**已定稿**（car35 定義已確認；最後更新 2026-10-07 20:30 by Hermes）

---

## 0. car35 是什麼？（已確認，原阻塞點已解除）

原文件把這列為「唯一阻塞點」，三種猜測（A 車系 / B 子系統 / C 新模組）**都不對**。

**car35 = 車行原本在用的舊系統**，就是
`C:\Users\fr\35-20231008\`（Delphi + Access，檔案日期 1997–2014）：

- `Car35.exe` 主程式（標題：汽車修配管理系統）
- `Data35.mdb` 128 MB 主資料庫（33 張表）
- 32 份 Crystal Report（`.rpt`）報表定義
- `MAIN.INI` / `MESSAGE.INI` 等設定檔（公司：遠新汽車實業有限公司）

所以「car35 功能擴充」＝**把舊系統有而新系統缺的功能與資料補上**，
不是新增車系或子系統。Phase 1~5 的架構因此改為「遷移 + 功能對齊」。

詳見同目錄：`WORKLIST.md`（工作清單）、`hermes-checklist.md`（實作清單）、
`17-module-editability.md`（可編修性）、`FIELD-MAPPING.md`（欄位對照）。

---

## 1. 現有 SRS 結構（已理解）

AutoRepair OS 是一個汽車修理廠管理系統，分 13 大功能模組 + 資料模型 + 非功能需求：

| # | 模組 | 涵蓋 FR |
|---|------|---------|
| 5.1 | CRM（客戶） | FR-CRM-01~05 |
| 5.2 | Vehicle History / Passbook | FR-VH-01~06 |
| 5.3 | Insurance（保險） | FR-INS-01~03 |
| 5.4 | Suppliers（供應商） | FR-SUP-01~03 |
| 5.5 | Inventory & Bin Location | FR-INV-01~05 |
| 5.6 | Work Orders & Execution | FR-WO-01~08 |
| 5.7 | Staff & Roles | FR-STF-01~04 |
| 5.8 | Finance & Billing (AR/AP) | FR-FIN-01~08 |
| 5.9 | Appointments & Scheduling | FR-APP-01~05 |
| 5.10 | Automation & Notifications | FR-NOTI-01~05 |
| 5.11 | Subcontract & Tools | FR-SUB-01, FR-EQP-01 |
| 5.12 | Analytics & KPIs | FR-ANA-01~07 |
| 5.13 | Printing & Export | FR-PRINT-01~07 |

技術棧：Go 1.22+、SQLite(WAL)、Docker + docker-compose、
server-rendered HTML + Tailwind（**實作未用 Templ/HTMX**，原文件此處有誤）。

---

## 2. 計畫架構（已依 car35 定義套用）

### Phase 0 — 需求澄清 ✅ 完成
- [x] 確認 car35 定義 → 舊系統（見第 0 節）
- [x] 確認差異點 → 見 `WORKLIST.md` 第二、三節
- [x] 確認優先級 → 見 `docs/CAR35_USAGE_FREQUENCY.md`（用實際使用頻率排，不是憑常識）

### Phase 1 — 資料模型擴充 ⏳ 部分完成
- [x] migration 004~012（保險公司、工單保險欄位、庫存異動、零件類別、
      員工 kind、保養拆項、健保眷屬、舊系統補遷移、保險公司舊編號）
- [ ] `customer_pay` 收款沖銷（P0.5，最痛）
- [ ] 進貨單（`Material Header/Detail` 10.4 萬張）
- [ ] 車主對應修復（不改 schema，改比對鍵）

### Phase 2 — Backend ⏳ 部分完成
- [x] `/insurances`、`/inspections`、`/maintenance-templates`、`/expenses`
- [x] 工單列表技師／零件篩選
- [ ] `/receivables`、供應商 CRUD、進貨單

### Phase 3 — Frontend ⏳ 部分完成
- [x] 導覽 15 項（含新增四頁）
- [x] 首頁數字卡可點
- [ ] `/reports/*` 死連結清理
- [ ] `web/index.html` 改正式登入頁

### Phase 4 — 測試與驗證 ✅ 進行中
- [x] `go test ./internal/...` 全過
- [x] HTTP 回歸：`test_auth 10/10`、`test_workflow 10/10`、
      `test_status_flow 13/13`、`test_phase2 25/25`、`test_phase3 27/27`
- [x] UX 量測腳本 `scripts/ux_probe.py`
- [x] Docker smoke test

### Phase 5 — 部署 ✅ 完成
- [x] `docker compose -p autorepairos up -d --force-recreate`
- [x] named volume（`autorepairos_autorepairos-data`），服務在 `http://localhost:8090`

---

## 3. 風險與注意（更新）

- **離線 100% 運作**：car35 功能不得依賴外部服務 ✅
- **SQLite WAL 寫入**：⚠️ 原文件說「必須符合 `SetMaxOpenConns(1)` 相容性」是**錯的**——
  `MaxOpenConns(1)` 會讓 nested query 死鎖。WAL 在 Windows bind-mount 上會
  disk I/O error(4618)，故改用 named volume。見 skill `sqlite-production-pitfalls`。
- **Chrome/Edge 相容** ✅
- **30 天內可還原**：所有變更都有 migration/rollback 路徑 ✅
- **⚠️ 對正式 DB 做實驗前先複製**：服務啟動 1 秒內就會寫 `data/backups/*.db`

---

## 4. 待辦

- [x] 確認 car35 定義
- [x] 確認差異點與客製範圍
- [x] 依確認結果填寫 Phase 0~5 詳細 FR 清單
- [ ] 輸出完整 SRS 擴充版本

---

## 5. 簽名

| 項目 | 產出者 | 時間 |
|---|---|---|
| 原計畫草案（含 car35 定義三選項） | 大C（助理） | 2026-10-07 11:01 |
| 解除阻塞點：car35 = 舊系統，改寫 Phase 0~5 狀態、更正技術棧與 WAL 註記 | **Hermes** | 2026-10-07 20:30 |
