# `templ.Raw` 稽核

日期：2026-10-25

## 範圍與計數

本次以目前工作樹執行 `grep templ.Raw`。結果為 **98 個文字命中**：

- **49 個原始 `.templ` 呼叫**：以下逐處列出並分類。
- **45 個 `_templ.go` 產生碼中的可執行鏡像**：與原始 `.templ` 呼叫重複，未在下表重複計算。
- **4 個 `_templ.go` 產生碼中的轉義字串**：`ap_templ.go:654`、`print_templ.go:51`、`quotation_templ.go:83`、`receipt_templ.go:74`；這些是產生碼尚未重生成時寫出的文字，不是可執行的 `templ.Raw` 呼叫。

因此，計畫中的「95 處」與目前工作樹的文字 grep 結果不一致；本表以原始 `.templ` 呼叫為稽核單位，沒有把產生碼鏡像重複算入。

## 分類清單

| 檔名:行號 | 等級 | 理由 |
|---|---|---|
| `web/components/layout.templ:59` | 🟡 待查 | `nav.Render` 產生動態導覽 HTML，需確認 active/role 與標籤內容的 escape 邊界。 |
| `web/pages/ap.templ:148` | 🟢 安全 | 純固定列印 CSS 結構，沒有動態資料。 |
| `web/pages/appointment.templ:51` | 🟡 待查 | 客戶下拉片段含資料庫姓名/電話，依 helper 的 escape 實作而定。 |
| `web/pages/appointment.templ:57` | 🟡 待查 | 車輛下拉片段含資料庫車牌/車型/客戶資料，依 helper 的 escape 實作而定。 |
| `web/pages/customer.templ:76` | 🔴 危險 | `c.Tags` 來自使用者/資料庫且 `tagPillsHTML` 逐字插入，未呼叫 `htmlEsc`。 |
| `web/pages/customer.templ:114` | 🔴 危險 | `c.Tags` 來自使用者/資料庫且 `tagPillsHTML` 逐字插入，未呼叫 `htmlEsc`。 |
| `web/pages/insurance_list.templ:50` | 🟡 待查 | 車輛選項是動態 HTML 片段，需確認來源欄位完整 escape。 |
| `web/pages/insurance_list.templ:55` | 🟡 待查 | 保險公司選項是動態 HTML 片段，需確認來源欄位完整 escape。 |
| `web/pages/insurance_list.templ:93` | 🟡 待查 | 公司分頁標籤含動態查詢狀態/連結，需確認 helper 的 attribute escape。 |
| `web/pages/inventory.templ:31` | 🟡 待查 | 庫存負庫存提示為動態片段，需確認是否只含固定標籤。 |
| `web/pages/inventory.templ:42` | 🟡 待查 | 類別選項含資料庫代碼/標籤，需確認 value 與文字均 escape。 |
| `web/pages/inventory.templ:60` | 🟡 待查 | 類別分頁標籤含動態查詢狀態/連結，需確認 attribute escape。 |
| `web/pages/inventory_forms.templ:18` | 🟡 待查 | 類別下拉片段含資料庫值，需確認 helper 的 escape。 |
| `web/pages/inventory_forms.templ:21` | 🟡 待查 | 供應商下拉片段含資料庫名稱，需確認 helper 的 escape。 |
| `web/pages/lookup.templ:27` | 🟡 待查 | 代碼分類分頁為動態 HTML，需確認連結與標籤 escape。 |
| `web/pages/print.templ:16` | 🟢 安全 | `data.Style` 由伺服器固定列印樣式提供，沒有使用者輸入。 |
| `web/pages/print.templ:19` | 🟡 待查 | `data.Body` 是預先產生的完整 HTML，需逐一確認內容來源與 escape。 |
| `web/pages/quotation.templ:48` | 🟢 安全 | `data.Style` 由伺服器固定報價單樣式提供，沒有使用者輸入。 |
| `web/pages/purchase_order.templ:108` | 🟡 待查 | 供應商下拉含資料庫名稱，需確認 helper 的 escape。 |
| `web/pages/purchase_order.templ:271` | 🟡 待查 | 零件類別選項含資料庫代碼/標籤，需確認 helper 的 escape。 |
| `web/pages/staff.templ:63` | 🟢 安全 | 角色選項為 handler 內固定常量，沒有動態資料。 |
| `web/pages/staff.templ:68` | 🟡 待查 | 部門選項來自 lookup 資料，需確認 code/label escape。 |
| `web/pages/subcontract.templ:34` | 🟡 待查 | 供應商下拉含資料庫名稱，需確認 helper 的 escape。 |
| `web/pages/receipt.templ:39` | 🟢 安全 | `data.Style` 由伺服器固定收據樣式提供，沒有使用者輸入。 |
| `web/pages/receipt.templ:61` | 🟡 待查 | `LineJSON` 含動態明細，雖在 `pre` 中仍需確認 JSON/HTML 邊界。 |
| `web/pages/vehicle.templ:22` | 🟡 待查 | 車型選項含資料庫 code/label，需確認 helper 的 escape。 |
| `web/pages/vehicle.templ:28` | 🟡 待查 | 車種選項含資料庫 code/label，需確認 helper 的 escape。 |
| `web/pages/vehicle.templ:29` | 🟡 待查 | 燃料選項含資料庫 code/label，需確認 helper 的 escape。 |
| `web/pages/vehicle.templ:30` | 🟡 待查 | 車身式樣選項含資料庫 code/label，需確認 helper 的 escape。 |
| `web/pages/vehicle.templ:93` | 🟡 待查 | 編輯頁車型選項含既有資料庫/使用者輸入值，需確認 escape。 |
| `web/pages/vehicle.templ:99` | 🟡 待查 | 編輯頁車種選項含資料庫 code/label，需確認 escape。 |
| `web/pages/vehicle.templ:100` | 🟡 待查 | 編輯頁燃料選項含資料庫 code/label，需確認 escape。 |
| `web/pages/vehicle.templ:101` | 🟡 待查 | 編輯頁車身式樣選項含資料庫 code/label，需確認 escape。 |
| `web/pages/vehicle_list.templ:41` | 🟡 待查 | 日期 cell 是動態 HTML 片段，需確認只由日期/固定標籤組成。 |
| `web/pages/vehicle_list.templ:42` | 🟡 待查 | 里程 cell 是動態 HTML 片段，需確認只由數字/固定標籤組成。 |
| `web/pages/vehicle_list.templ:70` | 🟡 待查 | 新增車型選項含資料庫/既有值，需確認 escape。 |
| `web/pages/vehicle_list.templ:76` | 🟡 待查 | 新增車種選項含資料庫 code/label，需確認 escape。 |
| `web/pages/vehicle_list.templ:77` | 🟡 待查 | 新增燃料選項含資料庫 code/label，需確認 escape。 |
| `web/pages/vehicle_list.templ:78` | 🟡 待查 | 新增車身式樣選項含資料庫 code/label，需確認 escape。 |
| `web/pages/vehicle_list.templ:165` | 🟡 待查 | 列表日期 cell 是動態 HTML 片段，需確認來源與 escape。 |
| `web/pages/vehicle_list.templ:166` | 🟡 待查 | 列表里程 cell 是動態 HTML 片段，需確認來源與 escape。 |
| `web/pages/vehicle_show.templ:82` | 🟡 待查 | 日期 cell 是動態 HTML 片段，需確認來源與 escape。 |
| `web/pages/vehicle_show.templ:83` | 🟡 待查 | 里程 cell 是動態 HTML 片段，需確認來源與 escape。 |
| `web/pages/vehicle_show.templ:99` | 🟡 待查 | 里程表是含資料庫欄位的完整 HTML，需逐欄確認 escape。 |
| `web/pages/vehicle_show.templ:103` | 🟡 待查 | 工單表是含客戶/工單資料的完整 HTML，需逐欄確認 escape。 |
| `web/pages/vehicle_show.templ:105` | 🟡 待查 | 零件表是含資料庫欄位的完整 HTML，需逐欄確認 escape。 |
| `web/pages/workorder_list.templ:46` | 🟡 待查 | 工單分頁標籤含動態查詢狀態/連結，需確認 attribute escape。 |
| `web/pages/workorder_list.templ:62` | 🟡 待查 | 技師選項含資料庫姓名/帳號/部門，需確認 helper 的 escape。 |
| `web/pages/workorder.templ:76` | 🟡 待查 | SOP 模板名稱來自資料庫，需確認名稱與類型均 escape。 |

## 統計（原始 `.templ` 呼叫）

| 等級 | 數量 |
|---|---:|
| 🟢 安全 | 4 |
| 🟡 待查 | 43 |
| 🔴 危險 | 2 |
| **合計** | **49** |

🔴 危險項目暫不修改，留給人工修正與後續批次。
