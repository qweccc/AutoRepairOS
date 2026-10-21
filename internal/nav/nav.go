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
	// Roles 限制可見角色；空 slice 表示所有角色都看得到。
	//
	// 必須與 main.go 的權限包裝器一致：CounterUpPage 的頁面只能列
	// owner/counter，否則技師會看到點進去被擋的入口——看得到卻進不去
	// 比看不到更糟，使用者會以為系統壞了。
	//
	// 權威來源是 main.go 的包裝器，不要憑常識猜：/staff、/reports、
	// /technicians/commissions、/purchase-orders、/inventory、/suppliers
	// 都是 AllStaff（技師看得到），實際只有 5 項受限。
	Roles []string
}

// 角色常數（與 internal/handler/authz.go 的 CHECK 約束一致）。
const (
	RoleOwner      = "owner"
	RoleCounter    = "counter"
	RoleTechnician = "technician"
)

// 常用角色組合，避免每個項目重複打一遍。
var (
	counterUp = []string{RoleOwner, RoleCounter}
)

// Routes 全站選單。
//
// 分組順序依車行作業節奏排：櫃檯最常用（接車→開單→收款）排最前，
// 進貨/庫存次之，管理與設定類最後。新增頁面時掛到對應的 Group。
//
// Path 必須與 main.go 註冊的路徑一致，否則 active 判斷會失效（該項目永遠不會亮起）。
// Roles 必須與 main.go 的權限包裝器一致（見 Route.Roles 的說明）。
var Routes = []Route{
	{"/", "總覽", "日常作業", nil},
	{"/customers", "客戶", "日常作業", nil},
	{"/vehicles", "車輛", "日常作業", nil},
	{"/work-orders", "工單", "日常作業", nil},
	{"/estimates", "估價草稿", "日常作業", nil},
	{"/appointments", "預約", "日常作業", nil},
	{"/receivables", "收款", "日常作業", counterUp},
	{"/inspections", "定檢", "日常作業", nil},
	{"/purchase-orders", "進貨單", "進貨庫存", nil},
	{"/suppliers", "廠商", "進貨庫存", nil},
	{"/inventory", "庫存", "進貨庫存", nil},
	{"/maintenance-templates", "保養套餐", "保養保險", nil},
	{"/insurances", "投保", "保養保險", nil},
	{"/insurance-companies", "保險公司", "保養保險", nil},
	{"/staff", "員工", "帳務人事", nil},
	{"/technicians/commissions", "技師佣金", "帳務人事", nil},
	{"/expenses", "支出", "帳務人事", counterUp},
	// /reports 在 main.go 註冊為 AllStaff，但 ReportsHome 開頭即 canSeeMoney(user)
	// → 技師會拿到 403（財務數字屬店內機密）。導覽要反映「實際能不能進去」，
	// 不是包裝器怎麼寫，否則技師看到報表連結卻點不進去。
	{"/reports", "報表", "帳務人事", counterUp},
	{"/analytics", "經營分析", "帳務人事", counterUp},
	{"/ap", "應付帳款", "帳務人事", counterUp},
	{"/lookup", "代碼對照", "後台設定", counterUp},
	{"/scheduled-tasks", "排程", "後台設定", nil},
	{"/subcontracts", "外包", "後台設定", nil},
	{"/equipment", "設備", "後台設定", nil},
	{"/phrases", "技師片語", "後台設定", nil},
}

// CanSee 回報 role 是否看得到這個選單項目。Roles 為空表示不限制。
//
// 未知或空字串的角色一律看不到受限項目（fail-closed）：寧可少顯示，
// 不要顯示一個點進去被擋的入口。
func (rt Route) CanSee(role string) bool {
	if len(rt.Roles) == 0 {
		return true
	}
	for _, r := range rt.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Render 產生選單的 HTML（只含 navgrp 群組本身，不含 <nav> 標籤）。
// active 是目前頁面路徑，命中的項目會加上 class="on"。
// role 是目前登入者的角色，用來過濾看不到的項目。
//
// 回傳值已做 HTML escape（Group 與 Name 都是程式內字面值，但保持慣例避免
// 日後有人把使用者可控的字串放進來）。
func Render(active, role string) string {
	var b strings.Builder
	curGroup := ""
	openDiv := false
	for _, rt := range Routes {
		if !rt.CanSee(role) {
			continue
		}
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
