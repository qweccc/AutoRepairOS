// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ============================================================
// 估價單（FR-PRINT-01）
//
// 與 /work-orders/{id}/receipt（結帳收據）刻意分成兩份，因為用途不同：
//   估價單 —— 施工前給客戶看，需要有效期、客戶簽名欄、依類別分組
//   收據   —— 結帳後交付，是收款憑證，含 LINE 線上核准
// 把兩者合成一份會出現「已結帳的單還印著有效期」這種矛盾。
//
// 版型（FR-PRINT-01 的 A4 / 熱感紙）：
//   ?paper=a4      A4 直向，明細完整（預設）
//   ?paper=thermal 熱感紙（80mm 寬），精簡：項目名 + 小計 + 總額
// 兩者共用同一份資料，只差 CSS 與明細欄位，不重複撈查詢。
// ============================================================

// quoteValidityDays 估價單有效期天數。
// 逾期報價失效是業界慣例（零件價格與工資會變），也避免客戶拿半年前的單來結帳。
const quoteValidityDays = 14

// PrintWorkOrderQuotation 產生可列印的估價單（FR-PRINT-01）。
func PrintWorkOrderQuotation(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+workOrderCols+workOrderFrom+` WHERE wo.id = ?`, id)
	wo, err := scanWorkOrder(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	items, itemTotal := loadWorkOrderItems(id)

	// 折扣與保固：migration 020 新增，舊工單為 0（COALESCE 保護）。
	var discount float64
	var isWarranty int
	db.DB.QueryRow(`SELECT COALESCE(discount_amount,0), COALESCE(is_warranty,0)
		FROM work_orders WHERE id = ?`, id).Scan(&discount, &isWarranty)

	// 總額權威來源：work_orders.total_amount（增刪項目時由 recalcWorkOrderTotal 重算）。
	// 不在此另行加總，避免與結帳時累計到 customers.total_spent 的金額不一致。
	total := wo.TotalAmount

	paper := strings.TrimSpace(r.URL.Query().Get("paper"))
	if paper != "thermal" {
		paper = "a4"
	}

	// 估價單日期與有效期。
	quoteDate := wo.CreatedAt
	if quoteDate.IsZero() {
		quoteDate = time.Now()
	}
	expiry := quoteDate.AddDate(0, 0, quoteValidityDays)
	if time.Now().After(expiry) {
		expiry = time.Now().AddDate(0, 0, quoteValidityDays)
	}

	// 依類別分組：工資 / 零件 / 外包各一小節，讓客戶看得懂錢花在哪。
	// 同一類別彙總小計，術科單據的慣例寫法。
	type group struct {
		label    string
		items    []model.WorkOrderItem
		subtotal float64
	}
	groups := []group{
		{label: "工資"},
		{label: "零件"},
		{label: "外包加工"},
	}
	gidx := map[string]int{"labor": 0, "part": 1, "subcontract": 2}
	for _, it := range items {
		if i, ok := gidx[it.ItemType]; ok {
			groups[i].items = append(groups[i].items, it)
			groups[i].subtotal += it.Subtotal
		}
	}

	// 明細表格：A4 給完整四欄，熱感紙只給描述 + 小計（80mm 放不下單價欄）。
	var b strings.Builder
	for _, g := range groups {
		if len(g.items) == 0 {
			continue
		}
		if paper == "a4" {
			b.WriteString(`<div class="q-group"><div class="q-group-head"><span>` + g.label +
				`</span><b>` + money(g.subtotal) + `</b></div>`)
			b.WriteString(`<table class="q-items"><tr><th>項目</th><th class="num">數量</th><th class="num">單價</th><th class="num">小計</th></tr>`)
			for _, it := range g.items {
				b.WriteString(`<tr><td>` + htmlEsc(it.Description) + `</td><td class="num">` +
					strconv.Itoa(it.Quantity) + `</td><td class="num">` + money(it.UnitPrice) +
					`</td><td class="num">` + money(it.Subtotal) + `</td></tr>`)
			}
			b.WriteString(`</table></div>`)
		} else {
			for _, it := range g.items {
				b.WriteString(`<tr><td>` + htmlEsc(it.Description) + `<span class="q-sub">` + g.label +
					` × ` + strconv.Itoa(it.Quantity) + `</span></td><td class="num">` +
					money(it.Subtotal) + `</td></tr>`)
			}
		}
	}

	// 熱感紙：單一扁平表格（沒有分組區塊）。
	thermalTable := ""
	if paper == "thermal" {
		thermalTable = `<table class="q-thermal">` + b.String() + `</table>`
	}

	// 金額摘要：項目小計 → 折扣 → 應付總額。
	// 折扣列只在真的有折扣時顯示，避免每張單都掛一個 0。
	discountRow := ""
	if discount > 0 {
		discountRow = `<div class="q-line"><span>折扣</span><b style="color:#b91c1c">-` +
			money(discount) + `</b></div>`
	}
	warrantyTag := ""
	if isWarranty == 1 {
		warrantyTag = `<span class="pill p-completed">保固案件</span>`
	}

	// 保固與條款：估價單一定要寫，否則客戶會爭執零件保固期。
	terms := `本估價單有效期 ` + strconv.Itoa(quoteValidityDays) + ` 天，逾期報價失效。
		零件保固依廠商規定，人為損壞與消耗品不在保固範圍。
		施工中如發現其他問題，將另行報價並經客戶同意後才動工。`

	body := `
  <div class="sheet">
    <div class="q-head">
      <div>
        <h1>🔧 ` + appName() + `</h1>
        <p class="q-subtitle">維修估價單 ` + warrantyTag + `</p>
      </div>
      <div class="q-head-meta">
        <div><span>估價單號</span><b>#` + strconv.FormatInt(wo.ID, 10) + `</b></div>
        <div><span>開立日期</span><b>` + quoteDate.Format("2006-01-02") + `</b></div>
        <div><span>有效期限</span><b>` + expiry.Format("2006-01-02") + `</b></div>
      </div>
    </div>

    <div class="q-parties">
      <div class="q-party">
        <span class="q-lbl">客戶</span>
        <b>` + htmlEsc(wo.CustomerName) + `</b>
        <span class="q-val">` + htmlEsc(wo.CustomerPhone) + `</span>
      </div>
      <div class="q-party">
        <span class="q-lbl">車輛</span>
        <b>` + htmlEsc(wo.PlateNumber) + `</b>
        <span class="q-val">` + htmlEsc(orDash(wo.BrandModel)) + ` / ` +
		strconv.Itoa(wo.CurrentMileage) + ` km</span>
      </div>
    </div>

    <div class="q-complaint">
      <span class="q-lbl">客戶反映</span>
      ` + htmlEsc(orDash(wo.CustomerComplaint)) + `
    </div>

    <div class="q-detail">
      <h2>維修項目與費用</h2>
      ` + thermalTable + b.String() + `
    </div>

    <div class="q-total">
      <div class="q-line"><span>項目小計</span><b>` + money(itemTotal) + `</b></div>
      ` + discountRow + `
      <div class="q-grand"><span>應付總額</span><b>` + money(total) + `</b></div>
    </div>

    <div class="q-terms">
      <b>估價說明與保固條款</b>
      <p>` + terms + `</p>
    </div>

    <div class="q-sign">
      <div class="q-sign-box">
        <span>客戶簽名（同意上述報價）</span>
        <div class="q-sign-line"></div>
      </div>
      <div class="q-sign-box">
        <span>承辦人</span>
        <div class="q-sign-line"></div>
        <span class="q-sign-name">` + htmlEsc(user.Username) + `</span>
      </div>
    </div>

    <div class="q-foot">
      <p class="tiny">列印時間：` + time.Now().Format("2006-01-02 15:04") +
		` / 櫃檯：` + htmlEsc(user.Username) + `</p>
    </div>
  </div>

  <div class="no-print">
    <button class="btn btn-p" onclick="window.print()">🖨 列印</button>
    <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/quotation?paper=a4">A4 版</a>
    <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/quotation?paper=thermal">熱感紙版</a>
    <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `">返回工單</a>
  </div>`

	sheetClass := "sheet"
	if paper == "thermal" {
		sheetClass = "sheet sheet-thermal"
	}
	body = strings.Replace(body, `class="sheet"`, `class="`+sheetClass+`"`, 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>估價單 #` + strconv.FormatInt(wo.ID, 10) + ` - AutoRepair OS</title>
<style>` + quotationStyle + `</style>
</head>
<body>` + body + `
<script>window.addEventListener('load', function(){ if (location.hash === '#print') window.print(); });</script>
</body>
</html>`))
}

