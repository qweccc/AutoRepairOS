package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

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
