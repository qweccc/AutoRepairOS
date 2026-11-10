package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
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
