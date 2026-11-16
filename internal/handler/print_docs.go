// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/i18n"
	"autorepairos/internal/model"
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
	amountB.WriteString(`<tr><th>` + i18n.T(r.Context(), "print.receipt.col_item") + `</th><th class="num">` + i18n.T(r.Context(), "print.receipt.col_amount") + `</th></tr>`)
	if v.DetailTotal > 0 && v.DetailTotal != v.PayTotal {
		amountB.WriteString(`<tr><td>` + i18n.T(r.Context(), "print.receipt.detail_total") + `</td><td class="num">` + money(v.DetailTotal) + `</td></tr>`)
	}
	if v.Discount > 0 {
		amountB.WriteString(`<tr><td>` + i18n.T(r.Context(), "print.receipt.discount") + `</td><td class="num" style="color:#b91c1c">-` +
			money(v.Discount) + `</td></tr>`)
	}
	amountB.WriteString(`<tr><td>` + i18n.T(r.Context(), "print.receipt.taxable") + `</td><td class="num">` + money(taxable) + `</td></tr>`)
	amountB.WriteString(`<tr><td>` + i18n.T(r.Context(), "print.receipt.tax") + `</td><td class="num">` + money(taxAmount) + `</td></tr>`)
	amountB.WriteString(`<tr class="grand"><td><b>` + i18n.T(r.Context(), "print.receipt.total") + `</b></td><td class="num"><b>` +
		money(v.PayTotal) + `</b></td></tr>`)
	amountB.WriteString(`</table>`)

	var payB strings.Builder
	payB.WriteString(`<table class="amt"><tr><th>` + i18n.T(r.Context(), "print.receipt.col_method") + `</th><th class="num">` + i18n.T(r.Context(), "print.receipt.col_amount") + `</th><th>` + i18n.T(r.Context(), "print.receipt.col_note") + `</th></tr>`)
	rows := payMethodRows(r.Context(), v.Cash, v.Card, v.CheckAmount, v.TransferAmount)
	if len(rows) == 0 {
		payB.WriteString(`<tr><td colspan="3" style="color:#64748b">` + i18n.T(r.Context(), "print.receipt.not_recorded") + `</td></tr>`)
	}
	for _, pm := range rows {
		note := "—"
		if pm.Code == "card" && v.CardNo != "" {
			note = i18n.T(r.Context(), "print.receipt.card_no") + " " + v.CardNo
		}
		if pm.Code == "check" {
			note = i18n.T(r.Context(), "print.receipt.check_no") + " " + orDash(v.CheckNo)
			if v.CheckDate != "" {
				note += " / " + i18n.T(r.Context(), "print.receipt.due") + " " + normDate(v.CheckDate)
			}
			if v.Bank != "" {
				note += " / " + v.Bank
			}
		}
		if pm.Code == "transfer" && v.TransferReference != "" {
			note = i18n.T(r.Context(), "print.receipt.transfer_ref") + " " + v.TransferReference
		}
		payB.WriteString(`<tr><td>` + pm.Label + `</td><td class="num">` + money(pm.Value) +
			`</td><td style="font-size:12.5px">` + htmlEsc(note) + `</td></tr>`)
	}
	payB.WriteString(`</table>`)

	voidBanner := ""
	if v.Voided != 0 {
		voidBanner = `<div class="void-banner">` + i18n.T(r.Context(), "print.receipt.voided") + `</div>`
	}

	body := `
  <div class="sheet">
    ` + printHead(i18n.T(r.Context(), "print.receipt.title"), []string{
		metaPair(i18n.T(r.Context(), "print.receipt.meta_voucher"), orDash(v.VoucherNo)),
		metaPair(i18n.T(r.Context(), "print.receipt.meta_date"), normDate(payDate)),
	}) + `
    ` + voidBanner + `
    <div class="c-parties">
      <div><span class="lbl">` + i18n.T(r.Context(), "print.receipt.customer") + `</span><b>` + htmlEsc(v.CustomerName) + `</b></div>
      <div><span class="lbl">` + i18n.T(r.Context(), "print.receipt.meta_voucher") + `</span><b>` + htmlEsc(orDash(v.VoucherNo)) + `</b></div>
    </div>

    <div class="c-sec"><h2>` + i18n.T(r.Context(), "print.receipt.section_amount") + `</h2>` + amountB.String() + `
      <p class="tiny">` + i18n.T(r.Context(), "print.receipt.tax_note") + `</p>
    </div>

    <div class="c-sec"><h2>` + i18n.T(r.Context(), "print.receipt.section_payment") + `</h2>` + payB.String() + `</div>

    ` + optNoteBlock(r.Context(), v.Notes) + `

    <div class="rpt-foot">
      <p class="tiny">` + i18n.T(r.Context(), "print.printed_at") + `` + time.Now().Format("2006-01-02 15:04") + `</p>
    </div>
  </div>
  ` + printToolbar(r.Context(), `<a class="btn btn-s" href="/receivables">` + i18n.T(r.Context(), "print.receipt.back") + `</a>`)

	renderPrintPage(w, i18n.T(r.Context(), "print.receipt.doc_title")+orDash(v.VoucherNo), body)
}

