# AutoRepairOS 功能缺口檢視與實作計畫（給 hermes）

> 產出時間：2026-10-07 16:15
> 產出者：大C（助理）
> 服務狀態：`http://127.0.0.1:8090/` 已啟動（200 OK）
> 目的：盤點「功能不完整的地方」→ 列出缺口 → 給 hermes 可執行的實作清單。
> **⚠️ 2026-10-08 大C 註：本檔最舊（16:15），Hermes 於 10-08 已補收款沖銷 + 車主對應 + 進貨單，
> 下列缺口已關閉，請以 `hermes-checklist.md`、`WORKLIST.md`、`docs/hermes-completed.md` 為準。**

---

## 一、盤點方法

1. **逐路由探測**：對 50+ 路由發 GET/POST，記下 404/405/500。
2. **碼掃描**：找 `TODO/FIXME/placeholder/未完成` 標記。
3. **DB 盤點**：`data_container_autorepair.db` 各業務表筆數與欄位。
4. **對比 Car35**：對照舊系統 G01~G18 業務模組。

---

## 二、✅ 已完成（勿動）

| 模組 | 狀態 | 備註 |
|---|---|---|
| 總覽儀表板 `/` | ✅ | 6 統計卡 + 最近 8 工單（`auth.go:IndexHandler`） |
| 客戶 `/customers` | ✅ | CRUD 完整（30 筆資料） |
| 車輛 `/vehicles` | ✅ | CRUD + 履歷頁（20 筆） |
| 工單 `/work-orders` | ✅ | 登錄/狀態/核准/SOP/明細/工時/收據/照片（17 筆） |
| 估價單 `/work-order_new` | ✅ | 估價流程 |
| 保險公司 `/insurance-companies` | ✅ | CRUD（主檔） |
| 供應商 `/suppliers` | ✅ | 列表 |
| 庫存 `/inventory` | ✅ | CRUD + 異動紀錄 + 負庫存補正（16 筆） |
| 員工技師 `/staff` | ✅ | 列表 + 健保獨立 |
| 保險投保 `/insurances` | ✅ | 列表（頁面存在但**無投保資料**） |
| 定檢 `/inspections` | ✅ | 列表（頁面存在但**無定檢資料**） |
| 保養套餐 `/maintenance-templates` | ✅ | 列表（頁面存在但**無資料**） |
| 支出 `/expenses` | ✅ | 列表（頁面存在但**無資料**） |
| 技師佣金 `/technicians/commissions` | ✅ | 列表 |
| 報表 `/reports` | ✅ | 營收卡/狀態分布/熱門項目/技師績效/庫存低水位（保險/自費分欄） |
| 排程與備份 `/scheduled-tasks` | ✅ | 4 任務 + 通知佇列 + 備份紀錄 |
| 離線資產 `/offline.js`、`/sw.js` | ✅ | IndexedDB 草稿快取 + Service Worker |

---

## 三、⚠️ 功能缺口（需要 hermes 實作）

### G1. 業務資料表完全缺失（最優先）

以下模組有 **UI 頁面但對應 SQLite 表不存在 / 為空**，登入後空白。舊 Car35 這幾項都有資料，是遷移遺漏。

| 表名 | 頁面 | 現況 | 舊系統來源 |
|---|---|---|---|
| `insurance_companies` | /insurance-companies | ❌ 表不存在 | 保險公司主檔 |
| `insurances` | /insurances | ❌ 表不存在 | 投保明細 |
| `inspections` | /inspections | ❌ 表不存在 | 定檢/保險檢查 |
| `maintenance_templates` | /maintenance-templates | ❌ 表不存在 | 保養套餐 |
| `expenses` | /expenses | ❌ 表不存在 | 支出登錄 |
| `customer_pay`（收款沖銷） | 無頁/流程 | ✅ **已補**（`d74f194`，遷 90,615 筆） | 應收帳款沖銷（舊 G08） |
| `apportion`（沖銷單號/分期） | 無頁/流程 | ❌ 表不存在 | 沖銷單號（舊 G08） |
| `work_order_photos` | /photos | ⚠️ 僅 3 筆 | 工單照片 |
| `technician_worklogs` | 工單頁 | ⚠️ 僅 3 筆 | 工時/技師履歷 |

