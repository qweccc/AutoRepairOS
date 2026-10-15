package handler

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	navpkg "autorepairos/internal/nav"
)

// appRoutes 與 nav() 已移至 internal/nav（單一選單定義來源）。
// 此處只保留 renderPage 需要的頁面樣式，且不再自帶 nav/shell 版面規則——
// 字串拼接頁與 templ 頁共用同一份 internal/handler/static/app.css，
// 避免兩邊版面規則走音（症狀：總覽與其他頁選單格式不同）。
const pageStyle = `
  * { box-sizing: border-box; }
  body { margin: 0; font-family: "Segoe UI", "Microsoft JhengHei", sans-serif;
         background: #0f172a; color: #e2e8f0; }
  a { color: #38bdf8; }
  header { background: #1e293b; padding: 14px 28px; border-bottom: 2px solid #334155;
           display: flex; align-items: center; justify-content: space-between; }
  header h1 { margin: 0; font-size: 20px; }
  .user { font-size: 13px; color: #cbd5e1; }
  .badge { background: #38bdf8; color: #0f172a; padding: 2px 10px;
           border-radius: 999px; font-weight: 700; font-size: 11px; }
  .logout { margin-left: 14px; padding: 6px 14px; background: #ef4444; color: #fff;
            border-radius: 8px; text-decoration: none; font-weight: 700; font-size: 12px; }
  .card { background: #1e293b; border: 1px solid #334155; border-radius: 12px;
          padding: 20px; margin-bottom: 18px; }
  .card h2 { margin: 0 0 14px 0; color: #38bdf8; font-size: 17px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { text-align: left; padding: 9px 10px; border-bottom: 1px solid #334155; font-size: 13.5px; }
  th { color: #94a3b8; font-weight: 600; }
  tr:hover td { background: #263449; }
  .stat { display: inline-block; margin: 0 32px 12px 0; text-decoration: none; }
  .stat:hover b { color: #7dd3fc; }
  .stat b { font-size: 26px; color: #f8fafc; display: block; }
  .stat span { color: #94a3b8; font-size: 12px; }
  .pill { padding: 2px 9px; border-radius: 999px; font-size: 11.5px; font-weight: 700; }
  .p-pending    { background: #475569; color: #e2e8f0; }
  .p-progress   { background: #1d4ed8; color: #dbeafe; }
  .p-waitparts  { background: #b45309; color: #fef3c7; }
  .p-completed  { background: #047857; color: #d1fae5; }
  .p-paid       { background: #065f46; color: #a7f3d0; }
  .p-apprpend   { background: #b45309; color: #fef3c7; }
  .p-appryes    { background: #047857; color: #d1fae5; }
  .p-apprno     { background: #7f1d1d; color: #fecaca; }
  .btn { display: inline-block; padding: 6px 13px; border-radius: 7px; font-size: 12.5px;
         text-decoration: none; font-weight: 600; border: 0; cursor: pointer; }
  .btn-p { background: #38bdf8; color: #0f172a; }
  .btn-s { background: #334155; color: #e2e8f0; }
  .btn-d { background: #7f1d1d; color: #fecaca; }
  .btn-o { background: #92400e; color: #fde68a; }
  form.inline { display: inline; }
  .frm { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; }
  .frm label { display: block; font-size: 12px; color: #94a3b8; }
  .frm input, .frm select, .frm textarea {
    width: 100%; padding: 8px 10px; margin-top: 4px; border-radius: 7px;
    border: 1px solid #334155; background: #0f172a; color: #e2e8f0; font-size: 13.5px;
    font-family: inherit; }
  .frm textarea { height: 62px; resize: vertical; }
  .frm .full { grid-column: 1 / -1; }
  .msg { padding: 10px 14px; border-radius: 8px; margin-bottom: 16px; font-size: 13.5px; }
  .msg-ok { background: #052e16; color: #86efac; border: 1px solid #166534; }
  .msg-err { background: #2a0b0b; color: #fca5a5; border: 1px solid #7f1d1d; }
  .empty { color: #64748b; padding: 26px; text-align: center; font-size: 13.5px; }
  .num { text-align: right; }
  .pgrid { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 14px; }
  .pcard { margin: 0; background: #0f172a; border: 1px solid #334155; border-radius: 10px; overflow: hidden; }
  .pcard img { width: 100%; height: 148px; object-fit: cover; display: block; }
  .pcard figcaption { padding: 10px; font-size: 12.5px; }
  .pname { font-weight: 600; margin: 6px 0 3px; word-break: break-all; }
  .pmeta { color: #64748b; font-size: 11.5px; }
  .pnote { color: #94a3b8; font-size: 12px; margin-top: 5px; font-style: italic; }
  .pcard .btn { margin-top: 8px; }
  @media (max-width: 768px) {
    header { padding: 10px 14px; }
    header h1 { font-size: 16px; }
    .user { font-size: 11px; }
    /* 窄屏：.shell 恢復 flex column 才能讓 nav 的 order:-1 生效（寬屏時 .shell 不是
       flex 容器，order 無效）；同時把 margin-right 與 fixed 重置，nav 改橫排。 */
    .shell { margin-right: 0; display: flex; flex-direction: column; }
    nav { order: -1; position: static; width: auto; height: auto; top: auto; right: auto;
          border-left: 0; border-top: 1px solid #334155;
          overflow-x: auto; overflow-y: visible;
          flex-direction: row; flex-wrap: wrap; padding: 6px 8px; }
    nav a { padding: 8px 10px; font-size: 12px; white-space: nowrap;
            border-left: 0; border-bottom: 2px solid transparent; }
    nav a.on { border-left: 0; border-bottom-color: #38bdf8; }
    .navgrp { flex-direction: row; flex-wrap: wrap; align-items: center; gap: 4px;
              padding: 0 8px; border-top: 0; border-left: 1px solid #334155; }
    .navgrp:first-child { padding-left: 0; border-left: 0; }
    .navlbl { font-size: 10px; letter-spacing: 0; padding: 0 4px; }
    main { padding: 14px 10px; }
    .card { padding: 14px; margin-bottom: 12px; }
    .card h2 { font-size: 15px; }
    table { font-size: 12px; }
    th, td { padding: 6px 4px; font-size: 12px; }
    .stat { display: block; margin: 0 0 12px 0; }
    .stat b { font-size: 20px; }
    .btn { padding: 5px 10px; font-size: 12px; }
    .frm { grid-template-columns: 1fr; }
    .frm .full { grid-column: 1; }
    .pgrid { grid-template-columns: 1fr 1fr; }
    .pcard img { height: 100px; }
  }
  @media (max-width: 480px) {
    nav a { padding: 8px 8px; font-size: 11px; }
    .pgrid { grid-template-columns: 1fr; }
  }
`

