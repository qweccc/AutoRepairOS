package handler

// T2.13 損益成本口徑（FR-FIN-06、FR-ANA-07）
//
// 這支檔是「成本」的唯一口径定義處：經營分析的損益分拆、保固成本（T2.14）、
// 技師效率（T2.17）、行銷方案毛利（T2.18）全都呼叫這裡的函式，
// 各自再寫一份 SQL 就是日後「同一頁兩個數字」的來源。
//
// 三類成本與來源：
//
//	零件成本：work_order_items(item_type='part') 關回 inventory_items.cost_price × quantity。
//	         查不到料號的零件不計成本（寧可低估，也不造假）。
//	外包成本：work_order_items(item_type='subcontract') 的 subtotal。
//	         外包沒有庫存成本主檔，工單明細自己記的就是成本。
//	工資成本：technician_worklogs.commission_amount（車廠實際付給技師的佣金）。
//
// 跨期歸屬（期間筛选的依據，兩種不同）：
//
//	零件／外包 → 工單建立日（date(work_orders.created_at)），與報表匯出、分析圖表一致。
//	工資      → 工時日（date(technician_worklogs.work_date)），成本屬於實際施工日。
//
// 重複計入防護：
//
//	work_order_items 的 labor 是「收入」，不是成本；工資成本只取佣金，
//	不再把 technician_worklogs.labor_amount（時薪×工時）也算一次，否則同一筆錢算兩次。
//
// 未知成本不以零冒充：
//
//	任何期間／工單只要沒有技師工時紀錄，LaborRecorded 就是 false，
//	呼叫端必須顯示「未記錄」，不可顯示 0（0 會被當成「算過且成本為零」）。
//
// 這支檔爲纯查询、不吃 db.TxExecutor，全部走全域 db.DB（與 analytics.go 一致）。

import (
	"fmt"
	"strings"

	"autorepairos/internal/db"
)

// analyticsCosts 是一段期間的三類成本。
type analyticsCosts struct {
	PartsCost       float64 // 零件成本（工單建立日歸屬）
	SubcontractCost float64 // 外包成本（工單建立日歸屬）
	LaborCost       float64 // 工資成本＝技師佣金（工時日歸屬）
	LaborRecorded   bool    // 期間內是否有任何技師工時紀錄
}

// Total 回傳三類成本合計（未知工資時只加已知的兩類，並搭配 LaborRecorded 顯示）。
func (c analyticsCosts) Total() float64 {
	return c.PartsCost + c.SubcontractCost + c.LaborCost
}

// analyticsPeriodCosts 依期間查詢三類成本。
//
// from/to 是 YYYY-MM-DD。零件／外包以工單建立日歸屬，工資以工時日歸屬，
// 兩者不共用同一個日期欄是刻意的（口徑不同，混用會讓跨月工單的成本跑錯期間）。
func analyticsPeriodCosts(from, to string) analyticsCosts {
	out := analyticsCosts{}
	if db.DB == nil {
		return out
	}

	// 零件／外包：工單建立日歸屬。
	db.DB.QueryRow(`SELECT
			COALESCE(SUM(CASE WHEN woi.item_type='part'
				THEN COALESCE(i.cost_price,0) * woi.quantity ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN woi.item_type='subcontract'
				THEN woi.subtotal ELSE 0 END),0)
		FROM work_order_items woi
		JOIN active_work_orders wo ON wo.id = woi.work_order_id
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?`, from, to).
		Scan(&out.PartsCost, &out.SubcontractCost)

	// 工資：工時日歸屬。LaborRecorded 由「有無任何工時紀錄」決定，
	// 與 commission_amount 是否為 0 無關（技師可能登記了工時但佣金率 0）。
	var laborRows int
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_technician_worklogs
		WHERE date(work_date) BETWEEN ? AND ?`, from, to).Scan(&laborRows)
	out.LaborRecorded = laborRows > 0
	if out.LaborRecorded {
		db.DB.QueryRow(`SELECT COALESCE(SUM(commission_amount),0)
			FROM active_technician_worklogs
			WHERE date(work_date) BETWEEN ? AND ?`, from, to).Scan(&out.LaborCost)
	}
	return out
}

// workOrderCostComponents 是「一張工單」的三類成本。
type workOrderCostComponents struct {
	PartsCost       float64
	SubcontractCost float64
	LaborCost       float64
	LaborRecorded   bool // 該工單是否有任何技師工時紀錄
}

// workOrderCostByID 查單張工單的成本。工單不存在或查不到時回零值。
func workOrderCostByID(woID int64) workOrderCostComponents {
	out := workOrderCostComponents{}
	if db.DB == nil || woID <= 0 {
		return out
	}
	db.DB.QueryRow(`SELECT
			COALESCE(SUM(CASE WHEN woi.item_type='part'
				THEN COALESCE(i.cost_price,0) * woi.quantity ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN woi.item_type='subcontract'
				THEN woi.subtotal ELSE 0 END),0)
		FROM work_order_items woi
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
		WHERE woi.work_order_id = ?`, woID).
		Scan(&out.PartsCost, &out.SubcontractCost)

	var laborRows int
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_technician_worklogs WHERE work_order_id = ?`, woID).
		Scan(&laborRows)
	out.LaborRecorded = laborRows > 0
	if out.LaborRecorded {
		db.DB.QueryRow(`SELECT COALESCE(SUM(commission_amount),0) FROM active_technician_worklogs
			WHERE work_order_id = ?`, woID).Scan(&out.LaborCost)
	}
	return out
}

