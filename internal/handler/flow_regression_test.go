package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// splitTopLevelArgs 依 depth 0 的逗號切開參數，忽略字串與括號內部的逗號。
//
// 為什麼不能只用 regex：Layout 的第一個參數常是
// `"進貨單 #"+strconv.FormatInt(po.ID, 10)`，裡面就有逗號。
// 用 regex 的 [^,]+ 會在那個逗號斷掉，抓到的「第三個參數」其實是標題的殘骸
// （實際踩過：三個呼叫點因此逃過掃描）。
func splitTopLevelArgs(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	inStr := false
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
				continue
			}
			if c == q {
				inStr = false
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inStr, q = true, c
			cur.WriteByte(c)
		case '(', '[', '{':
			depth++
			cur.WriteByte(c)
		case ')', ']', '}':
			depth--
			cur.WriteByte(c)
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
				continue
			}
			cur.WriteByte(c)
		default:
			cur.WriteByte(c)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

// matchParen 回傳 s[open] 對應的 ')' 索引。
func matchParen(s string, open int) int {
	depth := 0
	inStr := false
	var q byte
	for i := open; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' {
				i++
				continue
			}
			if c == q {
				inStr = false
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inStr, q = true, c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// TestLayoutAlwaysReceivesUser 斷言每個 components.Layout 呼叫的第三個參數
// 都是 `user`（即 model.User 變數），不是字串字面值或別的變數。
//
// 為什麼需要這支測試：Layout 的簽名已改成 (title, active string, user model.User,
// appName string)，編譯器已能擋掉傳字串字面值。這支測試補的是編譯器擋不到的
// 那一類——把「不是當前登入者」的 model.User 傳進去（例如把客戶物件誤傳成
// 登入者）。掃描原始碼是唯一能覆蓋全部頁面的方式：逐一對 28 個頁面發 HTTP
// 請求要各自登入一次，慢、而且新增頁面時容易漏。
func TestLayoutAlwaysReceivesUser(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "web", "pages", "*.templ"))
	if err != nil {
		t.Fatal(err)
	}
	comps, err := filepath.Glob(filepath.Join("..", "..", "web", "components", "*.templ"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, comps...)
	if len(files) == 0 {
		t.Fatal("找不到任何 .templ 檔，路徑假設可能已失效")
	}

	const marker = "components.Layout("
	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for off := 0; ; {
			i := strings.Index(src[off:], marker)
			if i < 0 {
				break
			}
			lp := off + i + len(marker) - 1
			rp := matchParen(src, lp)
			if rp < 0 {
				t.Fatalf("%s: Layout 呼叫的括號沒有閉合", filepath.Base(f))
			}
			args := splitTopLevelArgs(src[lp+1 : rp])
			if len(args) != 4 {
				t.Errorf("%s: Layout 應有 4 個參數（title, active, user, appName），實際 %d 個: %q",
					filepath.Base(f), len(args), args)
			} else if args[2] != "user" {
				line := 1 + strings.Count(src[:lp], "\n")
				t.Errorf("%s:%d: Layout 的第三個參數必須是 user，實際是 %q（會讓頁首顯示錯誤的登入者）",
					filepath.Base(f), line, args[2])
			}
			checked++
			off = rp
		}
	}
	if checked < 25 {
		t.Errorf("只掃到 %d 個 Layout 呼叫，明顯偏少——marker 或路徑可能已變，測試等於沒在檢查", checked)
	}
	t.Logf("已檢查 %d 個 Layout 呼叫", checked)
}