// quotationStyle 估價單的 CSS。A4 與熱感紙共用，只在 @media print 與
// .sheet-thermal 覆寫紙寬——兩邊的差異只有版面，不是不同資料。
const quotationStyle = `
  * { box-sizing: border-box; }
  body { margin: 0; padding: 24px; background: #0f172a; color: #0f172a;
         font-family: "Segoe UI", "Microsoft JhengHei", sans-serif; }
  .sheet { max-width: 760px; margin: 0 auto; background: #fff; color: #0f172a;
           padding: 34px 38px; border-radius: 10px; }
  .sheet-thermal { max-width: 76mm; padding: 8mm 5mm; font-size: 12px; }

  .q-head { display: flex; justify-content: space-between; align-items: flex-start;
            border-bottom: 3px solid #0f172a; padding-bottom: 14px; }
  .q-head h1 { margin: 0; font-size: 25px; }
  .q-subtitle { margin: 4px 0 0; color: #475569; font-size: 13.5px; }
  .q-head-meta { text-align: right; font-size: 13px; }
  .q-head-meta span { display: block; color: #64748b; font-size: 11px; }
  .q-head-meta b { font-size: 13.5px; }

  .q-parties { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin: 16px 0 12px; }
  .q-party { border: 1px solid #cbd5e1; border-radius: 7px; padding: 9px 12px; }
  .q-lbl { display: block; color: #64748b; font-size: 11.5px; }
  .q-val { display: block; color: #475569; font-size: 12.5px; margin-top: 2px; }
  .q-party b { font-size: 15px; }

  .q-complaint { border-left: 4px solid #38bdf8; padding: 6px 10px; margin: 12px 0;
                 background: #f1f5f9; font-size: 13px; border-radius: 0 6px 6px 0; }

  .q-detail h2 { margin: 16px 0 8px; font-size: 14px; color: #0369a1;
                 border-left: 4px solid #38bdf8; padding-left: 9px; }
  .q-group { margin-bottom: 12px; }
  .q-group-head { display: flex; justify-content: space-between; align-items: baseline;
                  padding: 5px 2px; border-bottom: 1px solid #94a3b8; font-size: 13px; }
  .q-group-head b { font-size: 13.5px; }
  table.q-items { width: 100%; border-collapse: collapse; }
  table.q-items th { text-align: left; padding: 5px 6px; font-size: 11.5px;
                     color: #64748b; border-bottom: 1px solid #e2e8f0; }
  table.q-items td { padding: 5px 6px; font-size: 13px; border-bottom: 1px solid #f1f5f9; }
  .num { text-align: right; }

  table.q-thermal { width: 100%; border-collapse: collapse; font-size: 11.5px; }
  table.q-thermal td { padding: 3px 0; border-bottom: 1px dotted #cbd5e1; vertical-align: top; }
  .q-sub { display: block; color: #64748b; font-size: 10.5px; }

  .q-total { margin-top: 14px; border-top: 2px solid #0f172a; padding-top: 10px; }
  .q-line { display: flex; justify-content: space-between; padding: 3px 0; font-size: 13.5px; }
  .q-grand { display: flex; justify-content: space-between; padding: 8px 0 2px;
             border-top: 2px solid #0f172a; margin-top: 6px;
             font-size: 21px; font-weight: 800; color: #0369a1; }

  .q-terms { margin-top: 18px; padding: 10px 12px; background: #f8fafc;
             border: 1px solid #e2e8f0; border-radius: 7px; font-size: 12px; }
  .q-terms b { font-size: 12.5px; }
  .q-terms p { margin: 5px 0 0; color: #475569; line-height: 1.7; white-space: pre-line; }

  .q-sign { display: grid; grid-template-columns: 1fr 1fr; gap: 26px; margin-top: 24px; }
  .q-sign-box span { display: block; font-size: 11.5px; color: #64748b; }
  .q-sign-line { border-bottom: 1px solid #334155; height: 34px; margin-bottom: 4px; }
  .q-sign-name { font-size: 13px !important; color: #0f172a !important; }
  .sheet-thermal .q-sign { grid-template-columns: 1fr; gap: 12px; }
  .sheet-thermal .q-sign-line { height: 26px; }

  .q-foot { margin-top: 18px; text-align: center; border-top: 1px dashed #cbd5e1; padding-top: 10px; }
  .tiny { color: #94a3b8; font-size: 11px; }
  .pill { display: inline-block; padding: 1px 8px; border-radius: 999px; font-size: 11.5px; }

  .no-print { max-width: 760px; margin: 16px auto 0; display: flex; gap: 9px; flex-wrap: wrap; }
  .btn { display: inline-block; padding: 9px 16px; border-radius: 8px; font-size: 13.5px;
         text-decoration: none; font-weight: 700; border: 0; cursor: pointer; }
  .btn-p { background: #38bdf8; color: #0f172a; }
  .btn-s { background: #334155; color: #e2e8f0; }

  @media print {
    body { background: #fff; padding: 0; }
    .no-print { display: none !important; }
    .sheet { max-width: none; box-shadow: none; border-radius: 0; padding: 0; }
    /* 熱感紙：固定 80mm 紙寬，避免瀏覽器自動縮放成 A4 版面。 */
    .sheet-thermal { max-width: 72mm; padding: 0; }
    @page { margin: 10mm; }
  }
`
