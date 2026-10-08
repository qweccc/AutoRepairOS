# Car35 → AutoRepair OS 遷移計畫

> **背景**：`C:\Users\fr\35-20231008` 是舊的 **Car35 汽車維修系統**（MS Access `.mdb` + 自訂 `.exe`/`.dll` + INI 配置 + 自繪報表）。現要把它的業務資料與功能搬進 **AutoRepair OS**（Go + Templ + SQLite）。
> **目標**：先做計畫（Phase 0~5），不急着實作。
> **狀態**：已勘探舊架構，產出對照表與遷移藍圖。

---

## 0. 舊系統本質

| 層 | 內容 | 說明 |
|----|------|------|
| 前端 | `Car35.exe` + 10 個 `u2*.dll` | 自繪窗體、自繪報表（GDI+）、自訂 UI 控件 |
| 資料 | `Data35.mdb` (133MB) | MS Access，含業務表 + 系統雜項表 |
| 配置 | `MAIN.INI / GETDATA.INI / MESSAGE.INI / PROGRAMS.INI / REPORT.INI / Querist.ini` | 中文字型、印表機設定、SQL 查詢串、版面尺寸 |
| 輔助 | `CheckData5.exe`、`COMP50.EXE`、`GETNAME/SETNUL/SETPASS`、`StatusMonitor`、Printer Driver | 資料抓取、印表機驅動、狀態偵測 |
| 外掛資料 | `master_data.json` / `old_data.json` | 備份用的 JSON（Customer/Car/Firm/Staff/Material） |

**關鍵洞察**：舊系統是「**維修廠收銀/庫存/保固追蹤**」系統，不是完整的 CRM/排程/LINE 通知。AutoRepair OS 涵蓋範圍遠大於舊系統。遷移 = **把舊系統的業務資料對齊到新系統的模組**，而非 1:1 搬運。

---

## 1. 舊系統模組 ↔ AutoRepair OS 模組 對照表

### 1.1 舊系統資料表（從 `Data35.mdb` 反解）

| MDB 表 | 業務意義 | 對應 AutoRepair 模組 |
|--------|----------|----------------------|
| **Car** | 車輛主檔（車號、車主、車種、引擎/車體號碼、檢查日期、有效期限） | CRM + Vehicle History |
| **Maintain Header** | 維修單主檔（車號、送修日期、完成日期、客戶、里程） | Work Orders |
| **Maintain Detail** | 維修單明細（項目、數量、單價、金額、技師） | Work Order Items |
| **Material Header / Detail** | 材料/零件主檔與出庫明細（進出價、安全庫存、廠商） | Inventory + Suppliers |
| **Check In** | 檢查/檢查項目 | Vehicle Inspection (Walkaround) |
| **Staff Total** | 員工主檔 | Staff |
| **Spend List / Spend Temp List** | 支出/採購流水 | AP (Accounts Payable) |
| **CitS / Cit / CitM** | 收銀機明細（消費/銷售流水） | Finance / Transaction |
| **AccM / AccS1 / AccS2** | 會計總帳（年度分月） | AR/AP Finance |
| **Ins** | 保固/保險記錄 | Insurance |
| **SysRel** | 系統關聯/雜項 | (捨棄) |
| **MSysModules** | Access 系統模組（雜項） | (捨棄) |

> 註：表名以上為從 `Data35.mdb` 二元檔反解的關鍵字（已確認：Maintain Header/Detail、Material Header/Detail、Check In、Car、Staff Total、Spend List、CitS/Cit、AccM、Ins、SysRel、MSysModules）。精確欄位需進一步解析。
| **Customer** (JSON) | 客戶資料 | CRM |
| **Car** (JSON) | 車輛資料 | Vehicle History |
| **Firm** (JSON) | 廠商資料 | Suppliers |
| **Staff** (JSON) | 員工資料 | Staff |
| **Material** (JSON) | 材料資料 | Inventory |

### 1.2 舊系統功能模組 ↔ AutoRepair OS

| 舊系統功能 | AutoRepair OS 對應 | 遷移難度 |
|------------|-------------------|----------|
| 車輛管理 (Car) | CRM + Vehicle History | 中 |
| 維修單 (Maintain Header/Detail) | Work Orders | 高（核心） |
| 材料/庫存 (Material) | Inventory + Bin Location | 中 |
| 廠商 (Firm) | Suppliers | 低 |
| 員工 (Staff) | Staff | 低 |
| 客戶 (Customer) | CRM | 低 |
| 支出/採購 (Spend List) | AP Aging | 中 |
| 檢查項目 (Check In) | Walkaround Inspection | 中 |
| 報表列印 (Carp*.rpt) | Printing & Export | 中（版面需重做） |
| 保固追蹤 (Vaild Date / End Check) | Vehicle History / Insurance | 中 |

---

## 2. 資料遷移藍圖

### 2.1 資料轉換（Legacy → SQLite）