**影響（2026-10-08 更新）**：4 張表（投保/定檢/保養套餐/支出）已建頁且有資料；
`customer_pay` 已補。剩餘缺口轉為：供應商 CRUD、進貨單回填、技師分工、票據類（全新功能）。

**⚠️ 重要發現：資料來源不是 mdb 二進位**
- `Data35.mdb`（133MB）實際是 **Jet 2 格式**（magic `00010000`，不是 Access 的 `D1CE1AEF`），`access_parser` 解析 MSysObjects 會崩潰。
- **真正可用的是 `master_data.json`（6.5MB）和 `old_data.json`（59MB）**——已匯出的 JSON。
- `master_data.json` 結構：`{"Customer": {...}, ...}`，欄位含 Customer/Supplier 等。
- **編碼陷阱**：該 JSON 是 Big5 被 UTF-8 二次編碼（雙層），直接 `json.load` 崩在 byte 0x9d。需先 `raw.decode("utf-8")`，再對每個字串做 Big5 去雙層。
- 已驗證去雙層可解：`s.encode("utf-8").decode("big5")` 對字串生效。
- `old_data.json`（59MB）可能是交易/明細資料，同樣需去雙層。

**建議 hermes 做法**：
1. 讀 `FIELD-MAPPING.md` 確認每張表欄位對應。
2. 新增 migration SQL 或 `schema.go` 建表函式（對齊 `schema2026-10-02` 風格）。
3. **從 `master_data.json` / `old_data.json` 遷移（不是 mdb）**，先寫個去雙層 + 解析腳本驗證筆數。
4. 若 JSON 缺某些表，至少讓頁面能「手動新增」先可用。

### G2. 報表 sub-page 路由 404（死連結）

- `/reports/maintenance`、`/reports/insurance`、`/reports/customer` 等 → **404**。
- 但 `/reports` 主頁本身完整（保險/自費分欄已涵蓋）。

**判斷**：需確認是否有程式碼或舊介面連結到這些 sub-page。若無，屬**死連結**，可清理或加 404 改善頁。

### G3. 前端 placeholder

- `web/index.html` 是「首頁佔位頁」，文案寫「此為首頁佔位頁」。登入後實際用 `/`（儀表板）。
- 建議：把 index.html 改成一張乾淨的登入頁（品牌 + 登入入口），或刪除重定向到登入。

### G4. 權限判斷缺口

- `reports.go` 已做 `canSeeMoney()` 權限檢查。
- 但 `insur-comp/1`、`/customers/1`、`/insurances/1` 等細節頁對一般員工返回 405/404（設計如此，財務數字僅櫃檯/管理員看）。**確認 UX 是否符合預期**（舊系統是否有此限制）。

---

### G1.5（新）資料來源與解碼方式

**別再解 mdb 二進位**——`Data35.mdb`（133MB）實際是 **Jet 2 格式**（magic `00010000`，非 Access 的 `D1CE1AEF`），`access_parser` 解 MSysObjects 會崩潰。真正常用的資料在兩個 JSON：

| 檔 | 大小 | 內容 | 解碼 | 狀態 | 對應 AutoRepair 表 |
|---|---|---|---|---|---|
| `master_data.json` | 6.5MB | 主檔（Customer/Supplier 等） | UTF-8 去一次，再對字串 Big5 去雙層 | 需寫解碼腳本驗證筆數 | customers/suppliers 等 |
| `old_data.json` | 59MB | 交易明細 | UTF-8 一次即可 | **已驗證可解析** | work_orders/work_order_items (Maintain*) / 進貨 (Material*) |

`old_data.json` 已解析：`Maintain Header/Detail`（估價、維修單）、`Material Header/Detail`（零件進貨）。欄位對應請對照 `FIELD-MAPPING.md`。

