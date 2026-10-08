# AutoRepairOS 改善計畫 — 可執行小階段分解

> 產出：2026-10-08 ｜ 產出者：大C（助理）
> 來源：實測 `autorepairos-improvement-plan.md`（Antigravity CLI）＋逐條對照
>        `internal/handler/*`、`main.go` 路由、`data/autorepair.db` 實測 DB
> 用途：把一份「大範圍功能擴充」計畫，拆成**可執行、可驗證**的小階段
> 範圍：**新建功能為主**（與 `open-order-workflow-plan.md` 的「既有功能修飾」是兩個方向）
> 狀態：✅ 已核實事實／提案，拆成 4 階段 12 任務

---

## 一、現況事實（2026-10-08 實測，非推測）

### 1.1 DB 筆數（實測 `data/autorepair.db`）

| 表 | 實測 | 計畫寫 | 差異 |
|---|---|---|---|
| work_orders | 85,825 | ✅ 85,825 | — |
| work_order_items | 344,745 | ✅ 344,745 | — |
| vehicles | 12,964 | ✅ 12,964 | — |
| customers | 6,512 | ✅ 6,512 | — |
| inventory_items | 10,352 | ✅ 10,352 | — |
| **receivables** | **0** | 90,615 | ⚠️ 主用 DB 收款為空，90,615 在 `autorepair_live.db` |
| users | 102 | — | — |

> **關鍵**：90,615 筆收款在 `autorepair_live.db`（backup/live 檔），主用 DB `receivables=0`。
> 這影響 **Phase 3.3（進貨對帳）** 與 **Phase 4.1（AR Aging）** —— 資料回填前驗不了。

### 1.2 程式碼現況（逐條核實）

| 計畫說 | 實測 | 性質 |
|---|---|---|
| `/suppliers` 唯讀清單 | 只有 `ListSuppliers`，**無** Create/Update/Delete | 提案 |
| `work_orders.sop_completed` 布林開關 | ✅ 有 `sop_completed` 欄，無清單表 | 事實 |
| 工單狀態流 pending→in_progress→waiting_parts→completed→paid | ✅ 全部狀態字存在 `workorders.go` | 事實 |
| 「我的工單」篩選 | ✅ `workorders.go` 有 `mine` 關鍵字 | 事實（前端未做） |
| 語音轉文字 | ❌ 無 speech | 提案 |
| 照片 Markup 圈選 | ❌ 無 markup | 提案 |
| 車身預檢圖 walkaround | ❌ 無此表（`work_order_walkarounds` MISSING） | 提案 |
| 庫存預扣 reserved_quantity | ❌ `inventory.go` 無此欄 | 提案 |
| 儲位 bin_location | ✅ 欄位存在，**UI 未突出** | 部分事實 |
| 收款 AR Aging | ❌ `receivables.go` 無 aging | 提案 |
| 收款調帳 adjust | ❌ 有 `EditReceivable` 但無調帳/部分沖銷 | 部分提案 |
| LINE 電子估價授權 | ✅ 有 `line_webhook.go`（Webhook 已建），無車主確認頁 | 部分提案 |
| 導覽 5 大分群 / 漢堡選單 | ❌ `layout.go` 無 navGroups/NavGroup、無 mobile | 提案 |

---

## 二、可執行小階段（4 階段 12 任務）

> 每個任務標註：**現況**／**改寫範圍**／**權限**／**驗證**。
> 優先序：先做**不依賴收款資料回填**的 UI/UX，再做**依賴資料**的財務模組。

### Phase 1 — 基礎 UX 急救（低風險、高回報）

| # | 任務 | 現況 | 改寫範圍 | 權限 | 驗證 |
|---|---|---|---|---|---|
| **P1.1** | 導覽 5 大分群 + 漢堡選單 RWD | `layout.go` 16 個平面連結，無分群、無手機選單 | `layout.go` 加 `navGroups` 結構化導航 + 手機 `<nav>` 折疊；路由依 role 分群 | all/counter_up/owner_only | 手機 375px 切漢堡選單展開收折 |
| **P1.2** | 開單頁快速建檔 Modal | `workorder_new.go` 查無車牌需跳頁建客戶/車輛 | 加「快速新增客戶/車輛」浮層，Fetch 寫 DB 後自動帶入工單 | counter_up | 輸入不存在的車牌 → 彈 Modal → 建完帶入 |
| **P1.3** | 工單明細無刷新新增 | `AddWorkOrderItem` 每次 POST 整頁刷新 | 偵測 `HX-Request`/`X-Requested-With`，回傳 `<tr>` 局部 append + 小計重算 | counter_up | AJAX 加 5 項不跳頁，小計即時 |

### Phase 2 — 技師施工大看板（中等風險、需前端的 Canvas/語音 API）

| # | 任務 | 現況 | 改寫範圍 | 權限 | 驗證 |
|---|---|---|---|---|---|
| **P2.1** | 技師平板大按鈕模式 | 桌面小按鈕、密集表格 | 新增路由 `/technician/workspace`：大卡片(48×48)、大按鈕、狀態滑動切換 | technician | 技師帳號切平板模式切換狀態 |
| **P2.2** | SOP 檢核清單 | `sop_completed` 布林開關 | 關聯保養套餐/範本，逐項勾選；全部確認才準完工 | counter_up | 勾完清單 → 狀態轉 completed |
| **P2.3** | 車身預檢圖 walkaround | 無此表 | migration `work_order_walkarounds` + Canvas 標記（scratch/dent/crack） | counter_up | 畫布點選傷痕 → 存座標到 DB |
| **P2.4** | 語音轉文字 + 常用片語 | 無 speech | `webkitSpeechRecognition` 轉繁中 + 短語庫貼入 | technician | 油手單指語音入工單備註 |

