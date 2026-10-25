package pages

import (
	"strings"
	"testing"
)

// TestTagPillsHTML_EscapesXSS 驗證 S4 修補：客戶標籤含 <script> 時，
// tagPillsHTML 必須 escape 成純文字，不得產生可執行的 <script> 標籤。
func TestTagPillsHTML_EscapesXSS(t *testing.T) {
	cases := []string{
		"<script>alert(1)</script>",
		"><img src=x onerror=alert(1)>",
		"normal,tag",
		"a'\"b",
	}
	for _, in := range cases {
		out := tagPillsHTML(in)
		// 原始 < > 不得出現在輸出（應已被 escape 成 &lt; &gt;）
		if strings.Contains(out, "<script>") {
			t.Errorf("tagPillsHTML(%q) 未 escape <script>：得 %q", in, out)
		}
		if strings.Contains(out, "<img") {
			t.Errorf("tagPillsHTML(%q) 未 escape <img：得 %q", in, out)
		}
		// 正常標籤文字應保留（escape 後為實體字元）
		if in == "normal,tag" && !strings.Contains(out, "normal") {
			t.Errorf("tagPillsHTML(%q) 遺漏正常文字：得 %q", in, out)
		}
	}
	// 空字串應回佔位符，不 panic
	if tagPillsHTML("") == "" {
		t.Error("tagPillsHTML(\"\") 不應回空字串")
	}
}
