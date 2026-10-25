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

// ============================================================
// 列印文件（FR-PRINT-02 ~ 06）
//
// 這一批補的是「結帳後」的單據。與先前的估價單（FR-PRINT-01）不同：
//   估價單 —— 施工前，有有效期與客戶簽名欄
//   這批   —— 施工後／收款後，是憑證與對帳用
//
// 五份文件：
//   FR-PRINT-02 完工單     工單完工證明，含施工項目與更換零件
//   FR-PRINT-03 發票/收據  收款明細，按支付方式與稅額分列
//   FR-PRINT-04 AR 催款單  依未收工單產生客戶催款單
//   FR-PRINT-05 AP 對帳單  供應商月結對帳（外包加工成本）
//   FR-PRINT-06 日結報表   當日營業額、支付方式分布、毛利簡報
//
// 共用的版面與工具放在本檔，各文件的差異只在內容區塊。
// 一律以瀏覽器列印（window.print + @media print）為目標。
// ============================================================

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

// ============ FR-PRINT-03 發票 / 收據 ============

// PrintReceiptWithTax 收款明細（按支付方式與稅額分列）。
//
// 這是 receipt.go 的收款版：receipt.go 那份是結帳時給客戶的收據（含 LINE 區塊），
// 這份是財務用——稅額拆分 + 支付方式分列 + 收款單號，供對帳與報帳。
func PrintReceiptWithTax(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	var v receivable
	var payDate string
	err := db.DB.QueryRow(`SELECT r.id, r.voucher_no, r.customer_id, COALESCE(c.name,''),
		COALESCE(r.pay_date,''), r.detail_total, r.real_total, r.discount, r.tax, r.pay_total,
		r.cash, r.card, r.check_amount, r.transfer_amount,
		COALESCE(r.transfer_reference,''),
		COALESCE(r.card_no,''), COALESCE(r.check_no,''), COALESCE(r.check_date,''),
		COALESCE(r.bank,''), COALESCE(r.notes,''), COALESCE(r.voided,0)
		FROM receivables r JOIN active_customers c ON c.id = r.customer_id WHERE r.id = ?`, id).
		Scan(&v.ID, &v.VoucherNo, &v.CustomerID, &v.CustomerName,
			&payDate, &v.DetailTotal, &v.RealTotal, &v.Discount, &v.Tax, &v.PayTotal,
			&v.Cash, &v.Card, &v.CheckAmount, &v.TransferAmount, &v.TransferReference,
			&v.CardNo, &v.CheckNo, &v.CheckDate, &v.Bank, &v.Notes, &v.Voided)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 稅額拆分：
	//   收款時有記錄 tax（> 0）→ 以該值為準，應稅額 = 總額 − 稅額
	//   沒有記錄（tax = 0）    → 由含稅總額內含反推（應稅 = 總額 ÷ 1.05）
	//
	// 兩者必須成套決定，不能混用：只推應稅額卻顯示稅額 0 會出現
	// 「應稅 10476 + 稅 0 ≠ 總額 11000」這種對不上的單據。
	tb := computeTaxBreakdown(v.PayTotal)
	taxAmount := tb.Tax
	taxable := tb.Taxable
	if v.Tax > 0 {
		taxAmount = v.Tax
		taxable = v.PayTotal - v.Tax
	}

	var amountB strings.Builder
	amountB.WriteString(`<table class="amt">`)
	amountB.WriteString(`<tr><th>項目</th><th class="num">金額</th></tr>`)
	if v.DetailTotal > 0 && v.DetailTotal != v.PayTotal {
		amountB.WriteString(`<tr><td>明細合計</td><td class="num">` + money(v.DetailTotal) + `</td></tr>`)
	}
	if v.Discount > 0 {
		amountB.WriteString(`<tr><td>折扣／折讓</td><td class="num" style="color:#b91c1c">-` +
			money(v.Discount) + `</td></tr>`)
	}
	amountB.WriteString(`<tr><td>應稅銷售額（未稅）</td><td class="num">` + money(taxable) + `</td></tr>`)
	amountB.WriteString(`<tr><td>營業稅（5%）</td><td class="num">` + money(taxAmount) + `</td></tr>`)
	amountB.WriteString(`<tr class="grand"><td><b>實收總額</b></td><td class="num"><b>` +
		money(v.PayTotal) + `</b></td></tr>`)
	amountB.WriteString(`</table>`)

	var payB strings.Builder
	payB.WriteString(`<table class="amt"><tr><th>支付方式</th><th class="num">金額</th><th>票據資訊</th></tr>`)
	rows := payMethodRows(v.Cash, v.Card, v.CheckAmount, v.TransferAmount)
	if len(rows) == 0 {
		payB.WriteString(`<tr><td colspan="3" style="color:#64748b">未記錄支付方式</td></tr>`)
	}
	for _, r := range rows {
		note := "—"
		if r.Label == "信用卡" && v.CardNo != "" {
			note = "卡號 " + v.CardNo
		}
		if r.Label == "票據" {
			note = "票號 " + orDash(v.CheckNo)
			if v.CheckDate != "" {
				note += " / 到期 " + normDate(v.CheckDate)
			}
			if v.Bank != "" {
				note += " / " + v.Bank
			}
		}
		if r.Label == "轉帳" && v.TransferReference != "" {
			note = "交易序號 " + v.TransferReference
		}
		payB.WriteString(`<tr><td>` + r.Label + `</td><td class="num">` + money(r.Value) +
			`</td><td style="font-size:12.5px">` + htmlEsc(note) + `</td></tr>`)
	}
	payB.WriteString(`</table>`)

	voidBanner := ""
	if v.Voided != 0 {
		voidBanner = `<div class="void-banner">此收款單已作廢</div>`
	}

	body := `
  <div class="sheet">
    ` + printHead("收款明細", []string{
		metaPair("收款單號", orDash(v.VoucherNo)),
		metaPair("收款日期", normDate(payDate)),
	}) + `
    ` + voidBanner + `
    <div class="c-parties">
      <div><span class="lbl">客戶</span><b>` + htmlEsc(v.CustomerName) + `</b></div>
      <div><span class="lbl">收款單號</span><b>` + htmlEsc(orDash(v.VoucherNo)) + `</b></div>
    </div>

    <div class="c-sec"><h2>金額明細</h2>` + amountB.String() + `
      <p class="tiny">稅額依含稅總額內含拆分（應稅 = 總額 ÷ 1.05）；若收款時另有記錄稅額則以該值為準。</p>
    </div>

    <div class="c-sec"><h2>支付方式</h2>` + payB.String() + `</div>

    ` + optNoteBlock(v.Notes) + `

    <div class="rpt-foot">
      <p class="tiny">列印時間：` + time.Now().Format("2006-01-02 15:04") + `</p>
    </div>
  </div>
  ` + printToolbar(`<a class="btn btn-s" href="/receivables">返回收款列表</a>`)

	renderPrintPage(w, "收款明細 "+orDash(v.VoucherNo), body)
}

