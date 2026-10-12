// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ============================================================
// 經營分析（SRS 5.12）
//
// 這頁補的是「資料都已存在、但一直沒算」的指標：
//   - FR-ANA-04 KPI 儀表板：ARO、進場數、毛利率、留存率，可切日/週/月
//   - FR-ANA-01 月營收與毛利，依 item_type 分組（工資/零件/外包）
//   - FR-ANA-03 高頻維修車款與熱銷零件
//   - FR-ANA-05 顧客留存：回訪率、新客/舊客
//   - FR-ANA-06 行銷方案成效：依 campaign_code 統計營收與毛利
//   - FR-ANA-07 現金流：labor/parts/subcontract 收入與成本概況
//   - FR-INV-04  死頭料：長期未動用零件與對應成本
//
// 毛利口徑：毛利 = 售價 - 成本。零件成本從 work_order_items.inventory_item_id
// 關回 inventory_items.cost_price（這個表沒有 part_number，用外鍵取成本）。
// 工資與外包在 work_order_items 沒有成本欄（舊系統未記錄），
// 因此這兩類的毛利等同售價——頁面上明確標示「工資/外包未記錄成本」，
// 不假裝有毛利數字。
// ============================================================

// periodRange 把日/週/月視圖轉成 [from, to]（含尾日）。
// 週以「週一為第一天」計算，符合台灣車廠的作息。
func periodRange(gran, from, to string) (string, string) {
	now := time.Now()
	switch gran {
	case "day":
		return now.Format("2006-01-02"), now.Format("2006-01-02")
	case "week":
		// time.Weekday: Sunday=0 ... Saturday=6
		offset := (int(now.Weekday()) + 6) % 7
		start := now.AddDate(0, 0, -offset)
		return start.Format("2006-01-02"), start.AddDate(0, 0, 6).Format("2006-01-02")
	case "month":
		f := now.Format("2006-01") + "-01"
		return f, now.Format("2006-01-02")
	default:
		if from != "" && to != "" {
			return from, to
		}
		return now.Format("2006-01") + "-01", now.Format("2006-01-02")
	}
}

// plRow 是損益分拆的一列。
type plRow struct {
	Type     string
	Label    string
	Revenue  float64
	Cost     float64
	Quantity int
}

// itemTypeLabel 對應 work_order_items.item_type（CHECK 約束的三值）。
var itemTypeLabel = map[string]string{
	"labor":       "工資",
	"part":        "零件",
	"subcontract": "外包加工",
}