### Phase 3 — 庫存物料與採購閉環（**依賴收款資料回填**）

| # | 任務 | 現況 | 改寫範圍 | 權限 | 驗證 |
|---|---|---|---|---|---|
| **P3.1** | 零件預扣 reserved_quantity | `inventory_items` 無此欄 | 加欄位 + 工單加料即預扣、刪料回扣、結帳 `paid` 實扣 | counter_up | 多工單並行領料不超賣 |
| **P3.2** | 儲位可見性 | 欄位有、UI 未突出 | 工單領料單/明細表突出 `bin_location` | all | 學徒一眼看到料架 |
| **P3.3** | 供應商 CRUD + 進貨單 | 只有 ListSuppliers | 補 Create/Edit/Delete + `/inventory/inbound` 進貨登錄 | counter_up | 建廠商 → 進貨 → 庫存異動 |

### Phase 4 — 財務深度 + 車主線上授權（**依賴收款資料回填**）

| # | 任務 | 現況 | 改寫範圍 | 權限 | 驗證 |
|---|---|---|---|---|---|
| **P4.1** | AR/Ap Aging 報表 | `receivables` 無 aging | `/reports` 加帳齡分級(0-30/31-60/61-90/90+) | counter_up | 分級顯示逾期 |
| **P4.2** | 收款調帳/部分沖銷 | 有 `EditReceivable` 無調帳 | 加 `POST /receivables/{id}/adjust` 調帳/部分沖銷 | counter_up | 調帳保留軌跡 |
| **P4.3** | LINE 電子估價授權 | Webhook 有、無車主確認頁 | 追加項目生成安全 Token 確認頁，車主「同意/暫不」同步 `approval_status` | customer | 車主手機點「同意施作」→ 工單 approved |

---

## 三、階段依賴與資料瓶頸

```
Phase 1 (UX) ──不依賴收款資料──> 可做
Phase 2 (技師) ──不依賴收款資料──> 可做
Phase 3 (庫存) ──進貨對帳──需要 90,615 收款回填到主用 DB
Phase 4 (財務) ──AR Aging / 調帳──需要收款資料
```

> **資料瓶頸**：主用 DB `receivables=0`，90,615 在 `autorepair_live.db`。
> - Phase 3.3（進貨與 10 萬筆進貨明細對帳）：**需先回填**才能驗
> - Phase 4.1（AR Aging）：**需先回填**才能驗
> - Phase 1/2 **不受影響**，可立即排程

---

## 四、預估工作量與建議排程

| 階段 | 任務數 | 預估工作量 | 建議排程 |
|---|---|---|---|
| Phase 1 | 3 | 小 | 立即，2~3 天 |
| Phase 2 | 4 | 中 | 2~4 週（Canvas/語音前端較重） |
| Phase 3 | 3 | 中 | 收款資料回填後 |
| Phase 4 | 3 | 大 | 收款資料回填後 |

**建議順序**：Phase 1 → Phase 2（先見效），Phase 3/4 等資料回填後再動。

---

## 五、開工前確認事項

1. **前端架構策略**：維持純 Go `strings.Builder` 零構建（現行方針），還是引入 `Templ`+Tailwind CLI？
   *建議維持零構建，避免破壞單一二進位檔 `autos.exe` 隨處可跑。*
2. **技師裝置情境**：現場技師主要用哪種裝置（固定觸控螢幕 / 10吋平板 / 手機）？影響 Phase 2 預設版面尺寸。
3. **資料回填**：是否要先把 `autorepair_live.db` 的 90,615 筆收款回填到主用 DB，才能動 Phase 3.3/4.1？
4. **預扣時間點**：工單加料即預扣，還是 `in_progress` 時才預扣？

---

## 六、驗證命令

```python
# 開工前現況快照
import sqlite3
c = sqlite3.connect('data/autorepair.db')
print("工單:", c.execute("SELECT COUNT(*) FROM work_orders").fetchone()[0])
print("工單項目:", c.execute("SELECT COUNT(*) FROM work_order_items").fetchone()[0])
print("車輛:", c.execute("SELECT COUNT(*) FROM vehicles").fetchone()[0])
print("客戶:", c.execute("SELECT COUNT(*) FROM customers").fetchone()[0])
print("庫存:", c.execute("SELECT COUNT(*) FROM inventory_items").fetchone()[0])
print("收款(主用DB):", c.execute("SELECT COUNT(*) FROM receivables").fetchone()[0])
print("工單狀態種數:", c.execute("SELECT COUNT(DISTINCT status) FROM work_orders").fetchone()[0])
print("技師工時筆數:", c.execute("SELECT COUNT(*) FROM technician_worklogs").fetchone()[0])
print("員工筆數:", c.execute("SELECT COUNT(*) FROM users WHERE role='technician'").fetchone()[0])
```

> 服務現況：`http://localhost:8090`（Docker）｜帳號 `owner` / `Owner001!`
> 關鍵現況：**`receivables` 主用 DB 為空（90,615 在 live DB）；`/suppliers` 唯讀；`layout.go` 無分群導覽。**
