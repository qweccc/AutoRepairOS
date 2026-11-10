package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
)

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
