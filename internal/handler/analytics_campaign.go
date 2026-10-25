package handler

import (
	"bytes"
	"context"
	"sort"
	"strconv"

	"autorepairos/internal/db"
	"autorepairos/web/pages"
	"github.com/a-h/templ"
)

// campaignAnalysisBlock 產生行銷方案毛利分析表格的 HTML 字串（FR-ANA-06）。
//
// 方案代碼歸屬規則（同一工單有多個代碼時的優先順序）：
//  1. 工單自己的 campaign_code 優先
//  2. 若工單沒有，則用客戶的 campaign_code
//  3. 兩者皆空則歸為「(未設定)」
//
// 此規則與原 analytics.go 區段的 COALESCE(wo.campaign_code, c.campaign_code, '(未設定)')
// 一致，確保不同模組計算結果相同。
//
// 為什麼包 NULLIF：表單送來的空白會被寫成 NULL，但直接寫入的舊資料可能是空字串。
// 空字串對 COALESCE 來說不是 NULL，會直接當成「有值」→ 畫面出現一個空白方案代碼，
// 使用者會以為是程式壞了。NULLIF 先把空字串轉回 NULL 再走 COALESCE。
//
// 成本口徑：使用 analytics_cost.go 的 workOrderCostsByIDs，批次查詢多張工單的成本，
// 避免 N+1 查詢。毛利 = 營收 - 成本。
//
// 參數：
//
//	from/to: YYYY-MM-DD 格式的日期區間
//	showCost: true 顯示成本與毛利欄位（僅 Owner）；false 完全隱藏
func campaignAnalysisComponent(from, to string, showCost bool) templ.Component {
	if db.DB == nil {
		return pages.CampaignAnalysisTable(nil, showCost)
	}

	// 依方案代碼分組，收集工單 ID（批次查成本用）
	type campaignGroup struct {
		Code            string
		OrderIDs        []int64
		OrderCount      int
		Revenue         float64
		PartsCost       float64
		SubcontractCost float64
		LaborCost       float64
		LaborRecorded   bool
	}

	groups := make(map[string]*campaignGroup)

	// 查詢工單：按方案代碼分組，並收集工單 ID
	rows, err := db.DB.Query(`SELECT
			COALESCE(NULLIF(wo.campaign_code,''), NULLIF(c.campaign_code,''), '(未設定)') AS code,
			wo.id,
			COALESCE(wo.total_amount,0) - COALESCE(wo.discount_amount,0) AS revenue
		FROM active_work_orders wo
		LEFT JOIN active_customers c ON c.id = wo.customer_id
		WHERE wo.status='paid' AND date(wo.created_at) BETWEEN ? AND ?
		ORDER BY code`, from, to)
	if err != nil {
		return pages.CampaignAnalysisTable(nil, showCost)
	}
	defer rows.Close()

	for rows.Next() {
		var code string
		var woID int64
		var revenue float64
		if err := rows.Scan(&code, &woID, &revenue); err != nil {
			continue
		}

		g, ok := groups[code]
		if !ok {
			g = &campaignGroup{Code: code}
			groups[code] = g
		}
		g.OrderIDs = append(g.OrderIDs, woID)
		g.OrderCount++
		g.Revenue += revenue
	}
	if err := rows.Err(); err != nil {
		return pages.CampaignAnalysisTable(nil, showCost)
	}

	if len(groups) == 0 {
		return pages.CampaignAnalysisTable(nil, showCost)
	}

	// 批次查成本：收集所有工單 ID
	var allIDs []int64
	for _, g := range groups {
		allIDs = append(allIDs, g.OrderIDs...)
	}
	costMap := workOrderCostsByIDs(allIDs)

	// 將成本分組加回對應的方案
	for _, g := range groups {
		var partsCost, subCost, laborCost float64
		laborRecorded := false

		for _, woID := range g.OrderIDs {
			c, ok := costMap[woID]
			if ok {
				partsCost += c.PartsCost
				subCost += c.SubcontractCost
				laborCost += c.LaborCost
				if c.LaborRecorded {
					laborRecorded = true
				}
			}
		}
		g.PartsCost = partsCost
		g.SubcontractCost = subCost
		g.LaborCost = laborCost
		g.LaborRecorded = laborRecorded
	}

	// 按營收排序（由高到低）
	sortedGroups := make([]*campaignGroup, 0, len(groups))
	for _, g := range groups {
		sortedGroups = append(sortedGroups, g)
	}
	sort.Slice(sortedGroups, func(i, j int) bool {
		if sortedGroups[i].Revenue == sortedGroups[j].Revenue {
			return sortedGroups[i].Code < sortedGroups[j].Code
		}
		return sortedGroups[i].Revenue > sortedGroups[j].Revenue
	})

	rowsOut := make([]pages.Phase5CampaignRow, 0, len(sortedGroups))
	for _, g := range sortedGroups {
		totalCost := g.PartsCost + g.SubcontractCost + g.LaborCost
		gross := g.Revenue - totalCost
		rowsOut = append(rowsOut, pages.Phase5CampaignRow{
			Code: g.Code, OrderCount: strconv.Itoa(g.OrderCount), Revenue: money(g.Revenue),
			Cost: money(totalCost), Gross: money(gross), LaborRecorded: g.LaborRecorded,
		})
	}
	return pages.CampaignAnalysisTable(rowsOut, showCost)
}

func campaignAnalysisBlock(from, to string, showCost bool) string {
	var out bytes.Buffer
	_ = campaignAnalysisComponent(from, to, showCost).Render(context.Background(), &out)
	return out.String()
}
