# Hermes 實作清單（AutoRepairOS 補齊）

> 產出：2026-10-07 ｜ 原產出者：大C（助理）
> **更新：2026-10-07 20:15 ｜ 更新者：Hermes**
> 目的：給 hermes 一份可執行的建表 + 前端頁清單。
> 狀態：⚠️ **第一、二、六節有事實錯誤，已更正**（見下）

---

## ⚠️ 更正：本文件查的是舊檔，不是正式 DB

原文件第一節查的是 `data_container_autorepair.db`（188 KB，2026-10-03 舊檔），
**不是** `data/autorepair.db`（42 MB）。兩者差距極大：

| 表 | 原文件寫的 | 實際（`data/autorepair.db`） |
|---|---|---|
| `insurances` | ❌ 不存在 | 表名是 **`vehicle_insurances`** — 2,438 筆 |
| `inspections` | ❌ 不存在 | 表名是 **`vehicle_inspections`** — 19,918 筆 |
| `maintenance_templates` | ❌ 不存在 | **存在，25 筆**（+ items 43 筆） |
| `expenses` | ❌ 不存在 | **存在，3,390 筆** |
| `technician_worklogs` | ⚠️ 只有 3 筆 | **32 筆** |
| `work_orders` | （舊檔 17 筆） | **85,825 筆** |

**結論更正**：原文件說「5 張業務表完全缺失 + 登入後為空白頁」，
實際**只有 `customer_pay` 真的缺**；其餘 4 張是**表名不同**（`vehicle_` 前綴）
＋查錯 DB。四個頁面都已在導覽上、有資料。

---

## 一、現行 DB 表（實際 22 張，勿動）

| 表名 | 筆數 | 用途 |
|---|---:|---|
| `customers` | 6,512 | 客戶（CRUD 正常） |
| `vehicles` | 12,964 | 車輛（CRUD 正常） |
| `work_orders` | 85,825 | 工單（CRUD 正常） |
| `work_order_items` | 344,745 | 工單明細 |
| `work_order_photos` | 25 | 工單照片 |
| `inventory_items` | 10,352 | 庫存主檔（CRUD 正常） |
| `inventory_movements` | **0** | 庫存異動（**表在但無資料**） |
| `suppliers` | 691 | 供應商（只讀） |
| `insurance_companies` | 16 | 保險公司（CRUD 正常） |
| `vehicle_insurances` | 2,438 | 車輛投保紀錄 ✅ 已有頁 |
| `vehicle_inspections` | 19,918 | 車輛定檢排程 ✅ 已有頁 |
| `maintenance_templates` | 25 | 保養套餐 ✅ 已有頁 |
| `maintenance_template_items` | 43 | 保養套餐明細 |
| `expenses` | 3,390 | 零用金／雜支 ✅ 已有頁 |
| `technician_worklogs` | 32 | 技師工時／佣金 |
| `scheduled_tasks` | 4 | 排程（功能有） |
| `notifications` | 0 | 通知佇列 |
| `users` | 102 | 使用者（含 40 位健保眷屬） |
| `sessions` / `backup_records` / `schema_migrations` | — | 基礎設施 |

**真正缺的只有 `customer_pay`。**

---

## 二、資料來源（更正）

原文件說 `Data35.mdb` 是 Jet 2 格式、`access_parser` 解 MSysObjects 會崩。
**實測可行**（Hermes 2026-10-07）：`access-parser 0.0.6` 可完整解析 128 MB 的
`Data35.mdb`，**33 張表全部讀出**（含 `Maintain Header` 95,440 筆、
`Maintain Detail` 390,425 筆）。`Error while parsing MSysObjects table chunk`
會刷 stderr 但**不影響結果**，用 `contextlib.redirect_stderr` 包起來即可。

```python
import contextlib, io
from access_parser import AccessParser
db = AccessParser(r"C:\Users\fr\35-20231008\Data35.mdb")
with contextlib.redirect_stderr(io.StringIO()):
    t = db.parse_table('Maintain Header')   # {欄位名: [值, ...]}
print(list(t.keys()))
```

