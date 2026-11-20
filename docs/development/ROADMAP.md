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

### [x] T2.1 Context 超時檢查
- 確認 vision.Client 使用 http.NewRequestWithContext 並設定逾時
- 涉及檔案：internal/integration/vision/client.go
- 驗收：超時時 Go Routine 不堆積

### [x] T2.2 圖片大小限制
- 檢查 Base64 長度並限制最大 10MB
- 涉及檔案：internal/handler/vision.go
- 驗收：超過限制回 413

### [x] T2.3 SupplierOptions 搜尋過濾（2026-11-09）
- 加入 search 參數，避免每次都撈 300 筆
- 涉及檔案：internal/handler/vision.go, web/pages/*.templ
- 驗收：關鍵字過濾正常 → `SupplierOptionsAPI` 支援 `?q=`（JSON + HX fragment 皆過濾），測試 `TestBatchISupplierOptionsSearch` 驗證

### [x] T2.4 共用錯誤處理封裝（2026-11-09）
- 封裝「解析圖片 → 呼叫辨識 → 回傳結果」Helper
- 涉及檔案：internal/handler/vision.go
- 驗收：減少重複程式碼 → 抽 `decodeVisionImage`（解析/驗證/503·400·413 共用），RecognizePlate/RecognizeInvoice 各縮短約 30 行

### [x] T2.5 辨識進度回報（2026-11-19，polling 版）
- 後端：`internal/handler/vision_jobs.go` 記憶體 job store（15 分鐘 TTL、建立時清掃）；
  `/api/vision/recognize` 與 `/api/vision/invoice` 加 `?async=1` 回 job_id 背景辨識；
  新增 `GET /api/vision/jobs/{id}`（RequireLogin）回 status/elapsed_ms/結果。同步模式保留（向後相容）。
- 前端：`static/scan.js` 與 `invoice_scan.templ` 改 async＋每 2 秒 polling，
  按鈕顯示「辨識中… Ns」經過秒數；舊 server 無 job_id 時退回同步行為。
- 測試：`vision_jobs_test.go`（done/error/404/TTL 清掃）。
- 驗收：使用者感知進度 ✓（秒數即時更新）。

## DATE 欄位 Scan 回歸（2026-11-10，提高 handler 覆蓋率時發現）

`appointments.appointment_date`、`equipment_maintenance.maintenance_date`／`next_maintenance_date` 在 migration 宣告為 `DATE`（NUMERIC affinity）。modernc.org/sqlite 把這種欄位 Scan 進 Go `string` 時會轉成 RFC3339Nano（實測 `2026-11-10` → `2026-11-10T00:00:00Z`），造成：

- 預約日曆（`appointmentCalendar`）用 `2006-01-02` 當 map key → 永遠 miss → 每一天都顯示「此日沒有預約」（真實功能失效）
- 預約列表／編輯頁／即時搜尋、設備保養紀錄的日期顯示全錯

修法：SELECT 改成 `CAST(col AS TEXT)`，讓 Scan 拿到原始儲存字串。涉及 5 個預約讀取點（`appointments.go` 編輯表單、`appointments_list.go` 列表／日曆／搜尋 API、`workorder_new.go` 預約帶入）與 `equipment.go` 保養紀錄 2 欄。

迴歸測試：`internal/handler/appointments_calendar_date_test.go`（用真實 migration schema）。舊日曆測試的 fixture 自行宣告成 TEXT schema，所以沒抓到這個 bug；實測把 CAST 拿掉，該測試即 FAIL。

## 2026-11-10 品質精進輪：成果

### 覆蓋率（`go test ./... -cover` 實測）

| Package | 起點 | 結果 |
|---|---|---|
| internal/handler | 75.5% | **83.4%** |
| web/pages | 70.6% | **82.4%** |
| web/components | 77.3% | **91.2%** |
| internal/service | 74.4% | 95.2% |
| internal/db | 65.7% | 97.8% |
| internal/session | 7.8% | 95.3% |
| internal/cron | — | 96.1% |
| internal/config | — | 98.3% |
| internal/i18n | — | 96.5% |
| internal/seed | — | 98.1% |
| internal/integration/* | — | 95.4–100% |

全 17 package PASS、`go build ./...` 乾淨。

### handler 拆分與表單錯誤體驗

- 過大的 handler 依職責拆成單一目的檔案：`appointments_list.go`、`inventory_list.go`、`photos_list.go`、`receivables_list.go`、`staff_list.go`、`vehicles_list.go`、`workorder_list.go`、`workorder_show.go`、`ap_list.go`、`analytics_home.go`／`analytics_export.go`、`print_statements.go`／`print_workorder.go`、`sop_templates.go`／`sop_records.go`、`purchase_orders_items.go`、`layout_format.go`／`layout_helpers.go`、`line_binding.go`（`workorder_crud.go` 1132 行变更、淨刪 7160 行）。
- 輸入驗證失敗改走 `formError()`：HTMX 請求回傳 `msg-err` fragment 原地顯示，全頁請求回傳帶「返回表單重填」連結的頁面殼（`internal/handler/layout_helpers.go`）。DB／權限／商業規則錯誤仍保留 `fail()`。

### 雙語（i18n）

- `internal/i18n` + `locales/zh-TW.json`／`en.json`（各 1337 key，完全對齊）；字典以 `//go:embed` 編進 binary，exe（cwd=release/）與 docker image（cwd=/app）不再依賴工作目錄下的 `locales/`（舊版兩邊都載入失敗，介面上直接出現 `dashboard.title` 這種 raw key）。`POST /set-lang`、`POST /set-theme` 偏好端點，`static/prefs.js` 前端切換；語系與主題皆改為下拉選單。預設語系固定為繁體中文，不再依 Accept-Language 自動切換。
- `web/pages`、`web/components` 全數靜態字串改為 `i18n.T(ctx, ...)`，`*_templ.go` 重新產生。

### Templ 覆蓋手法

templ 生成的 `*_templ.go` 用 4KB bufio 緩衝，failing-writer 只在最後一次 flush 才觸發，所以 per-write error 分支涵蓋不到。做法：在測試的 `init()` 把 `templruntime.DefaultBufferSize` 設為 1，每次 write 都 flush（`web/pages/pages_writer_sweep_test.go`，函式列表由本機產生器（不進版控）從 `*_templ.go` 簽名產生）。components 77.3%→91.2% 由此而來。

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
## 2026-11 SRS 全量複查與交付輪（至 2026-11-19）

80 條 FR 分 5 組平行查核、逐條對程式給證據後分批修正並推送；同輪完成測試、
無障礙與交付環保收斂。以下數字皆為實跑結果，可重現。

### SRS 缺口收斂（commit 軸）

| 項目 | 成果 |
|---|---|
| 安全性／權限 | FR-STF-02（`/staff` 寫入限 owner，新增 `OwnerOnlyPage`）、CRM-04 通知綁定檢查、IDOR（列印/照片頁）修正 |
| 資料一致性 | 工單明細／估價項目改軟刪（migration 052／053）、員工電話（054）、照片標記指定明細（055）、收款賒帳（056） |
| 金流 | FR-FIN-01 賒帳/月結、FR-FIN-02 結帳需有足額收款紀錄且防超額收款、FR-FIN-03/04 應收清單＋帳齡 |
| 其他 | FR-APP-05 當日進場前導、FR-INS-03 催保單列印、FR-INV-05 庫位、FR-SUB-02 外包預計完成日、FR-WO-05/06/07、FR-PRINT-07 列印鈕、FR-ANA-05 今年營收 |

### 測試與驗證（皆為實跑數字）

- `go test ./...`：**17/17 package 全綠**（含 Templ drift CI 同步指令）。
- 角色 × GET 路由矩陣（104 條路由 × 3 角色 = **312 次**）：越權 0、死路 0、5xx 0；4 條「無畫面入口的收緊防線」與 1 條 IDOR 正常拒絕已分類標註。
- HTTP 併發結帳：8 緒結帳只扣一次庫、4 緒全額收款只成立一筆（修掉原本會收 4× 的超額收款）。
- 屬性測試（固定種子亂數序列）：庫存不為負、reserved ≤ stock、異動對帳成立。
- a11y 快測：10/10 頁 + 雙主題 14/14 對比 PASS；真瀏覽器 Tab 焦點 2px 外框。
- 語系網格：3 角色 × 26 頁 × 2 語系，漏 key 0、英文頁残留判讀完成。

### 交付與部署

- exe：`release/AutoRepairOS-2026-11-19_090450.exe`（git 992d69d）；冒煙全 PASS（含修復端點、收款表單 action、css md5、啟動 log git 修訂）。
- Docker：`--force-recreate` 後 054/055/056 套用、8090 正常、正式 volume 資料無損（85,825 工單 / 6,512 客戶 / 12,964 車輛）。
- 正式 DB 健檢：`PRAGMA integrity_check = ok`、`foreign_key_check` 無違規。
- 交付環保：`release/` 剛好 6 件交付物（清掉舊 zip/目錄、`release/data`，以及會 shadowing 內嵌字典的 `release/locales`）；docker 舊 tag 清空僅留 `latest`；`cmd/bench` 與 `coverage` 移出版控（改放 `unpublished/`）。

### 工具（均在 `unpublished/analysis-and-tests/tools/`，不進版控）

`route_migration_gap.py`、`gen_restore.py`、`release_smoke.py`、`locale_grid.py`、
`role_route_matrix.py`（含 KNOWN_NO_ENTRY 分類）、`e2e_checkout_fixture.py`、
`a11y_check.py`（HTML＋WCAG 雙主題，FAIL exit 1）、`http_edge_probe.py`、`bench/`。

### 仍待處理

- [x] T2.5 辨識進度回報（2026-11-19 完成，polling 版，見上）。
- [x] FR-IN-06 庫存盤點（2026-11-19）：盤點單（全庫/分類/庫位快照）→逐筆實盤（HTMX 列更新＋進度 OOB＋條碼搜尋/相機）→完成以絕對值校正庫存（movement source=stocktake＋audit）；migration 058、限櫃檯以上、測試 3 案。解决健檢發現的 4001 筆負庫存 legacy。
- [x] FR-APP-06 LINE 預約掛號（2026-11-20）：已綁定客戶傳「預約」→日期→車輛→服務→確認，建立 booked 預約；記憶體對話狀態 10min TTL、日期寬容解析（ISO/M-D/明天/後天、限 60 天）、多車擇一、随时取消；測試 6 案。
- [x] 內網限定部署（2026-11-20，用戶政策：web 內網、外部只開 LINE webhook、客戶走聊天）：compose web 埠改綁 127.0.0.1＋LAN_BIND_IP（實測 loopback/LAN 200、vEthernet 拒連）；exe 加 BIND_IP 設定；tunnel ingress 為遠端管理（dashboard），需確認 path ^/webhook/line$＋catch-all 404；.env 的 LINE_WEBHOOK_PUBLIC_URL 是已死的 quick tunnel 舊值，待用戶提供正式 hostname 更新。

## 2026-11-19 全量複查修補

本輪依 `.hermes/plans/2026-11-19_141211-full-review.md` 收斂高風險缺口：

- P0：全站安全標頭、可信代理登入限流（含帳號維度與容量清掃）、收款作廢與庫存回沖 audit trail。
- P1：收款單號改為 SQLite 原子 `RETURNING`、session fail-closed/改密清除舊 session、每日 session 清理、通知每批 200 並單次最多 1000、SOP 查詢失敗改 fail-closed。
- 功能與文件：工單新增 campaign code 輸入、英文報表單位補齊、備份 bind mount 移出資料 volume、SRS 部署圖與 Thermo 進度表同步；LINE 履歷加入下次保養日期/里程建議。
- Migration 057 新增 `audit_log`、收款作廢操作者/時間欄位與索引。
- 補強：owner-only `/audit-log`、照片佇列每 5 分鐘自動重試（5 次後 `permanent_failure`）、052–057 rollback 檔與 `MigrateDownTo`、scratch volume 還原工具 `gen_restore.py`。

驗證：`go build ./...`、`go test ./...`、Templ `generate -check -path web` 均通過。

### 第二輪補完與部署（2026-11-19 晚，Hermes）

- 稽核留痕補齊：工單明細軟刪（交易內）、PO 品項軟刪、估價項目刪除、零件價格異動（before/after）；`/audit-log` 改 Templ＋i18n＋owner 導覽。
- FR-CRM-05 補完：工單明細頁 CampaignRow 可編輯 campaign_code；開單 datalist 帶入既有代碼。
- 維運 CLI：`--backup-now`（手動完整備份）、`--migrate-down-to <version>`（回滾前強制備份）。
- 文件：USER_GUIDE 同步密碼政策／備份外部掛載／稽核頁／campaign／照片自動重試／登入安全；SRS §8.5 部署圖說明修正。
- 已部署（docker compose up -d --build --force-recreate）：login HTTP 200、4 個安全標頭上線、migration 057 已套用、排程器 8 任務、BACKUP_DIR=/backups（主機 bind mount，移出資料 volume）。

### 還原演練紀錄（2026-11-19 16:1x，首次實際演練）

- 備份檔：`autorepair_20261119_161044.tar.gz`（11,625,336 bytes；DB 43,167,444 bytes；照片 0 張——尚未上傳過照片）。
- 工具：`gen_restore.py`（還原到 scratch volume `autorepairos-restore-scratch`）＋`restore_drill.sh`（筆數對帳）。
- `PRAGMA integrity_check` = **ok**。
- 筆數對帳（restored vs 正式快照，全數一致）：
  | 表 | 還原 | 正式 |
  |---|---|---|
  | work_orders | 85825 | 85825 |
  | customers | 6512 | 6512 |
  | vehicles | 12964 | 12964 |
  | receivables | 1 | 1 |
  | inventory_items | 10363 | 10363 |
  | work_order_items | 344745 | 344745 |
- 照片：tar 0 vs 正式 0（一致）。演練後 scratch volume 已刪除，未觸碰正式資料。
- 注意：備份 tar 內 DB 檔名帶時間戳（`autorepair_<stamp>.db`），非 `autorepair.db`；gen_restore.py 已修正為動態尋找 *.db。

### 效能基準實測（2026-11-19，NFR §7.1 證據）

工具：`unpublished/analysis-and-tests/tools/bench`（本輪修正：列表測試改真實 JOIN＋排序＋完整取回、5 次取最慢，不再只量 COUNT；unpublished/go.mod 模組名修正為 `autorepairos/unpublished`＋replace 才能編譯）。
環境：Windows 11 / Go 1.27 / modernc SQLite WAL；scratch 測試庫，scale=10000（實際產生：客戶 10000、車輛 15000、庫存 5000、工單 20000、通知 1000）。

| 項目 | 實測 | 門檻（SRS §7.1） | 結果 |
|---|---|---|---|
| 車輛列表（10k 筆，JOIN lookup＋里程彙整） | 26.5ms | <300ms | PASS |
| 工單列表（10k 筆，28 欄 5 JOIN，5 次最慢） | 52.0ms | <300ms | PASS |
| 庫存列表（5k 筆，5 次最慢） | 8.6ms | <300ms | PASS |
| 客戶列表（10k 筆，5 次最慢） | 20.7ms | <300ms | PASS |
| 通知批次（1000 筆 pending→sent，5 批×200） | 6.0s | <1 分鐘 | PASS |

通知吞吐實測 1000 筆／6 秒，遠超 SRS「每分鐘 1000 筆」要求（本輪 cron 改每批 200、單次上限 1000 後之實測）。
