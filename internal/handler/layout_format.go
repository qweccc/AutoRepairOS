package handler

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/i18n"
)

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

func atofDefault(s string, def float64) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return def
	}
	return f
}

func today() string { return time.Now().Format("2006-01-02") }

// nowStamp 現在時間（秒級），供庫存異動等紀錄使用。
func nowStamp() string { return time.Now().Format("2006-01-02 15:04:05") }

// parseFlexTime 寬鬆解析 SQLite 的時間字串。
//
// 為什麼需要：舊系統 CAR35 遷移進來的 created_at 是 '2016/02/22'，
// 新開的工單則是 '2006-01-02 15:04:05'。若直接 Scan 進 time.Time，
// driver 解析失敗會讓整個 Scan 出錯，而呼叫端多半 `continue` 掉，
// 結果是「查得到但一列都不顯示」——非常難察覺的靜默失敗。
//
// 這裡改成先掃成字串再自行解析，無法解析時回傳零值而非報錯。
func parseFlexTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"2006/01/02 15:04:05",
		"2006/01/02",
		"2006/1/2",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	// 只取前 10 碼再試一次（容忍尾端有雜訊）
	if len(s) > 10 {
		for _, l := range []string{"2006-01-02", "2006/01/02"} {
			if t, err := time.Parse(l, s[:10]); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// nullDate 處理 SQLite DATE 欄位：空字串或非法值回傳空字串。
func nullDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}

func nullStr(s string) string { return strings.TrimSpace(s) }

func htmlEsc(s string) string { return html.EscapeString(s) }

func selIf(cond bool) string {
	if cond {
		return " selected"
	}
	return ""
}

// 狀態／核准標籤一律走 i18n 字典（見 locales/*.json 的 workorder.status.*、
// workorder.approval.*）。字典缺鍵時退回原始代碼，避免畫面直接出現
// "workorder.status.xxx" 這種 key。
func workOrderStatusLabel(ctx context.Context, status string) string {
	key := "workorder.status." + status
	if v := i18n.T(ctx, key); v != key {
		return v
	}
	return status
}

func approvalLabel(ctx context.Context, status string) string {
	key := "workorder.approval." + status
	if v := i18n.T(ctx, key); v != key {
		return v
	}
	return status
}

var knownWorkOrderStatus = map[string]bool{
	"pending":       true,
	"in_progress":   true,
	"waiting_parts": true,
	"completed":     true,
	"paid":          true,
}

var knownApprovalStatus = map[string]bool{
	"not_required": true,
	"pending":      true,
	"approved":     true,
	"rejected":     true,
}

func isKnownWorkOrderStatus(s string) bool { return knownWorkOrderStatus[s] }
func isKnownApprovalStatus(s string) bool  { return knownApprovalStatus[s] }

var statusPill = map[string]string{
	"pending":       "p-pending",
	"in_progress":   "p-progress",
	"waiting_parts": "p-waitparts",
	"completed":     "p-completed",
	"paid":          "p-paid",
}

var approvalPill = map[string]string{
	"not_required": "",
	"pending":      "p-apprpend",
	"approved":     "p-appryes",
	"rejected":     "p-apprno",
}

func money(f float64) string { return fmt.Sprintf("%.0f", f) }

// pctText 把 part/total 算成百分比字串，供報表說明行使用。
// total 為 0 時回傳 "—"，避免除以零顯示 NaN。
func pctText(part, total float64) string {
	if total <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", part/total*100)
}

func dateText(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func mustInt(s string) int64 { return int64(atoiDefault(s, 0)) }

// countRows 給統計卡使用，查詢失敗時回 0。
func countRows(query string, args ...interface{}) int {
	var n int
	if err := db.DB.QueryRow(query, args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

// ============================================================
// 分頁
// ============================================================
//
// 為什麼需要：舊系統 CAR35 有 9.5 萬張工單、1.5 萬台車、8 千位客戶，
// 全部遷移進來後，列表頁若一次查全部會渲染數萬列，瀏覽器直接卡死。
// 這裡統一提供 LIMIT/OFFSET 分頁與頁碼導覽。

const pageSizeDefault = 50

// pageSize 讀取每頁筆數，限制在 10..500，預設 50。
func pageSize(r *http.Request) int {
	n := atoiDefault(r.URL.Query().Get("per"), pageSizeDefault)
	if n < 10 {
		n = 10
	}
	if n > 500 {
		n = 500
	}
	return n
}

// pageNo 讀取頁碼，最小為 1。
func pageNo(r *http.Request) int {
	n := atoiDefault(r.URL.Query().Get("page"), 1)
	if n < 1 {
		n = 1
	}
	return n
}

// pager 頁碼導覽列。
//
// path    列表頁路徑，如 "/work-orders"
// query   既有的查詢參數（不含 page），會被保留在換頁連結裡
// total   符合條件的總筆數
// page    目前頁碼（1 起算）
// size    每頁筆數
func pager(path string, query url.Values, total, page, size int) string {
	if size <= 0 || total <= size {
		return ""
	}
	last := (total + size - 1) / size
	if page > last {
		page = last
	}

	link := func(p int, label, cls string) string {
		q := url.Values{}
		for k, vs := range query {
			if k == "page" {
				continue
			}
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		q.Set("page", strconv.Itoa(p))
		return `<a class="btn ` + cls + `" href="` + path + `?` + q.Encode() + `">` + label + `</a>`
	}

	var b strings.Builder
	b.WriteString(`<div style="margin:14px 0;display:flex;gap:6px;align-items:center;flex-wrap:wrap">`)
	if page > 1 {
		b.WriteString(link(1, "« 首頁", "btn-s"))
		b.WriteString(link(page-1, "‹ 上一頁", "btn-s"))
	} else {
		b.WriteString(`<span class="btn btn-s" style="opacity:.35">« 首頁</span>`)
		b.WriteString(`<span class="btn btn-s" style="opacity:.35">‹ 上一頁</span>`)
	}

	// 目前頁附近最多顯示 7 個頁碼
	start := page - 3
	if start < 1 {
		start = 1
	}
	end := start + 6
	if end > last {
		end = last
		start = end - 6
		if start < 1 {
			start = 1
		}
	}
	for p := start; p <= end; p++ {
		if p == page {
			b.WriteString(`<span class="btn btn-p">` + strconv.Itoa(p) + `</span>`)
		} else {
			b.WriteString(link(p, strconv.Itoa(p), "btn-s"))
		}
	}

	if page < last {
		b.WriteString(link(page+1, "下一頁 ›", "btn-s"))
		b.WriteString(link(last, "末頁 »", "btn-s"))
	} else {
		b.WriteString(`<span class="btn btn-s" style="opacity:.35">下一頁 ›</span>`)
		b.WriteString(`<span class="btn btn-s" style="opacity:.35">末頁 »</span>`)
	}

	b.WriteString(`<span style="color:#94a3b8;font-size:12.5px;margin-left:10px">第 ` +
		strconv.Itoa(page) + ` / ` + strconv.Itoa(last) + ` 頁，共 ` +
		strconv.Itoa(total) + ` 筆</span>`)
	b.WriteString(`</div>`)
	return b.String()
}

// perPageSel 每頁筆數選擇器。
func perPageSel(path string, query url.Values, size int) string {
	link := func(n int) string {
		q := url.Values{}
		for k, vs := range query {
			if k == "per" || k == "page" {
				continue
			}
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		q.Set("per", strconv.Itoa(n))
		cls := "btn-s"
		if n == size {
			cls = "btn-p"
		}
		return `<a class="btn ` + cls + `" href="` + path + `?` + q.Encode() + `">` + strconv.Itoa(n) + `</a>`
	}
	return `<span style="color:#64748b;font-size:12.5px;margin-left:8px">每頁</span> ` +
		link(20) + link(50) + link(100) + link(200)
}

// hiddenInputs 把查詢參數（排除指定 key）輸出成 hidden input，供表單保留篩選條件。
func hiddenInputs(query url.Values, exclude ...string) string {
	skip := map[string]bool{}
	for _, e := range exclude {
		skip[e] = true
	}
	var b strings.Builder
	for k, vs := range query {
		if skip[k] {
			continue
		}
		for _, v := range vs {
			if v == "" {
				continue
			}
			b.WriteString(`<input type="hidden" name="` + htmlEsc(k) + `" value="` + htmlEsc(v) + `">`)
		}
	}
	return b.String()
}