func AnalyticsHome(w http.ResponseWriter, r *http.Request, user model.User) {
	if !canSeeMoney(user) {
		http.Error(w, "權限不足：經營分析僅限櫃檯人員與系統管理員使用", http.StatusForbidden)
		return
	}

	qv := r.URL.Query()
	gran := strings.TrimSpace(qv.Get("gran"))
	from := strings.TrimSpace(qv.Get("from"))
	to := strings.TrimSpace(qv.Get("to"))
	if gran == "" {
		gran = "month"
	}
	if gran != "day" && gran != "week" && gran != "month" {
		gran = "month"
	}
	from, to = periodRange(gran, from, to)

	// ============ KPI（FR-ANA-04）============
	// ARO = 已結帳工單平均金額；進場數 = 期間開立的工單張數（含未結帳）。
	var revenue, partsCost float64
	var paidCount, orderCount int
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount - COALESCE(discount_amount,0)),0), COUNT(1)
		FROM work_orders WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&revenue, &paidCount)
	db.DB.QueryRow(`SELECT COUNT(1) FROM work_orders WHERE date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&orderCount)

	// 零件成本：從 work_order_items.inventory_item_id 關回庫存主檔帶 cost_price。
	// 找不到料號的零件不計成本（寧可低估毛利也不造假）。
	db.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(i.cost_price,0) * woi.quantity),0)
		FROM work_order_items woi
		JOIN work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN inventory_items i ON i.id = woi.inventory_item_id
		WHERE woi.item_type='part' AND wo.status='paid'
			AND date(wo.created_at) BETWEEN ? AND ?`, from, to).Scan(&partsCost)

	aro := 0.0
	if paidCount > 0 {
		aro = revenue / float64(paidCount)
	}
	gross := revenue - partsCost
	grossPct := 0.0
	if revenue > 0 {
		grossPct = gross / revenue * 100
	}

	// ============ 損益分拆（FR-ANA-01 / FR-FIN-06 / FR-ANA-07）============
	plRows := []plRow{
		{Type: "labor", Label: itemTypeLabel["labor"]},
		{Type: "part", Label: itemTypeLabel["part"]},
		{Type: "subcontract", Label: itemTypeLabel["subcontract"]},
	}
	plIdx := map[string]int{}
	for i, p := range plRows {
		plIdx[p.Type] = i
	}
	itemRows, _ := db.DB.Query(`SELECT woi.item_type,
			COALESCE(SUM(woi.subtotal),0), COALESCE(SUM(woi.quantity),0),
			COALESCE(SUM(CASE WHEN woi.item_type='part'
				THEN COALESCE(i.cost_price,0) * woi.quantity ELSE 0 END),0)
		FROM work_order_items woi
		JOIN work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN inventory_items i ON i.id = woi.inventory_item_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY woi.item_type`, from, to)
	if itemRows != nil {
		for itemRows.Next() {
			var t string
			var rev, cost float64
			var qty int
			if itemRows.Scan(&t, &rev, &qty, &cost) == nil {
				if i, ok := plIdx[t]; ok {
					plRows[i].Revenue = rev
					plRows[i].Cost = cost
					plRows[i].Quantity = qty
				}
			}
		}
		itemRows.Close()
	}

	// ============ 高頻維修車款（FR-ANA-03）============
	// vehicles 只有 brand_model（廠牌車型存在同一欄），沒有獨立的 make/model 欄。
	// 用錯欄位名會讓整條查詢失敗、這張表靜默變空。
	carRows, _ := db.DB.Query(`SELECT COALESCE(v.brand_model,'(未填車型)'), COUNT(1)
		FROM work_orders wo JOIN vehicles v ON v.id = wo.vehicle_id
		WHERE date(wo.created_at) BETWEEN ? AND ?
		GROUP BY COALESCE(v.brand_model,'(未填車型)')
		ORDER BY COUNT(1) DESC LIMIT 10`, from, to)
	var carBody strings.Builder
	carBody.WriteString(`<table><tr><th>車型</th><th class="num">進廠次數</th></tr>`)
	carCount := 0
	if carRows != nil {
		for carRows.Next() {
			var mdl string
			var n int
			if carRows.Scan(&mdl, &n) == nil {
				carBody.WriteString(`<tr><td>` + htmlEsc(mdl) +
					`</td><td class="num">` + strconv.Itoa(n) + `</td></tr>`)
				carCount++
			}
		}
		carRows.Close()
	}
	carBody.WriteString(`</table>`)
	carTable := carBody.String()
	if carCount == 0 {
		carTable = `<div class="empty">期間無維修紀錄</div>`
	}

	// ============ 熱銷零件（FR-ANA-03）============
	partRows, _ := db.DB.Query(`SELECT COALESCE(i.name, woi.description), COALESCE(SUM(woi.quantity),0),
			COALESCE(SUM(woi.subtotal),0)
		FROM work_order_items woi
		JOIN work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN inventory_items i ON i.id = woi.inventory_item_id
		WHERE woi.item_type='part' AND wo.status='paid'
			AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY COALESCE(i.name, woi.description)
		ORDER BY SUM(woi.quantity) DESC LIMIT 10`, from, to)
	var pb strings.Builder
	pb.WriteString(`<table><tr><th>零件</th><th class="num">數量</th><th class="num">營收</th></tr>`)
	partCount := 0
	if partRows != nil {
		for partRows.Next() {
			var name string
			var qty int
			var amt float64
			if partRows.Scan(&name, &qty, &amt) == nil {
				pb.WriteString(`<tr><td>` + htmlEsc(name) + `</td><td class="num">` + strconv.Itoa(qty) +
					`</td><td class="num">` + money(amt) + `</td></tr>`)
				partCount++
			}
		}
		partRows.Close()
	}
	pb.WriteString(`</table>`)
	partTable := pb.String()
	if partCount == 0 {
		partTable = `<div class="empty">期間無零件銷售</div>`
	}

	// ============ 顧客留存（FR-ANA-05）============
	// 新客 = 首次消費落在區間內；舊客 = 區間前就有消費紀錄。
	// 回訪率 = 期間有 ≥2 張已結帳工單的客戶 / 期間有消費的客戶。
	var newCust, oldCust, activeCust, repeatCust int
	db.DB.QueryRow(`SELECT COUNT(1) FROM (
		SELECT wo.customer_id
		FROM work_orders wo
		WHERE wo.status='paid'
		GROUP BY wo.customer_id
		HAVING MIN(date(wo.created_at)) BETWEEN ? AND ?)`, from, to).Scan(&newCust)
	db.DB.QueryRow(`SELECT COUNT(1) FROM (
		SELECT wo.customer_id
		FROM work_orders wo
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY wo.customer_id
		HAVING MIN(date(wo.created_at)) < ?)`, from, to, from).Scan(&oldCust)
	db.DB.QueryRow(`SELECT COUNT(DISTINCT customer_id) FROM work_orders
		WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).Scan(&activeCust)
	db.DB.QueryRow(`SELECT COUNT(1) FROM (
		SELECT customer_id FROM work_orders
		WHERE status='paid' AND date(created_at) BETWEEN ? AND ?
		GROUP BY customer_id HAVING COUNT(1) >= 2)`, from, to).Scan(&repeatCust)

	repeatRate := 0.0
	if activeCust > 0 {
		repeatRate = float64(repeatCust) / float64(activeCust) * 100
	}
	avgPerCust := 0.0
	if activeCust > 0 {
		avgPerCust = revenue / float64(activeCust)
	}

	// ============ 行銷方案成效（FR-ANA-06）============
	campRows, _ := db.DB.Query(`SELECT COALESCE(wo.campaign_code, c.campaign_code, '(未設定)'),
			COUNT(1), COALESCE(SUM(wo.total_amount - COALESCE(wo.discount_amount,0)),0)
		FROM work_orders wo
		LEFT JOIN customers c ON c.id = wo.customer_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY COALESCE(wo.campaign_code, c.campaign_code, '(未設定)')
		ORDER BY SUM(wo.total_amount - COALESCE(wo.discount_amount,0)) DESC`, from, to)
	var cb strings.Builder
	cb.WriteString(`<table><tr><th>方案代碼</th><th class="num">工單數</th><th class="num">營收</th></tr>`)
	campCount := 0
	if campRows != nil {
		for campRows.Next() {
			var code string
			var n int
			var amt float64
			if campRows.Scan(&code, &n, &amt) == nil {
				cb.WriteString(`<tr><td><code>` + htmlEsc(code) + `</code></td><td class="num">` +
					strconv.Itoa(n) + `</td><td class="num">` + money(amt) + `</td></tr>`)
				campCount++
			}
		}
		campRows.Close()
	}
	cb.WriteString(`</table>`)
	campTable := cb.String()
	if campCount == 0 {
		campTable = `<div class="empty">期間無已結帳工單</div>`
	}

	// ============ 折扣與保固（FR-FIN-07）============
	var discountTotal, warrantyRev float64
	var warrantyCount int
	db.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(discount_amount,0)),0) FROM work_orders
		WHERE date(created_at) BETWEEN ? AND ?`, from, to).Scan(&discountTotal)
	db.DB.QueryRow(`SELECT COUNT(1), COALESCE(SUM(total_amount),0) FROM work_orders
		WHERE is_warranty=1 AND date(created_at) BETWEEN ? AND ?`, from, to).Scan(&warrantyCount, &warrantyRev)

	// ============ 死頭料（FR-INV-04）============
	// 長期未動用 = 沒有任何異動紀錄，或最後一次異動在 180 天前。
	// inventory_movements 的外鍵是 inventory_item_id（不是 item_id），
	// 用錯欄位名會讓整個子查詢報錯、這張表靜默變空。
	deadRows, _ := db.DB.Query(`SELECT i.part_number, i.name, i.stock_quantity,
			COALESCE(i.cost_price,0) * i.stock_quantity AS dead_cost,
			COALESCE((SELECT MAX(im.created_at) FROM inventory_movements im
				WHERE im.inventory_item_id = i.id), '') AS last_move
		FROM inventory_items i
		WHERE i.stock_quantity > 0
			AND (last_move = '' OR date(last_move) < date('now','localtime','-180 day'))
		ORDER BY dead_cost DESC LIMIT 20`)
	var db2 strings.Builder
	db2.WriteString(`<table><tr><th>料號</th><th>名稱</th><th class="num">庫存</th><th class="num">滯銷成本</th><th>最後異動</th></tr>`)
	deadCount := 0
	deadCostTotal := 0.0
	if deadRows != nil {
		for deadRows.Next() {
			var part, name, lastMove string
			var stock int
			var cost float64
			if deadRows.Scan(&part, &name, &stock, &cost, &lastMove) == nil {
				lm := "從未異動"
				if t := parseFlexTime(lastMove); !t.IsZero() {
					lm = t.Format("2006-01-02")
				}
				db2.WriteString(`<tr><td><code>` + htmlEsc(part) + `</code></td><td>` + htmlEsc(name) +
					`</td><td class="num">` + strconv.Itoa(stock) + `</td><td class="num" style="color:#f87171">` +
					money(cost) + `</td><td style="font-size:12.5px;color:#94a3b8">` + lm + `</td></tr>`)
				deadCount++
				deadCostTotal += cost
			}
		}
		deadRows.Close()
	}
	db2.WriteString(`</table>`)
	deadTable := db2.String()
	if deadCount == 0 {
		deadTable = `<div class="empty">沒有長期未動用的零件</div>`
	}

	// ============ 渲染 ============
	granTab := func(label, key string) string {
		cls := "btn-s"
		if gran == key {
			cls = "btn-p"
		}
		return `<a class="btn ` + cls + `" href="/analytics?gran=` + key + `">` + label + `</a>`
	}

	var plBody strings.Builder
	plBody.WriteString(`<table><tr><th>類別</th><th class="num">營收</th><th class="num">成本</th><th class="num">毛利</th><th class="num">毛利率</th></tr>`)
	for _, p := range plRows {
		g := p.Revenue - p.Cost
		gp := 0.0
		if p.Revenue > 0 {
			gp = g / p.Revenue * 100
		}
		costCell := money(p.Cost)
		if p.Type != "part" {
			// 工資與外包在舊系統未記錄成本，明確標示而非顯示 0 假裝算過。
			costCell = `<span style="color:#64748b">未記錄</span>`
		}
		plBody.WriteString(`<tr><td>` + p.Label + `</td><td class="num">` + money(p.Revenue) +
			`</td><td class="num">` + costCell + `</td><td class="num">` + money(g) +
			`</td><td class="num">` + fmt.Sprintf("%.1f%%", gp) + `</td></tr>`)
	}
	plBody.WriteString(`<tr><td><b>合計</b></td><td class="num"><b>` + money(revenue) +
		`</b></td><td class="num"><b>` + money(partsCost) + `</b></td><td class="num"><b>` +
		money(gross) + `</b></td><td class="num"><b>` + fmt.Sprintf("%.1f%%", grossPct) + `</b></td></tr></table>`)

	body := flash(r) + `
    <div class="card">
      <h2>經營分析</h2>
      <div style="margin-bottom:12px">` +
		granTab("今日", "day") + ` ` + granTab("本週", "week") + ` ` + granTab("本月", "month") +
		`</div>
      <form method="GET" action="/analytics" style="margin-bottom:14px">
        <input type="hidden" name="gran" value="custom">
        <label style="font-size:12px;color:#94a3b8">起</label>
        <input name="from" value="` + htmlEsc(from) + `" placeholder="YYYY-MM-DD"
               style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:140px">
        <label style="font-size:12px;color:#94a3b8;margin-left:10px">迄</label>
        <input name="to" value="` + htmlEsc(to) + `" placeholder="YYYY-MM-DD"
               style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:140px">
        <button class="btn btn-p" type="submit">查詢</button>
        <a class="btn btn-s" href="/analytics/export?from=` + htmlEsc(from) + `&amp;to=` + htmlEsc(to) + `">匯出 CSV</a>
      </form>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">統計區間：` + htmlEsc(from) + ` ~ ` + htmlEsc(to) + `</p>

      <div style="margin-bottom:14px">
        <div class="stat"><b>` + money(aro) + `</b><span>ARO 平均工單金額</span></div>
        <div class="stat"><b>` + strconv.Itoa(orderCount) + `</b><span>車輛進場數</span></div>
        <div class="stat"><b>` + fmt.Sprintf("%.1f%%", grossPct) + `</b><span>毛利率</span></div>
        <div class="stat"><b>` + fmt.Sprintf("%.1f%%", repeatRate) + `</b><span>顧客回訪率</span></div>
      </div>
      <p style="color:#64748b;font-size:12.5px">
        毛利只計入零件成本（工資與外包在舊系統未記錄成本，故不計）。
        ARO 以已結帳工單計算，進場數含未結帳工單。
      </p>
    </div>

    <div class="card">
      <h2>損益分拆（工資 / 零件 / 外包）</h2>` + plBody.String() + `
    </div>

    <div class="card">
      <h2>顧客留存分析</h2>
      <div style="margin-bottom:12px">
        <div class="stat"><b>` + strconv.Itoa(activeCust) + `</b><span>期間消費客戶</span></div>
        <div class="stat"><b>` + strconv.Itoa(newCust) + `</b><span>新客</span></div>
        <div class="stat"><b>` + strconv.Itoa(oldCust) + `</b><span>舊客</span></div>
        <div class="stat"><b>` + strconv.Itoa(repeatCust) + `</b><span>回訪客戶</span></div>
        <div class="stat"><b>` + money(avgPerCust) + `</b><span>平均每客貢獻</span></div>
      </div>
    </div>

    <div class="card">
      <h2>行銷方案成效</h2>` + campTable + `
    </div>

    <div class="card">
      <h2>高頻維修車款 Top 10</h2>` + carTable + `
    </div>

    <div class="card">
      <h2>熱銷零件 Top 10</h2>` + partTable + `
    </div>

    <div class="card">
      <h2>折扣與保固</h2>
      <div style="margin-bottom:12px">
        <div class="stat"><b>` + money(discountTotal) + `</b><span>折扣總額</span></div>
        <div class="stat"><b>` + strconv.Itoa(warrantyCount) + `</b><span>保固工單數</span></div>
        <div class="stat"><b>` + money(warrantyRev) + `</b><span>保固工單金額</span></div>
      </div>
    </div>

    <div class="card">
      <h2>死頭料分析（180 天未動用，前 20 名）</h2>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">
        滯銷成本合計 <b style="color:#f87171">` + money(deadCostTotal) + `</b>，共 ` +
		strconv.Itoa(deadCount) + ` 項。這些是長期沒人領用的零件，可考慮退貨或降價出清。
      </p>` + deadTable + `
    </div>`

	renderPage(w, "經營分析", "/analytics", user.Username, user.Role, body)
}

