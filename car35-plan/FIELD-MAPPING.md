# Car35 → AutoRepair 欄位對照表（整理分類）

> 來源：Car35 應用程式 UI 截圖（使用者：陳素珠 A級）+ mdb 表實測筆數
> 狀態：整理分類中，尚未寫遷移腳本

---

## 一、5 張主資料表

### 1. 客戶資料處理 `Customer` ↔ `customers`
**主鍵：`id`**（04001-213）

| mdb 欄位 | AutoRepair 欄位 | 備註 |
|---|---|---|
| id | id | 主鍵 |
| name | name | 簡稱 |
| name_full | name_full | 全銜 ❗缺口 |
| phone | phone | 電話 |
| invoice_title | invoice_title | 發票抬頭 ❗缺口 |
| contact_person | contact_person | 連絡人 ❗缺口 |
| pager_no | pager_no | 呼叫器 ❗缺口 |
| cell_phone | cell_phone | 大哥大 ❗缺口 |
| code_no | code_no | 字號 ❗缺口 |
| address | address | 客戶地址 |
| invoice_address | invoice_address | 發票地址 ❗缺口 |
| comm_address | comm_address | 通訊地址 ❗缺口 |
| ar_account | ar_account | 應收科目 ❗缺口 |
| due_date | due_date | 結帳日 ❗缺口 |
| note_account | note_account | 應收票據科目 ❗缺口 |
| birthday | birthday | 出生日期 ❗缺口 |
| notes | notes | 備註 ❗缺口 |

**AutoRepair 現況欄位**：id/name/phone/line_user_id/address/notification_preference/total_spent/unpaid_amount/created_at
→ 需新增：name_full, invoice_title, contact_person, pager_no, cell_phone, code_no, invoice_address, comm_address, ar_account, due_date, note_account, birthday, notes

---

### 2. 車籍資料處理 `Car` ↔ `vehicles`
**主鍵：`plate_number`**（5457-F8）

| mdb 欄位 | AutoRepair 欄位 | 備註 |
|---|---|---|
| plate_number | plate_number | 車號 |
| brand | brand | 廠牌（dropdown AA.國瑞）❗缺口 |
| model | model | 型式 ❗缺口 |
| color | color | 顏色（dropdown 10.灰）❗缺口 |
| class | class | 車種（dropdown 01.自小客）❗缺口 |
| fuel | fuel | 燃料（dropdown）❗缺口 |
| cylinders | cylinders | 汽缸（dropdown）❗缺口 |
| mileage | mileage | 里程 ❗缺口 |
| manufacture_year | manufacture_year | 年份（dropdown）❗缺口 |
| license_date | license_date | 發照日期 ❗缺口 |
| expiry_date | expiry_date | 有效日期 ❗缺口 |
| vin | vin | 車身號碼 ❗缺口 |
| owner_no | owner_no | 車主編號 ❗缺口 |
| unit | unit | 單位（dropdown）❗缺口 |
| driver | driver | 駕駛人 ❗缺口 |
| driver_phone | driver_phone | 駕駛電話 ❗缺口 |
| inspection_due_date | inspection_due_date | 指定驗車日 ✓ |
| inspection_actual | inspection_actual | 實際驗車日期 ❗缺口 |
| notes | notes | 備註 ❗缺口 |

**底部清單**：
- 車輛投保明細清單 `Car Insurance List`（2,667）→ 對應 `vehicle_insurances`
- 車輛驗收明細清單 `Car Check List`（21,176）→ 對應 `vehicle_inspections`

**AutoRepair 現況欄位**：id/customer_id/plate_number/brand_model(合併)/manufacture_year/vin/engine_number/inspection_due_date/next_maintenance_date/_mileage/_interval*/last_maintenance_date/_mileage/created_at
→ brand_model 已合併 brand+model；需新增 color/class/fuel/cylinders/mileage/license_date/expiry_date/owner_no/unit/driver/driver_phone/inspection_actual/notes 等

---

### 3. 廠商資料處理 `Firm` ↔ `suppliers`
**主鍵：`id`**（07-12）

| mdb 欄位 | AutoRepair 欄位 | 備註 |
|---|---|---|
| id | id | 主鍵 |
| name | name | 名稱 |
| tax_id | tax_id | 統一編號 ❗缺口 |
| phone | phone | 電話 |
| fax | fax | 傳真 ❗缺口 |
| address | address | 公司地址 |
| factory_address | factory_address | 工廠地址 ❗缺口 |
| invoice_address | invoice_address | 發票地址 ❗缺口 |
| ap_account | ap_account | 應付科目 ❗缺口 |
| note_account | note_account | 應付票據科目 ❗缺口 |
| notes | notes | 備註 ❗缺口 |
| name_short | name_short | 簡稱 ❗缺口 |
| contact_person | contact_person | 連絡人 ❗缺口 |
| vehicle_no | vehicle_no | 車輛關聯（07-12→車輛）❗缺口 |
| color | color | 顏色（關聯車輛）❗缺口 |
| engine_no | engine_no | 引擎號碼（關聯車輛）❗缺口 |

**AutoRepair 現況欄位**：id/name/phone/contact_person/address/notes/created_at/vendor_type
→ 需新增：tax_id/fax/factory_address/invoice_address/ap_account/note_account/notes/name_short/vehicle_no/color/engine_no

---

### 4. 零件資料處理 `Material` ↔ `inventory_items`
**主鍵：`part_number`**（BE071）

