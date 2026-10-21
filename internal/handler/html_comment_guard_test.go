package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoGoCommentsInsideHTMLStrings 掃描 handler 套件，找出「被寫在 HTML
// 字串裡面的 // 註解」。
//
// 為什麼需要這條：本專案的頁面是手寫的 HTML 字串（不是 templ），而 Go 的
// // 註解只在**程式碼**裡有意義。寫在字串裡它就是一串普通文字，會原封不動
// 送到瀏覽器、直接顯示在畫面上給車廠看。
//
// 實際踩到：為 T4.6 的表單加 appointment_id 時，把「為什麼留在表單裡」的
// 說明用 // 寫在 <form> 與 <input> 之間——畫面上就會出現三行中文註解。
// 這種錯誤編譯器不會講話（字串內容一律合法），測試也不會失敗（HTML 還是
// 有效的），只有實際打開頁面才看得到。
//
// 判斷方式：逐行追蹤是否位於反引號 raw string 內；在 raw string 內且行首
// （去縮排後）是 // 的行必然是誤寫。限定 .go 檔——.templ 的 // 是 templ
// 語法內的 Go 註解，不在此列。
//
// 例外：<script> 區塊裡的 // 是**合法的 JavaScript 註解**，不會顯示給使用者
// （本專案的表單頁把 JS 直接寫在 HTML 字串裡）。因此要追蹤 script 狀態，
// 只檢查 script 之外的 // 行。
func TestNoGoCommentsInsideHTMLStrings(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")

		inRaw := false
		inScript := false
		for i, line := range lines {
			// 統計本行的反引號數量（跳脫不適用於 raw string，故直接數）
			backticks := strings.Count(line, "`")

			// 本行「開始時」若在 raw string 內、不在 script 內，
			// 且內容看起來像註解 → 誤寫
			if inRaw && !inScript {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					offenders = append(offenders, filepath.ToSlash(name)+":"+itoa(int64(i+1))+
						": "+strings.TrimSpace(line))
				}
			}

			// 追蹤 script 區塊（只在本行確實是 HTML 內容時才切換）
			if inRaw {
				if strings.Contains(line, "<script") {
					inScript = true
				}
				if strings.Contains(line, "</script>") {
					inScript = false
				}
			}

			// 切換 raw string 狀態：奇數個反引號代表狀態反轉
			if backticks%2 == 1 {
				inRaw = !inRaw
				inScript = false // 離開字串時重設
			}
		}
	}

	if len(offenders) > 0 {
		t.Errorf("發現 %d 處 // 註解被寫在 HTML 字串裡——這些文字會直接顯示在畫面上：\n  %s\n\n"+
			"改法：把說明移到字串外的 Go 註解，或改用 <!-- -->（若確實要留在 HTML 裡）。",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
