package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// PrintWorkOrderReceipt 產生可列印的結帳單頁面。
// 以瀏覽器列印（window.print + @media print）為目標，後續可接 ESC/POS 熱感印表機。
func PrintWorkOrderReceipt(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+workOrderCols+workOrderFrom+` WHERE wo.id = ?`, id)
	wo, err := scanWorkOrder(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// IDOR 防護：技師只能查看指派給自己的工單收據
	if user.Role == "technician" && wo.TechnicianID != user.ID {
		http.Error(w, "無權查看此工單", http.StatusForbidden)
		return
	}

	items, itemTotal := loadWorkOrderItems(id)
	// 以 work_orders.total_amount 為權威總額（增刪項目時會重算），
	// 不另行加稅，避免與結帳時累計到客戶 total_spent 的金額不一致。
	//
	// 這裡不實作稅率：台灣汽車維修業的報價通常是含稅價，稅額已內含在工單
	// 總額中。若之後要另列稅額，必須同步處理總額來源與 total_spent 的計算，
	// 否則會出現收據金額與已入帳金額不一致。
	// 原本的「稅金/調整」欄位是 total - itemTotal，恆為 0，屬誤導性死碼，已移除。
	total := wo.TotalAmount

	// LINE 綁定：讀出客戶的 line_user_id，並用 Flex 估價單模板（明細兩欄 + 金額 + 線上核准）。
	var lineJSON string
	var lineUserID string
	if err := db.DB.QueryRow("SELECT COALESCE(line_user_id,'') FROM active_customers WHERE id=?", wo.CustomerID).Scan(&lineUserID); err == nil && lineUserID != "" {
		quoteItems := []line.ItemLine{}
		for _, it := range items {
			quoteItems = append(quoteItems, line.ItemLine{Desc: it.Description, Total: it.Subtotal})
		}
		em := line.EstimateMessage{
			Title:      "估價單 #" + strconv.FormatInt(wo.ID, 10),
			SubText:    wo.PlateNumber + " / " + wo.CustomerName,
			Items:      quoteItems,
			Total:      total,
			ApproveURL: "/work-orders/" + strconv.FormatInt(wo.ID, 10) + "/receipt",
		}
		flexJSON, _ := json.MarshalIndent(em.ToFlex(), "", "  ")
		lineJSON = string(flexJSON)
		w.Header().Add("X-Line-User-ID", lineUserID)
	}

	pageItems := make([]pages.ReceiptItem, 0, len(items))
	for _, it := range items {
		pageItems = append(pageItems, pages.ReceiptItem{
			Description: it.Description,
			Quantity:    strconv.Itoa(it.Quantity),
			UnitPrice:   money(it.UnitPrice),
			Subtotal:    money(it.Subtotal),
		})
	}
	data := pages.ReceiptPageData{
		AppName: appName(), WorkOrderID: strconv.FormatInt(wo.ID, 10), Date: today(),
		Status: workOrderStatusLabel[wo.Status], Approval: approvalLabel[wo.ApprovalStatus],
		CustomerName: wo.CustomerName, CustomerPhone: wo.CustomerPhone,
		PlateNumber: wo.PlateNumber, BrandModel: wo.BrandModel,
		Mileage: strconv.Itoa(wo.CurrentMileage), Technician: optionalPrintValue(wo.TechnicianName),
		Complaint: optionalPrintValue(wo.CustomerComplaint),
		Items:     pageItems, ItemTotal: money(itemTotal), Total: money(total),
		Username: user.Username, LineBound: lineJSON != "", LineJSON: lineJSON, Style: receiptStyle,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ReceiptPage(data).Render(r.Context(), w)
}

func optionalPrintValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return ""
	}
	return value
}

const receiptStyle = `
  * { box-sizing: border-box; }
  body { margin: 0; padding: 26px; background: #0f172a; color: #e2e8f0;
         font-family: "Segoe UI", "Microsoft JhengHei", sans-serif; }
  .sheet { max-width: 720px; margin: 0 auto; background: #f8fafc; color: #0f172a;
           padding: 34px 38px; border-radius: 10px; }
  .rpt-head { text-align: center; border-bottom: 3px solid #0f172a; padding-bottom: 14px; }
  .rpt-head h1 { margin: 0; font-size: 26px; }
  .rpt-head p { margin: 5px 0 0; color: #475569; font-size: 14px; }
  .rpt-meta { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px;
              margin: 18px 0; font-size: 13.5px; }
  .rpt-meta span { display: block; color: #64748b; font-size: 11.5px; }
  .rpt-meta b { font-size: 14.5px; }
  .rpt-sec { margin: 16px 0; }
  .rpt-sec h2 { margin: 0 0 7px; font-size: 14px; color: #0369a1;
                border-left: 4px solid #38bdf8; padding-left: 9px; }
  .rpt-sec p { margin: 4px 0; font-size: 13.5px; line-height: 1.7; }
  .lbl { color: #64748b; font-size: 12.5px; }
  table.rpt { width: 100%; border-collapse: collapse; margin-top: 8px; }
  table.rpt th { text-align: left; padding: 8px 6px; border-bottom: 2px solid #cbd5e1;
                 font-size: 12.5px; color: #475569; }
  table.rpt td { padding: 8px 6px; border-bottom: 1px solid #e2e8f0; font-size: 13.5px; }
  .num { text-align: right; }
  .rpt-total { margin-top: 18px; border-top: 2px solid #0f172a; padding-top: 12px; }
  .rpt-total div { display: flex; justify-content: space-between; padding: 4px 0; font-size: 14px; }
  .rpt-total .grand { font-size: 20px; font-weight: 800; color: #0369a1;
                      border-top: 2px solid #0f172a; margin-top: 8px; padding-top: 10px; }
  .rpt-note { margin-top: 8px; font-size: 11.5px; color: #94a3b8; text-align: right; }
  .rpt-foot { margin-top: 26px; text-align: center; border-top: 1px dashed #cbd5e1; padding-top: 14px; }
  .rpt-foot p { margin: 4px 0; font-size: 14px; }
  .tiny { color: #94a3b8; font-size: 11.5px; }
  .rpt-line { margin: 8px 0 4px; }
  .rpt-line span { font-size: 12px; color: #64748b; }
  .rpt-flex { background: #f1f5f9; border: 1px dashed #94a3b8; border-radius: 8px; padding: 12px; font-size: 11px; color: #475569; white-space: pre-wrap; }
  .no-print { max-width: 720px; margin: 18px auto 0; display: flex; gap: 10px; }
  .btn { display: inline-block; padding: 9px 18px; border-radius: 8px; font-size: 14px;
         text-decoration: none; font-weight: 700; border: 0; cursor: pointer; }
  .btn-p { background: #38bdf8; color: #0f172a; }
  .btn-s { background: #334155; color: #e2e8f0; }
  .btn-d { background: #7f1d1d; color: #fecaca; }
  form.inline { display: inline; }

  @media print {
    body { background: #fff; padding: 0; color: #000; }
    .no-print { display: none !important; }
    .sheet { max-width: none; box-shadow: none; border-radius: 0; padding: 0; }
    @page { margin: 12mm; }
  }
`
