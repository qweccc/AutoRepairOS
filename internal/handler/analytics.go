// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
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
// FR-ANA-01 圖表沿用 reports_export.go 的已結帳工單、建立日期、折扣及零件成本口徑。
// T2.15 工單 CSV 沒有外包成本欄，依需求以 CSV 口徑優先，圖表毛利只扣零件成本。
// 工單分組沿用 work_order_items.item_type 的既有 labor/part/subcontract 值；
// customer.tags 為逗號分隔標籤，vehicle.vehicle_type 以 lookup label 顯示。
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
	case "year":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -11, 0)
		return start.Format("2006-01-02"), now.Format("2006-01-02")
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

type analyticsChartDimensions struct {
	CustomerTags string
	VehicleType  string
}

type analyticsMonthTotal struct {
	Revenue float64
	Cost    float64
}

type analyticsGroupTotal struct {
	Revenue float64
	Cost    float64
	Orders  map[int64]struct{}
}

type analyticsChartData struct {
	Points   []pages.AnalyticsTrendPoint
	Groups   []pages.AnalyticsGroupRow
	SVGWidth int
}

func loadAnalyticsChartData(ctx context.Context, from, to, groupBy string, showCost bool) (analyticsChartData, error) {
	// Keep the paid-order population, revenue split, discount, and parts-cost
	// definitions tied to the T2.15 CSV loader. That CSV has no subcontract-cost
	// column, so its documented export basis takes precedence here: gross profit
	// subtracts parts cost only rather than presenting a non-reconciling figure.
	rows, err := loadReportExportRows(ctx, from, to, showCost)
	if err != nil {
		return analyticsChartData{}, fmt.Errorf("load analytics report rows: %w", err)
	}
	dimensions := make(map[int64]analyticsChartDimensions, len(rows))
	dimensionRows, err := db.DB.QueryContext(ctx, `SELECT wo.id, COALESCE(c.tags,''),
			COALESCE((SELECT li.label FROM lookup_items li
				WHERE li.category='vehicle_type' AND li.code=v.vehicle_type
				ORDER BY li.sort_order LIMIT 1), NULLIF(v.vehicle_type,''), '(未填車款)')
		FROM active_work_orders wo
		LEFT JOIN active_customers c ON c.id=wo.customer_id
		LEFT JOIN active_vehicles v ON v.id=wo.vehicle_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?`, from, to)
	if err != nil {
		return analyticsChartData{}, fmt.Errorf("query analytics chart dimensions: %w", err)
	}
	for dimensionRows.Next() {
		var id int64
		var item analyticsChartDimensions
		if err := dimensionRows.Scan(&id, &item.CustomerTags, &item.VehicleType); err != nil {
			_ = dimensionRows.Close()
			return analyticsChartData{}, fmt.Errorf("scan analytics chart dimensions: %w", err)
		}
		dimensions[id] = item
	}
	if err := dimensionRows.Err(); err != nil {
		_ = dimensionRows.Close()
		return analyticsChartData{}, fmt.Errorf("read analytics chart dimensions: %w", err)
	}
	if err := dimensionRows.Close(); err != nil {
		return analyticsChartData{}, fmt.Errorf("close analytics chart dimensions: %w", err)
	}
	return aggregateAnalyticsChart(rows, dimensions, from, to, groupBy, showCost)
}

