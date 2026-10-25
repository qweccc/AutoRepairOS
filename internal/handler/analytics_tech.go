package handler

// T2.17 技師效率指標（FR-ANA-02）
//
// 依技師呈現：完成工單數、工時總計、每工時計價、每工時計毛利。
//
// 計算規則與口徑定義（與 analytics_cost.go 成本口徑保持一致）：
//
//   1. 完成工單依據：以 technician_worklogs 為主要依據（非 work_orders.primary_technician_id）。
//   2. 完成工單數：COUNT(DISTINCT wl.work_order_id)。多人共同派工時，同一工單各自計入
//      參與技師名下，但同一技師對同一工單的多筆工時只計為 1 張，不重複計數。
//   3. 工時總計：SUM(wl.hours)。
//   4. 每工時計價：SUM(wl.labor_amount) / SUM(wl.hours)。為技師加權平均時薪；
//      若工時為 0 則顯示「—」，避免除以零。
//   5. 每工時計毛利：(SUM(wl.labor_amount) - SUM(wl.commission_amount)) / SUM(wl.hours)。
//      即工資收入扣除佣金成本後除以總工時。若技師無工時紀錄，以 costLabel 顯示「未記錄」；
//      若有紀錄但工時為 0 則顯示「—」；若佣金為 0 但有工時紀錄，佣金顯示 0 而非未記錄。
//   6. 跨期歸屬：以 wl.work_date 為準（實際施工日），與工資成本口徑一致。
//   7. 無工時工單：期間內已完成（status IN ('completed', 'paid') 且在 created_at 期間內）
//      但完全無工時紀錄的工單不計入技師平均指標，另以備註列呈現缺口數量。
//   8. 權限控制：毛利與佣金屬成本資訊，僅 Owner（showCost=true）可查閱；
//      Counter（showCost=false）不顯示佣金與毛利欄位，僅顯示工單數、工時及每工時計價。
//      非主管人員（!canSeeMoney）則無法存取經營分析。

import (
	"bytes"
	"context"
	"strconv"
	"time"

	"autorepairos/internal/db"
	"autorepairos/web/pages"
	"github.com/a-h/templ"
)

// technicianEfficiencyRow 記錄單一技師在指定期間的效率數據。
type technicianEfficiencyRow struct {
	TechnicianID    int64
	Name            string
	OrderCount      int
	TotalHours      float64
	TotalLabor      float64
	TotalCommission float64
	LaborRecorded   bool
}

// technicianEfficiencyBlock 產生技師效率指標表格與說明的 HTML 字串（FR-ANA-02）。
//
// 參數：
//
//	from/to: YYYY-MM-DD 格式的日期區間
//	showCost: true 顯示佣金與每工時計毛利（僅 Owner）；false 僅顯示工單數、工時與每工時計價
func technicianEfficiencyComponent(from, to string, showCost bool) templ.Component {
	if db.DB == nil {
		return pages.TechnicianEfficiencyTable(nil, "0", showCost)
	}

	if from == "" {
		from = time.Now().Format("2006-01") + "-01"
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}

	// 1. 查詢各技師的工單數、總工時、工資收入與佣金成本
	rows, err := db.DB.Query(`SELECT
			wl.technician_id,
			COALESCE(NULLIF(u.name, ''), '技師#' || wl.technician_id),
			COUNT(DISTINCT wl.work_order_id),
			COALESCE(SUM(wl.hours), 0),
			COALESCE(SUM(CASE WHEN wl.labor_amount != 0 THEN wl.labor_amount ELSE wl.hours * COALESCE(wl.hourly_rate, 0) END), 0),
			COALESCE(SUM(wl.commission_amount), 0),
			COUNT(wl.id)
		FROM active_technician_worklogs wl
		LEFT JOIN users u ON u.id = wl.technician_id
		WHERE date(wl.work_date) BETWEEN ? AND ?
		GROUP BY wl.technician_id, u.name
		ORDER BY COUNT(DISTINCT wl.work_order_id) DESC, SUM(wl.hours) DESC, wl.technician_id ASC`, from, to)
	if err != nil {
		return pages.TechnicianEfficiencyTable(nil, "0", showCost)
	}
	defer rows.Close()

	var techList []technicianEfficiencyRow
	for rows.Next() {
		var r technicianEfficiencyRow
		var logCount int
		if err := rows.Scan(&r.TechnicianID, &r.Name, &r.OrderCount, &r.TotalHours, &r.TotalLabor, &r.TotalCommission, &logCount); err != nil {
			continue
		}
		r.LaborRecorded = logCount > 0
		techList = append(techList, r)
	}

	// 2. 查詢期間內已完成（completed 或 paid）但無任何工時紀錄的工單數（揭露資料缺口）
	var unloggedCount int
	_ = db.DB.QueryRow(`SELECT COUNT(1)
		FROM active_work_orders wo
		WHERE wo.status IN ('completed', 'paid')
		  AND date(wo.created_at) BETWEEN ? AND ?
		  AND NOT EXISTS (
		      SELECT 1 FROM active_technician_worklogs wl WHERE wl.work_order_id = wo.id
		  )`, from, to).Scan(&unloggedCount)

	rowsOut := make([]pages.Phase5TechRow, 0, len(techList))
	for _, t := range techList {
		price := "—"
		margin := "—"
		if t.TotalHours > 0 {
			price = money(t.TotalLabor / t.TotalHours)
			if t.LaborRecorded {
				margin = money((t.TotalLabor - t.TotalCommission) / t.TotalHours)
			}
		}
		rowsOut = append(rowsOut, pages.Phase5TechRow{
			Name: t.Name, OrderCount: strconv.Itoa(t.OrderCount), Hours: trimNum(t.TotalHours),
			HourlyPrice: price, Commission: costLabel(t.LaborRecorded, t.TotalCommission),
			Margin: margin, LaborRecorded: t.LaborRecorded,
		})
	}
	return pages.TechnicianEfficiencyTable(rowsOut, strconv.Itoa(unloggedCount), showCost)
}

func technicianEfficiencyBlock(from, to string, showCost bool) string {
	var out bytes.Buffer
	_ = technicianEfficiencyComponent(from, to, showCost).Render(context.Background(), &out)
	return out.String()
}
