// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
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
	// 營收 = SUM(total_amount) WHERE status='paid'（修理成本視角）。
	// 免額 (deductible_amount) 為客戶自付部分，不從車行營收扣除。
	// 若需「保險公司實際支付」視角，使用 SUM(total_amount - deductible_amount)。
	var insRevenue, selfRevenue float64
	var insCount, selfCount int
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0), COUNT(1) FROM active_work_orders
		WHERE status='paid' AND is_insurance=1 AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&insRevenue, &insCount)
	db.DB.QueryRow(`SELECT COALESCE(SUM(total_amount),0), COUNT(1) FROM active_work_orders
		WHERE status='paid' AND is_insurance=0 AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&selfRevenue, &selfCount)

	// ============ 狀態分布（保險 / 自費分欄）============
	// GROUP BY is_insurance, status 產生兩個並排表。
	statusRows, _ := db.DB.Query(`SELECT is_insurance, status, COUNT(1) FROM active_work_orders
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
	statusRowsFor := func(ins bool) []pages.Phase5ReportStatusRow {
		idx := 0
		if ins {
			idx = 1
		}
		out := make([]pages.Phase5ReportStatusRow, 0, 5)
		for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid"} {
			out = append(out, pages.Phase5ReportStatusRow{Label: workOrderStatusLabel[s], Pill: statusPill[s], Count: strconv.Itoa(statusCounts[idx][s])})
		}
		return out
	}

	// ============ 熱門項目（保險 / 自費分欄）============
	// work_order_items 沒有 created_at，日期要用所屬工單的建立日，
	// 否則 SQL 直接報 no such column，整個熱門項目區塊會靜默變空。
	// 每張表只放一列（GROUP BY 讓每個 description 一列），所以要分組累積。
	itemRows, _ := db.DB.Query(`SELECT wo.is_insurance, woi.description,
		COALESCE(SUM(woi.quantity),0), COALESCE(SUM(woi.subtotal),0)
		FROM work_order_items woi
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		WHERE date(wo.created_at) BETWEEN ? AND ?
		GROUP BY wo.is_insurance, woi.description
		ORDER BY wo.is_insurance, SUM(woi.subtotal) DESC`, from, to)
	itemRowsByIns := map[int][]pages.Phase5ReportItemRow{}
	if itemRows != nil {
		for itemRows.Next() {
			var ins int
			var desc string
			var qty int
			var sub float64
			if itemRows.Scan(&ins, &desc, &qty, &sub) != nil {
				continue
			}
			itemRowsByIns[ins] = append(itemRowsByIns[ins], pages.Phase5ReportItemRow{Description: desc, Quantity: strconv.Itoa(qty), Revenue: money(sub)})
		}
		itemRows.Close()
	}
	itemRowsFor := func(ins bool) []pages.Phase5ReportItemRow {
		idx := 0
		if ins {
			idx = 1
		}
		rows := itemRowsByIns[idx]
		if len(rows) > 10 {
			rows = rows[:10]
		}
		return rows
	}

	// ============ 技師績效（不動）============
	techRows, _ := db.DB.Query(`SELECT COALESCE(u.name,'-'), COUNT(wl.id),
		COALESCE(SUM(wl.hours),0), COALESCE(SUM(wl.commission_amount),0)
		FROM active_technician_worklogs wl JOIN users u ON u.id = wl.technician_id
		WHERE date(wl.work_date) BETWEEN ? AND ?
		GROUP BY u.id, u.name ORDER BY SUM(wl.commission_amount) DESC`, from, to)
	var techTable []pages.Phase5ReportTechRow
	if techRows != nil {
		for techRows.Next() {
			var name string
			var orders int
			var hours, comm float64
			if techRows.Scan(&name, &orders, &hours, &comm) == nil {
				techTable = append(techTable, pages.Phase5ReportTechRow{Name: name, Orders: strconv.Itoa(orders), Hours: trimNum(hours), Commission: money(comm)})
			}
		}
		techRows.Close()
	}

	// ============ 庫存低水位（不動）============
	lowRows, _ := db.DB.Query(`SELECT part_number, name, stock_quantity, min_safety_stock
		FROM active_inventory_items WHERE stock_quantity <= min_safety_stock ORDER BY stock_quantity ASC LIMIT 10`)
	var lowTable []pages.Phase5ReportLowRow
	if lowRows != nil {
		for lowRows.Next() {
			var part, name string
			var stock, minStock int
			if lowRows.Scan(&part, &name, &stock, &minStock) == nil {
				cls := "p-apprpend"
				if stock == 0 {
					cls = "p-apprno"
				}
				lowTable = append(lowTable, pages.Phase5ReportLowRow{Part: part, Name: name, Stock: strconv.Itoa(stock), MinStock: strconv.Itoa(minStock), Pill: cls})
			}
		}
		lowRows.Close()
	}

	// ============ 應收帳齡分析 (AR Aging) ============
	// 已結帳但收款合計不足的工單，依結帳日（updated_at）距今天數分桶
	agingCounts := map[string]int{"0-30": 0, "31-60": 0, "61-90": 0, "90+": 0}
	agingAmounts := map[string]float64{"0-30": 0, "31-60": 0, "61-90": 0, "90+": 0}
	agingTotalCount := 0
	agingTotalAmount := 0.0
	agingRows, _ := db.DB.Query(`SELECT wo.id, wo.total_amount, COALESCE(wo.updated_at, wo.created_at),
		COALESCE((SELECT SUM(r.pay_total) FROM receivables r WHERE r.work_order_id = wo.id AND r.voided = 0), 0)
		FROM active_work_orders wo WHERE wo.status = 'completed'`)
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

	aging := []pages.Phase5ReportAgingRow{
		{Label: "0–30 天", Count: strconv.Itoa(agingCounts["0-30"]), Amount: money(agingAmounts["0-30"])},
		{Label: "31–60 天", Count: strconv.Itoa(agingCounts["31-60"]), Amount: money(agingAmounts["31-60"])},
		{Label: "61–90 天", Count: strconv.Itoa(agingCounts["61-90"]), Amount: money(agingAmounts["61-90"])},
		{Label: "90+ 天", Count: strconv.Itoa(agingCounts["90+"]), Amount: money(agingAmounts["90+"]), Class: "color-red"},
	}
	_ = pages.ReportsPage(pages.Phase5ReportData{
		From: from, To: to, InsLabel: insLabel, SelfLabel: selfLabel,
		InsRevenue: money(insRevenue), SelfRevenue: money(selfRevenue), TotalRevenue: money(insRevenue + selfRevenue),
		InsCount: strconv.Itoa(insCount), SelfCount: strconv.Itoa(selfCount), TotalCount: strconv.Itoa(insCount + selfCount),
		InsurancePct: pctText(insRevenue, insRevenue+selfRevenue), AnalyticsLink: user.Role == "owner" || user.Role == "counter",
		StatusSelf: statusRowsFor(false), StatusIns: statusRowsFor(true), ItemsSelf: itemRowsFor(false), ItemsIns: itemRowsFor(true),
		Tech: techTable, Low: lowTable, Aging: aging, AgingTotalCount: strconv.Itoa(agingTotalCount), AgingTotalAmount: money(agingTotalAmount),
	}, user).Render(r.Context(), w)
}
