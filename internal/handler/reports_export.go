package handler

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

type reportExportRow struct {
	WorkOrderID  int64
	Customer     string
	Date         string
	LaborRevenue float64
	PartsRevenue float64
	Subcontract  float64
	PartsCost    float64
	Payment      string
	Discount     float64
	Total        float64
}

// ReportsExport downloads paid work orders using the same created-date period as ReportsHome.
func ReportsExport(w http.ResponseWriter, r *http.Request, user model.User) {
	if !canSeeMoney(user) {
		http.Error(w, "權限不足：營收報表僅限櫃檯人員與系統管理員使用", http.StatusForbidden)
		return
	}

	from, to, err := reportExportPeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	showCost := canExportReportCosts(user)
	rows, err := loadReportExportRows(r.Context(), from, to, showCost)
	if err != nil {
		http.Error(w, "報表匯出失敗", http.StatusInternalServerError)
		return
	}
	data, err := encodeReportExportCSV(rows, showCost)
	if err != nil {
		http.Error(w, "報表匯出失敗", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="work_orders_%s_%s.csv"`, from, to))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		log.Printf("write work-order CSV export: %v", err)
	}
}

func canExportReportCosts(user model.User) bool {
	return user.Role == "owner"
}

func reportExportPeriod(r *http.Request) (string, string, error) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" {
		from = today()[:7] + "-01"
	}
	if to == "" {
		to = today()
	}
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return "", "", errors.New("起始日期格式需為 YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return "", "", errors.New("結束日期格式需為 YYYY-MM-DD")
	}
	if start.After(end) {
		return "", "", errors.New("起始日期不可晚於結束日期")
	}
	return from, to, nil
}

func loadReportExportRows(ctx context.Context, from, to string, showCost bool) ([]reportExportRow, error) {
	if db.DB == nil {
		return nil, errors.New("database is not configured")
	}
	costColumn := ""
	if showCost {
		costColumn = `COALESCE((SELECT SUM(COALESCE(i.cost_price,0) * cost_item.quantity)
			FROM work_order_items cost_item
			LEFT JOIN active_inventory_items i ON i.id = cost_item.inventory_item_id
			WHERE cost_item.work_order_id = wo.id AND cost_item.item_type='part'),0),`
	}
	query := `SELECT wo.id, COALESCE(c.name,''), date(wo.created_at),
		COALESCE(SUM(CASE WHEN item.item_type='labor' THEN item.subtotal ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN item.item_type='part' THEN item.subtotal ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN item.item_type='subcontract' THEN item.subtotal ELSE 0 END),0),
		` + costColumn + `
		COALESCE((
			SELECT group_concat(method, '、') FROM (
				SELECT method FROM (
					SELECT '現金' AS method, 1 AS priority WHERE EXISTS (
						SELECT 1 FROM receivables r WHERE COALESCE(r.voided,0)=0
							AND (r.work_order_id=wo.id OR EXISTS (
								SELECT 1 FROM receivable_payments rp
								WHERE rp.receivable_id=r.id AND rp.work_order_id=wo.id))
							AND COALESCE(r.cash,0)>0)
					UNION ALL
					SELECT '信用卡', 2 WHERE EXISTS (
						SELECT 1 FROM receivables r WHERE COALESCE(r.voided,0)=0
							AND (r.work_order_id=wo.id OR EXISTS (
								SELECT 1 FROM receivable_payments rp
								WHERE rp.receivable_id=r.id AND rp.work_order_id=wo.id))
							AND COALESCE(r.card,0)>0)
					UNION ALL
					SELECT '支票', 3 WHERE EXISTS (
						SELECT 1 FROM receivables r WHERE COALESCE(r.voided,0)=0
							AND (r.work_order_id=wo.id OR EXISTS (
								SELECT 1 FROM receivable_payments rp
								WHERE rp.receivable_id=r.id AND rp.work_order_id=wo.id))
							AND COALESCE(r.check_amount,0)>0)
					UNION ALL
					SELECT '轉帳', 4 WHERE EXISTS (
						SELECT 1 FROM receivables r WHERE COALESCE(r.voided,0)=0
							AND (r.work_order_id=wo.id OR EXISTS (
								SELECT 1 FROM receivable_payments rp
								WHERE rp.receivable_id=r.id AND rp.work_order_id=wo.id))
							AND COALESCE(r.transfer_amount,0)>0)
				) ORDER BY priority
			)
		), '未記錄'),
		COALESCE(wo.discount_amount,0), COALESCE(wo.total_amount,0)
		FROM active_work_orders wo
		LEFT JOIN active_customers c ON c.id=wo.customer_id
		LEFT JOIN work_order_items item ON item.work_order_id=wo.id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		GROUP BY wo.id, c.name
		ORDER BY wo.id`

	rows, err := db.DB.QueryContext(ctx, query, from, to)
	if err != nil {
		return nil, fmt.Errorf("query report export rows: %w", err)
	}
	defer rows.Close()

	result := make([]reportExportRow, 0)
	for rows.Next() {
		var row reportExportRow
		values := []interface{}{
			&row.WorkOrderID, &row.Customer, &row.Date,
			&row.LaborRevenue, &row.PartsRevenue, &row.Subcontract,
		}
		if showCost {
			values = append(values, &row.PartsCost)
		}
		values = append(values, &row.Payment, &row.Discount, &row.Total)
		if err := rows.Scan(values...); err != nil {
			return nil, fmt.Errorf("scan report export row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read report export rows: %w", err)
	}
	return result, nil
}

func encodeReportExportCSV(rows []reportExportRow, showCost bool) ([]byte, error) {
	var output bytes.Buffer
	if _, err := output.Write([]byte{0xef, 0xbb, 0xbf}); err != nil {
		return nil, err
	}
	writer := csv.NewWriter(&output)
	header := []string{"工單編號", "客戶", "日期", "工資收入", "零件收入", "外包收入"}
	if showCost {
		header = append(header, "零件成本")
	}
	header = append(header, "支付方式", "折扣", "總計")
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	for _, row := range rows {
		record := []string{
			fmt.Sprintf("%d", row.WorkOrderID),
			safeSpreadsheetText(row.Customer),
			safeSpreadsheetText(row.Date),
			formatReportAmount(row.LaborRevenue),
			formatReportAmount(row.PartsRevenue),
			formatReportAmount(row.Subcontract),
		}
		if showCost {
			record = append(record, formatReportAmount(row.PartsCost))
		}
		record = append(record,
			safeSpreadsheetText(row.Payment),
			formatReportAmount(row.Discount),
			formatReportAmount(row.Total),
		)
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func formatReportAmount(value float64) string {
	return fmt.Sprintf("%.2f", value)
}

// Prefix formula-like text with an apostrophe, including formulas after leading whitespace.
func safeSpreadsheetText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
