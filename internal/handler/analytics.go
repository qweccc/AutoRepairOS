// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
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