func aggregateAnalyticsChart(rows []reportExportRow, dimensions map[int64]analyticsChartDimensions, from, to, groupBy string, showCost bool) (analyticsChartData, error) {
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return analyticsChartData{}, fmt.Errorf("parse analytics start date: %w", err)
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return analyticsChartData{}, fmt.Errorf("parse analytics end date: %w", err)
	}
	if start.After(end) {
		return analyticsChartData{}, fmt.Errorf("analytics start date is after end date")
	}

	months := make([]string, 0)
	monthly := make(map[string]analyticsMonthTotal)
	for month := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC); !month.After(time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)); month = month.AddDate(0, 1, 0) {
		key := month.Format("2006-01")
		months = append(months, key)
		monthly[key] = analyticsMonthTotal{}
	}

	groups := make(map[string]*analyticsGroupTotal)
	groupLabels := make([]string, 0)
	addGroup := func(label string, id int64, revenue, cost float64) {
		total, ok := groups[label]
		if !ok {
			total = &analyticsGroupTotal{Orders: make(map[int64]struct{})}
			groups[label] = total
			groupLabels = append(groupLabels, label)
		}
		total.Revenue += revenue
		if showCost {
			total.Cost += cost
		}
		total.Orders[id] = struct{}{}
	}

	for _, row := range rows {
		netRevenue := row.Total - row.Discount
		month := row.Date[:min(len(row.Date), 7)]
		monthTotal, ok := monthly[month]
		if !ok {
			continue
		}
		monthTotal.Revenue += netRevenue
		if showCost {
			monthTotal.Cost += row.PartsCost
		}
		monthly[month] = monthTotal

		dimension := dimensions[row.WorkOrderID]
		switch groupBy {
		case "kind":
			// Work orders have no separate kind column; use the three existing
			// work_order_items.item_type values and prorate discounts to keep totals reconciled.
			items := []struct {
				kind   string
				amount float64
			}{
				{"labor", row.LaborRevenue},
				{"part", row.PartsRevenue},
				{"subcontract", row.Subcontract},
			}
			itemRevenue := row.LaborRevenue + row.PartsRevenue + row.Subcontract
			if itemRevenue <= 0 {
				addGroup("(未分類)", row.WorkOrderID, netRevenue, row.PartsCost)
				continue
			}
			for _, item := range items {
				if item.amount == 0 {
					continue
				}
				allocatedRevenue := netRevenue * item.amount / itemRevenue
				cost := 0.0
				if item.kind == "part" {
					cost = row.PartsCost
				}
				addGroup(itemTypeLabel[item.kind], row.WorkOrderID, allocatedRevenue, cost)
			}
		case "customer":
			tags := splitAnalyticsTags(dimension.CustomerTags)
			share := 1 / float64(len(tags))
			for _, tag := range tags {
				addGroup(tag, row.WorkOrderID, netRevenue*share, row.PartsCost*share)
			}
		case "vehicle":
			label := strings.TrimSpace(dimension.VehicleType)
			if label == "" {
				label = "(未填車款)"
			}
			addGroup(label, row.WorkOrderID, netRevenue, row.PartsCost)
		}
	}

	var maxRevenue float64
	for _, total := range monthly {
		if total.Revenue > maxRevenue {
			maxRevenue = total.Revenue
		}
	}
	if maxRevenue < 0 {
		maxRevenue = 0
	}
	width := max(960, len(months)*72+48)
	band := float64(width) / float64(max(1, len(months)))
	points := make([]pages.AnalyticsTrendPoint, 0, len(months))
	for i, month := range months {
		total := monthly[month]
		revenueSize := 0
		grossSize := 0
		if maxRevenue > 0 && total.Revenue > 0 {
			revenueSize = int(total.Revenue / maxRevenue * 160)
		}
		if showCost {
			gross := total.Revenue - total.Cost
			if maxRevenue > 0 && gross > 0 {
				grossSize = int(gross / maxRevenue * 160)
			}
		}
		center := int((float64(i) + 0.5) * band)
		points = append(points, pages.AnalyticsTrendPoint{
			Label:       month,
			Revenue:     formatReportAmount(total.Revenue),
			Cost:        formatReportAmount(total.Cost),
			Gross:       formatReportAmount(total.Revenue - total.Cost),
			RevenueX:    center - 24,
			GrossX:      center + 2,
			LabelX:      center,
			RevenueY:    210 - revenueSize,
			GrossY:      210 - grossSize,
			RevenueSize: revenueSize,
			GrossSize:   grossSize,
		})
	}

	sort.Slice(groupLabels, func(i, j int) bool {
		if groups[groupLabels[i]].Revenue == groups[groupLabels[j]].Revenue {
			return groupLabels[i] < groupLabels[j]
		}
		return groups[groupLabels[i]].Revenue > groups[groupLabels[j]].Revenue
	})
	var groupMax float64
	for _, total := range groups {
		if total.Revenue > groupMax {
			groupMax = total.Revenue
		}
	}
	groupRows := make([]pages.AnalyticsGroupRow, 0, len(groupLabels))
	for _, label := range groupLabels {
		total := groups[label]
		width := 0
		if groupMax > 0 && total.Revenue > 0 {
			width = int(total.Revenue / groupMax * 100)
		}
		groupRows = append(groupRows, pages.AnalyticsGroupRow{
			Label:        label,
			Orders:       strconv.Itoa(len(total.Orders)),
			Revenue:      formatReportAmount(total.Revenue),
			Cost:         formatReportAmount(total.Cost),
			Gross:        formatReportAmount(total.Revenue - total.Cost),
			RevenueWidth: width,
		})
	}
	return analyticsChartData{Points: points, Groups: groupRows, SVGWidth: width}, nil
}

