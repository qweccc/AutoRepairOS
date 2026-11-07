// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
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
// T2.13 起的成本口徑（詳見 internal/handler/analytics_cost.go）：
//
//	毛利 = 營收 － 零件成本 － 外包成本 － 工資成本
//	工資成本 = 技師佣金（technician_worklogs.commission_amount）
//
// 零件／外包歸「工單建立日」，工資歸「工時日」（成本屬於實際施工日）。
// 期間內完全沒有技師工時紀錄時，成本與毛利顯示「未記錄」，不以 0 冒充——
// 0 一定會被看成「算過且成本為零」，而「未記錄」才會讓人知道要補資料。
//
// 口徑一致是這頁的鐵律：KPI 毛利率、損益分拆、月趨勢圖、分析摘要 CSV 全部
// 走同一套成本，任何一處分開算就會出現「同一頁兩個不一樣的毛利率」。
//
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
//
// T2.13：Cost 是三類成本中「歸給這一類」的部分——零件取庫存成本、外包取明細
// 小計、工資取技師佣金。CostRecorded 表示這筆成本是否真的有來源可算：
// 工資在完全沒有技師工時紀錄時為 false，畫面必須顯示「未記錄」而非 0，
// 否則會被當成「算過且成本為零」。
type plRow struct {
	Type         string
	Label        string
	Revenue      float64
	Cost         float64
	Quantity     int
	CostRecorded bool
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
	Revenue       float64
	Cost          float64 // 零件成本（歸工單建立月）
	Subcontract   float64 // 外包成本（歸工單建立月）
	Labor         float64 // 工資成本＝技師佣金（歸工時月，與上面兩者歸屬不同）
	LaborRecorded bool    // 當月是否有任何技師工時紀錄
}

// TotalCost 回傳三類成本合計。工資未記錄時只加已知的兩類。
func (m analyticsMonthTotal) TotalCost() float64 {
	return m.Cost + m.Subcontract + m.Labor
}

