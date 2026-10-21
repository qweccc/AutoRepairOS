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