func splitAnalyticsTags(tags string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, tag := range strings.Split(tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	if len(result) == 0 {
		return []string{"(未標記)"}
	}
	return result
}

func selectedAttr(selected bool) string {
	if selected {
		return ` selected`
	}
	return ""
}

func ownerOnlyStat(user model.User, value, label string) string {
	if !canExportReportCosts(user) {
		return ""
	}
	return `<div class="stat"><b>` + value + `</b><span>` + label + `</span></div>`
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
		gran = "year"
	}
	if gran != "day" && gran != "week" && gran != "month" && gran != "year" && gran != "custom" {
		gran = "year"
	}
	if gran == "custom" {
		var err error
		from, to, err = reportExportPeriod(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		from, to = periodRange(gran, from, to)
	}
	groupBy := strings.TrimSpace(qv.Get("group"))
	if groupBy != "kind" && groupBy != "customer" && groupBy != "vehicle" {
		groupBy = "kind"
	}
	showCost := canExportReportCosts(user)
	chartData, err := loadAnalyticsChartData(r.Context(), from, to, groupBy, showCost)
	if err != nil {
		http.Error(w, "經營分析圖表載入失敗", http.StatusInternalServerError)
		return
	}
	var chartHTML bytes.Buffer
	if err := pages.AnalyticsCharts(chartData.Points, chartData.Groups, chartData.SVGWidth, showCost).Render(r.Context(), &chartHTML); err != nil {
		http.Error(w, "經營分析圖表載入失敗", http.StatusInternalServerError)
		return
	}

	// ============ KPI（FR-ANA-04）============
	// ARO = 已結帳工單平均金額；進場數 = 期間開立的工單張數（含未結帳）。
	var revenue, partsCost float64
	var paidCount, orderCount int
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount - COALESCE(discount_amount,0)),0), COUNT(1)
		FROM active_work_orders WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&revenue, &paidCount)
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_work_orders WHERE date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&orderCount)

	// 零件成本：從 work_order_items.inventory_item_id 關回庫存主檔帶 cost_price。
	// 找不到料號的零件不計成本（寧可低估毛利也不造假）。
	if showCost {
		db.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(i.cost_price,0) * woi.quantity),0)
			FROM work_order_items woi
			JOIN active_work_orders wo ON wo.id = woi.work_order_id
			LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
			WHERE woi.item_type='part' AND wo.status='paid'
				AND date(wo.created_at) BETWEEN ? AND ?`, from, to).Scan(&partsCost)
	}

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
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
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
		FROM active_work_orders wo JOIN active_vehicles v ON v.id = wo.vehicle_id
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
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
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
		FROM active_work_orders wo
		WHERE wo.status='paid'
		GROUP BY wo.customer_id
		HAVING MIN(date(wo.created_at)) BETWEEN ? AND ?)`, from, to).Scan(&newCust)
	db.DB.QueryRow(`SELECT COUNT(1) FROM (
		SELECT wo.customer_id
		FROM active_work_orders wo
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY wo.customer_id
		HAVING MIN(date(wo.created_at)) < ?)`, from, to, from).Scan(&oldCust)
	db.DB.QueryRow(`SELECT COUNT(DISTINCT customer_id) FROM active_work_orders
		WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).Scan(&activeCust)
	db.DB.QueryRow(`SELECT COUNT(1) FROM (
		SELECT customer_id FROM active_work_orders
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
		FROM active_work_orders wo
		LEFT JOIN active_customers c ON c.id = wo.customer_id
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
	db.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(discount_amount,0)),0) FROM active_work_orders
		WHERE date(created_at) BETWEEN ? AND ?`, from, to).Scan(&discountTotal)
	db.DB.QueryRow(`SELECT COUNT(1), COALESCE(SUM(total_amount),0) FROM active_work_orders
		WHERE is_warranty=1 AND date(created_at) BETWEEN ? AND ?`, from, to).Scan(&warrantyCount, &warrantyRev)

	// ============ 死頭料（FR-INV-04）============
	// 長期未動用 = 沒有任何異動紀錄，或最後一次異動在 180 天前。
	// inventory_movements 的外鍵是 inventory_item_id（不是 item_id），
	// 用錯欄位名會讓整個子查詢報錯、這張表靜默變空。
	deadRows, _ := db.DB.Query(`SELECT i.part_number, i.name, i.stock_quantity,
			COALESCE(i.cost_price,0) * i.stock_quantity AS dead_cost,
			COALESCE((SELECT MAX(im.created_at) FROM inventory_movements im
				WHERE im.inventory_item_id = i.id), '') AS last_move
		FROM active_inventory_items i
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
		return `<a class="btn ` + cls + `" href="/analytics?gran=` + key + `&amp;group=` + groupBy + `">` + label + `</a>`
	}

	var plBody strings.Builder
	plBody.WriteString(`<table><tr><th>類別</th><th class="num">營收</th>`)
	if showCost {
		plBody.WriteString(`<th class="num">成本</th><th class="num">毛利</th><th class="num">毛利率</th>`)
	}
	plBody.WriteString(`</tr>`)
	for _, p := range plRows {
		g := p.Revenue - p.Cost
		gp := 0.0
		if p.Revenue > 0 {
			gp = g / p.Revenue * 100
		}
		plBody.WriteString(`<tr><td>` + p.Label + `</td><td class="num">` + money(p.Revenue) + `</td>`)
		if showCost {
			costCell := money(p.Cost)
			if p.Type != "part" {
				// 工資與外包成本未出現在 T2.15 CSV，避免把缺失成本顯示成零。
				costCell = `<span style="color:#64748b">未記錄</span>`
			}
			plBody.WriteString(`<td class="num">` + costCell + `</td><td class="num">` + money(g) +
				`</td><td class="num">` + fmt.Sprintf("%.1f%%", gp) + `</td>`)
		}
		plBody.WriteString(`</tr>`)
	}
	plBody.WriteString(`<tr><td><b>合計</b></td><td class="num"><b>` + money(revenue) + `</b></td>`)
	if showCost {
		plBody.WriteString(`<td class="num"><b>` + money(partsCost) + `</b></td><td class="num"><b>` +
			money(gross) + `</b></td><td class="num"><b>` + fmt.Sprintf("%.1f%%", grossPct) + `</b></td>`)
	}
	plBody.WriteString(`</tr></table>`)

	exportLink := `<a class="btn btn-s" href="/reports/export?from=` + htmlEsc(from) + `&amp;to=` + htmlEsc(to) + `">匯出工單 CSV</a>`
	if canExportReportCosts(user) {
		exportLink += ` <a class="btn btn-s" href="/analytics/export?from=` + htmlEsc(from) + `&amp;to=` + htmlEsc(to) + `">匯出分析摘要 CSV</a>`
	}
	grossNote := `成本與毛利僅限系統管理員檢視。`
	if showCost {
		grossNote = `毛利與報表匯出一致，僅扣除 CSV 有列出的零件成本；CSV 未列外包成本，因此不計入。`
	}
	body := flash(r) + `
    <div class="card">
      <h2>經營分析</h2>
      <div style="margin-bottom:12px">` +
		granTab("今日", "day") + ` ` + granTab("本週", "week") + ` ` + granTab("本月", "month") +
		` ` + granTab("近 12 個月", "year") +
		`</div>
      <form method="GET" action="/analytics" style="margin-bottom:14px">
        <input type="hidden" name="gran" value="custom">
        <input type="hidden" name="group" value="` + htmlEsc(groupBy) + `">
        <label style="font-size:12px;color:#94a3b8">起</label>
        <input name="from" value="` + htmlEsc(from) + `" placeholder="YYYY-MM-DD"
               style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:140px">
        <label style="font-size:12px;color:#94a3b8;margin-left:10px">迄</label>
        <input name="to" value="` + htmlEsc(to) + `" placeholder="YYYY-MM-DD"
               style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:140px">
        <label style="font-size:12px;color:#94a3b8;margin-left:10px">分組</label>
        <select name="group" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0">
          <option value="kind"` + selectedAttr(groupBy == "kind") + `>工單項目類型</option>
          <option value="customer"` + selectedAttr(groupBy == "customer") + `>客群標籤</option>
          <option value="vehicle"` + selectedAttr(groupBy == "vehicle") + `>車種</option>
        </select>
        <button class="btn btn-p" type="submit">查詢</button>
        ` + exportLink + `
      </form>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">統計區間：` + htmlEsc(from) + ` ~ ` + htmlEsc(to) + `</p>

      <div style="margin-bottom:14px">
        <div class="stat"><b>` + money(aro) + `</b><span>ARO 平均工單金額</span></div>
        <div class="stat"><b>` + strconv.Itoa(orderCount) + `</b><span>車輛進場數</span></div>
        ` + ownerOnlyStat(user, fmt.Sprintf("%.1f%%", grossPct), "毛利率") + `
        <div class="stat"><b>` + fmt.Sprintf("%.1f%%", repeatRate) + `</b><span>顧客回訪率</span></div>
      </div>
      <p style="color:#64748b;font-size:12.5px">
        ` + grossNote + `
        ARO 以已結帳工單計算，進場數含未結帳工單。
      </p>
    </div>` + chartHTML.String() + `

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

    </div>`
	if showCost {
		body += `
    <div class="card">
      <h2>死頭料分析（180 天未動用，前 20 名）</h2>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">
        滯銷成本合計 <b style="color:#f87171">` + money(deadCostTotal) + `</b>，共 ` +
			strconv.Itoa(deadCount) + ` 項。這些是長期沒人領用的零件，可考慮退貨或降價出清。
      </p>` + deadTable + `
    </div>`
	}

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
	if !canExportReportCosts(user) {
		http.Error(w, "權限不足：含成本的經營分析匯出僅限系統管理員使用", http.StatusForbidden)
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
		FROM active_work_orders WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&revenue, &paidCount)
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_work_orders WHERE date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&orderCount)
	db.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(i.cost_price,0) * woi.quantity),0)
		FROM work_order_items woi
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
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
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
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
				LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
				WHERE woi.work_order_id = wo.id AND woi.item_type='part'),0)
		FROM active_work_orders wo LEFT JOIN active_customers c ON c.id = wo.customer_id
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
	s = safeSpreadsheetText(s)
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