// optNoteBlock 備註區塊（有內容才顯示）。
func optNoteBlock(notes string) string {
	if strings.TrimSpace(notes) == "" {
		return ""
	}
	return `
    <div class="c-sec"><h2>備註</h2><p>` + htmlEsc(notes) + `</p></div>`
}

// ============ FR-PRINT-04 AR 催款單 ============

// PrintARStatement 客戶應收帳款催款單。
//
// 內容：該客戶所有未收清的工單（status='completed' 但收款不足），
// 依結帳日（或建立日）計算帳齡並分級，讓催款有依據。
func PrintARStatement(w http.ResponseWriter, r *http.Request, user model.User) {
	customerID := mustInt(r.PathValue("id"))

	var custName, phone, addr string
	if err := db.DB.QueryRow(`SELECT name, COALESCE(phone,''), COALESCE(address,'')
		FROM active_customers WHERE id = ?`, customerID).
		Scan(&custName, &phone, &addr); err != nil {
		http.NotFound(w, r)
		return
	}

	type arRow struct {
		woID     int64
		plate    string
		date     string
		total    float64
		received float64
		unpaid   float64
		bucket   string
		days     int
	}
	var rows []arRow
	arRows, err := db.DB.Query(`SELECT wo.id, COALESCE(v.plate_number,''),
		COALESCE(wo.updated_at, wo.created_at), COALESCE(wo.total_amount,0),
		COALESCE((SELECT SUM(r.pay_total) FROM receivables r
			WHERE r.work_order_id = wo.id AND r.voided = 0), 0)
		FROM active_work_orders wo
		JOIN active_vehicles v ON v.id = wo.vehicle_id
		WHERE wo.customer_id = ? AND wo.status = 'completed'
		ORDER BY COALESCE(wo.updated_at, wo.created_at)`, customerID)
	if err == nil {
		for arRows.Next() {
			var x arRow
			var d string
			if arRows.Scan(&x.woID, &x.plate, &d, &x.total, &x.received) != nil {
				continue
			}
			x.unpaid = x.total - x.received
			if x.unpaid <= 0 {
				continue
			}
			if t := parseFlexTime(d); !t.IsZero() {
				x.days = int(time.Since(t).Hours() / 24)
				x.date = t.Format("2006-01-02")
			}
			switch {
			case x.days > 90:
				x.bucket = "90+ 天"
			case x.days > 60:
				x.bucket = "61-90 天"
			case x.days > 30:
				x.bucket = "31-60 天"
			default:
				x.bucket = "0-30 天"
			}
			rows = append(rows, x)
		}
		arRows.Close()
	}

	totalUnpaid := 0.0
	for _, x := range rows {
		totalUnpaid += x.unpaid
	}

	var b strings.Builder
	if len(rows) == 0 {
		b.WriteString(`<div class="empty">此客戶目前沒有未收清的帳款</div>`)
	} else {
		b.WriteString(`<table><tr><th>工單</th><th>車牌</th><th>日期</th><th class="num">帳齡</th>
			<th class="num">未收金額</th></tr>`)
		for _, x := range rows {
			overdueCls := ""
			if x.days > 60 {
				overdueCls = ` style="color:#b91c1c;font-weight:700"`
			}
			b.WriteString(`<tr><td><a href="/work-orders/` + strconv.FormatInt(x.woID, 10) + `">#` +
				strconv.FormatInt(x.woID, 10) + `</a></td><td>` + htmlEsc(x.plate) +
				`</td><td>` + htmlEsc(orDash(x.date)) + `</td><td class="num"` + overdueCls + `>` +
				x.bucket + `</td><td class="num">` + money(x.unpaid) + `</td></tr>`)
		}
		b.WriteString(`<tr class="grand"><td colspan="4"><b>應收合計</b></td><td class="num"><b>` +
			money(totalUnpaid) + `</b></td></tr></table>`)
	}

	body := `
  <div class="sheet">
    ` + printHead("應收帳款催款單", []string{
		metaPair("日期", today()),
		metaPair("客戶編號", strconv.FormatInt(customerID, 10)),
	}) + `

    <div class="c-parties">
      <div><span class="lbl">客戶</span><b>` + htmlEsc(custName) + `</b>
        <span class="lbl">電話</span>` + htmlEsc(orDash(phone)) + `</div>
      <div><span class="lbl">地址</span>` + htmlEsc(orDash(addr)) + `</div>
    </div>

    <div class="c-sec">
      <h2>未收帳款明細</h2>` + b.String() + `
    </div>

    <div class="c-notice">
      <p>以上款項已逾期未收，敬請於文到七日內惠予處理。如有疑問請與本店聯絡，謝謝。</p>
    </div>

    <div class="rpt-foot">
      <p class="tiny">列印時間：` + time.Now().Format("2006-01-02 15:04") + ` / 經辦：` +
		htmlEsc(user.Username) + `</p>
    </div>
  </div>
  ` + printToolbar(`<a class="btn btn-s" href="/receivables?customer=`+strconv.FormatInt(customerID, 10)+`">查看收款紀錄</a>`,
		`<a class="btn btn-s" href="/customers">返回客戶</a>`)

	renderPrintPage(w, "催款單 "+custName, body)
}

