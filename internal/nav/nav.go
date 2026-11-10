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
	"context"
	"html"
	"strings"

	"autorepairos/internal/i18n"
)

// Route 是選單上的一個項目。Group 相同且連續的項目會被包成同一個 navgrp，
// Group 標籤顯示在每組開頭。
type Route struct {
	Path  string
	Name  string
	Group string
	// NameKey/GroupKey 是 i18n 字典 key；Render 會用 T(ctx, key) 翻譯，
	// 字典缺 key 時 fallback 回 Name/Group（保持中文為預設）。
	NameKey  string
	GroupKey string
	// Roles 限制可見角色；空 slice 表示所有角色都看得到。
	//
	// 必須與 main.go 的權限包裝器一致：CounterUpPage 的頁面只能列
	// owner/counter，否則技師會看到點進去被擋的入口——看得到卻進不去
	// 比看不到更糟，使用者會以為系統壞了。
	//
	// 權威來源是 main.go 的包裝器，不要憑常識猜：/staff、/reports、
	// /technicians/commissions、/purchase-orders、/inventory、/suppliers
	// 都是 AllStaff（技師看得到）。實際只有 6 項受限（counterUp）：
	// /receivables、/expenses、/reports、/analytics、/ap、/lookup。
	// 其中 /reports 是特例：main.go 包的是 AllStaff，但 ReportsHome 內部
	// 用 canSeeMoney 讓技師 403，所以選單仍必須擋（見 nav_roles_sync_test.go
	// 的 effectiveRoleOverrides）。
	//
	// 手冊 docs/USER_GUIDE_zh-TW.md 有同一份清單，由
	// TestManualRoleVisibilityMatchesNav 交叉比對，改這裡要一起改。
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
	{Path: "/", Name: "總覽", Group: "日常作業", NameKey: "nav.home", GroupKey: "group.daily", Roles: nil},
	{Path: "/customers", Name: "客戶", Group: "日常作業", NameKey: "nav.customers", GroupKey: "group.daily", Roles: nil},
	{Path: "/vehicles", Name: "車輛", Group: "日常作業", NameKey: "nav.vehicles", GroupKey: "group.daily", Roles: nil},
	{Path: "/work-orders", Name: "工單", Group: "日常作業", NameKey: "nav.workorders", GroupKey: "group.daily", Roles: nil},
	{Path: "/estimates", Name: "估價草稿", Group: "日常作業", NameKey: "nav.estimates", GroupKey: "group.daily", Roles: nil},
	{Path: "/appointments", Name: "預約", Group: "日常作業", NameKey: "nav.appointments", GroupKey: "group.daily", Roles: nil},
	{Path: "/receivables", Name: "收款", Group: "日常作業", NameKey: "nav.receivables", GroupKey: "group.daily", Roles: counterUp},
	{Path: "/inspections", Name: "定檢", Group: "日常作業", NameKey: "nav.inspections", GroupKey: "group.daily", Roles: nil},
	{Path: "/purchase-orders", Name: "進貨單", Group: "進貨庫存", NameKey: "nav.purchaseOrders", GroupKey: "group.stock", Roles: nil},
	{Path: "/suppliers", Name: "廠商", Group: "進貨庫存", NameKey: "nav.suppliers", GroupKey: "group.stock", Roles: nil},
	{Path: "/inventory", Name: "庫存", Group: "進貨庫存", NameKey: "nav.inventory", GroupKey: "group.stock", Roles: nil},
	{Path: "/maintenance-templates", Name: "保養套餐", Group: "保養保險", NameKey: "nav.maintenanceTemplates", GroupKey: "group.care", Roles: nil},
	{Path: "/sop-templates", Name: "SOP 模板", Group: "保養保險", NameKey: "nav.sopTemplates", GroupKey: "group.care", Roles: nil},
	{Path: "/insurances", Name: "投保", Group: "保養保險", NameKey: "nav.insurances", GroupKey: "group.care", Roles: nil},
	{Path: "/insurance-companies", Name: "保險公司", Group: "保養保險", NameKey: "nav.insuranceCompanies", GroupKey: "group.care", Roles: nil},
	{Path: "/staff", Name: "員工", Group: "帳務人事", NameKey: "nav.staff", GroupKey: "group.account", Roles: nil},
	{Path: "/technicians/commissions", Name: "技師佣金", Group: "帳務人事", NameKey: "nav.commissions", GroupKey: "group.account", Roles: nil},
	{Path: "/expenses", Name: "支出", Group: "帳務人事", NameKey: "nav.expenses", GroupKey: "group.account", Roles: counterUp},
	// /reports 在 main.go 註冊為 AllStaff，但 ReportsHome 開頭即 canSeeMoney(user)
	// → 技師會拿到 403（財務數字屬店內機密）。導覽要反映「實際能不能進去」，
	// 不是包裝器怎麼寫，否則技師看到報表連結卻點不進去。
	{Path: "/reports", Name: "報表", Group: "帳務人事", NameKey: "nav.reports", GroupKey: "group.account", Roles: counterUp},
	{Path: "/analytics", Name: "經營分析", Group: "帳務人事", NameKey: "nav.analytics", GroupKey: "group.account", Roles: counterUp},
	{Path: "/ap", Name: "應付帳款", Group: "帳務人事", NameKey: "nav.ap", GroupKey: "group.account", Roles: counterUp},
	{Path: "/lookup", Name: "代碼對照", Group: "後台設定", NameKey: "nav.lookup", GroupKey: "group.admin", Roles: counterUp},
	{Path: "/scheduled-tasks", Name: "排程", Group: "後台設定", NameKey: "nav.scheduled", GroupKey: "group.admin", Roles: nil},
	{Path: "/subcontracts", Name: "外包", Group: "後台設定", NameKey: "nav.subcontracts", GroupKey: "group.admin", Roles: nil},
	{Path: "/equipment", Name: "設備", Group: "後台設定", NameKey: "nav.equipment", GroupKey: "group.admin", Roles: nil},
	{Path: "/phrases", Name: "技師片語", Group: "後台設定", NameKey: "nav.phrases", GroupKey: "group.admin", Roles: nil},
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
func Render(ctx context.Context, active, role string) string {
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
				groupLabel := rt.Group
				if rt.GroupKey != "" {
					if t := i18n.T(ctx, rt.GroupKey); t != rt.GroupKey {
						groupLabel = t
					}
				}
				b.WriteString(`<div class="navgrp"><span class="navlbl">` +
					html.EscapeString(groupLabel) + `</span>`)
			}
		}
		cls := ""
		if rt.Path == active {
			cls = ` class="on"`
		}
		name := rt.Name
		if rt.NameKey != "" {
			if t := i18n.T(ctx, rt.NameKey); t != rt.NameKey {
				name = t
			}
		}
		b.WriteString(`<a href="` + rt.Path + `"` + cls + `>` +
			html.EscapeString(name) + `</a>`)
	}
	if openDiv {
		b.WriteString(`</div>`)
	}
	return b.String()
}
