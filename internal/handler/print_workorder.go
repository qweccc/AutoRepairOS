// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// taxRate 營業稅率。台灣一般稅率 5%。
//
// 這個值顯示在單據上，但不參與金額計算——見 computeTaxBreakdown 的說明。
const taxRate = 0.05

// taxBreakdown 稅額拆分結果。
type taxBreakdown struct {
	Taxable float64 // 應稅銷售額（未稅）
	Tax     float64 // 稅額
	Total   float64 // 含稅總額
}

// computeTaxBreakdown 由含稅總額反推應稅額與稅額。
//
// 為什麼是「內含」而非「外加」：台灣汽車維修業的報價慣例是含稅價，
// 客戶看到的就是最後要付的數字。若改成外加，收據上的總額會與工單
// total_amount 不一致，也會與結帳時累計到 customers.total_spent 的金額對不上。
//
// 內含公式：應稅 = total / (1 + rate)，稅額 = total - 應稅。
// 這與統一發票的「銷售額 = 含稅價 ÷ 1.05」一致。
func computeTaxBreakdown(total float64) taxBreakdown {
	if total <= 0 {
		return taxBreakdown{}
	}
	taxable := total / (1 + taxRate)
	return taxBreakdown{
		Taxable: taxable,
		Tax:     total - taxable,
		Total:   total,
	}
}

// payMethodRows 收款方式的拆分列。
//
// 一筆收款可能同時使用多種方式，因此逐項列出而非互斥分類。
func payMethodRows(cash, card, check, transfer float64) []struct {
	Label string
	Value float64
	Note  string
} {
	var out []struct {
		Label string
		Value float64
		Note  string
	}
	if cash > 0 {
		out = append(out, struct {
			Label string
			Value float64
			Note  string
		}{"現金", cash, ""})
	}
	if card > 0 {
		out = append(out, struct {
			Label string
			Value float64
			Note  string
		}{"信用卡", card, ""})
	}
	if check > 0 {
		out = append(out, struct {
			Label string
			Value float64
			Note  string
		}{"票據", check, ""})
	}
	if transfer > 0 {
		out = append(out, struct {
			Label string
			Value float64
			Note  string
		}{"轉帳", transfer, ""})
	}
	return out
}

// renderPrintPage 統一的列印頁面外框（含 no-print 工具列與自動列印）。
func renderPrintPage(w http.ResponseWriter, title string, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.PrintPage(pages.PrintPageData{Title: title, Style: printStyle, Body: body}).Render(context.Background(), w)
}