| mdb 欄位 | AutoRepair 欄位 | 備註 |
|---|---|---|
| part_number | part_number | 編號（主鍵）|
| name | name | 名稱 |
| spec | spec | 規格 ❗缺口 |
| category | category | 零件類別（dropdown B.電機音響）|
| class | class | 類別（dropdown 0.零件）❗缺口 |
| barcode | barcode | 條碼 ❗缺口 |
| bin_location | bin_location | 庫位 |
| cost_price | cost_price | 最後進價 ❗缺口 |
| selling_price | selling_price | 標準售價 ✓ |
| stock_quantity | stock_quantity | 目前存量 ✓ |
| min_safety_stock | min_safety_stock | 安全存量 ✓ |
| last_in_date | last_in_date | 最後入庫日期 ❗缺口 |
| last_out_date | last_out_date | 最後出庫日期 ❗缺口 |
| stock_count_date | stock_count_date | 盤點日期 ❗缺口 |
| supplier | supplier | 來往廠商 ❗缺口 |
| notes | notes | 備註 ✓ |

**底部明細視窗**（多對多：零件 ↔ 規格/車種/來往廠商）：
- spec（規格）、vehicle_class（車種）、supplier（來往廠商）

**AutoRepair 現況欄位**：id/supplier_id/part_number/barcode/name/bin_location/cost_price/selling_price/stock_quantity/min_safety_stock/stock_corrected_at/stock_corrected_from/category/item_kind
→ 需新增：spec/class/barcode(name已存)/last_in_date/last_out_date/stock_count_date/supplier

---

### 5. 員工資料處理 `Staff` ↔ `users`
**主鍵：`id`**（G011）

| mdb 欄位 | AutoRepair 欄位 | 備註 |
|---|---|---|
| id | id | 主鍵 |
| name | name | 姓名 |
| tax_id | tax_id | 字號 ❗缺口 |
| birthday | birthday | 生日（73/08/20）❗缺口 |
| address | address | 地址 ❗缺口 |
| phone | phone | 電話（04-3303786）❗缺口 |
| pager_no | pager_no | 呼叫器 ❗缺口 |
| unit | unit | 部門（dropdown A.引擎）❗缺口 |
| bank_account | bank_account | 銀行帳號（0220-765-798892）✓ |
| hire_date | hire_date | 到職日期（88/05/26）❗缺口 |
| retire_date | retire_date | 離職日期（89/02/11）❗缺口 |
| notes | notes | 備註 ❗缺口 |

**AutoRepair 現況欄位**：id/username/password/role/name/created_at/staff_branch/staff_kind/national_id/birthday/insurance_start/insurance_end/bank_account/dependent_of
→ 需新增：tax_id/address/phone/pager_no/unit/dept/hire_date/retire_date/notes
→ staff_branch 對應「部門」；staff_kind 對應員工/廠商/眷屬分類

---

## 二、保險公司（獨立表，勿從 Staff 推論）

### `Insurance Company`（15 筆）
| mdb 欄位 | AutoRepair 欄位 |
|---|---|
| code | code | 005 |
| name | name | 華山產物保險(股)公司 |
| tax_id | tax_id | 51084024 |
| fax | fax | 04-3285620 |
| phone | phone | 04-23215689 |
| phone2 | phone2 | 04-23285621 |
| contact_person | contact_person | |
| address | address | 台中市台中港路一段199號4F |
| invoice_address | invoice_address | 台中市台中港路一段199號4F |
| name_short | name_short | 華山產物 |
| notes | notes | |

→ AutoRepair 已有 `insurance_companies` 表 ✓

---

## 三、銀行資料 `Bank`（160 筆，lookup）

| mdb 欄位 | AutoRepair 欄位 |
|---|---|
| id | id | 000 |
| name | name | 中央銀行國庫局 |
| name_short | name_short | 中央銀行 |
| bank_account | bank_account | |
| phone | phone | 電話號碼 |
| contact_person | contact_person | 聯絡人員 |
| open_date | open_date | 開帳日期 |
| notes | notes | 備註 |

---

## 四、Lookup 表（筆數少、適合 --lookups 匯出）

| 表 | mdb 欄位 | AutoRepair |
|---|---|---|
| Car Class（車種） | 01.自小客 | 對應 class |
| Gas Class（燃料） | — | 對應 fuel |
| Store（倉別） | — | — |
| Branch（部門/支部） | A.引擎 | staff_branch |
| Class | — | — |
| Unit Class（單位） | — | unit |
| Unit Class（56 筆）| — | unit |
| Color Class（顏色） | 10.灰 | color |

---

## 五、Dropdown 字典來源（lookup 值）

「其他資料設定」列出 dropdown 選項的資料來源檔，這是前面車籍/員工 dropdown 的選項值來源（如「10.灰」「AA.國瑞」「A.引擎」）：

| 資料檔 | 對應欄位 | 備註 |
|---|---|---|
| 零件類別 | Material.category | |
| 汽車廠牌 | Car.brand | |
| 廠牌車型 | Car.model | |
| 部門 | Staff.unit / staff_branch | |
| 保險類別 | Insurance.category | |
| 零件庫位 | Material.bin_location | |
| 車種類別 | Car.class | |
| 顏色類別 | Car.color | |
| 燃料類別 | Car.fuel | |
| 單位設定 | Staff.unit / unit_class | |
| 字庫 | 字型檔（T001.TTF 等）| |

> 這些 dropdown 的選項值存在 mdb 表裡（或字典檔），是 lookup 值，適合用 --lookups 匯出。

---

## 六、待辦清單

- [ ] 5 張主表欄位對應寫進 `migrate_access.py`
- [ ] 研究 INI 設定檔（MAIN.INI / GETDATA.INI）
- [ ] 確認收銀機明細（CitS/Cit）是否遷移
- [ ] 確認日常作業交易表對應（Maintain*/Material*/Customer Pay/Spend List）
