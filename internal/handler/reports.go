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

// ReportsHome 報表總覽：營收、工單、庫存、技師績效。
// 營收卡、狀態分布、熱門項目皆按保險/自費分欄顯示；技師/庫存與保險無關,保持合併。
func ReportsHome(w http.ResponseWriter, r *http.Request, user model.User) {
	// 技師不看營收報表（財務數字屬店內機密）。
	if !canSeeMoney(user) {
		http.Error(w, "權限不足：營收報表僅限櫃檯人員與系統管理員使用", http.StatusForbidden)
		return
	}

	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" {
		from = today()[:7] + "-01"
	}
	if to == "" {
		to = today()
	}

	// 保險 / 自費 分欄標記：is_insurance=1 為保險單,0 為自費單。
	insLabel := "保險"
	selfLabel := "自費"

	// ============ 營收卡（保險 / 自費分欄）============
	// 營收 = SUM(total_amount) WHERE status='paid'。
	// 純 filter 拆兩組:保險 = SUM(total_amount WHERE is_insurance=1),自費 = SUM(total_amount WHERE is_insurance=0)。
	//
	// TODO(業務): 保險營收要不要扣免額?
	//   目前保險營收 = SUM(total_amount)(修理成本視角)。
	//   若會計定義為「保險公司實際拿到的」,則應 = SUM(total_amount) - SUM(deductible_amount)。
	//   免額的會計歸屬待 owner 確認後再決定(不影響分欄結構)。
	var insRevenue, selfRevenue float64
	var insCount, selfCount int
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0), COUNT(1) FROM work_orders
		WHERE status='paid' AND is_insurance=1 AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&insRevenue, &insCount)
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0), COUNT(1) FROM work_orders
		WHERE status='paid' AND is_insurance=0 AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&selfRevenue, &selfCount)

	// 兩組並列的營收卡(左右各一)。
	revenueCard := strings.Builder{}
	revenueCard.WriteString(`<div style="display:flex;gap:28px;flex-wrap:wrap">`)
	revenueCard.WriteString(`<div class="stat"><b>` + money(insRevenue) + `</b><span>` + insLabel + `營收</span></div>`)
	revenueCard.WriteString(`<div class="stat"><b>` + money(selfRevenue) + `</b><span>` + selfLabel + `營收</span></div>`)
	revenueCard.WriteString(`</div>`)
	revenueCard.WriteString(`<div style="margin-top:8px;color:#64748b;font-size:12px">已結帳工單：` +
		`<b>` + strconv.Itoa(insCount) + `</b> ` + insLabel + ` / <b>` + strconv.Itoa(selfCount) + `</b> ` + selfLabel + `</div>`)

	// ============ 狀態分布（保險 / 自費分欄）============
	// GROUP BY is_insurance, status 產生兩個並排表。
	statusRows, _ := db.DB.Query(`SELECT is_insurance, status, COUNT(1) FROM work_orders
		WHERE date(created_at) BETWEEN ? AND ?
		GROUP BY is_insurance, status`, from, to)
	statusCounts := map[int]map[string]int{}
	if statusRows != nil {
		for statusRows.Next() {
			var ins int
			var s string
			var n int
			if statusRows.Scan(&ins, &s, &n) == nil {
				if statusCounts[ins] == nil {
					statusCounts[ins] = map[string]int{}
				}
				statusCounts[ins][s] = n
			}
		}
		statusRows.Close()
	}
	buildStatusBlock := func(ins bool) string {
		sb := strings.Builder{}
		lbl := selfLabel
		if ins {
			lbl = insLabel
		}
		sb.WriteString(`<table><tr><th>` + lbl + `工單狀態</th><th class="num">數量</th></tr>`)
		for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid"} {
			n := statusCounts[0][s]
			if ins {
				n = statusCounts[1][s]
			}
			sb.WriteString(`<tr><td><span class="pill ` + statusPill[s] + `">` + workOrderStatusLabel[s] +
				`</span></td><td class="num">` + strconv.Itoa(n) + `</td></tr>`)
		}
		sb.WriteString(`</table>`)
		return sb.String()
	}
	statusBlocks := []string{buildStatusBlock(false), buildStatusBlock(true)}

	// ============ 熱門項目（保險 / 自費分欄）============
	// work_order_items 沒有 created_at，日期要用所屬工單的建立日，
	// 否則 SQL 直接報 no such column，整個熱門項目區塊會靜默變空。
	// 每張表只放一列（GROUP BY 讓每個 description 一列），所以要分組累積。
	itemRows, _ := db.DB.Query(`SELECT wo.is_insurance, woi.description,
		COALESCE(SUM(woi.quantity),0), COALESCE(SUM(woi.subtotal),0)
		FROM work_order_items woi
		JOIN work_orders wo ON wo.id = woi.work_order_id
		WHERE date(wo.created_at) BETWEEN ? AND ?
		GROUP BY wo.is_insurance, woi.description
		ORDER BY wo.is_insurance, SUM(woi.subtotal) DESC`, from, to)
	itemRowsByIns := map[int][]string{}
	itemCount := 0
	if itemRows != nil {
		for itemRows.Next() {
			var ins int
			var desc string
			var qty int
			var sub float64
			if itemRows.Scan(&ins, &desc, &qty, &sub) != nil {
				continue
			}
			itemRowsByIns[ins] = append(itemRowsByIns[ins], `<tr><td>`+htmlEsc(desc)+
				`</td><td class="num">`+strconv.Itoa(qty)+`</td><td class="num">`+money(sub)+`</td></tr>`)
			itemCount++
		}
		itemRows.Close()
	}
	buildItemBlock := func(ins bool) string {
		lbl := selfLabel
		idx := 0
		if ins {
			lbl = insLabel
			idx = 1
		}
		rows := itemRowsByIns[idx]
		// 只取前 10 名（SQL 不截斷，讓兩個分欄各自都有完整的 Top 10）
		if len(rows) > 10 {
			rows = rows[:10]
		}
		if len(rows) == 0 {
			return `<div class="empty">尚無` + lbl + `工單項目</div>`
		}
		return `<table><tr><th>` + lbl + `熱門項目</th><th class="num">數量</th><th class="num">營收</th></tr>` +
			strings.Join(rows, "") + `</table>`
	}
	itemBlocks := []string{buildItemBlock(false), buildItemBlock(true)}

	// ============ 技師績效（不動）============
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

	// ============ 庫存低水位（不動）============
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

	// ============ 應收帳齡分析 (AR Aging) ============
	// 已結帳但收款合計不足的工單，依結帳日（updated_at）距今天數分桶
	agingCounts := map[string]int{"0-30": 0, "31-60": 0, "61-90": 0, "90+": 0}
	agingAmounts := map[string]float64{"0-30": 0, "31-60": 0, "61-90": 0, "90+": 0}
	agingTotalCount := 0
	agingTotalAmount := 0.0
	agingRows, _ := db.DB.Query(`SELECT wo.id, wo.total_amount, COALESCE(wo.updated_at, wo.created_at),
		COALESCE((SELECT SUM(r.pay_total) FROM receivables r WHERE r.work_order_id = wo.id AND r.voided = 0), 0)
		FROM work_orders wo WHERE wo.status = 'completed'`)
	if agingRows != nil {
		for agingRows.Next() {
			var total, received float64
			var updatedAt string
			if agingRows.Scan(new(int64), &total, &updatedAt, &received) != nil {
				continue
			}
			unpaid := total - received
			if unpaid <= 0 {
				continue
			}
			days := 0
			if t := parseFlexTime(updatedAt); !t.IsZero() {
				days = int(time.Since(t).Hours() / 24)
			}
			bucket := "0-30"
			switch {
			case days > 90:
				bucket = "90+"
			case days > 60:
				bucket = "61-90"
			case days > 30:
				bucket = "31-60"
			}
			agingCounts[bucket]++
			agingAmounts[bucket] += unpaid
			agingTotalCount++
			agingTotalAmount += unpaid
		}
		agingRows.Close()
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
        <div class="stat"><b>` + money(insRevenue+selfRevenue) + `</b><span>營收總額</span></div>
        <div class="stat"><b>` + money(insRevenue) + `</b><span>` + insLabel + `營收</span></div>
        <div class="stat"><b>` + money(selfRevenue) + `</b><span>` + selfLabel + `營收</span></div>
        <div class="stat"><b>` + strconv.Itoa(insCount+selfCount) + `</b><span>已結帳工單</span></div>
        <div class="stat"><b>` + strconv.Itoa(insCount) + `</b><span>` + insLabel + `件</span></div>
        <div class="stat"><b>` + strconv.Itoa(selfCount) + `</b><span>` + selfLabel + `件</span></div>
      </div>
      <p style="color:#64748b;font-size:12.5px">營收 = 已結帳工單金額合計；區間以工單建立日為準。
        保險件佔比 ` + pctText(insRevenue, insRevenue+selfRevenue) + `。</p>
      <p style="color:#64748b;font-size:12.5px">統計區間：` + htmlEsc(from) + ` ~ ` + htmlEsc(to) + `</p>
    </div>

    <div class="card">
      <h2>工單狀態分布（` + insLabel + ` / ` + selfLabel + `）</h2>` + strings.Join(statusBlocks, "<br>") + `
    </div>

    <div class="card">
      <h2>熱門項目 Top 10（` + insLabel + ` / ` + selfLabel + `）</h2>` + strings.Join(itemBlocks, "<br>") + `
    </div>

    <div class="card">
      <h2>技師績效</h2>` + techTable + `
    </div>

    <div class="card">
      <h2>應收帳齡分析 (AR Aging)</h2>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">
        已結帳但尚未收款的工單，依結帳日（或建立日）計算帳齡分級。
      </p>
      <table>
        <tr><th>帳齡區間</th><th class="num">工單數</th><th class="num">應收金額</th></tr>
        <tr><td>0–30 天</td><td class="num">` + strconv.Itoa(agingCounts["0-30"]) + `</td><td class="num">` + money(agingAmounts["0-30"]) + `</td></tr>
        <tr><td>31–60 天</td><td class="num">` + strconv.Itoa(agingCounts["31-60"]) + `</td><td class="num">` + money(agingAmounts["31-60"]) + `</td></tr>
        <tr><td>61–90 天</td><td class="num">` + strconv.Itoa(agingCounts["61-90"]) + `</td><td class="num">` + money(agingAmounts["61-90"]) + `</td></tr>
        <tr><td>90+ 天</td><td class="num" style="color:#f87171">` + strconv.Itoa(agingCounts["90+"]) + `</td><td class="num" style="color:#f87171">` + money(agingAmounts["90+"]) + `</td></tr>
        <tr><td><b>合計</b></td><td class="num"><b>` + strconv.Itoa(agingTotalCount) + `</b></td><td class="num"><b>` + money(agingTotalAmount) + `</b></td></tr>
      </table>
    </div>

    <div class="card">
      <h2>庫存低水位警示</h2>` + lowTable + `
    </div>`

	renderPage(w, "報表", "/reports", user.Username, user.Role, body)
}