編碼：`access-parser` 以 latin-1 讀出 Big5，需還原 `s.encode('latin-1').decode('big5')`。
（`master_data.json` 的雙層編碼陷阱仍然有效，但**不必再靠 JSON**，
直接讀 mdb 更完整。）

可重跑：`python scripts/analyze_car35_data.py` → `docs/car35_data_gap.json`。

---

## 三、要建的表（依優先序）

### P0.5 — 收款沖銷（`customer_pay`）— ✅ **已完成**（commit `d74f194`，2026-10-08）

資料來源 `Customer Pay` 90,762 筆（可對到 87,625 張工單）：

| 欄位 | 舊系統 | 實測非空筆數 |
|---|---|---:|
| 單號 | `No` | 90,762 |
| 維修單號 | `Maintain No` | 90,759 |
| 客戶 / 車號 | `Customer No` / `Car No` | 90,756 / 90,757 |
| 結帳日期 | `Pay Date` | 90,757 |
| 折扣 / 折讓 / 稅額 | `Discount Price` / `Tax Price` | 16,155 / 8,076 |
| **現金** | `Cash Total` | **83,094** |
| **信用卡 + 卡號** | `Card Total` / `Card No` | **7,126 / 265** |
| **票額 / 票號 / 到期日** | `Check Total` / `Check No` / `Check Date` | **741 / 23 / 19** |
| 行庫 | `Bank Class` | 19 |
| 本期應收 / 已收 | `Real Total` / `Pay Total` | 90,742 / 90,736 |

- **頁**：`/receivables`（列表 + 新增沖銷單 + 客戶應收餘額卡片）。
- **關聯**：沖銷時扣減工單狀態 `completed→paid`，或扣減客戶應收餘額。
- **資料**：已從 mdb 遷移 90,615 筆。
- **遷移工具**：`tools/migrate_access.py` 已支援 `--receivables` 選項。

---

### P1 — 歷史明細類 — ✅ **已完成**（commit `0d0b1eb`，2026-10-07 19:43）

| 表名（實際） | 頁 | 筆數 | 狀態 |
|---|---|---:|---|
| `vehicle_insurances` | `/insurances` | 2,438 | ✅ 已建頁（導覽「投保」） |
| `vehicle_inspections` | `/inspections` | 19,918 | ✅ 已建頁（導覽「定檢」） |
| `maintenance_templates` | `/maintenance-templates` | 25 | ✅ 已建頁（導覽「保養套餐」） |
| `expenses` | `/expenses` | 3,390 | ✅ 已建頁（導覽「支出」） |

> 原文件列的 `insurances` / `inspections` 表名有誤，實際是 `vehicle_` 前綴。
> 四頁都在 `http://localhost:8090` 上線並有資料。

---

### P2 — 供應商補 CRUD（只讀 → 可用）— ⬜ **尚未實作**

- **頁**：`/suppliers`（已存在頁，只讀；695 家：材料商 563、協力廠商 132）。
- **做法**：照 `insurance_companies` 的 CRUD handler 改寫。
- **補充**：舊系統 `Firm` 表的 `Tax No` 150 筆、`Fax` 235 筆、`Sales Name` 209 筆
  可一併補進欄位。

---

### P3 — 進貨／採購單 — ⬜ **尚未實作**

- 舊系統 `Material Header` 103,916 筆、`Material Detail` 166,315 筆。
- **查證結果：這是真的進貨單**（不是領料單）——
  `Firm No` 101,373/103,365 能對到廠商主檔、
  明細單價對應零件的 `In Price`（進價）而非 `Out Price`（售價）。
- 目前 `inventory_movements` 是 **0 筆**，可直接用這 10.4 萬張單回填。

---

## 四、前端頁