// Gross 回傳毛利；工資未記錄時回傳 0 並搭配 GrossRecorded 判斷。
func (m analyticsMonthTotal) Gross() float64 {
	return m.Revenue - m.TotalCost()
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

	// T2.13：圖表的毛利要與損益分拆、CSV 匯出同口徑，因此工資成本（技師佣金）
	// 也要入圖。它的歸屬是「工時月」而非「工單建立月」，與營收不同軸是刻意的
	// ——成本屬於實際施工日，強行塞回工單月會讓跨月工單的毛利失真。
	// 當月完全沒有工時紀錄時標記未記錄，不以 0 冒充已計算。
	if showCost && db.DB != nil {
		laborRows, err := db.DB.Query(`SELECT strftime('%Y-%m', work_date),
				COALESCE(SUM(commission_amount),0), COUNT(1)
			FROM active_technician_worklogs
			WHERE date(work_date) BETWEEN ? AND ?
			GROUP BY strftime('%Y-%m', work_date)`, from, to)
		if err == nil {
			for laborRows.Next() {
				var key string
				var labor float64
				var n int
				if laborRows.Scan(&key, &labor, &n) == nil {
					if total, ok := monthly[key]; ok {
						total.Labor = labor
						total.LaborRecorded = n > 0
						monthly[key] = total
					}
				}
			}
			laborRows.Close()
		}
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
			// 零件與外包都歸工單建立月（與營收同軸），工資另以工時月歸屬。
			monthTotal.Cost += row.PartsCost
			monthTotal.Subcontract += row.SubcontractCost
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
			gross := total.Gross()
			if maxRevenue > 0 && gross > 0 {
				grossSize = int(gross / maxRevenue * 160)
			}
		}
		center := int((float64(i) + 0.5) * band)
		costText := formatReportAmount(total.TotalCost())
		grossText := formatReportAmount(total.Gross())
		if showCost && !total.LaborRecorded {
			costText = "未記錄"
			grossText = "未記錄"
		}
		points = append(points, pages.AnalyticsTrendPoint{
			Label:        month,
			Revenue:      formatReportAmount(total.Revenue),
			Cost:         costText,
			Gross:        grossText,
			CostRecorded: !showCost || total.LaborRecorded,
			RevenueX:     center - 24,
			GrossX:       center + 2,
			LabelX:       center,
			RevenueY:     210 - revenueSize,
			GrossY:       210 - grossSize,
			RevenueSize:  revenueSize,
			GrossSize:    grossSize,
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
	validGrans := map[string]bool{"day": true, "week": true, "month": true, "year": true, "custom": true}
	if !validGrans[gran] {
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
	validGroups := map[string]bool{"kind": true, "customer": true, "vehicle": true}
	if !validGroups[groupBy] {
		groupBy = "kind"
	}
	showCost := canExportReportCosts(user)
	chartData, err := loadAnalyticsChartData(r.Context(), from, to, groupBy, showCost)
	if err != nil {
		http.Error(w, "經營分析圖表載入失敗", http.StatusInternalServerError)
		return
	}

	// ============ KPI（FR-ANA-04）============
	// ARO = 已結帳工單平均金額；進場數 = 期間開立的工單張數（含未結帳）。
	var revenue float64
	var paidCount, orderCount int
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount - COALESCE(discount_amount,0)),0), COUNT(1)
		FROM active_work_orders WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&revenue, &paidCount)
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_work_orders WHERE date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&orderCount)

	// T2.13：三類成本（零件／外包／工資）统一由 analyticsPeriodCosts 計算，
	// 口径與損益分拆、分析摘要 CSV 相同，避免同一頁出現兩個不一樣的毛利率。
	// 工资沒有工時紀錄時不可用 0 冒充，改以「未記錄」顯示。
	var costs analyticsCosts
	if showCost {
		costs = analyticsPeriodCosts(from, to)
	}

	aro := 0.0
	if paidCount > 0 {
		aro = revenue / float64(paidCount)
	}
	gross := revenue - costs.Total()
	grossPct := 0.0
	if revenue > 0 {
		grossPct = gross / revenue * 100
	}

	// ============ 損益分拆（FR-ANA-01 / FR-FIN-06 / FR-ANA-07）============
	// T2.13：三類成本都要納入，不再只有零件成本。
	// 零件／外包由同一條查詢带回（各自獨立 CASE），工资由 analyticsPeriodCosts
	// 以 work_date 歸屬（與零件／外包的工單建立日歸屬刻意不同，見該函式註解）。
	plRows := []plRow{
		{Type: "labor", Label: itemTypeLabel["labor"], CostRecorded: false},
		{Type: "part", Label: itemTypeLabel["part"], CostRecorded: true},
		{Type: "subcontract", Label: itemTypeLabel["subcontract"], CostRecorded: true},
	}
	plIdx := map[string]int{}
	for i, p := range plRows {
		plIdx[p.Type] = i
	}
	itemRows, _ := db.DB.Query(`SELECT woi.item_type,
			COALESCE(SUM(woi.subtotal),0), COALESCE(SUM(woi.quantity),0),
			COALESCE(SUM(CASE WHEN woi.item_type='part'
				THEN COALESCE(i.cost_price,0) * woi.quantity ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN woi.item_type='subcontract'
				THEN woi.subtotal ELSE 0 END),0)
		FROM work_order_items woi
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY woi.item_type`, from, to)
	if itemRows != nil {
		for itemRows.Next() {
			var t string
			var rev, partCost, subCost float64
			var qty int
			if itemRows.Scan(&t, &rev, &qty, &partCost, &subCost) == nil {
				if i, ok := plIdx[t]; ok {
					plRows[i].Revenue = rev
					plRows[i].Quantity = qty
					switch t {
					case "part":
						plRows[i].Cost = partCost
					case "subcontract":
						plRows[i].Cost = subCost
					}
				}
			}
		}
		itemRows.Close()
	}
	// 工資成本：技師佣金。完全沒有工時紀錄時標成未記錄（不可用 0 冒充）。
	if i, ok := plIdx["labor"]; ok {
		plRows[i].Cost = costs.LaborCost
		plRows[i].CostRecorded = costs.LaborRecorded
	}

	// ============ 高頻維修車款（FR-ANA-03）============
	// vehicles 只有 brand_model（廠牌車型存在同一欄），沒有獨立的 make/model 欄。
	// 用錯欄位名會讓整條查詢失敗、這張表靜默變空。
	carRows, _ := db.DB.Query(`SELECT COALESCE(v.brand_model,'(未填車型)'), COUNT(1)
		FROM active_work_orders wo JOIN active_vehicles v ON v.id = wo.vehicle_id
		WHERE date(wo.created_at) BETWEEN ? AND ?
		GROUP BY COALESCE(v.brand_model,'(未填車型)')
		ORDER BY COUNT(1) DESC LIMIT 10`, from, to)
	var carTable []pages.Phase5Table
	var carData [][]pages.Phase5Cell
	if carRows != nil {
		for carRows.Next() {
			var mdl string
			var n int
			if carRows.Scan(&mdl, &n) == nil {
				carData = append(carData, []pages.Phase5Cell{{Value: mdl}, {Value: strconv.Itoa(n), Class: "num"}})
			}
		}
		carRows.Close()
	}
	carTable = append(carTable, pages.Phase5Table{Headers: []string{"車型", "進廠次數"}, Rows: carData, Empty: "期間無維修紀錄"})

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
	var partData [][]pages.Phase5Cell
	if partRows != nil {
		for partRows.Next() {
			var name string
			var qty int
			var amt float64
			if partRows.Scan(&name, &qty, &amt) == nil {
				partData = append(partData, []pages.Phase5Cell{{Value: name}, {Value: strconv.Itoa(qty), Class: "num"}, {Value: money(amt), Class: "num"}})
			}
		}
		partRows.Close()
	}
	partTable := []pages.Phase5Table{{Headers: []string{"零件", "數量", "營收"}, Rows: partData, Empty: "期間無零件銷售"}}

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

	// ============ 行銷方案成效（FR-ANA-06 / T2.18）============
	// 由 analytics_campaign.go 的 campaignAnalysisBlock 產生——工單數、營收之外，
	// 依 T2.13 的成本口徑補上成本與毛利（同一個方案的成本用批次查詢取回，不做 N+1）。
	campTable := campaignAnalysisComponent(from, to, showCost)

	// ============ 技師效率（FR-ANA-02 / T2.17）============
	// 由 analytics_tech.go 的 technicianEfficiencyBlock 產生。
	techEfficiencyTable := technicianEfficiencyComponent(from, to, showCost)

	// ============ 折扣與保固（FR-FIN-07 / T2.14 保固成本統計）============
	var discountTotal float64
	db.DB.QueryRow(`SELECT COALESCE(SUM(COALESCE(discount_amount,0)),0) FROM active_work_orders
		WHERE date(created_at) BETWEEN ? AND ?`, from, to).Scan(&discountTotal)

	// 保固收入用「折扣後」金額（warrantyStats 內），與損益分拆、方案毛利同口徑——
	// 若用未折扣金額，同一張保固工單在「保固毛利」與「損益毛利」會算出兩個不同數字。
	warrantyCount, warrantyRev, warrantyDiscount := warrantyStats(from, to)
	warrantyCostTotal, warrantyLaborRecorded := warrantyCostTotals(from, to)
	warrantyGross := warrantyRev - warrantyCostTotal

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
	var deadData [][]pages.Phase5Cell
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
				deadData = append(deadData, []pages.Phase5Cell{{Value: part}, {Value: name}, {Value: strconv.Itoa(stock), Class: "num"}, {Value: money(cost), Class: "num"}, {Value: lm}})
				deadCount++
				deadCostTotal += cost
			}
		}
		deadRows.Close()
	}
	deadTable := []pages.Phase5Table{{Headers: []string{"料號", "名稱", "庫存", "滯銷成本", "最後異動"}, Rows: deadData, Empty: "沒有長期未動用的零件"}}

	plHeaders := []string{"類別", "營收"}
	if showCost {
		plHeaders = append(plHeaders, "成本", "毛利", "毛利率")
	}
	var plData [][]pages.Phase5Cell
	for _, p := range plRows {
		row := []pages.Phase5Cell{{Value: p.Label}, {Value: money(p.Revenue), Class: "num"}}
		if showCost {
			if !p.CostRecorded {
				row = append(row, pages.Phase5Cell{Value: "未記錄", Class: "num"}, pages.Phase5Cell{Value: "未記錄", Class: "num"}, pages.Phase5Cell{Value: "—", Class: "num"})
			} else {
				g := p.Revenue - p.Cost
				gp := 0.0
				if p.Revenue > 0 {
					gp = g / p.Revenue * 100
				}
				row = append(row, pages.Phase5Cell{Value: money(p.Cost), Class: "num"}, pages.Phase5Cell{Value: money(g), Class: "num"}, pages.Phase5Cell{Value: fmt.Sprintf("%.1f%%", gp), Class: "num"})
			}
		}
		plData = append(plData, row)
	}
	totalRow := []pages.Phase5Cell{{Value: "合計"}, {Value: money(revenue), Class: "num"}}
	if showCost {
		if costs.LaborRecorded {
			totalRow = append(totalRow, pages.Phase5Cell{Value: money(costs.Total()), Class: "num"}, pages.Phase5Cell{Value: money(gross), Class: "num"}, pages.Phase5Cell{Value: fmt.Sprintf("%.1f%%", grossPct), Class: "num"})
		} else {
			totalRow = append(totalRow, pages.Phase5Cell{Value: "未記錄", Class: "num"}, pages.Phase5Cell{Value: "未記錄", Class: "num"}, pages.Phase5Cell{Value: "—", Class: "num"})
		}
	}
	plData = append(plData, totalRow)
	plTable := []pages.Phase5Table{{Headers: plHeaders, Rows: plData}}

	grossNote := `成本與毛利僅限系統管理員檢視。`
	if showCost {
		grossNote = `毛利＝營收－零件成本－外包成本－工資成本（工資為技師佣金）。` +
			`工資若該期間沒有任何技師工時紀錄，會顯示「未記錄」而不是 0，` +
			`此時毛利與毛利率同樣標記未記錄，不以 0 冒充已計算。`
	}
	_ = pages.AnalyticsPage(pages.Phase5AnalyticsData{
		From: from, To: to, GroupBy: groupBy, Gran: gran, ShowCost: showCost,
		Chart:      pages.AnalyticsCharts(chartData.Points, chartData.Groups, chartData.SVGWidth, showCost),
		ProfitLoss: plTable, Cars: carTable, Parts: partTable, Dead: deadTable,
		ActiveCust: strconv.Itoa(activeCust), NewCust: strconv.Itoa(newCust), OldCust: strconv.Itoa(oldCust), RepeatCust: strconv.Itoa(repeatCust), AvgPerCust: money(avgPerCust),
		DiscountTotal: money(discountTotal), WarrantyCount: strconv.Itoa(warrantyCount), WarrantyRevenue: money(warrantyRev), WarrantyDiscount: money(warrantyDiscount),
		WarrantyCostTotal: money(warrantyCostTotal), WarrantyGross: money(warrantyGross), WarrantyLaborRecorded: warrantyLaborRecorded,
		DeadCostTotal: money(deadCostTotal), DeadCount: strconv.Itoa(deadCount), Campaign: campTable, Technician: techEfficiencyTable,
		Flash: flash(r), ARO: money(aro), OrderCount: strconv.Itoa(orderCount), GrossStat: grossStatValue(showCost, costs, grossPct), RepeatRate: fmt.Sprintf("%.1f%%", repeatRate), GrossNote: grossNote, CanExportCosts: showCost,
	}, user).Render(r.Context(), w)
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

	// T2.13：三類成本由 analyticsPeriodCosts 統一計算，與頁面損益分拆同口徑。
	costs := analyticsPeriodCosts(from, to)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="analytics_`+from+`_`+to+`.csv"`)

	var revenue float64
	var paidCount, orderCount int
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount - COALESCE(discount_amount,0)),0), COUNT(1)
		FROM active_work_orders WHERE status='paid' AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&revenue, &paidCount)
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_work_orders WHERE date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&orderCount)

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
	// T2.13：三類成本分列，毛利＝營收－三類成本。工資未記錄時標記未記錄（不以 0 冒充）。
	b.WriteString("零件成本," + fmt.Sprintf("%.0f", costs.PartsCost) + "\n")
	b.WriteString("外包成本," + fmt.Sprintf("%.0f", costs.SubcontractCost) + "\n")
	if costs.LaborRecorded {
		b.WriteString("工資成本（技師佣金）," + fmt.Sprintf("%.0f", costs.LaborCost) + "\n")
		b.WriteString("毛利（營收-三類成本）," + fmt.Sprintf("%.0f", revenue-costs.Total()) + "\n")
	} else {
		b.WriteString("工資成本（技師佣金）,未記錄\n")
		b.WriteString("毛利（營收-三類成本）,未記錄\n")
	}
	if revenue > 0 && costs.LaborRecorded {
		b.WriteString("毛利率(%)," + fmt.Sprintf("%.1f", (revenue-costs.Total())/revenue*100) + "\n")
	} else {
		b.WriteString("毛利率(%),未記錄\n")
	}

	// 類別明細
	b.WriteString("\n類別,營收,成本,數量\n")
	rows, _ := db.DB.Query(`SELECT woi.item_type, COALESCE(SUM(woi.subtotal),0), COALESCE(SUM(woi.quantity),0),
			COALESCE(SUM(CASE WHEN woi.item_type='part' THEN COALESCE(i.cost_price,0)*woi.quantity ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN woi.item_type='subcontract' THEN woi.subtotal ELSE 0 END),0)
		FROM work_order_items woi
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY woi.item_type`, from, to)
	if rows != nil {
		for rows.Next() {
			var t string
			var rev, partCost, subCost float64
			var qty int
			if rows.Scan(&t, &rev, &qty, &partCost, &subCost) == nil {
				label := itemTypeLabel[t]
				if label == "" {
					label = t
				}
				// 工資成本是「期間匯總的技師佣金」（見上方指標區），無法在不造假的前提下
				// 拆給明細列，因此該列顯示「-」而不是 0。
				costCell := fmt.Sprintf("%.0f", partCost+subCost)
				if t == "labor" {
					costCell = "-"
				}
				b.WriteString(csvField(label) + "," + fmt.Sprintf("%.0f", rev) + "," +
					costCell + "," + strconv.Itoa(qty) + "\n")
			}
		}
		rows.Close()
	}

	// 工單明細（FR-FIN-08：工單編號、客戶、收入拆分、零件成本、支付方式、折扣）
	// T2.13：多列外包成本（與零件成本同一條查詢可取得）。工資成本為期間匯總的
	// 技師佣金，無法誠實歸給單張工單時不硬湊，故不在此列出。
	b.WriteString("\n工單編號,客戶,日期,營收,折扣,保固,零件成本,外包成本\n")
	woRows, _ := db.DB.Query(`SELECT wo.id, COALESCE(c.name,''), date(wo.created_at),
			COALESCE(wo.total_amount,0), COALESCE(wo.discount_amount,0), COALESCE(wo.is_warranty,0),
			COALESCE((SELECT SUM(COALESCE(i.cost_price,0)*woi.quantity)
				FROM work_order_items woi
				LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
				WHERE woi.work_order_id = wo.id AND woi.item_type='part'),0),
			COALESCE((SELECT SUM(sub.subtotal) FROM work_order_items sub
				WHERE sub.work_order_id = wo.id AND sub.item_type='subcontract'),0)
		FROM active_work_orders wo LEFT JOIN active_customers c ON c.id = wo.customer_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		ORDER BY wo.id`, from, to)
	if woRows != nil {
		for woRows.Next() {
			var id int64
			var name, d string
			var total, disc, pcost, subCost float64
			var warr int
			if woRows.Scan(&id, &name, &d, &total, &disc, &warr, &pcost, &subCost) == nil {
				b.WriteString(strconv.FormatInt(id, 10) + "," + csvField(name) + "," + csvField(d) + "," +
					fmt.Sprintf("%.0f", total) + "," + fmt.Sprintf("%.0f", disc) + "," +
					strconv.Itoa(warr) + "," + fmt.Sprintf("%.0f", pcost) + "," +
					fmt.Sprintf("%.0f", subCost) + "\n")
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