// optNoteBlock 備註區塊（有內容才顯示）。
func optNoteBlock(ctx context.Context, notes string) string {
	if strings.TrimSpace(notes) == "" {
		return ""
	}
	return `
    <div class="c-sec"><h2>` + i18n.T(ctx, "print.receipt.note_title") + `</h2><p>` + htmlEsc(notes) + `</p></div>`
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
				x.bucket = i18n.T(r.Context(), "print.ar.bucket.over90")
			case x.days > 60:
				x.bucket = i18n.T(r.Context(), "print.ar.bucket.61_90")
			case x.days > 30:
				x.bucket = i18n.T(r.Context(), "print.ar.bucket.31_60")
			default:
				x.bucket = i18n.T(r.Context(), "print.ar.bucket.0_30")
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
		b.WriteString(`<div class="empty">` + i18n.T(r.Context(), "print.ar.empty") + `</div>`)
	} else {
		b.WriteString(`<table><tr><th>` + i18n.T(r.Context(), "print.ar.col_wo") + `</th><th>` + i18n.T(r.Context(), "print.ar.col_plate") + `</th><th>` + i18n.T(r.Context(), "print.ar.col_date") + `</th><th class="num">` + i18n.T(r.Context(), "print.ar.col_age") + `</th>
			<th class="num">` + i18n.T(r.Context(), "print.ar.col_unpaid") + `</th></tr>`)
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
		b.WriteString(`<tr class="grand"><td colspan="4"><b>` + i18n.T(r.Context(), "print.ar.total") + `</b></td><td class="num"><b>` +
			money(totalUnpaid) + `</b></td></tr></table>`)
	}

	body := `
  <div class="sheet">
    ` + printHead(i18n.T(r.Context(), "print.ar.title"), []string{
		metaPair(i18n.T(r.Context(), "print.ar.meta_date"), today()),
		metaPair(i18n.T(r.Context(), "print.ar.meta_customer_no"), strconv.FormatInt(customerID, 10)),
	}) + `

    <div class="c-parties">
      <div><span class="lbl">` + i18n.T(r.Context(), "print.ar.customer") + `</span><b>` + htmlEsc(custName) + `</b>
        <span class="lbl">` + i18n.T(r.Context(), "print.ar.phone") + `</span>` + htmlEsc(orDash(phone)) + `</div>
      <div><span class="lbl">` + i18n.T(r.Context(), "print.ar.address") + `</span>` + htmlEsc(orDash(addr)) + `</div>
    </div>

    <div class="c-sec">
      <h2>` + i18n.T(r.Context(), "print.ar.section_unpaid") + `</h2>` + b.String() + `
    </div>

    <div class="c-notice">
      <p>` + i18n.T(r.Context(), "print.ar.notice") + `</p>
    </div>

    <div class="rpt-foot">
      <p class="tiny">` + i18n.T(r.Context(), "print.ar.print_time") + `` + time.Now().Format("2006-01-02 15:04") + `  / ` + i18n.T(r.Context(), "print.operator") + `` +
		htmlEsc(user.Username) + `</p>
    </div>
  </div>
  ` + printToolbar(r.Context(), `<a class="btn btn-s" href="/receivables?customer=`+strconv.FormatInt(customerID, 10)+`">` + i18n.T(r.Context(), "print.ar.view_receipts") + `</a>`,
		`<a class="btn btn-s" href="/customers">` + i18n.T(r.Context(), "print.ar.back_customers") + `</a>`)

	renderPrintPage(w, i18n.T(r.Context(), "print.ar.doc_title")+custName, body)
}
