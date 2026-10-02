package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
	"autorepairos/internal/model"
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
	var lineBlock string
	var lineUserID string
	if err := db.DB.QueryRow("SELECT COALESCE(line_user_id,'') FROM customers WHERE id=?", wo.CustomerID).Scan(&lineUserID); err == nil && lineUserID != "" {
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
		lineBlock = `<div class="rpt-line"><span>LINE 估價單（明細兩欄 + 金額 + 線上核准）</span></div>` +
			`<pre class="rpt-flex">` + string(flexJSON) + `</pre>`
		w.Header().Add("X-Line-User-ID", lineUserID)
	} else {
		lineBlock = "<div class=\"rpt-line\"><span>LINE 估價單：客戶未綁定 line_user_id</span></div>"
	}

	var b strings.Builder
	b.WriteString(`<table class="rpt"><tr><th>項目</th><th class="num">數量</th><th class="num">單價</th><th class="num">小計</th></tr>`)
	for _, it := range items {
		b.WriteString(`<tr><td>` + htmlEsc(it.Description) + `</td><td class="num">` +
			strconv.Itoa(it.Quantity) + `</td><td class="num">` + money(it.UnitPrice) +
			`</td><td class="num">` + money(it.Subtotal) + `</td></tr>`)
	}
	b.WriteString(`</table>`)

	receipt := `
  <div class="sheet">
    <div class="rpt-head">
      <h1>🔧 AutoRepair OS</h1>
      <p>車行維修估價單 / 收據</p>
    </div>

    <div class="rpt-meta">
      <div><span>工單編號</span><b>#` + strconv.FormatInt(wo.ID, 10) + `</b></div>
      <div><span>日期</span><b>` + today() + `</b></div>
      <div><span>狀態</span><b>` + workOrderStatusLabel[wo.Status] + `</b></div>
      <div><span>核准</span><b>` + approvalLabel[wo.ApprovalStatus] + `</b></div>
    </div>

    <div class="rpt-sec">
      <h2>客戶</h2>
      <p><b>` + htmlEsc(wo.CustomerName) + `</b> / ` + htmlEsc(wo.CustomerPhone) + `</p>
    </div>

    <div class="rpt-sec">
      <h2>LINE 估價單</h2>
      ` + lineBlock + `
    </div>

    <div class="rpt-sec">
      <h2>車輛</h2>
      <p><b>` + htmlEsc(wo.PlateNumber) + `</b> — ` + htmlEsc(wo.BrandModel) + `</p>
      <p>進廠里程：` + strconv.Itoa(wo.CurrentMileage) + ` km` +
		optLine("技師", wo.TechnicianName) + optLine("客戶反映", orDash(wo.CustomerComplaint)) + `</p>
    </div>

    <div class="rpt-sec">
      <h2>明細</h2>` + b.String() + `
    </div>

    <div class="rpt-total">
      <div><span>項目小計</span><b>` + money(itemTotal) + `</b></div>
      <div class="grand"><span>應付總額（含稅）</span><b>` + money(total) + `</b></div>
    </div>

    <div class="rpt-foot">
      <p>謝謝惠顧！</p>
      <p class="tiny">列印時間：` + today() + ` / 櫃檯：` + htmlEsc(user.Username) + `</p>
    </div>
  </div>

  <div class="no-print">
    <button class="btn btn-p" onclick="window.print()">🖨 列印</button>
    <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `">返回工單</a>
    <form class="inline" method="POST" action="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/status" style="display:inline">
      <input type="hidden" name="status" value="paid">
      <button class="btn btn-d">標記為已結帳</button>
    </form>
  </div>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>收據 #` + strconv.FormatInt(wo.ID, 10) + ` - AutoRepair OS</title>
<style>` + receiptStyle + `</style>
</head>
<body>` + receipt + `
<script>window.addEventListener('load', function(){ if (location.hash === '#print') window.print(); });</script>
</body>
</html>`))
}

func optLine(label, val string) string {
	if strings.TrimSpace(val) == "" || val == "-" {
		return ""
	}
	return `<br><span class="lbl">` + label + `：</span>` + htmlEsc(val)
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

var _ = fmt.Sprintf