// ============ FR-PRINT-05 AP 月結對帳單 ============

// PrintAPStatement 供應商月結對帳單（外包加工成本）。
//
// 對帳範圍：指定月份內、已完成的外包加工（subcontracts.status='completed'），
// 依供應商彙總成本金額。這是 AP（應付帳款）的實際來源——車行的應付主要
// 就是外包給輪胎行、鈑金廠、烤漆廠的加工費。
//
// 為什麼不含「採購零件」：系統目前沒有採購進貨單據表（inventory 只有數量與
// 成本價，沒有進貨日期與供應商對帳所需的單號）。零件採購的對帳需要先進貨單
// 功能，這份單據先涵蓋已有完整資料的外包加工，並在頁面上說明範圍。
func PrintAPStatement(w http.ResponseWriter, r *http.Request, user model.User) {
	supplierID := mustInt(r.PathValue("id"))
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	if len(month) != 7 || month[4] != '-' {
		month = time.Now().Format("2006-01")
	}
	from := month + "-01"
	to := month + "-31"

	var sup model.Supplier
	if err := db.DB.QueryRow(`SELECT id, name, COALESCE(phone,''), COALESCE(contact_person,''),
		COALESCE(address,''), COALESCE(payment_terms,'')
		FROM active_suppliers WHERE id = ?`, supplierID).
		Scan(&sup.ID, &sup.Name, &sup.Phone, &sup.ContactPerson,
			&sup.Address, &sup.PaymentTerms); err != nil {
		http.NotFound(w, r)
		return
	}

	type apRow struct {
		id     int64
		woID   int64
		plate  string
		desc   string
		cost   float64
		charge float64
		date   string
	}
	var rows []apRow
	apRows, err := db.DB.Query(`SELECT sc.id, sc.work_order_id, COALESCE(v.plate_number,''),
		sc.description, COALESCE(sc.cost_amount,0), COALESCE(sc.charge_amount,0),
		COALESCE(sc.completed_date,'')
		FROM active_subcontracts sc
		JOIN active_work_orders wo ON wo.id = sc.work_order_id
		JOIN active_vehicles v ON v.id = wo.vehicle_id
		WHERE sc.supplier_id = ? AND sc.status = 'completed'
			AND COALESCE(sc.completed_date,'') BETWEEN ? AND ?
		ORDER BY sc.completed_date, sc.id`, supplierID, from, to)
	if err == nil {
		for apRows.Next() {
			var x apRow
			if apRows.Scan(&x.id, &x.woID, &x.plate, &x.desc, &x.cost, &x.charge, &x.date) != nil {
				continue
			}
			rows = append(rows, x)
		}
		apRows.Close()
	}

	totalCost, totalCharge := 0.0, 0.0
	for _, x := range rows {
		totalCost += x.cost
		totalCharge += x.charge
	}

	var b strings.Builder
	if len(rows) == 0 {
		b.WriteString(`<div class="empty">此月份沒有已完成的外包加工紀錄</div>`)
	} else {
		b.WriteString(`<table><tr><th>完成日</th><th>工單</th><th>車牌</th><th>加工內容</th>
			<th class="num">外包成本</th><th class="num">向客收費</th></tr>`)
		for _, x := range rows {
			b.WriteString(`<tr><td>` + htmlEsc(normDate(x.date)) + `</td><td>#` +
				strconv.FormatInt(x.woID, 10) + `</td><td>` + htmlEsc(x.plate) +
				`</td><td>` + htmlEsc(x.desc) + `</td><td class="num">` + money(x.cost) +
				`</td><td class="num">` + money(x.charge) + `</td></tr>`)
		}
		b.WriteString(`<tr class="grand"><td colspan="4"><b>合計</b></td><td class="num"><b>` +
			money(totalCost) + `</b></td><td class="num"><b>` + money(totalCharge) +
			`</b></td></tr></table>`)
	}

	termsLabel := paymentTermsLabel(sup.PaymentTerms)
	if termsLabel == "" {
		termsLabel = "未設定"
	}

	body := `
  <div class="sheet">
    ` + printHead("外包加工月結對帳單", []string{
		metaPair("帳款月份", month),
		metaPair("對帳日期", today()),
	}) + `

    <div class="c-parties">
      <div><span class="lbl">廠商</span><b>` + htmlEsc(sup.Name) + `</b>
        <span class="lbl">聯絡人</span>` + htmlEsc(orDash(sup.ContactPerson)) + `</div>
      <div><span class="lbl">電話</span>` + htmlEsc(orDash(sup.Phone)) + `</div>
      <div><span class="lbl">付款條件</span>` + termsLabel + `</div>
    </div>

    <div class="c-sec">
      <h2>` + htmlEsc(month) + ` 已完成外包加工</h2>` + b.String() + `
    </div>

    <div class="c-notice">
      <p>上述金額請核對無誤後，依付款條件（` + termsLabel + `）開立請款單據。</p>
      <p class="tiny">對帳範圍僅含外包加工紀錄；零件採購的對帳需先進貨單功能，目前不在本單據範圍內。</p>
    </div>

    <div class="c-sign">
      <div><span>廠商確認簽章</span><div class="line"></div></div>
      <div><span>本店經辦</span><div class="line"></div><span class="name">` + htmlEsc(user.Username) + `</span></div>
    </div>
  </div>
  ` + printToolbar(`<a class="btn btn-s" href="/suppliers/`+strconv.FormatInt(sup.ID, 10)+`/edit">編輯廠商</a>`,
		`<a class="btn btn-s" href="/suppliers">返回廠商</a>`)

	renderPrintPage(w, "AP 對帳單 "+sup.Name+" "+month, body)
}

