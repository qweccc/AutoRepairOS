package routes

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestNewRegistersPhase1Routes(t *testing.T) {
	r := New(Deps{})
	var paths []string
	chi.Walk(r, func(method, route string, h http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		paths = append(paths, method+" "+route)
		return nil
	})
	want := map[string]bool{
		"GET /api/parts":              true,
		"GET /api/customers/search":   true,
		"GET /api/inventory/search":   true,
		"GET /api/work-orders/search": true,
		"GET /api/vehicles/search":    true,
	}
	for _, p := range paths {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("missing routes: %v", want)
	}
}

// TestEveryRenderedPathHasARoute 防止「路由遷移漏掛」再次發生。
//
// 起因：35195c1 把路由從 main.go 搬到 routes.go 時漏了一批，畫面上
// 「新增／編輯／匯出／搜尋」全部回 404（handler 與連結都還在，只有注册消失）。
// 這種漏不會讓 build 或既有測試變紅——只有真的按到才知道。
//
// 做法：掃 .templ 原始檔裡寫死的 href/action/hx-get/hx-post（跳過字串拼接），
// 斷言每個路徑都能對上一條已註冊的 GET 或 POST 路由（含 {param} 形狀）。
func TestEveryRenderedPathHasARoute(t *testing.T) {
	r := New(Deps{})
	var patterns []string
	chi.Walk(r, func(method, route string, h http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		patterns = append(patterns, method+" "+route)
		return nil
	})
	matchers := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		matchers = append(matchers, routeToRegex(p))
	}

	candidates := map[string]bool{}
	attrRe := regexp.MustCompile(`(?s)(?:href|action|hx-get|hx-post)\s*=\s*"(/[^"]*)"`)
	for _, f := range templFiles(t, "../../web") {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range attrRe.FindAllStringSubmatch(string(src), -1) {
			p := strings.SplitN(m[1], "?", 2)[0]
			p = strings.TrimSuffix(p, "/")
			if p == "" || strings.ContainsAny(p, "+${}") || strings.HasPrefix(p, "//") {
				continue // 動態拼接或外部網址，無法定靜態對帳
			}
			if strings.HasPrefix(p, "/static/") {
				continue // 靜態資源由 internal/handler/static.go 的 embed 服務，不在路由表
			}
			candidates[p] = true
		}
	}
	if len(candidates) < 30 {
		t.Fatalf("只掃到 %d 個靜態路徑，掃描方式可能已失效（歸零就等於沒在檢查）", len(candidates))
	}

	var missing []string
	for p := range candidates {
		ok := false
		for _, re := range matchers {
			if re.MatchString(p) {
				ok = true
				break
			}
		}
		if !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("這些畫面上的路徑找不到對應路由（點選會 404）：\n  %s", strings.Join(missing, "\n  "))
	}
}

// routeToRegex 把 chi 的路由_pattern（含 {param}、{$}）轉成可比對路徑的 regex。
func routeToRegex(methodAndRoute string) *regexp.Regexp {
	parts := strings.SplitN(methodAndRoute, " ", 2)
	p := parts[1]
	p = strings.TrimSuffix(p, "/{$}")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		p = "/"
	}
	var b strings.Builder
	b.WriteString("^")
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		b.WriteString("/")
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			b.WriteString("[^/]+")
			continue
		}
		b.WriteString(regexp.QuoteMeta(seg))
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func templFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".templ") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