| 項目 | 狀態 |
|---|---|
| `/receivables` 收款沖銷 | ✅ 已做（`d74f194`；本地 dev DB 尚未回填需重新部署） |
| `/reports/*` 死連結清理 | ⬜ 未做 |
| `web/index.html` 改正式登入頁 | ⬜ 未做 |
| `/insurances`、`/inspections`、`/maintenance-templates`、`/expenses` | ✅ 已做 |
| 工單列表技師／零件篩選 | ✅ 已做（commit `14f960d`） |
| 供應商 CRUD（P2） | ⬜ 未做 |

---

## 五、實作順序建議（更新）

1. ~~建 4 張歷史明細表 + 頁面~~ ✅ 完成（`0d0b1eb`）
2. **`customer_pay` 表 + `/receivables` 頁**（P0.5，最痛）
3. 供應商 CRUD（P2）
4. 進貨單（P3，10.4 萬張單可回填 `inventory_movements`）
5. 報表死連結

---

## 六、驗證命令（更正）

原文件用 `data_container_autorepair.db`，**請改用正式 DB**：

```bash
cd C:/Users/fr/AutoRepairOS
python -c "
import sqlite3
c = sqlite3.connect('data/autorepair.db')
for t in ['customers','vehicles','work_orders','vehicle_insurances',
          'vehicle_inspections','maintenance_templates','expenses']:
    print(t, c.execute('select count(*) from '+t).fetchone()[0])
"
```

服務現況：`http://localhost:8090`（Docker，`docker compose -p autorepairos`）。
帳號 `owner` / `Owner001!`。

---

## 七、注意（環境限制，更正）

- 原文件說「PowerShell 5.1 無法用 .NET 類型」—— 本機 shell 是 **git-bash**，
  直接寫 `.py` 檔執行即可，不需要 PowerShell。
- `access-parser` 讀 mdb 需要 1–3 分鐘（39 萬筆的 `Maintain Detail` 最慢），
  請快取 `parse_table` 結果，不要每問一個欄位就重讀。
- Docker 服務啟動 1 秒內就會寫入 `data/backups/*.db`，
  要對正式 DB 做實驗請先複製一份。
- 服務常駐在 8090 會鎖住 `data/autorepair.db`，做遷移前先 `docker compose -p autorepairos stop`。

---

## 八、簽名

| 項目 | 產出者 | 時間 | commit |
|---|---|---|---|
| 本文件事實更正（第一、二、六、七節） | **Hermes** | 2026-10-07 20:15 | 見下方 |
| 4 張歷史明細表 + 4 個頁面 + 導覽 | 大C（Hermes 整理提交） | 2026-10-07 19:43 | `0d0b1eb` |
| 收款沖銷模組 + customer_pay 遷移（90,615 筆） | Hermes | 2026-10-08 | `d74f194` |
| 車主對應姓名 fallback（車輛 86.5%→99.7%） | Hermes | 2026-10-08 | `0f9872c` |
| 供應商 CRUD（P2） | — | ⬜ 待辦 | — |
| 工單列表技師／零件篩選 + 零件欄 | **Hermes** | 2026-10-07 20:04 | `14f960d` |
| 首頁數字卡可點 / 技師下拉過濾行政 / 按鈕改名 | **Hermes** | 2026-10-07 16:09 | `66e929f` |
| CAR35 資料盤點（逐表逐欄非空筆數） | **Hermes** | 2026-10-07 14:29 | `a87e60a` 等 5 筆 |
| UX 順手度檢視（實跑量測） | **Hermes** | 2026-10-07 15:09 | `b267ced` |
| `customer_pay` / 供應商 CRUD / 進貨單 | — | ⬜ 待辦 | — |

> 更正依據：
> - `data/autorepair.db` 實查（22 張表、各表筆數）
> - `Data35.mdb` 逐表解析（`scripts/analyze_car35_data.py`，33 張表全讀出）
> - `http://localhost:8090` 實跑驗證（owner 登入、各頁 HTTP 200）
