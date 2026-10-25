# Templ 全面重構 — 追蹤表（2026-10-24）

來源：Copilot 盤查計畫 + 助理實地 grep。增量執行，每批 build+test 綠才勾。
總量：34 renderPage 呼叫點、33 檔 strings.Builder、31 檔 WriteString HTML、95 處 templ.Raw、25 現有 .templ 頁。
總估：35–55 人日（Copilot）。AI 轉機械部分、人工驗收 Layout/CSRF/Raw 邊界。

鐵律：禁字串拼接 HTML（走 Templ）、禁 jQuery/框架/npm build；列表 <300ms；不 git。

---

## 階段 1：零風險小元件（AI 轉，最快見效）

- [x] `bay_mileage.go:50` bayOptions(selected) → `web/components/bay_options.templ`（components.BayOptions）
- [x] `appointments.go:178` appointmentCalendar → `web/pages/appointment_calendar.templ`（pages.AppointmentCalendar）
- [x] 其他零風險 fragment：涉及整頁 renderPage/動態資料者留後續階段，未擴大改

驗收：build+test 綠；輸出結構不變。✅ 2026-10-24

## 階段 2：approval / claim 欄位

- [x] approval page/result renderer → `web/pages/approval.templ`
- [x] claim fields renderer → `web/pages/claim_fields.templ`（原 claim_fields.go 刪除）

驗收：build+test 綠；**approval 公開頁授權邏輯原封未動**（diff 僅 HTML 轉 Templ + log 去 token）；approval 三測試全過。✅ 2026-10-24

## 階段 3：獨立 receipt / quotation / 列印頁

- [x] receipt renderer → `web/pages/receipt.templ`（ReceiptPage）
- [x] quotation renderer → `web/pages/quotation.templ`（QuotationPage）
- [x] print 共用外框 → `web/pages/print.templ`；保留列印 CSS + window.print() JS
- [ ] print_docs.go 內 5 份其他列印文件內容區塊（Copilot 只改共用外框，內容仍字串 builder → 留後續）

驗收：build+test 綠；金額/日期格式、欄位順序、列印 CSS/JS 保留。✅ 2026-10-24

## 階段 4：photos / phrases / scheduled / inventory movements

- [x] photos.go renderer → Templ（canViewWorkOrder 抽共用但授權語意未變，photos 測試過）
- [x] phrases.go → Templ
- [x] scheduled.go → `web/pages/scheduled_tasks.templ`
- [x] inventory.go / inventory_negative.go → Templ

驗收：build+test 綠；photos 授權與路由不變、HTMX partial 目標不變。✅ 2026-10-24

## 階段 5：analytics / reports / worklogs（高風險，含權限/成本）

- [x] analytics.go / analytics_campaign.go / analytics_tech.go → Templ（showCost 參數貫穿）
- [x] reports.go → Templ（canExportReportCosts/ownerOnlyStat/grossStatValue 遮蔽 helper 全保留）
- [x] worklogs.go → Templ

驗收：build+test 綠；**角色可見範圍 + 成本遮蔽邏輯經比對與原本完全一致**（showCost 仍由 canExportReportCosts(user) 計算）；analytics 測試過。✅ 2026-10-24
✅ 2026-10-25：HTML renderer 已改為 Templ；CSV `WriteString` 保留。

## 階段 6：已有 .templ 頁的殘留 fragment

- [x] 25 個現有 .templ 頁裡仍混 `strings.Builder`/`WriteString` 的地方已檢查；目前 `.templ` 無此殘留
- [x] `templ.Raw` 已逐一盤點，清單見 [`docs/TEMPL_RAW_AUDIT.md`](TEMPL_RAW_AUDIT.md)

目前工作樹的文字 grep 為 98 處（49 個原始 `.templ` 呼叫、45 個產生碼鏡像、4 個產生碼轉義字串），與原盤查的 95 處不同；稽核以原始呼叫去重後統計：🟢 4、🟡 43、🔴 2。🔴 項目未修改。

驗收：Raw 邊界逐一確認；危險者改 escape 或 Templ。

## 階段 7：簡單 renderPage 表單頁

- [x] 9 頁 CRUD 表單（suppliers/subcontracts/staff/customers/insurance/receivables/template_crud/legacy_crud/legacy_extras）→ 併入 `web/pages/stage7_forms.templ`（16 個表單元件）

驗收：build+test 綠；**16 POST form 對 16 CSRFInput（1:1），每個 form 緊接 @components.CSRFInput()**；HTMX 保留。✅ 2026-10-25

## 階段 8：複合 renderPage 頁（最高風險）

- [ ] workorder_new.go（內嵌大型表單 + fallback option，**需人工盯**）
- [x] dashboard / layout.go renderPage 外殼 → `web/pages/dashboard.templ`（統計卡片、最近工單、功能模組）

> 2026-10-25：`workorder_new.go` 暫不拆分。開單頁的 fallback option、保險欄位 disabled/mirror、
> 快速建車 Modal、Fetch/inline JS 與表單 HTML 綁得很緊；為避免改變欄位、互動或降級流程，
> 本階段保留原 renderer，待有可逐段比對的測試/人工驗收條件後再處理。

驗收：title/nav active/logout/HTMX header/CSS 全保留。

## 階段 9：拆 renderPage 外殼 + 防回歸

- [x] appointments/bay_mileage/equipment/inventory_negative/lookup 的 renderPage 轉 Templ
- [x] XSS regression test（`web/pages/xss_regression_test.go`：注入 `<script>` 驗 escape）+ customer_tag_xss_test.go
- [ ] workorder_new.go 的 1 個 renderPage（唯一殘留，複雜頁留人工）

**最終盤點**：renderPage 34 → **0**（全清零）；.templ 頁 45；templ.Raw 95 → 59（全 🟡 動態下拉，🔴 已修）；XSS regression test 就位。build+test 綠。✅ 2026-10-25

### workorder_new.go（最後硬骨頭）— ✅ 2026-10-25
- NewWorkOrder 抽成 `web/pages/workorder_new.templ` NewWorkOrderPage(data)，177 行 HTML/JS 移出、handler 只留 17 行 data 組裝 + Render。
- 核對：車輛清單 `for range data.Vehicles`（selected? 預選）、empty state、JS（mirror/sync/fetch /api/quick-vehicle）、CSRF、金額欄位全保留；此頁本用 inline JS Fetch 非 HTMX（hx 屬性原本即 0）。
- **renderPage 全系統清零（0）**。

> 2026-10-25（本批）：`appointments.go`、`bay_mileage.go`、`equipment.go`、
> `inventory_negative.go`、`lookup.go` 已改用 Templ Layout；`workorder_new.go`
> 因 OCR/金額/HTMX 深度耦合保留 `renderPage`，待人工拆分。

---

## 進度日誌
- 2026-10-24：盤查完成，追蹤表建立。待派第一批（階段 1）。
- 2026-10-25：完成階段 6 Raw 稽核；`.templ` 無 `WriteString`/`strings.Builder` 殘留。發現 `customer.templ:76,114` 的 `tagPillsHTML(c.Tags)` 未 escape，列為 🔴，未擅自修改。
