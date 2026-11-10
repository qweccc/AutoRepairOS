// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
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