| 舊資料 | 新表 | 轉換注意 |
|--------|------|----------|
| Car | `customers` + `vehicles` | 車主→客戶，車輛→車輛；車號 UNIQUE |
| Maintain Header | `work_orders` | status 依完成日期推斷；current_mileage 從表取 |
| Maintain Detail | `work_order_items` | item_type: 勞力→labor / 零件→part |
| Material Header | `inventory_items` | 含 part_number/barcode/bin_location |
| Material Detail | (歷史出庫流水，選留) | 可轉 `transactions` 或只存統計 |
| Firm | `suppliers` | 含 now_price/up_price |
| Staff | `staff` | 含 id_no, bank_no, telephone |
| Spend List | `accounts_payable` | 含 due_date |
| Customer (JSON) | `customers` | line_user_id 需補（舊系統無 LINE） |

### 2.2 資料完整性檢查（遷移前）
- [ ] 各表筆數統計（Car/Maintain/Material/Firm/Staff/Customer）
- [ ] NULL / 空白欄位檢查
- [ ] 日期格式轉換（舊系統用 `112/10/07` 台曆格式）
- [ ] 重複 car_no / customer_no 去重
- [ ] 金額欄位（REAL）與位址欄位（TEXT）驗證
- [ ] 印表機/字型 INI 設定**不遷移**（新系統用 Tailwind + 現代印表機）

---

## 3. 遷移 Phase 規劃

### Phase 0 — 需求澄清（Blocker）
- [ ] **確認優先級**：哪些舊資料是「珍貴業務資料」必須完整搬？哪些可捨棄（如歷史出庫明細）？
- [ ] **確認範圍**：只做資料遷移，還是要把舊系統功能對齊到 AutoRepair 各模組？
- [ ] **確認保固/檢查邏輯**：舊系統 `Vaild Date`/`End Check Date` 的計算規則是否要帶過？

### Phase 1 — 資料探索與驗證
- [ ] 解出 `Data35.mdb` 完整 schema（筆數、欄位類型、NULL 率）—— 需 Access 工具（mdbtools / python pypykdtree，此機無）
- [ ] 產出《資料品質報告》（缺失、重複、格式問題）
- [ ] 確認 JSON 備份資料（master_data.json）與 .mdb 是否一致
- [ ] 確認收銀機明細（CitS）是否要遷移（影響 AR 歷史）

### Phase 2 — SQLite Schema 設計
- [ ] 依 AutoRepair OS 現有 schema 擴充（見 SRS §6）
- [ ] 新增遷移用欄位（如 `import_source`, `import_date`）
- [ ] Migration SQL 腳本（backward-compatible）

### Phase 3 — 資料遷移程式
- [ ] 寫 Go 遷移工具（讀 .mdb → 寫 SQLite）
- [ ] 批次處理 + 進度回報 + 錯誤日誌
- [ ] 回滾機制（保留原始 .mdb）

### Phase 4 — 對齊舊系統功能
- [ ] Work Orders 流程對照（送修→維修中→待料→完成→收款）
- [ ] Inventory 進出庫邏輯對照
- [ ] Suppliers / Staff / CRM 基本資料對照
- [ ] 報表版面重做（Tailwind 替代自繪）

### Phase 5 — 驗證與部署
- [ ] 資料筆數對照（舊 vs 新）
- [ ] 關鍵查詢驗證（維修單總額、庫存餘額、AR/AP 餘額）
- [ ] Docker 環境 smoke test
- [ ] 離線模式驗證（AutoRepair 核心要求）

---

## 4. 風險與注意

| 風險 | 說明 | 對策 |
|------|------|------|
| 自繪報表無源碼 | `u2*.dll` 是編譯好的，無法直接搬版面 | 用 AutoRepair 現代報表重做，保留資料 |
| MS Access 格式舊 | `.mdb` 老格式解讀可能損毀 | 遷移前先備份 + 解讀驗證 |
| 日期格式特殊 | `112/10/07` 台曆 | 轉換為 ISO 8601 |
| LINE 通知無對照 | 舊系統無 LINE | 客戶通知偏好設為非 LINE |
| 印表機驅動 | 自訂印表機驅動 | 用系統標準印表機，版面用 CSS |

---

## 5. 建議優先順序

1. **先確認 Phase 0 三個問題**（優先級/範圍/保固邏輯）
2. **Phase 1 資料驗證**（確認資料品質，避免搬錯）
3. **Phase 2~3 資料遷移**（核心價值，先確保資料不丟）
4. **Phase 4 功能對齊**（逐步把舊功能搬到新模組）

---

## 下一步

確認 **Phase 0** 三個問題後，我就能：
- 產出完整《資料品質報告》
- 設計 SQLite Migration schema
- 開始寫資料遷移程式

要我先深入解 `Data35.mdb` 的完整 schema（筆數、欄位、NULL 率）嗎？這會決定資料品質的準確度。