**建議 hermes 做法**：
1. 先寫解碼腳本：`master_data.json` 需 `raw.decode("utf-8")` + 字串再 Big5 去雙層；`old_data.json` UTF-8 即可。
2. 驗證各表筆數與欄位對齊 `FIELD-MAPPING.md`。
3. 建表 + 遷移（對齊 `schema2026-10-02` 風格）。
4. 若 JSON 缺某些表，至少讓頁面能「手動新增」先可用。

---

## 四、實作順序建議（給 hermes）

### 優先 P0：補業務資料表 + 遷移

1. **確認資料來源**：別解 mdb（Jet 2 格式崩潰）。用 `master_data.json`（主檔）+ `old_data.json`（交易明細，已驗證可解）。
   - 檢查 `GETDATA.INI` 的 G01~G18 批次檔對應舊表名，確認 JSON 涵蓋哪些表。
2. **建表**：`insurance_companies / insurances / inspections / maintenance_templates / expenses`。
3. **遷移**：寫 migration 腳本把 JSON → SQLite（先對 `old_data.json` 的 Maintain/Material，再 `master_data.json` 主檔），對筆數。
4. **驗證**：登入後 5 個列表頁有資料顯示。

### 優先 P0.5：收款沖銷（流程斷點，老闆每天要記錢）

**驗證**：DB 確認**無** `customer_pay` / `apportion` / `receivable` 表，也**無**對應路由/頁面。
**影響**：接車→施工→完工全通，但最後「錢沒地方記」——無法記收款、無法追蹤應收/已收。

建議設計（對照舊 Car35 G08 應收帳款沖銷）：
- **表**：`customer_pay`（日期、單號 voucher_no、客戶 customer_id、沖銷單號 apportion_no、車號、結帳日期 due_date、折扣 discount、折讓 allowance、稅額 tax、收款日期、現金 cash、信用卡 credit_card、票額 check_amount、票號 check_no、行庫 bank、本期應收 receivable_cur、本期已收 received_cur、備註）。
- **頁**：新增 `/receivables` 頁（列表 + 新增沖銷單 + 客戶應收餘額卡片）。
- **關聯**：沖銷時扣減工單 `completed`→`paid`，或扣減客戶應收餘額。
- **資料**：先手動新增幾筆讓老闆可用，再從 mdb/JSON 遷移舊收款紀錄。

### P1：前端完善

5. 改 `web/index.html` 為正式登入頁。
6. 清理 `/reports/*` 死連結（或加 404 友善頁）。

### P2：對齊舊系統功能

7. 確認「保險投保明細」「定檢」「保養套餐」「支出」舊系統的欄位是否要全量搬。
8. 照片/工時補全（舊系統有照片库）。

---

## 五、快速驗證命令

```powershell
# 探測所有路由
Invoke-WebRequest -Uri "http://127.0.0.1:8090/reports" -UseBasicParsing
Invoke-WebRequest -Uri "http://127.0.0.1:8090/insurances" -UseBasicParsing
Invoke-WebRequest -Uri "http://127.0.0.1:8090/expenses" -UseBasicParsing
```

```python
# DB 各表筆數
import sqlite3
con = sqlite3.connect("data_container_autorepair.db")
[con.execute("select count(*) from " + t) for t in
 ["insurance_companies","insurances","inspections","maintenance_templates","expenses","work_order_photos","technician_worklogs"]]
```

---

## 六、結論

- **功能（UI/路由）**：90% 已完成，僅有報表 sub-page 死連結 + index.html 占位。
- **資料（最痛點）**：5 個業務模組表 + `customer_pay` 收款沖銷表完全缺失，登入後為空白頁。
- **流程斷點（最痛）**：「收款沖銷」沒模組沒表——老闆每天要記錢，這是整個財務流程唯一的斷點。
- **hermes 首要任務**：
  1. **先補收款沖銷**（`customer_pay` 表 + `/receivables` 頁 + 扣減工單狀態），讓老闆能記錢。
  2. 補 5 張業務表 + 從 JSON 遷移資料，讓列表頁有內容。
  3. 供應商/支出/投保補 CRUD，讓只讀頁能用。
