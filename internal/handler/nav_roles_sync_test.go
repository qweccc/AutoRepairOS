package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// wrapperRoles 回傳每個權限包裝器允許的角色。
// 依 internal/handler/authz.go 的實作。
var wrapperRoles = map[string][]string{
	"AllStaff":      {"owner", "counter", "technician"},
	"CounterUpPage": {"owner", "counter"},
	"CounterUp":     {"owner", "counter"},
	"OwnerOnly":     {"owner"},
}

// effectiveRoleOverrides 修正「main.go 包裝器允許的角色」與「handler 實際允許的角色」
// 不一致的路徑。
//
// 包裝器只是第一道；有些 handler 內部再做一次更嚴的檢查。導覽必須反映
// 「實際能不能進去」，不是包裝器怎麼寫——漏掉這種情況的症狀就是使用者
// 看到連結、點進去卻被擋。
var effectiveRoleOverrides = map[string][]string{
	// ReportsHome 開頭即 `if !canSeeMoney(user) { 403 }`（財務數字屬店內機密），
	// 但 main.go 註冊為 AllStaff。實際只有 owner/counter 進得去。
	"/reports": {"owner", "counter"},
}

// navRoleVars nav.go 裡代表角色組合的變數。
var navRoleVars = map[string][]string{
	"nil":       {"owner", "counter", "technician"},
	"counterUp": {"owner", "counter"},
	"ownerOnly": {"owner"},
	"allStaff":  {"owner", "counter", "technician"},
}

// TestNavRolesMatchMainGoPermissions 斷言選單的可見角色與 main.go 的權限包裝器一致。
//
// 為什麼需要這支測試：nav.Route.Roles 是手寫的，而真正的權限在 main.go。
// 兩邊走音不會有任何症狀——直到技師看到一個點進去被擋的入口（看得到卻
// 進不去比看不到更糟，使用者會以為系統壞了），或反過來，該看的頁面消失。
// 這種漂移只能靠把兩份定義放在一起比對來防。
//
// 順帶驗證每個選單路徑都有對應的 GET 路由：沒有的話那個選單項會 404。
func TestNavRolesMatchMainGoPermissions(t *testing.T) {
	mainSrc := readRepoFile(t, filepath.Join("internal", "routes", "routes.go"))
	navSrc := readRepoFile(t, filepath.Join("internal", "nav", "nav.go"))

	// main.go: "GET /path", handler.Wrapper(Handler)
	// 注意根路徑寫成 "/{$}"（stdlib ServeMux 的精確匹配語法），
	// 要正規化成 "/" 才能與選單對上。
	routeRe := regexp.MustCompile(`r\.Get\("([^"]*)",\s*(?:handler\.)?([A-Za-z]+)\(`)
	routes := map[string]string{}
	for _, m := range routeRe.FindAllStringSubmatch(mainSrc, -1) {
		routes[normalizeRoutePath(m[1])] = m[2]
	}
	if len(routes) < 20 {
		t.Fatalf("只從 main.go 解析到 %d 條 GET 路由，明顯偏少——解析方式可能已失效，測試等於沒在檢查", len(routes))
	}

	// nav.go: {Path: "/path", Name: "名稱", Group: "群組", ..., Roles: rolesExpr}
	navRe := regexp.MustCompile(`\{Path:\s*"([^"]+)",\s*Name:\s*"[^"]+",\s*Group:\s*"[^"]+",\s*NameKey:\s*"[^"]+",\s*GroupKey:\s*"[^"]+",\s*Roles:\s*([A-Za-z]+)\}`)
	entries := navRe.FindAllStringSubmatch(navSrc, -1)
	if len(entries) < 20 {
		t.Fatalf("只從 nav.go 解析到 %d 個選單項目，明顯偏少——格式可能已變，測試等於沒在檢查", len(entries))
	}

	for _, e := range entries {
		path, rolesExpr := e[1], e[2]

		wantRoles, ok := navRoleVars[rolesExpr]
		if !ok {
			t.Errorf("選單 %q 的 Roles 運算式 %q 不認得，請更新 navRoleVars", path, rolesExpr)
			continue
		}

		wrapper, ok := routes[path]
		if !ok {
			t.Errorf("選單項目 %q 在 main.go 找不到對應的 GET 路由——這個選單項會 404", path)
			continue
		}
		gotRoles, ok := wrapperRoles[wrapper]
		if !ok {
			t.Errorf("路徑 %q 的包裝器 %q 不認得，請更新 wrapperRoles", path, wrapper)
			continue
		}
		// handler 內部若有更嚴的檢查，以實際允許的角色為準
		if eff, ok := effectiveRoleOverrides[path]; ok {
			gotRoles = eff
		}

		if !sameRoleSet(wantRoles, gotRoles) {
			t.Errorf("選單 %q 的可見角色與實際權限不一致：\n  nav.Routes 寫 %v\n  實際可進入的角色 %v（包裝器 %s%s）\n"+
				"（使用者會看到點進去被擋的入口，或該看的頁面消失）",
				path, wantRoles, gotRoles, wrapper,
				func() string {
					if _, ok := effectiveRoleOverrides[path]; ok {
						return "，但 handler 內部另有更嚴的檢查"
					}
					return ""
				}())
		}
	}
}

func sameRoleSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, y := range b {
		if !seen[y] {
			return false
		}
	}
	return true
}

// TestManualRoleVisibilityMatchesNav 斷言手冊裡「技師看不到／看得到」的清單
// 與 internal/nav/nav.go 的實際設定一致。
//
// 為什麼需要：手冊的這段是**對程式的宣稱**，而它不會自己更新——新增頁面或
// 調整角色可見性時，程式改了、手冊沒改，車廠就會照著錯的清單期待。
// 實際踩到：手寫時漏了「排程」（25 項裡技師看得到 19 項，只寫了 18 項）。
// 這種漏字用眼睛看不出來，只能靠比對。
func TestManualRoleVisibilityMatchesNav(t *testing.T) {
	navSrc := readRepoFile(t, filepath.Join("internal", "nav", "nav.go"))
	manual := readRepoFile(t, filepath.Join("docs", "USER_GUIDE_zh-TW.md"))

	entries := regexp.MustCompile(`\{Path:\s*"([^"]+)",\s*Name:\s*"([^"]+)",\s*Group:\s*"[^"]+",\s*NameKey:\s*"[^"]+",\s*GroupKey:\s*"[^"]+",\s*Roles:\s*([A-Za-z]+)\}`).
		FindAllStringSubmatch(navSrc, -1)
	if len(entries) < 20 {
		t.Fatalf("只從 nav.go 解析到 %d 個選單項目，解析方式可能已失效", len(entries))
	}

	var hidden, visible []string
	for _, e := range entries {
		if e[3] != "nil" {
			hidden = append(hidden, e[2])
		} else {
			visible = append(visible, e[2])
		}
	}

	// 手冊的兩個清單（以「、」分隔）
	hiddenLine := manualLineContaining(t, manual, "技師看不到這")
	visibleLine := manualLineContaining(t, manual, "技師看得到**：")

	if !strings.Contains(hiddenLine, "這 "+strconv.Itoa(len(hidden))+" 項") {
		t.Errorf("手冊說「技師看不到這 N 項」但 nav.go 實際是 %d 項\n  手冊: %s", len(hidden), hiddenLine)
	}
	manualHidden := splitIdeographicList(hiddenLine)
	if !sameStringSet(manualHidden, hidden) {
		t.Errorf("手冊的「技師看不到」清單與 nav.go 不一致：\n  手冊: %v\n  nav : %v", manualHidden, hidden)
	}

	manualVisible := splitIdeographicList(visibleLine)
	if !sameStringSet(manualVisible, visible) {
		t.Errorf("手冊的「技師看得到」清單與 nav.go 不一致（漏字或多字）：\n  手冊 (%d): %v\n  nav  (%d): %v",
			len(manualVisible), manualVisible, len(visible), visible)
	}
}

// manualLineContaining 回傳手冊中含有 needle 的那一行（找不到就 Fatal）。
func manualLineContaining(t *testing.T, manual, needle string) string {
	t.Helper()
	for _, line := range strings.Split(manual, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("手冊找不到含 %q 的行——這段說明可能被改寫或移除了，測試需要同步更新", needle)
	return ""
}

// splitIdeographicList 取出「：」之後以「、」分隔的項目（去掉句尾標點）。
func splitIdeographicList(line string) []string {
	if i := strings.Index(line, "："); i >= 0 {
		line = line[i+len("："):]
	}
	line = strings.TrimSpace(line)
	line = strings.TrimSuffix(line, "。")
	line = strings.TrimSuffix(line, "）")
	// 去掉行內的括號補充說明
	if i := strings.Index(line, "（"); i >= 0 {
		line = line[:i]
	}
	var out []string
	for _, p := range strings.Split(line, "、") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, y := range b {
		if !seen[y] {
			return false
		}
	}
	return true
}

// normalizeRoutePath 把 main.go 的路由樣式正規化成選單用的路徑。
// 根路徑在 stdlib ServeMux 寫成 "/{$}"（精確匹配語法），選單寫 "/"。
func normalizeRoutePath(p string) string {
	return strings.Replace(p, "{$}", "", 1)
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("讀取 %s 失敗: %v", rel, err)
	}
	return string(raw)
}

// TestNavPathsHaveGetRoutes 單獨檢查「每個選單路徑都有 GET 路由」，
// 讓 404 的死連結有一個明確的失敗訊息（而不是混在角色比對裡）。
func TestNavPathsHaveGetRoutes(t *testing.T) {
	mainSrc := readRepoFile(t, filepath.Join("internal", "routes", "routes.go"))
	navSrc := readRepoFile(t, filepath.Join("internal", "nav", "nav.go"))

	routeRe := regexp.MustCompile(`r\.Get\("([^"]*)",`)
	routes := map[string]bool{}
	for _, m := range routeRe.FindAllStringSubmatch(mainSrc, -1) {
		routes[normalizeRoutePath(m[1])] = true
	}

	navRe := regexp.MustCompile(`\{"(/[^"]*)",\s*"([^"]*)",\s*"([^"]*)",`)
	for _, m := range navRe.FindAllStringSubmatch(navSrc, -1) {
		if !routes[m[1]] {
			t.Errorf("選單項目 %q（%s）沒有對應的 GET 路由，點下去會 404", m[1], m[2])
		}
	}
}
