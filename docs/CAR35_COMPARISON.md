# CAR35 對照分析

> 對照標的：`C:\Users\fr\35-20231008`（CAR35 車行管理系統，Delphi + Access，
> 檔案日期 1997–2014，Data35.mdb 約 128 MB）

## 分析方法

CAR35 沒有提供文件，因此從 **32 份 Crystal Report (.rpt)** 反推資料模型。
`.rpt` 是二進位，但內含 SQL 語句與欄位參照（如 `Header.Tax`、`Car.Insurance`），
挖出 `表.欄位` 配對後即可還原資料表結構。

```bash
python scripts/analyze_car35.py   # 挖欄位
python scripts/compare_car35.py   # 產生對照表
```

### 報表編碼規則

| 檔名 | 業務範圍 |
|------|----------|
| `Carp1x` | 客戶 / 車輛主檔 |
| `Carp2x` | 廠商 / 往來 |
| `Carp3x` | 估價 / 工單核心 |
| `Carp4x` | 技師 / 工資 |
| `Carp5x` | 收據 / 結帳 |
| `Carp6x` | 庫存 |
| `Carp7x` | 客戶車輛關聯查詢 |
| `Carp8x` | 人事 / 財務 / 廠商 |
| `Carp9x` | 會計總帳 |
| `Label` / `Letter` | 郵寄標籤 / 信件 |

### CAR35 核心資料表

`Car` `Customer` `Material` `Staff` `Firm` `Company` `Model` `OEM`
`Class` `Store` `Branch` `Bank` `Temp` `In` `Out`

## 覆蓋狀況

AutoRepair OS 13 個資料表，覆蓋 CAR35 約 60% 的日常營運需求。

### 已覆蓋

| 業務 | AutoRepair OS | CAR35 對應 |
|------|---------------|-------------|
| 客戶主檔 | `customers` + 搜尋 + LINE 綁定 | Carp14 / 71 |
| 車輛主檔 | `vehicles` + 里程預填 | Carp31 / 74 |
| 工單核心 | `work_orders` + 狀態流轉 + 審批 + SOP | Carp51 |
| 估價明細 | `work_order_items`（工資/零件/外包） | Carp32 |
| 結帳 | `status=paid` + 扣料 + 累計消費 + 內容鎖定 | Carp52 |
| 庫存現況 | `inventory_items` + 低水位警示 | Carp61 / 73 |
| 庫存異動 | 扣料 / 回補 / 調整 | Carp62 / 84 |
| 技師工資佣金 | `technician_worklogs` | Carp41 / 42 |
| 廠商 | `suppliers`（僅基本資料） | Carp72 / 78 |
| 收據列印 | `receipt` + print CSS | Carp52 |

### CAR35 有、我們沒有（依營收影響排序）

#### P0-1 保險理賠 ← 最重要

CAR35 有 `Company`（保險公司）與 `Header.Insurance`，出現在 **Carp32 / 77 / 78 / 91 / 92**
五份報表，包含「保險理賠估價」「保險公司別帳務」「保險 + 稅務拆分」。

保險理賠通常佔車行營收 **5–7 成**。缺少這塊等於放掉主要收入來源。

需要：
- `insurance_companies` 表（公司名、統編、聯絡人、稅號、地址）
- `work_orders` 加 `insurance_company_id` / `claim_no` / `is_insurance`
- 估價單可切「保險理賠」模式（代客向保險公司請款）
- 收據 / 報表區分「保險件」與「自費件」

#### P0-2 付款方式拆分

CAR35 在 Carp13 / 23 / 52 / 92 都拆 `Cash` / `Check` / `Card`，
另外 Carp91 有 `Pay` vs `Real`（實收 vs 應收）對照。

我們目前沒有支付方式欄位，無法分析資金流，也無法對帳。

#### P1-1 應收帳款明細

Carp82 有 `In.Amount` / `In.Due` / `In.Status` — 金額、到期日、收款狀態。
我們只有 `customers.unpaid_amount` 一個數字，無明細、無到期日、無分期。

車行的分期付款很常見。

#### P1-2 廠商往來與採購

Carp21 / 22 / 23 / 83 處理廠商出貨、付款、應付帳齡。
我們的 `suppliers` 只有基本資料，庫存要手動調漲 → **進貨成本無法追蹤**。

#### P2 加值

- **代客保管**（Carp51 `Header.Consign`）— 車主留廠的貴重物品
- **客戶生日關懷**（Carp81 `Birthday` / `Age` / `Month`）— 可做 LINE 行銷
- **車輛年檢提醒**（Carp75 `Temp.License` / `Temp.Order`）

### 明確不做

- 總帳會計、稅務申報（SRS 已排除，CAR35 的 Carp9x 過時且不符台灣現行稅制）
- 分店管理（SRS 為單店）

## AutoRepair OS 超越 CAR35 之處

| 項目 | 說明 |
|------|------|
| LINE 推播 | Messaging API + Flex 估價單 + 客戶綁定；CAR35 無 |
| 離線優先 | IndexedDB + Service Worker + 背景重試；CAR35 無 |
| 排程引擎 | 通知佇列 / 每日備份 / 保養掃描；CAR35 無 |
| 照片上傳 | 車損照存本機；CAR35 無 |
| 權限模型 | 三角色 + 預設拒絕；CAR35 幾乎無權限控管 |
| 備份安全 | `VACUUM INTO` 一致性快照 + 30 天保留；CAR35 需人工備份 |

## 建議開發順序

```
P0  保險公司 + 理賠工單   ← 直接影響營收，先做
P0  付款方式拆分          ← 影響對帳與資金流分析
P1  應收帳款明細          ← 分期付款需求
P1  廠商往來與採購        ← 成本追蹤
P2  代客保管 / 生日關懷 / 年檢提醒
```