// AnalyticsExport 匯出經營分析摘要為 CSV（FR-ANA-07 / FR-FIN-08 / FR-PRINT-07）。
// 用 BOM 開頭（\ufeff）讓 Excel 正確辨識 UTF-8，否則中文會變亂碼。
func AnalyticsExport(w http.ResponseWriter, r *http.Request) {
	user, err := currentUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if !canSeeMoney(user) {
		http.Error(w, "權限不足：經營分析僅限櫃檯人員與系統管理員使用", http.StatusForbidden)
		return
	}
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" || to == "" {
		from, to = periodRange("month", "", "")
	}

	var revenue, partsCost float64
	var paidCount, orderCount int
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount - COALESCE(discount_amount,0)),0), COUNT(1)
		FROM work_orders WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&revenue, &paidCount)
	db.DB.QueryRow(`SELECT COUNT(1) FROM work_orders WHERE date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&orderCount)
	db.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(i.cost_price,0) * woi.quantity),0)
		FROM work_order_items woi
		JOIN work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN inventory_items i ON i.id = woi.inventory_item_id
		WHERE woi.item_type='part' AND wo.status='paid'
			AND date(wo.created_at) BETWEEN ? AND ?`, from, to).Scan(&partsCost)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="analytics_`+from+`_`+to+`.csv"`)

	var b strings.Builder
	b.WriteString("指標,數值\n")
	b.WriteString("統計區間起," + csvField(from) + "\n")
	b.WriteString("統計區間迄," + csvField(to) + "\n")
	b.WriteString("營收（已結帳）," + fmt.Sprintf("%.0f", revenue) + "\n")
	b.WriteString("已結帳工單數," + strconv.Itoa(paidCount) + "\n")
	b.WriteString("車輛進場數（含未結帳）," + strconv.Itoa(orderCount) + "\n")
	aro := 0.0
	if paidCount > 0 {
		aro = revenue / float64(paidCount)
	}
	b.WriteString("ARO 平均工單金額," + fmt.Sprintf("%.0f", aro) + "\n")
	b.WriteString("零件成本," + fmt.Sprintf("%.0f", partsCost) + "\n")
	b.WriteString("毛利（營收-零件成本）," + fmt.Sprintf("%.0f", revenue-partsCost) + "\n")
	if revenue > 0 {
		b.WriteString("毛利率(%)," + fmt.Sprintf("%.1f", (revenue-partsCost)/revenue*100) + "\n")
	} else {
		b.WriteString("毛利率(%),0.0\n")
	}

	// 類別明細
	b.WriteString("\n類別,營收,成本,數量\n")
	rows, _ := db.DB.Query(`SELECT woi.item_type, COALESCE(SUM(woi.subtotal),0), COALESCE(SUM(woi.quantity),0),
			COALESCE(SUM(CASE WHEN woi.item_type='part' THEN COALESCE(i.cost_price,0)*woi.quantity ELSE 0 END),0)
		FROM work_order_items woi
		JOIN work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN inventory_items i ON i.id = woi.inventory_item_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY woi.item_type`, from, to)
	if rows != nil {
		for rows.Next() {
			var t string
			var rev, cost float64
			var qty int
			if rows.Scan(&t, &rev, &qty, &cost) == nil {
				label := itemTypeLabel[t]
				if label == "" {
					label = t
				}
				b.WriteString(csvField(label) + "," + fmt.Sprintf("%.0f", rev) + "," +
					fmt.Sprintf("%.0f", cost) + "," + strconv.Itoa(qty) + "\n")
			}
		}
		rows.Close()
	}

	// 工單明細（FR-FIN-08：工單編號、客戶、收入拆分、零件成本、支付方式、折扣）
	b.WriteString("\n工單編號,客戶,日期,營收,折扣,保固,零件成本\n")
	woRows, _ := db.DB.Query(`SELECT wo.id, COALESCE(c.name,''), date(wo.created_at),
			COALESCE(wo.total_amount,0), COALESCE(wo.discount_amount,0), COALESCE(wo.is_warranty,0),
			COALESCE((SELECT SUM(COALESCE(i.cost_price,0)*woi.quantity)
				FROM work_order_items woi
				LEFT JOIN inventory_items i ON i.id = woi.inventory_item_id
				WHERE woi.work_order_id = wo.id AND woi.item_type='part'),0)
		FROM work_orders wo LEFT JOIN customers c ON c.id = wo.customer_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		ORDER BY wo.id`, from, to)
	if woRows != nil {
		for woRows.Next() {
			var id int64
			var name, d string
			var total, disc, pcost float64
			var warr int
			if woRows.Scan(&id, &name, &d, &total, &disc, &warr, &pcost) == nil {
				b.WriteString(strconv.FormatInt(id, 10) + "," + csvField(name) + "," + csvField(d) + "," +
					fmt.Sprintf("%.0f", total) + "," + fmt.Sprintf("%.0f", disc) + "," +
					strconv.Itoa(warr) + "," + fmt.Sprintf("%.0f", pcost) + "\n")
			}
		}
		woRows.Close()
	}

	// BOM：Excel 靠它判斷 UTF-8，少了中文會變亂碼。
	w.Write([]byte("\ufeff"))
	w.Write([]byte(b.String()))
}

// csvField 處理 CSV 欄位：含逗號/引號/換行的值要用引號包住並把引號加倍。
func csvField(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
