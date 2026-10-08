# Car35 → AutoRepair OS 遷移工作清單

> 整理自：舊系統 UI 截圖 + mdb 實測筆數 + migration-plan + hermes 實戰 skill
> 狀態：**部分完成**（見文末簽名表；最後更新 2026-10-08 10:18 by 大C）
> 
> **2026-10-08 大C 實測更正**：依 `d74f194`/`0f9872c`，下列已從「未遷」改成「已遷」：
> - 2.6 `Customer Pay`（90,762）→ ✅ 已遷 `customer_pay`（90,615 筆）
> - 2.6 `Material Header/Detail` → ✅ 已遷（回填 `inventory_movements`）
>
> ⚠️ 本清單的「未實作」標記已過期：第 2.6 節多項、第 3.3 節兩項已完成，
> 詳見文末〈八、完成紀錄〉。

---

## 一、待決定的關鍵問題（Phase 0）

- [ ] **優先級**：哪些舊資料是珍貴業務資料必須完整搬？（歷史出庫明細可捨）→ 用戶已答：**全搬**
- [ ] **範圍**：只做資料遷移，還是要把舊功能對齊到新模組？→ 用戶已答：**舊功能對齊**
- [x] **保固/檢查邏輯**：舊 `Vaild Date`/`End Check Date` 計算規則要不要帶過？→ `Vaild`=保固、`End Check Date`=其他
- [ ] **收銀機明細（CitS/Cit）**：是否要遷移？（影響 AR 歷史）→ **未定**
      （Hermes 註：實測 `Data35.mdb` **沒有** CitS/Cit/CitM 這三張表，
       只有 `Maintain Header/Detail`、`Material Header/Detail`、`Customer Pay`。
       此項可直接移除。）

---

## 二、資料遷移（核心，先做）

### 2.1 環境準備
- [x] 確認 `access-parser` 已裝（0.0.6 ✓）
- [x] 修 `migrate_access.py` dry-run bug（要求目標 DB 已有 AutoRepair schema）
- [x] 修 PowerShell 控制台編碼問題（cp1252 擋 UTF-8，需 `PYTHONUTF8=1`）
      （Hermes 註：本機 shell 是 git-bash，直接寫 `.py` 執行即可；
       會 print 中文的腳本在 Windows console 仍會崩，故寫進 `out.txt`）
- [x] 確認 `Data35.mdb` 來源檔存在（133MB ✓，且 `access-parser` 可完整解析）

### 2.2 5 張主資料表（欄位對應已整理見 FIELD-MAPPING.md）
- [x] **Customer** → `customers`：已遷 6,512 筆（CAR35 8,118 筆，客戶名稱 100% 對得上）
      補欄位（`tax_name`/`contact_person`/`birthday`/`closing_day` 等）**待做**
- [x] **Car** → `vehicles`：已遷 14,963 筆（原 12,964 + 姓名 fallback 救回 1,999）
      ✅ 車主對應已修復（`0f9872c`，2026-10-08）：覆蓋率 86.5% → 99.7%
- [x] **Firm** → `suppliers`：已遷 691 家（材料商 563 + 協力廠商 132）
      補欄位（`tax_id`/`fax`/`sales_name`）**待做**
- [x] **Material** → `inventory_items`：已遷 10,352 筆
- [x] **Staff** → `users`：已遷 102 筆（含 40 位健保眷屬，`staff_kind` 已區分）

### 2.3 保險公司（獨立表，勿從 Staff 推論）
- [x] **Insurance Company**（15 筆）→ `insurance_companies`：16 筆（含 1 筆測試資料）
      並補 `legacy_no` 讓投保紀錄能對應（migration 012）
- [x] 確認 AutoRepair 已有 `insurance_companies` 表 ✓

### 2.4 銀行資料（lookup）
- [ ] **Bank**（160 筆）→ 對應銀行表
      （Hermes 註：實測 `Bank` 表的 `Account ID`/`Create Date`/`Telephone`/`Comm Name`
       **全部 0 筆**，舊系統只用到「銀行名稱 + 簡稱」。
       要做付款方式時銀行只需 `code` + `name`，不必做帳號欄位。）

