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

- [ ] suppliers.go / subcontracts.go / staff.go / customers.go / insurance.go / receivables.go / template_crud.go / legacy_crud.go / legacy_extras.go（各 1–3 renderPage）

驗收：POST form CSRF hidden input 不遺失；HTMX CSRF header 保留。

## 階段 8：複合 renderPage 頁（最高風險）

- [ ] workorder_new.go（內嵌大型表單 + fallback option，**需人工盯**）
- [ ] dashboard / layout.go renderPage 外殼

驗收：title/nav active/logout/HTMX header/CSS 全保留。

## 階段 9：拆 renderPage 外殼 + 防回歸

- [ ] 移除 layout.go renderPage，全部頁改 Templ Layout
- [ ] withCSRFFormFields / withCSRFHTMXActions 改 Templ component 取代
- [ ] 34 個 renderPage 呼叫點清零
- [ ] XSS regression tests + 重算 Raw/renderPage 殘留

驗收：`grep renderPage(` 回 0；所有 POST/HTMX 過 CSRF 測試；build/test/Templ gen 全綠。

---

## 進度日誌
- 2026-10-24：盤查完成，追蹤表建立。待派第一批（階段 1）。
- 2026-10-25：完成階段 6 Raw 稽核；`.templ` 無 `WriteString`/`strings.Builder` 殘留。發現 `customer.templ:76,114` 的 `tagPillsHTML(c.Tags)` 未 escape，列為 🔴，未擅自修改。
