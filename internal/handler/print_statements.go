package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/i18n"
	"autorepairos/internal/model"
)

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
		b.WriteString(`<div class="empty">` + i18n.T(r.Context(), "print.ap.empty") + `</div>`)
	} else {
		b.WriteString(`<table><tr><th>` + i18n.T(r.Context(), "print.ap.col_date") + `</th><th>` + i18n.T(r.Context(), "print.ap.col_wo") + `</th><th>` + i18n.T(r.Context(), "print.ap.col_plate") + `</th><th>` + i18n.T(r.Context(), "print.ap.col_content") + `</th>
			<th class="num">` + i18n.T(r.Context(), "print.ap.col_cost") + `</th><th class="num">` + i18n.T(r.Context(), "print.ap.col_charge") + `</th></tr>`)
		for _, x := range rows {
			b.WriteString(`<tr><td>` + htmlEsc(normDate(x.date)) + `</td><td>#` +
				strconv.FormatInt(x.woID, 10) + `</td><td>` + htmlEsc(x.plate) +
				`</td><td>` + htmlEsc(x.desc) + `</td><td class="num">` + money(x.cost) +
				`</td><td class="num">` + money(x.charge) + `</td></tr>`)
		}
		b.WriteString(`<tr class="grand"><td colspan="4"><b>` + i18n.T(r.Context(), "print.ap.total") + `</b></td><td class="num"><b>` +
			money(totalCost) + `</b></td><td class="num"><b>` + money(totalCharge) +
			`</b></td></tr></table>`)
	}

	termsLabel := paymentTermsLabelOf(r.Context(), sup.PaymentTerms)
	if termsLabel == "" {
		termsLabel = i18n.T(r.Context(), "supplier.payment.unset")
	}

	body := `
  <div class="sheet">
    ` + printHead(i18n.T(r.Context(), "print.ap.title"), []string{
		metaPair(i18n.T(r.Context(), "print.ap.meta_month"), month),
		metaPair(i18n.T(r.Context(), "print.ap.meta_date"), today()),
	}) + `

    <div class="c-parties">
      <div><span class="lbl">` + i18n.T(r.Context(), "print.ap.supplier") + `</span><b>` + htmlEsc(sup.Name) + `</b>
        <span class="lbl">` + i18n.T(r.Context(), "print.ap.contact") + `</span>` + htmlEsc(orDash(sup.ContactPerson)) + `</div>
      <div><span class="lbl">` + i18n.T(r.Context(), "print.ap.phone") + `</span>` + htmlEsc(orDash(sup.Phone)) + `</div>
      <div><span class="lbl">` + i18n.T(r.Context(), "print.ap.payment_terms") + `</span>` + termsLabel + `</div>
    </div>

    <div class="c-sec">
      <h2>` + fmt.Sprintf(i18n.T(r.Context(), "print.ap.section_title"), htmlEsc(month)) + `</h2>` + b.String() + `
    </div>

    <div class="c-notice">
      <p>` + fmt.Sprintf(i18n.T(r.Context(), "print.ap.notice"), termsLabel) + `</p>
      <p class="tiny">` + i18n.T(r.Context(), "print.ap.scope") + `</p>
    </div>

    <div class="c-sign">
      <div><span>` + i18n.T(r.Context(), "print.ap.sign_supplier") + `</span><div class="line"></div></div>
      <div><span>` + i18n.T(r.Context(), "print.ap.sign_shop") + `</span><div class="line"></div><span class="name">` + htmlEsc(user.Username) + `</span></div>
    </div>
  </div>
  ` + printToolbar(r.Context(), `<a class="btn btn-s" href="/suppliers/`+strconv.FormatInt(sup.ID, 10)+`/edit">` + i18n.T(r.Context(), "print.ap.back_edit") + `</a>`,
		`<a class="btn btn-s" href="/suppliers">` + i18n.T(r.Context(), "print.ap.back_suppliers") + `</a>`)

	renderPrintPage(w, fmt.Sprintf(i18n.T(r.Context(), "print.ap.doc_title"), sup.Name, month), body)
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
		FROM active_work_order_items woi
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
	payB.WriteString(`<table class="amt"><tr><th>` + i18n.T(r.Context(), "print.daily.col_method") + `</th><th class="num">` + i18n.T(r.Context(), "print.daily.col_amount") + `</th><th class="num">` + i18n.T(r.Context(), "print.daily.col_pct") + `</th></tr>`)
	for _, x := range payMethodRows(r.Context(), cash, card, checkAmt, transferAmt) {
		pct := 0.0
		if payTotal > 0 {
			pct = x.Value / payTotal * 100
		}
		payB.WriteString(`<tr><td>` + x.Label + `</td><td class="num">` + money(x.Value) +
			`</td><td class="num">` + strconv.FormatFloat(pct, 'f', 1, 64) + `%</td></tr>`)
	}
	if len(payMethodRows(r.Context(), cash, card, checkAmt, transferAmt)) == 0 {
		payB.WriteString(`<tr><td colspan="3" style="color:#64748b">` + i18n.T(r.Context(), "print.daily.empty") + `</td></tr>`)
	}
	payB.WriteString(`<tr class="grand"><td><b>` + i18n.T(r.Context(), "print.daily.total") + `</b></td><td class="num"><b>` + money(payTotal) +
		`</b></td><td class="num"></td></tr></table>`)

	body := `
  <div class="sheet">
    ` + printHead(i18n.T(r.Context(), "print.daily.title"), []string{
		metaPair(i18n.T(r.Context(), "print.daily.meta_date"), date),
		metaPair(i18n.T(r.Context(), "print.daily.meta_print"), today()),
	}) + `

    <div class="kpi-row">
      <div class="kpi"><b>` + money(woRevenue) + `</b><span>` + i18n.T(r.Context(), "print.daily.kpi_revenue") + `</span></div>
      <div class="kpi"><b>` + strconv.Itoa(paidCount) + `</b><span>` + i18n.T(r.Context(), "print.daily.kpi_paid") + `</span></div>
      <div class="kpi"><b>` + money(gross) + `</b><span>` + i18n.T(r.Context(), "print.daily.kpi_gross") + `</span></div>
      <div class="kpi"><b>` + strconv.FormatFloat(grossPct, 'f', 1, 64) + `%</b><span>` + i18n.T(r.Context(), "print.daily.kpi_margin") + `</span></div>
    </div>

    <div class="c-sec"><h2>` + i18n.T(r.Context(), "print.daily.section_pay") + `</h2>` + payB.String() + `
      <p class="tiny">` + fmt.Sprintf(i18n.T(r.Context(), "print.daily.note_basis"), strconv.Itoa(receiptCount)) + `</p>
    </div>

    <div class="c-sec"><h2>` + i18n.T(r.Context(), "print.daily.section_wo") + `</h2>
      <table class="amt">
        <tr><th>` + i18n.T(r.Context(), "print.daily.col_item") + `</th><th class="num">` + i18n.T(r.Context(), "print.daily.col_qty_amount") + `</th></tr>
        <tr><td>` + i18n.T(r.Context(), "print.daily.row_paid") + `</td><td class="num">` + strconv.Itoa(paidCount) + " " + i18n.T(r.Context(), "print.daily.sheets") + `</td></tr>
        <tr><td>` + i18n.T(r.Context(), "print.daily.row_insurance") + `</td><td class="num">` + strconv.Itoa(insCount) + " " + i18n.T(r.Context(), "print.daily.sheets") + `</td></tr>
        <tr><td>` + i18n.T(r.Context(), "print.daily.row_discount") + `</td><td class="num">` + money(discountTotal) + `</td></tr>
        <tr><td>` + i18n.T(r.Context(), "print.daily.row_warranty") + `</td><td class="num">` + money(warrantyRev) + `</td></tr>
        <tr><td>` + i18n.T(r.Context(), "print.daily.row_parts_cost") + `</td><td class="num">` + money(partsCost) + `</td></tr>
        <tr class="grand"><td><b>` + i18n.T(r.Context(), "print.daily.gross_formula") + `</b></td><td class="num"><b>` +
		money(gross) + `</b></td></tr>
      </table>
      <p class="tiny">` + i18n.T(r.Context(), "print.daily.note_gross") + `</p>
    </div>

    <div class="rpt-foot">
      <p class="tiny">` + i18n.T(r.Context(), "print.printed_at") + `` + time.Now().Format("2006-01-02 15:04") + `  / ` + i18n.T(r.Context(), "print.counter") + `` +
		htmlEsc(user.Username) + `</p>
    </div>
  </div>
  ` + printToolbar(r.Context(), `<a class="btn btn-s" href="/reports">` + i18n.T(r.Context(), "print.daily.reports") + `</a>`,
		`<a class="btn btn-s" href="/analytics/export?from=`+date+`&amp;to=`+date+`">` + i18n.T(r.Context(), "print.daily.export_csv") + `</a>`)

	renderPrintPage(w, fmt.Sprintf(i18n.T(r.Context(), "print.daily.doc_title"), date), body)
}