// printToolbar 文件下方的操作列（不列印）。
func printToolbar(links ...string) string {
	var b strings.Builder
	b.WriteString(`<div class="no-print">`)
	b.WriteString(`<button class="btn btn-p" onclick="window.print()">🖨 列印</button>`)
	for _, l := range links {
		b.WriteString(" " + l)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// printHead 文件表頭：店名 + 文件名稱 + 右側關鍵資訊。
func printHead(docTitle string, right []string) string {
	var b strings.Builder
	b.WriteString(`<div class="rpt-head"><div><h1>🔧 ` + appName() + `</h1><p>` + htmlEsc(docTitle) + `</p></div>`)
	if len(right) > 0 {
		b.WriteString(`<div class="rpt-head-meta">`)
		for _, r := range right {
			b.WriteString(`<div>` + r + `</div>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// metaPair 產生表頭的一組「標籤 + 值」。
func metaPair(label, value string) string {
	return `<span>` + htmlEsc(label) + `</span><b>` + htmlEsc(value) + `</b>`
}

// itemTypeLabelShort 項目類型簡稱（供完工單的零件清單用）。
func itemTypeLabelShort(t string) string {
	switch t {
	case "labor":
		return "工資"
	case "part":
		return "零件"
	case "subcontract":
		return "外包"
	}
	return t
}

// ============ FR-PRINT-02 完工單 ============

// PrintWorkOrderCompletion 工單完工證明。
//
// 與收據的差別：完工單是「做了什麼」的證明（交車時給客戶留存），
// 不含金額；收據是「收了多少」的憑證。分開是慣例，也避免完工單
// 被當成付款證明使用。
func PrintWorkOrderCompletion(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+workOrderCols+workOrderFrom+` WHERE wo.id = ?`, id)
	wo, err := scanWorkOrder(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if user.Role == "technician" && wo.TechnicianID != user.ID {
		http.Error(w, "無權限檢視此工單", http.StatusForbidden)
		return
	}
	if user.Role == "reception" {
		http.Error(w, "無權限", http.StatusForbidden)
		return
	}

	// IDOR 防護：技師只能查看指派給自己的工單
	if user.Role == "technician" && wo.TechnicianID != user.ID {
		http.Error(w, "無權查看此工單", http.StatusForbidden)
		return
	}

	items, _ := loadWorkOrderItems(id)

	// 依類別分組：工資 / 零件 / 外包。
	type group struct {
		label string
		items []model.WorkOrderItem
	}
	groups := []group{{label: "維修工資"}, {label: "更換零件"}, {label: "外包加工"}}
	gidx := map[string]int{"labor": 0, "part": 1, "subcontract": 2}
	for _, it := range items {
		if i, ok := gidx[it.ItemType]; ok {
			groups[i].items = append(groups[i].items, it)
		}
	}

	var b strings.Builder
	for _, g := range groups {
		if len(g.items) == 0 {
			continue
		}
		b.WriteString(`<div class="c-group"><div class="c-group-head">` + g.label + `</div><ul>`)
		for _, it := range g.items {
			qty := ""
			if it.Quantity > 1 {
				qty = ` × ` + strconv.Itoa(it.Quantity)
			}
			b.WriteString(`<li>` + htmlEsc(it.Description) + qty + `</li>`)
		}
		b.WriteString(`</ul></div>`)
	}
	content := b.String()
	if content == "" {
		content = `<div class="empty">此工單沒有維修項目</div>`
	}

	// 完工日期：狀態為已結帳/待結帳時取 updated_at，否則標示施工中。
	completedText := wo.UpdatedAt.Format("2006-01-02")
	if wo.Status == "pending" || wo.Status == "in_progress" {
		completedText = "（施工中）"
	}

	body := `
  <div class="sheet">
    ` + printHead("維修完工證明", []string{
		metaPair("工單編號", "#"+strconv.FormatInt(wo.ID, 10)),
		metaPair("完工日期", completedText),
	}) + `

    <div class="c-parties">
      <div><span class="lbl">車主</span><b>` + htmlEsc(wo.CustomerName) + `</b>
        <span class="lbl">聯絡電話</span>` + htmlEsc(orDash(wo.CustomerPhone)) + `</div>
      <div><span class="lbl">車輛</span><b>` + htmlEsc(wo.PlateNumber) + `</b>
        <span class="lbl">車型</span>` + htmlEsc(orDash(wo.BrandModel)) + `</div>
      <div><span class="lbl">進廠里程</span><b>` + strconv.Itoa(wo.CurrentMileage) + ` km</b>
        <span class="lbl">主責技師</span>` + htmlEsc(orDash(wo.TechnicianName)) + `</div>
    </div>

    <div class="c-sec">
      <h2>客戶反映</h2>
      <p>` + htmlEsc(orDash(wo.CustomerComplaint)) + `</p>
    </div>

    <div class="c-sec">
      <h2>施工項目與更換零件</h2>` + content + `
    </div>

    <div class="c-sign">
      <div><span>客戶簽收</span><div class="line"></div></div>
      <div><span>交車人員</span><div class="line"></div><span class="name">` + htmlEsc(user.Username) + `</span></div>
      <div><span>完工日期</span><div class="line"></div><span class="name">` + time.Now().Format("2006-01-02") + `</span></div>
    </div>

    <div class="rpt-foot">
      <p class="tiny">本證明列印時間：` + time.Now().Format("2006-01-02 15:04") + `</p>
    </div>
  </div>
  ` + printToolbar(`<a class="btn btn-s" href="/work-orders/`+strconv.FormatInt(wo.ID, 10)+`">返回工單</a>`)

	renderPrintPage(w, "完工證明 #"+strconv.FormatInt(wo.ID, 10), body)
}

// printStyle 列印文件的共用 CSS。
//
// 與 receipt.go 的 receiptStyle 刻意分開：那份已有自己的版面（含 LINE 區塊），
// 混用會互相干擾。兩者都以「瀏覽器列印」為目標，@media print 只做三件事：
// 白底、隱藏工具列、移除圓角陰影。
const printStyle = `
  * { box-sizing: border-box; }
  body { margin: 0; padding: 24px; background: #0f172a;
         font-family: "Segoe UI", "Microsoft JhengHei", sans-serif; }
  .sheet { max-width: 760px; margin: 0 auto; background: #fff; color: #0f172a;
           padding: 34px 38px; border-radius: 10px; }

  .rpt-head { display: flex; justify-content: space-between; align-items: flex-start;
              border-bottom: 3px solid #0f172a; padding-bottom: 14px; }
  .rpt-head h1 { margin: 0; font-size: 25px; }
  .rpt-head p { margin: 4px 0 0; color: #475569; font-size: 13.5px; }
  .rpt-head-meta { text-align: right; font-size: 13px; }
  .rpt-head-meta span { display: block; color: #64748b; font-size: 11px; }
  .rpt-head-meta b { font-size: 13.5px; }

  .c-parties { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
               gap: 14px; margin: 16px 0 12px; }
  .c-parties > div { border: 1px solid #cbd5e1; border-radius: 7px; padding: 9px 12px; }
  .lbl { display: block; color: #64748b; font-size: 11.5px; }
  .c-parties b { font-size: 15px; }

  .c-sec { margin: 18px 0; }
  .c-sec h2 { margin: 0 0 8px; font-size: 14px; color: #0369a1;
              border-left: 4px solid #38bdf8; padding-left: 9px; }
  .c-sec p { margin: 4px 0; font-size: 13.5px; line-height: 1.7; }

  table { width: 100%; border-collapse: collapse; margin-top: 6px; }
  th { text-align: left; padding: 7px 6px; font-size: 12px; color: #64748b;
       border-bottom: 2px solid #cbd5e1; }
  td { padding: 7px 6px; font-size: 13.5px; border-bottom: 1px solid #e2e8f0; }
  .num { text-align: right; }
  tr.grand td { border-top: 2px solid #0f172a; border-bottom: 0;
                font-size: 15px; padding-top: 10px; }

  table.amt { max-width: 520px; }

  .c-group { margin-bottom: 12px; }
  .c-group-head { padding: 5px 2px; border-bottom: 1px solid #94a3b8;
                  font-size: 13px; font-weight: 700; color: #0369a1; }
  .c-group ul { margin: 6px 0 0; padding-left: 20px; }
  .c-group li { font-size: 13.5px; line-height: 1.9; }

  .kpi-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin: 16px 0; }
  .kpi { border: 1px solid #cbd5e1; border-radius: 8px; padding: 10px 12px; text-align: center; }
  .kpi b { display: block; font-size: 19px; color: #0369a1; }
  .kpi span { font-size: 11.5px; color: #64748b; }

  .c-notice { margin-top: 18px; padding: 11px 13px; background: #f8fafc;
              border: 1px solid #e2e8f0; border-radius: 7px; font-size: 12.5px; }
  .c-notice p { margin: 4px 0; line-height: 1.7; }
  .void-banner { margin: 14px 0 0; padding: 9px 13px; background: #fef2f2;
                 border: 1px solid #fecaca; color: #b91c1c; border-radius: 7px;
                 font-weight: 700; text-align: center; }

  .c-sign { display: grid; grid-template-columns: repeat(3, 1fr); gap: 22px; margin-top: 26px; }
  .c-sign span { display: block; font-size: 11.5px; color: #64748b; }
  .c-sign .line { border-bottom: 1px solid #334155; height: 32px; margin-bottom: 4px; }
  .c-sign .name { font-size: 13px !important; color: #0f172a !important; }

  .rpt-foot { margin-top: 22px; text-align: center; border-top: 1px dashed #cbd5e1; padding-top: 10px; }
  .tiny { color: #94a3b8; font-size: 11px; }
  .empty { padding: 18px; text-align: center; color: #64748b; font-size: 13px;
           background: #f8fafc; border-radius: 7px; }

  .no-print { max-width: 760px; margin: 16px auto 0; display: flex; gap: 9px; flex-wrap: wrap; }
  .btn { display: inline-block; padding: 9px 16px; border-radius: 8px; font-size: 13.5px;
         text-decoration: none; font-weight: 700; border: 0; cursor: pointer; }
  .btn-p { background: #38bdf8; color: #0f172a; }
  .btn-s { background: #334155; color: #e2e8f0; }

  @media print {
    body { background: #fff; padding: 0; }
    .no-print { display: none !important; }
    .sheet { max-width: none; box-shadow: none; border-radius: 0; padding: 0; }
    @page { margin: 12mm; }
  }
`