// TestNewCustomerFormHasNoLineUserIDField 斷言「新增客戶」表單裡沒有
// line_user_id 欄位。
//
// 為什麼：CreateCustomer 明確不讀這個欄位（只讀 telegram_user_id），
// 留著輸入框只會讓人以為填了就會綁定——實際被靜默丟棄，通知永遠送不到，
// 而且不會有任何錯誤訊息。LINE 綁定必須走編輯頁的專用端點（有唯一性約束）。
//
// 只掃表單區塊而非整個檔案：客戶列表的 <th>LINE ID</th> 是顯示欄位，
// 掃全檔會誤判。
func TestNewCustomerFormHasNoLineUserIDField(t *testing.T) {
	path := filepath.Join("..", "..", "web", "pages", "customer.templ")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)

	const formOpen = `action="/customers"`
	i := strings.Index(src, formOpen)
	if i < 0 {
		t.Fatal("找不到 action=\"/customers\" 的新增客戶表單，路徑或寫法可能已變，測試等於沒在檢查")
	}
	j := strings.Index(src[i:], "</form>")
	if j < 0 {
		t.Fatal("新增客戶表單沒有 </form>，無法界定範圍")
	}
	form := src[i : i+j]

	if strings.Contains(form, `name="line_user_id"`) {
		t.Error("新增客戶表單不該有 line_user_id 欄位：CreateCustomer 不讀它，填了會被靜默丟棄；" +
			"LINE 綁定應走編輯頁的 /customers/{id}/line-bind 專用端點")
	}
	if !strings.Contains(form, `name="telegram_user_id"`) {
		t.Error("新增客戶表單應保留 telegram_user_id（客戶可用 @userinfobot 查到自己的 ID，直接輸入是合理的）")
	}
}

// TestBothLayoutsLoadOfflineJS 斷言兩種版面都載入 /offline.js。
//
// 為什麼需要這支測試：全站有兩個版面——templ 頁用 web/components/layout.templ，
// 字串拼接頁用 internal/handler/layout.go 的 renderPage。離線模組原本只掛在
// 後者，於是所有 templ 頁（含工單照片頁）都拿不到手冊承諾的「離線暫存、
// 恢復連線後補傳」。這種「只補了一半」的症狀是靜默的：頁面看起來完全正常，
// 只有真的斷線時才發現資料沒被暫存。
func TestBothLayoutsLoadOfflineJS(t *testing.T) {
	layouts := []string{
		filepath.Join("..", "..", "web", "components", "layout.templ"),
		filepath.Join("..", "..", "internal", "handler", "layout.go"),
	}
	for _, f := range layouts {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `src="/offline.js"`) {
			t.Errorf("%s 沒有載入 /offline.js——該版面渲染的頁面拿不到離線暫存與上傳佇列（頁面看起來正常，只有斷線時才發現資料沒被保存）",
				filepath.Base(f))
		}
	}
}

// TestOfflineJSServedAtRootPath 斷言 /offline.js 掛在根路徑，且 sw.js 把它
// 列在 NETWORK_FIRST。
//
// 為什麼：/offline.js 同時被 SHELL 預快取，若不在 NETWORK_FIRST 就會落到
// service worker 最後的 cache-first 分支——修好 offline.js 之後既有瀏覽器
// 永遠拿到舊版（而這個檔案負責離線佇列，錯了會靜默丟資料）。車廠人員不會
// 按 Shift+F5。
func TestOfflineJSServedAtRootPath(t *testing.T) {
	sw, err := os.ReadFile(filepath.Join("..", "..", "internal", "handler", "static", "sw.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(sw)

	nf := src
	if i := strings.Index(src, "const NETWORK_FIRST"); i >= 0 {
		if j := strings.Index(src[i:], "];"); j >= 0 {
			nf = src[i : i+j]
		}
	}
	if !strings.Contains(nf, "'/offline.js'") {
		t.Error("sw.js 的 NETWORK_FIRST 沒有 '/offline.js'——它同時在 SHELL 被預快取，" +
			"會落到 cache-first 分支，改版後既有瀏覽器永遠拿到舊檔")
	}
	if strings.Contains(nf, "'/static/offline.js'") {
		t.Error("/offline.js 掛在根路徑（internal/handler/static.go 的 RegisterStatic），" +
			"不是 /static/ 底下；寫成 /static/offline.js 會讓 network-first 判斷永遠不命中")
	}
}