### 2.5 Lookup 表（筆數少、適合 --lookups 匯出 JSON）
- [x] Car Class（車種）、Gas Class（燃料）、Branch（部門）、Unit Class（單位）、Color Class（顏色）
      → 已匯出 `data/legacy_lookups.json`（12 張表）
- [x] Dropdown 字典來源檔（零件類別/汽車廠牌/廠牌車型/部門/保險類別/零件庫位/車種/顏色/燃料/單位/字庫）
      → `Model` 929、`OEM` 74、`Color Class` 100、`Car Class` 21、`Insurance Class` 12 等

### 2.6 日常作業交易表
- [x] **Maintain Header**（95,440）→ `work_orders`：已遷 85,825 筆
      （缺 9,615 筆，多數是上述車主對應問題）
- [x] **Maintain Detail**（390,425）→ `work_order_items`：已遷 344,745 筆
      ⚠️ **技師分工未遷**：舊系統 231,188/390,425 筆明細有 `Staff No`，
      我們 `technician_worklogs` 只有 32 筆 → 技師產值／佣金算不出來
- [x] **Material Header**（103,916）→ 進貨主檔：**已遷**（`d74f194`，回填 `inventory_movements`）
      （Hermes 查證：這是**真的進貨單**——`Firm No` 101,373/103,365 能對到廠商主檔、
       明細單價對應零件的 `In Price` 進價而非售價。可直接回填 `inventory_movements`）
- [x] **Material Detail**（166,315）→ 進貨明細：**已遷**（`d74f194`，料號 163,908 對零件主檔）
- [x] **Customer Pay**（90,762）→ 應收帳款沖銷：**已遷**（`d74f194`，最痛已補）
- [x] **Spend List**（3,390）→ 支出：已遷（`expenses`）
- [ ] **應收票據/應付票據** → 票據類（check_no/到期日）
      （Hermes 註：`Check In`、`Check Out`、`Firm Pay` 三張表**都是 0 筆**，
       舊系統從未使用 → 這是**全新功能**不是遷移。）

### 2.7 收銀機明細（未定）
- [ ] **CitS / Cit / CitM** → 消費/銷售流水（影響 AR/expense 歷史）
      （Hermes 註：`Data35.mdb` 無此三表，**此項移除**。AR 歷史改用 `Customer Pay`。）

---

## 三、功能對齊（舊功能 → 新模組）

### 3.1 基本資料模組
- [x] 客戶（CRM）— `/customers` 完整 CRUD
- [x] 車輛（Vehicle History）— `/vehicles` 完整 CRUD + 履歷頁
- [x] 廠商（Suppliers）— `/suppliers` 列表（**補 CRUD 待做**）
- [x] 員工（Staff）— `/staff` 列表（**補 edit/create 待做**）
- [x] 零件（Inventory + Bin Location）— `/inventory` 完整 CRUD + 調整 + 負補正

### 3.2 日常作業模組
- [x] 估價/維修單登錄（Work Orders）：送修→維修中→待料→完成→收款
      狀態流轉完整；列表已補**技師／零件篩選**（`14f960d`）
- [x] 零件進貨登錄（Inventory 進貨）：**已做**（`d74f194`，Material Header/Detail 已回填 `inventory_movements`）
- [x] 應收帳款沖銷（AR）：**已做**（`d74f194`，customer_pay 已遷 90,615 筆）
- [ ] 應付帳款（AP）：**未做**（舊系統空表，全新功能）

### 3.3 保險/檢查
- [x] 車輛投保明細清單（`vehicle_insurances`，2,438）— `/insurances` 已上線
- [x] 車輛驗收明細清單（`vehicle_inspections`，19,918）— `/inspections` 已上線
      （已做「已逾期／待檢驗」狀態判斷）
- [ ] 保固追蹤邏輯（`Vaild Date`）：**未做**
- [ ] 檢查日期邏輯（`End Check Date` / `Start Check Date`）：**未做**
      （Hermes 註：`Car.Start Check Date` 4,896 筆、`Car.Vaild Date` 4,853 筆有值；
       `End Check Date` 只有 190 筆 → 實際完成幾乎沒回填）

### 3.4 報表
- [ ] 客戶報表、廠商報表、出險報表、車輛維修記錄、維修人員記錄、當日維修記錄、零件庫存、其他明細表
      （`/reports` 現有統計頁；子頁死連結待清）
