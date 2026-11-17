package vision

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var normalizedDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// FuzzNormalizeDate 鎖住日期正規化的契約：結果只能是「空」、「去掉空白的原字串」
// 或標準的 YYYY-MM-DD，且月／日落在合理範圍。
// 模糊測試專撞邊界（民國年、各種分隔符、2/30、非數字），確認不會吐出壞值或 panic。
func FuzzNormalizeDate(f *testing.F) {
	for _, s := range []string{
		"115/10/14", "2026-10-14", "2026.10.14", "2026 10 14",
		"", "   ", " 115/10/14 ", "2026/02/30", "115/13/01",
		"abc", "115/10/14/1", "0/1/1", "9999-12-31", "115/ 10/14",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := normalizeDate(in)
		if got == "" {
			if strings.TrimSpace(in) != "" {
				t.Fatalf("normalizeDate(%q) 回空值，但輸入非空", in)
			}
			return
		}
		if got == strings.TrimSpace(in) {
			return // 解析不了時回原值是既有設計
		}
		if !normalizedDateRe.MatchString(got) {
			t.Fatalf("normalizeDate(%q) = %q：既非原值也非 YYYY-MM-DD", in, got)
		}
		m, errM := strconv.Atoi(got[5:7])
		d, errD := strconv.Atoi(got[8:10])
		if errM != nil || errD != nil {
			t.Fatalf("normalizeDate(%q) = %q：月份／日期不是數字", in, got)
		}
		if m < 1 || m > 12 || d < 1 || d > 31 {
			t.Fatalf("normalizeDate(%q) = %q：月／日超出範圍", in, got)
		}
	})
}

// FuzzJSONNumber 鎖住金額解析的契約：必須回有限數字；合法 JSON number 不得被改寫
// （OCR 回填的金額全靠它，靜默漏抓就是錢算錯）。
func FuzzJSONNumber(f *testing.F) {
	for _, s := range []string{
		`123`, `-5`, `1.5`, `"1,234"`, `"NT$99"`, `"$99"`, `" 42 "`,
		`"abc"`, `""`, `null`, `true`, `[1]`, `{"a":1}`, `"1e3"`, `NaN`, `1e999`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		n := jsonNumber(json.RawMessage(raw))
		if math.IsNaN(n) || math.IsInf(n, 0) {
			t.Fatalf("jsonNumber(%s) = %v：不可回 NaN／Inf", raw, n)
		}
		var num float64
		if err := json.Unmarshal([]byte(raw), &num); err == nil && n != num {
			t.Fatalf("jsonNumber(%s) = %v，但它是合法 JSON number，應為 %v", raw, n, num)
		}
	})
}