// ============ FR-PRINT-06 日結報表 ============

// PrintDailyReport 日結報表：當日營業額、支付方式分布、毛利簡報。
//
// 一頁式摘要，供櫃檯打烊前核對：今日收了多少、用什麼方式收、
// 毛利多少。區間以收款日（pay_date）為準，不是工單建立日——
// 日結要對的是「今天進來的錢」。
func PrintDailyReport(w http.ResponseWriter, r *http.Request, user model.User) {
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	if date == "" {
		date = today()
	}

	// 支付方式分布（以收款紀錄為準）
	var cash, card, checkAmt, transferAmt, payTotal float64
	var receiptCount int
	db.DB.QueryRow(`SELECT COUNT(1), COALESCE(SUM(cash),0), COALESCE(SUM(card),0),
		COALESCE(SUM(check_amount),0), COALESCE(SUM(transfer_amount),0),
		COALESCE(SUM(pay_total),0)
		FROM receivables WHERE COALESCE(pay_date,'') = ? AND COALESCE(voided,0) = 0`, date).
		Scan(&receiptCount, &cash, &card, &checkAmt, &transferAmt, &payTotal)

	// 工單統計（當日結帳的工單）
	var paidCount, insCount int
	var woRevenue, discountTotal, warrantyRev float64
	db.DB.QueryRow(`SELECT COUNT(1),
		COALESCE(SUM(CASE WHEN is_insurance=1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(total_amount - COALESCE(discount_amount,0)),0),
		COALESCE(SUM(COALESCE(discount_amount,0)),0),
		COALESCE(SUM(CASE WHEN is_warranty=1 THEN total_amount ELSE 0 END),0)
		FROM active_work_orders WHERE status='paid' AND date(created_at) = ?`, date).
		Scan(&paidCount, &insCount, &woRevenue, &discountTotal, &warrantyRev)

	// 零件成本（當日結帳工單的零件，從庫存主檔帶成本價）
	var partsCost float64
	db.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(i.cost_price,0) * woi.quantity),0)
		FROM work_order_items woi
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
		WHERE woi.item_type='part' AND wo.status='paid' AND date(wo.created_at) = ?`, date).
		Scan(&partsCost)

	gross := woRevenue - partsCost
	grossPct := 0.0
	if woRevenue > 0 {
		grossPct = gross / woRevenue * 100
	}

	var payB strings.Builder
	payB.WriteString(`<table class="amt"><tr><th>支付方式</th><th class="num">金額</th><th class="num">佔比</th></tr>`)
	for _, x := range payMethodRows(cash, card, checkAmt, transferAmt) {
		pct := 0.0
		if payTotal > 0 {
			pct = x.Value / payTotal * 100
		}
		payB.WriteString(`<tr><td>` + x.Label + `</td><td class="num">` + money(x.Value) +
			`</td><td class="num">` + strconv.FormatFloat(pct, 'f', 1, 64) + `%</td></tr>`)
	}
	if len(payMethodRows(cash, card, checkAmt, transferAmt)) == 0 {
		payB.WriteString(`<tr><td colspan="3" style="color:#64748b">今日無收款紀錄</td></tr>`)
	}
	payB.WriteString(`<tr class="grand"><td><b>收款合計</b></td><td class="num"><b>` + money(payTotal) +
		`</b></td><td class="num"></td></tr></table>`)

	body := `
  <div class="sheet">
    ` + printHead("日結報表", []string{
		metaPair("日期", date),
		metaPair("列印", today()),
	}) + `

    <div class="kpi-row">
      <div class="kpi"><b>` + money(woRevenue) + `</b><span>營業額</span></div>
      <div class="kpi"><b>` + strconv.Itoa(paidCount) + `</b><span>結帳工單</span></div>
      <div class="kpi"><b>` + money(gross) + `</b><span>毛利</span></div>
      <div class="kpi"><b>` + strconv.FormatFloat(grossPct, 'f', 1, 64) + `%</b><span>毛利率</span></div>
    </div>

    <div class="c-sec"><h2>支付方式分布</h2>` + payB.String() + `
      <p class="tiny">依收款紀錄（` + strconv.Itoa(receiptCount) + ` 筆）統計，以收款日為準。</p>
    </div>

    <div class="c-sec"><h2>工單統計</h2>
      <table class="amt">
        <tr><th>項目</th><th class="num">數量／金額</th></tr>
        <tr><td>已結帳工單</td><td class="num">` + strconv.Itoa(paidCount) + ` 張</td></tr>
        <tr><td>其中保險理賠件</td><td class="num">` + strconv.Itoa(insCount) + ` 張</td></tr>
        <tr><td>折扣總額</td><td class="num">` + money(discountTotal) + `</td></tr>
        <tr><td>保固工單金額</td><td class="num">` + money(warrantyRev) + `</td></tr>
        <tr><td>零件成本</td><td class="num">` + money(partsCost) + `</td></tr>
        <tr class="grand"><td><b>毛利（營業額 − 零件成本）</b></td><td class="num"><b>` +
		money(gross) + `</b></td></tr>
      </table>
      <p class="tiny">毛利只計零件成本；工資與外包在舊系統未記錄成本，故不計入。</p>
    </div>

    <div class="rpt-foot">
      <p class="tiny">列印時間：` + time.Now().Format("2006-01-02 15:04") + ` / 櫃檯：` +
		htmlEsc(user.Username) + `</p>
    </div>
  </div>
  ` + printToolbar(`<a class="btn btn-s" href="/reports">營運報表</a>`,
		`<a class="btn btn-s" href="/analytics/export?from=`+date+`&amp;to=`+date+`">匯出 CSV</a>`)

	renderPrintPage(w, "日結報表 "+date, body)
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