// RequireAuth 擋未登入者，並把使用者注入給頁面 handler。
func RequireAuth(fn func(http.ResponseWriter, *http.Request, model.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fn(w, r, user)
	}
}

// RequireLogin 擋未登入者，給不需要使用者資訊的 handler 使用。
func RequireLogin(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := currentUser(r); err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fn(w, r)
	}
}

// appName 取得軟體名稱。handler 套件不依賴 config（避免循環），
// 直接讀環境變數；未設定時沿用預設值。
func appName() string {
	if v := strings.TrimSpace(os.Getenv("APP_NAME")); v != "" {
		return v
	}
	return "AutoRepair OS"
}

func renderPage(w http.ResponseWriter, title, active, username, role, body string) {
	app := appName()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title) + ` - ` + html.EscapeString(app) + `</title>
<style>` + pageStyle + `</style>
</head>
<body>
  <header>
    <h1>🔧 ` + html.EscapeString(app) + `</h1>
    <div class="user">登入中：<code>` + html.EscapeString(username) + `</code> <span class="badge">` + html.EscapeString(role) + `</span>
    <a href="/logout" class="logout">登出</a>
    </div>
  </header>
  <div class="shell">
  <main>` + body + `</main>
  <nav>` + navpkg.Render(active) + `</nav>
  </div>
  <script src="/offline.js"></script>
</body>
</html>`))
}

func flash(r *http.Request) string {
	msg := r.URL.Query().Get("msg")
	if msg == "" {
		return ""
	}
	return `<div class="msg msg-ok">` + html.EscapeString(msg) + `</div>`
}

func fail(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

func redirectMsg(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?msg="+urlQueryEscape(msg), http.StatusFound)
}

func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

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

var workOrderStatusLabel = map[string]string{
	"pending":       "待處理",
	"in_progress":   "進行中",
	"waiting_parts": "待料",
	"completed":     "待結帳",
	"paid":          "已結帳",
}

var approvalLabel = map[string]string{
	"not_required": "-",
	"pending":      "待核准",
	"approved":     "已核准",
	"rejected":     "已拒絕",
}

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