// workOrderCostsByIDs 一次查回多張工單的成本，避免 N+1。
// 回傳的 map 只包含「有工單資料」的 id（查不到的 id 不在 map 內）。
func workOrderCostsByIDs(ids []int64) map[int64]workOrderCostComponents {
	out := make(map[int64]workOrderCostComponents, len(ids))
	if db.DB == nil || len(ids) == 0 {
		return out
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	in := strings.Join(placeholders, ",")

	// 零件／外包：一張工單一列。
	rows, err := db.DB.Query(`SELECT woi.work_order_id,
			COALESCE(SUM(CASE WHEN woi.item_type='part'
				THEN COALESCE(i.cost_price,0) * woi.quantity ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN woi.item_type='subcontract'
				THEN woi.subtotal ELSE 0 END),0)
		FROM work_order_items woi
		LEFT JOIN active_inventory_items i ON i.id = woi.inventory_item_id
		WHERE woi.work_order_id IN (`+in+`)
		GROUP BY woi.work_order_id`, args...)
	if err == nil {
		for rows.Next() {
			var id int64
			var c workOrderCostComponents
			if rows.Scan(&id, &c.PartsCost, &c.SubcontractCost) == nil {
				out[id] = c
			}
		}
		rows.Close()
	}

	// 工資：同樣一張工單一列，並標記是否有工時紀錄。
	laborRows, err := db.DB.Query(`SELECT work_order_id, COUNT(1),
			COALESCE(SUM(commission_amount),0)
		FROM active_technician_worklogs
		WHERE work_order_id IN (`+in+`)
		GROUP BY work_order_id`, args...)
	if err == nil {
		for laborRows.Next() {
			var id int64
			var n int
			var labor float64
			if laborRows.Scan(&id, &n, &labor) == nil {
				c := out[id]
				c.LaborCost = labor
				c.LaborRecorded = n > 0
				out[id] = c
			}
		}
		laborRows.Close()
	}
	return out
}

// costLabel 把「未知成本」與「已計算為 0」分開顯示。
// recorded=false → 「未記錄」；true 且值為 0 → 「0」。
func costLabel(recorded bool, value float64) string {
	if !recorded {
		return "未記錄"
	}
	return money(value)
}

// grossStatValue 回傳 KPI「毛利率」的顯示值。
//
// 工資沒有工時紀錄時，毛利與毛利率都算不出來，顯示「未記錄」而不是一個
// 用 0 成本算出來、看起來正常的數字。
func grossStatValue(showCost bool, costs analyticsCosts, grossPct float64) string {
	if !showCost {
		return fmt.Sprintf("%.1f%%", grossPct)
	}
	if !costs.LaborRecorded {
		return "未記錄"
	}
	return fmt.Sprintf("%.1f%%", grossPct)
}

// warrantyStats 回傳保固工單數、折扣後收入與折讓。
//
// 獨立成函式是為了可測：保固收入必須用「折扣後」金額，與損益分拆、方案毛利
// 同口徑；若用未折扣金額，同一張保固工單會在兩個區塊算出不同的毛利。
func warrantyStats(from, to string) (count int, revenue, discount float64) {
	if db.DB == nil {
		return 0, 0, 0
	}
	db.DB.QueryRow(`SELECT COUNT(1),
			COALESCE(SUM(total_amount - COALESCE(discount_amount,0)),0),
			COALESCE(SUM(COALESCE(discount_amount,0)),0)
		FROM active_work_orders
		WHERE is_warranty=1 AND date(created_at) BETWEEN ? AND ?`, from, to).
		Scan(&count, &revenue, &discount)
	return count, revenue, discount
}

// warrantyCostTotals 把保固工單的成本加總（T2.14）。
// 回傳 (總成本, 是否有任何工時紀錄)——沒有工時紀錄時呼叫端要顯示「未記錄」。
func warrantyCostTotals(from, to string) (float64, bool) {
	if db.DB == nil {
		return 0, false
	}
	ids := make([]int64, 0, 16)
	rows, err := db.DB.Query(`SELECT id FROM active_work_orders
		WHERE is_warranty=1 AND date(created_at) BETWEEN ? AND ?`, from, to)
	if err != nil {
		return 0, false
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()

	var total float64
	var recorded bool
	for _, c := range workOrderCostsByIDs(ids) {
		total += c.PartsCost + c.SubcontractCost + c.LaborCost
		if c.LaborRecorded {
			recorded = true
		}
	}
	return total, recorded
}

// warrantyCostBlock 是 T2.14 保固成本統計的 HTML 區段。
//
// showCost 為 false（非 Owner）時回空字串——成本屬營利資訊，不可讓櫃檯看到。
// 工資無工時紀錄時以「未記錄」呈現，毛利同樣不顯示數字。
func warrantyCostBlock(showCost bool, costTotal, gross float64, laborRecorded bool) string {
	if !showCost {
		return ""
	}
	unrecorded := `<b><span style="color:#64748b">未記錄</span></b>`
	costCell := `<b>` + money(costTotal) + `</b>`
	grossCell := `<b>` + money(gross) + `</b>`
	if !laborRecorded {
		costCell = unrecorded
		grossCell = unrecorded
	}
	return `
      <p style="color:#64748b;font-size:12.5px;margin:10px 0 6px">保固成本＝零件成本＋外包成本＋工資成本（與損益分拆同一套計算）。</p>
      <div style="margin-bottom:12px">
        <div class="stat">` + costCell + `<span>保固成本</span></div>
        <div class="stat">` + grossCell + `<span>保固毛利</span></div>
      </div>`
}