- [ ] 自繪報表（10 個 u2*.dll）無源碼，需重做版面

---

## 四、系統設定（工具箱）

### 4.1 INI 設定檔
- [x] **MAIN.INI**：資料路徑、印表機、字型 → 已讀取分析（公司資料：遠新汽車實業有限公司）
- [ ] **GETDATA.INI**：批次設定（G01~G18 外掛資料抓取）
- [x] **MESSAGE.INI**：UI 欄位提示字串 → 已用來確認欄位語意（見 skill `legacy-system-feature-extraction`）
- [ ] **PROGRAMS.INI / REPORT.INI / Querist.ini**

### 4.2 字型檔
- [ ] 字庫速查（T001.TTF 等）→ 中文字型

### 4.3 密碼/使用者
- [x] 密碼設定（INI / 使用者檔）→ `users` 表 + bcrypt
- [x] 變更使用者（對應 users 表）→ 三角色權限模型

---

## 五、驗證與部署
- [x] 各表筆數對照（舊 mdb vs 新 SQLite）→ `scripts/analyze_car35_data.py`（可重跑）
- [x] 日期格式轉換（`112/10/07` 台曆 → ISO 8601）→ `parseFlexTime` / `normDate`
- [ ] NULL / 空白欄位檢查
- [ ] 重複 car_no / customer_no 去重
- [x] Docker 環境 smoke test（DB 在 named volume：stop → 清 -wal/-shm → cp → recreate）
      → 服務在 `http://localhost:8090`，`docker compose -p autorepairos`

---

## 六、風險與注意
- [x] 自繪報表無源碼 → 用現代報表重做，保留資料
- [x] `.mdb` 老格式解讀可能損毀 → 遷移前先備份 + 解讀驗證
      （實測 `access-parser 0.0.6` 可完整讀出 33 張表，無損毀）
- [x] 日期格式特殊（台曆）→ 轉換為 ISO 8601
- [ ] LINE 通知無對照 → 客戶通知偏好設為非 LINE
- [x] 印表機驅動 → 用系統標準印表機，版面用 CSS

---

## 七、Hermes 補充：本清單漏掉的兩件事

1. **車主對應缺口**（第 2.2 節已註）——這是**最高價值的單一修復**：
   改一個比對鍵就能讓車輛覆蓋率 86.5% → 99.8%、工單 90.1% → 99.9%。
   不動任何畫面。
2. **工單的「可查詢性」**（原清單只盤點「可編修性」）——
   85,825 筆工單原本只能靠關鍵字搜尋，已補技師／零件篩選（`14f960d`）。

---

## 八、完成紀錄

| 日期 | 項目 | 產出者 | commit |
|---|---|---|---|
| 2026-10-07 | 逐表逐欄資料盤點（33 張表、非空筆數） | Hermes | `a87e60a` 等 5 筆 |
| 2026-10-07 | 畫面級對照（截圖欄位 → 我們的欄位） | Hermes | `8528435` |
| 2026-10-07 | 功能使用頻率盤點（決定開發順序） | Hermes | `b267ced` |
| 2026-10-07 | UX 順手度檢視（實跑量測） | Hermes | `b267ced` |
| 2026-10-07 | 首頁數字卡可點 / 技師下拉過濾 / 按鈕改名 | Hermes | `66e929f` |
| 2026-10-07 | 四頁歷史明細（投保/定檢/保養套餐/支出） | 大C（Hermes 提交） | `0d0b1eb` |
| 2026-10-07 | 工單列表技師／零件篩選 + 零件欄 | Hermes | `14f960d` |
| 2026-10-08 | `customer_pay` 收款沖銷（90,615 筆） | Hermes | `d74f194` |
| 2026-10-08 | 進貨單 Material Header/Detail 回填 | Hermes | `d74f194` |
| — | 供應商 CRUD（P2）／技師分工回填 | ⬜ 待辦 | — |

> 服務現況：`http://localhost:8090`（Docker）｜帳號 `owner` / `Owner001!`
> 可重跑的分析：`python scripts/analyze_car35_data.py`、`python scripts/ux_probe.py`
