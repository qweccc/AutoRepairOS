// Package nav 是全站選單的單一定義來源。
//
// 為什麼需要這個套件：選單曾經有兩份獨立的定義，各自會走音——
//   - web/components/layout.templ 的 NavItems（templ 頁用）
//   - internal/handler/layout.go 的 appRoutes + nav()（renderPage 字串拼接頁用）
//
// 兩邊的項目與分組要手動同步，漏一邊的症狀是「總覽與其他頁選單格式/項目不同」，
// 極難察覺（因為多數頁面看起來都正常）。
//
// 現在兩邊都呼叫 nav.Render(active) 取得同一份 HTML，新增/調整選單只改這裡。
package nav

import (
	"html"
	"strings"
)

// Route 是選單上的一個項目。Group 相同且連續的項目會被包成同一個 navgrp，
// Group 標籤顯示在每組開頭。
type Route struct {
	Path  string
	Name  string
	Group string
}

// Routes 全站選單。
//
// 分組順序依車行作業節奏排：櫃檯最常用（接車→開單→收款）排最前，
// 進貨/庫存次之，管理與設定類最後。新增頁面時掛到對應的 Group。
//
// Path 必須與 main.go 註冊的路徑一致，否則 active 判斷會失效（該項目永遠不會亮起）。
var Routes = []Route{
	{"/", "總覽", "日常作業"},
	{"/customers", "客戶", "日常作業"},
	{"/vehicles", "車輛", "日常作業"},
	{"/work-orders", "工單", "日常作業"},
	{"/estimates", "估價草稿", "日常作業"},
	{"/appointments", "預約", "日常作業"},
	{"/receivables", "收款", "日常作業"},
	{"/inspections", "定檢", "日常作業"},
	{"/purchase-orders", "進貨單", "進貨庫存"},
	{"/suppliers", "廠商", "進貨庫存"},
	{"/inventory", "庫存", "進貨庫存"},
	{"/maintenance-templates", "保養套餐", "保養保險"},
	{"/insurances", "投保", "保養保險"},
	{"/insurance-companies", "保險公司", "保養保險"},
	{"/staff", "員工", "帳務人事"},
	{"/technicians/commissions", "技師佣金", "帳務人事"},
	{"/expenses", "支出", "帳務人事"},
	{"/reports", "報表", "帳務人事"},
	{"/analytics", "經營分析", "帳務人事"},
	{"/ap", "應付帳款", "帳務人事"},
	{"/lookup", "代碼對照", "後台設定"},
	{"/scheduled-tasks", "排程", "後台設定"},
	{"/subcontracts", "外包", "後台設定"},
	{"/equipment", "設備", "後台設定"},
	{"/phrases", "技師片語", "後台設定"},
}

// Render 產生選單的 HTML（只含 navgrp 群組本身，不含 <nav> 標籤）。
// active 是目前頁面路徑，命中的項目會加上 class="on"。
//
// 回傳值已做 HTML escape（Group 與 Name 都是程式內字面值，但保持慣例避免
// 日後有人把使用者可控的字串放進來）。
func Render(active string) string {
	var b strings.Builder
	curGroup := ""
	openDiv := false
	for _, rt := range Routes {
		if rt.Group != curGroup {
			if openDiv {
				b.WriteString(`</div>`)
			}
			curGroup = rt.Group
			openDiv = rt.Group != ""
			if openDiv {
				b.WriteString(`<div class="navgrp"><span class="navlbl">` +
					html.EscapeString(rt.Group) + `</span>`)
			}
		}
		cls := ""
		if rt.Path == active {
			cls = ` class="on"`
		}
		b.WriteString(`<a href="` + rt.Path + `"` + cls + `>` +
			html.EscapeString(rt.Name) + `</a>`)
	}
	if openDiv {
		b.WriteString(`</div>`)
	}
	return b.String()
}
