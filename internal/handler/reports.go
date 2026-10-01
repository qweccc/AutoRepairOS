package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ReportsHome 報表總覽：營收、工單、庫存、技師績效。
func ReportsHome(w http.ResponseWriter, r *http.Request, user model.User) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" {
		from = today()[:7] + "-01"
	}
	if to == "" {
		to = today()
	}

	// 營收（已結帳工單）
	var revenue, orderCount, avgTicket float64
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0), COUNT(1), COALESCE(AVG(total_amount),0)
		FROM work_orders WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&revenue, &orderCount, &avgTicket)

	// 工單狀態分布
	statusRows, _ := db.DB.Query(`SELECT status, COUNT(1) FROM work_orders
		WHERE date(created_at) BETWEEN ? AND ? GROUP BY status`, from, to)
	statusCounts := make(map[string]int)
	if statusRows != nil {
		for statusRows.Next() {
			var s string
			var n int
			if statusRows.Scan(&s, &n) == nil {
				statusCounts[s] = n
			}
		}
		statusRows.Close()
	}

	var sb strings.Builder
	sb.WriteString(`<table><tr><th>狀態</th><th class="num">數量</th></tr>`)
	for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid"} {
		sb.WriteString(`<tr><td><span class="pill ` + statusPill[s] + `">` + workOrderStatusLabel[s] +
			`</span></td><td class="num">` + strconv.Itoa(statusCounts[s]) + `</td></tr>`)
	}
	sb.WriteString(`</table>`)

	// 熱門服務項目
	itemRows, _ := db.DB.Query(`SELECT description, COALESCE(SUM(quantity),0), COALESCE(SUM(subtotal),0)
		FROM work_order_items GROUP BY description
		ORDER BY SUM(subtotal) DESC LIMIT 10`)
	var ib strings.Builder
	ib.WriteString(`<table><tr><th>項目</th><th class="num">數量</th><th class="num">營收</th></tr>`)
	itemCount := 0
	if itemRows != nil {
		for itemRows.Next() {
			var desc string
			var qty int
			var sub float64
			if itemRows.Scan(&desc, &qty, &sub) == nil {
				ib.WriteString(`<tr><td>` + htmlEsc(desc) + `</td><td class="num">` + strconv.Itoa(qty) +
					`</td><td class="num">` + money(sub) + `</td></tr>`)
				itemCount++
			}
		}
		itemRows.Close()
	}
	ib.WriteString(`</table>`)
	itemTable := ib.String()
	if itemCount == 0 {
		itemTable = `<div class="empty">尚無工單項目</div>`
	}

	// 技師績效
	techRows, _ := db.DB.Query(`SELECT COALESCE(u.name,'-'), COUNT(wl.id),
		COALESCE(SUM(wl.hours),0), COALESCE(SUM(wl.commission_amount),0)
		FROM technician_worklogs wl JOIN users u ON u.id = wl.technician_id
		WHERE date(wl.work_date) BETWEEN ? AND ?
		GROUP BY u.id, u.name ORDER BY SUM(wl.commission_amount) DESC`, from, to)
	var tb strings.Builder
	tb.WriteString(`<table><tr><th>技師</th><th class="num">工單數</th><th class="num">總工時</th><th class="num">總佣金</th></tr>`)
	techCount := 0
	if techRows != nil {
		for techRows.Next() {
			var name string
			var orders int
			var hours, comm float64
			if techRows.Scan(&name, &orders, &hours, &comm) == nil {
				tb.WriteString(`<tr><td>` + htmlEsc(name) + `</td><td class="num">` + strconv.Itoa(orders) +
					`</td><td class="num">` + trimNum(hours) + `</td><td class="num">` + money(comm) + `</td></tr>`)
				techCount++
			}
		}
		techRows.Close()
	}
	tb.WriteString(`</table>`)
	techTable := tb.String()
	if techCount == 0 {
		techTable = `<div class="empty">尚無技師工時紀錄</div>`
	}

	// 庫存低水位
	lowRows, _ := db.DB.Query(`SELECT part_number, name, stock_quantity, min_safety_stock
		FROM inventory_items WHERE stock_quantity <= min_safety_stock ORDER BY stock_quantity ASC LIMIT 10`)
	var lb strings.Builder
	lb.WriteString(`<table><tr><th>料號</th><th>名稱</th><th class="num">現有</th><th class="num">安全存量</th></tr>`)
	lowCount := 0
	if lowRows != nil {
		for lowRows.Next() {
			var part, name string
			var stock, minStock int
			if lowRows.Scan(&part, &name, &stock, &minStock) == nil {
				cls := "p-apprpend"
				if stock == 0 {
					cls = "p-apprno"
				}
				lb.WriteString(`<tr><td><code>` + htmlEsc(part) + `</code></td><td>` + htmlEsc(name) +
					`</td><td class="num"><span class="pill ` + cls + `">` + strconv.Itoa(stock) +
					`</span></td><td class="num">` + strconv.Itoa(minStock) + `</td></tr>`)
				lowCount++
			}
		}
		lowRows.Close()
	}
	lb.WriteString(`</table>`)
	lowTable := lb.String()
	if lowCount == 0 {
		lowTable = `<div class="empty">庫存水位充足</div>`
	}

	body := `
    <div class="card">
      <h2>報表總覽</h2>
      <form method="GET" action="/reports" style="margin-bottom:16px">
        <label style="font-size:12px;color:#94a3b8">起</label>
        <input name="from" value="` + htmlEsc(from) + `" placeholder="YYYY-MM-DD"
               style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:140px">
        <label style="font-size:12px;color:#94a3b8;margin-left:10px">迄</label>
        <input name="to" value="` + htmlEsc(to) + `" placeholder="YYYY-MM-DD"
               style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:140px">
        <button class="btn btn-p" type="submit">查詢</button>
      </form>

      <div style="margin-bottom:14px">
        <div class="stat"><b>` + money(revenue) + `</b><span>營收總額</span></div>
        <div class="stat"><b>` + money(orderCount) + `</b><span>已結帳工單</span></div>
        <div class="stat"><b>` + money(avgTicket) + `</b><span>平均客單</span></div>
      </div>
      <p style="color:#64748b;font-size:12.5px">統計區間：` + htmlEsc(from) + ` ~ ` + htmlEsc(to) + `</p>
    </div>

    <div class="card">
      <h2>工單狀態分布</h2>` + sb.String() + `
    </div>

    <div class="card">
      <h2>熱門項目 Top 10</h2>` + itemTable + `
    </div>

    <div class="card">
      <h2>技師績效</h2>` + techTable + `
    </div>

    <div class="card">
      <h2>庫存低水位警示</h2>` + lowTable + `
    </div>`

	renderPage(w, "報表", "/reports", user.Username, user.Role, body)
}
