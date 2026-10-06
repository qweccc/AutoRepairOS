package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// 員工與健保資料。
//
// 為什麼獨立一頁：舊系統 CAR35 的 Staff 表把「員工」「健保眷屬」「協力廠商」
// 混在一起（用編號後綴區分，如 E003 王彩秀 / E03-1 嚴一舒）。匯入時已拆開：
//   - 員工與眷屬留在 users（歷史工單的外鍵不能斷）
//   - 協力廠商移到 suppliers
//
// 這一頁專門呈現人事與健保，因為投保單位要維護的是：
//   誰在保、誰的眷屬在保、加保/退保日、身分證號
// 這些跟「技師佣金」是兩件不同的事，混在一起會讓佣金表出現一排 0 的眷屬。

type staffRow struct {
	ID             int64
	Username       string
	Name           string
	Role           string
	Branch         string
	Kind           string
	NationalID     string
	Birthday       string
	InsuranceStart string
	InsuranceEnd   string
	DependentOf    int64
	DepOfName      string
}

var staffKindLabel = map[string]string{
	"employee":  "員工",
	"dependent": "健保眷屬",
	"family":    "健保眷屬", // 舊值，相容用
	"vendor":    "協力廠商",
}

// ListStaff 員工與健保資料頁。
func ListStaff(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	kind := strings.TrimSpace(qv.Get("kind"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE COALESCE(u.staff_kind,'employee') <> 'vendor'`
	var args []interface{}
	if q != "" {
		where += ` AND (u.name LIKE ? OR u.username LIKE ? OR COALESCE(u.national_id,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if kind != "" {
		where += ` AND COALESCE(u.staff_kind,'employee') = ?`
		args = append(args, kind)
	}

	total := countRows(`SELECT COUNT(1) FROM users u`+where, args...)

	query := `SELECT u.id, u.username, COALESCE(u.name,''), u.role,
		COALESCE(u.staff_branch,''), COALESCE(u.staff_kind,'employee'),
		COALESCE(u.national_id,''), COALESCE(u.birthday,''),
		COALESCE(u.insurance_start,''), COALESCE(u.insurance_end,''),
		COALESCE(u.dependent_of,0), COALESCE(p.name,'')
		FROM users u
		LEFT JOIN users p ON p.id = u.dependent_of` + where +
		` ORDER BY COALESCE(u.staff_kind,'employee'), u.staff_branch, u.username
		LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢員工資料失敗")
		return
	}
	defer rows.Close()

	var list []staffRow
	for rows.Next() {
		var s staffRow
		if err := rows.Scan(&s.ID, &s.Username, &s.Name, &s.Role, &s.Branch, &s.Kind,
			&s.NationalID, &s.Birthday, &s.InsuranceStart, &s.InsuranceEnd,
			&s.DependentOf, &s.DepOfName); err != nil {
			continue
		}
		list = append(list, s)
	}

	empCount := countRows(`SELECT COUNT(1) FROM users WHERE COALESCE(staff_kind,'employee')='employee'`)
	depCount := countRows(`SELECT COUNT(1) FROM users WHERE staff_kind='dependent'`)
	insuredCount := countRows(`SELECT COUNT(1) FROM users WHERE COALESCE(staff_kind,'employee')<>'vendor'
		AND COALESCE(insurance_end,'')='' AND COALESCE(insurance_start,'')<>''`)

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>姓名</th><th>編號</th><th>身分</th><th>部門</th>` +
		`<th>依附</th><th>身分證號</th><th>生日</th><th>加保日</th><th>退保日</th></tr>`)
	for _, s := range list {
		kindText := staffKindLabel[s.Kind]
		cls := "background:#1e3a5f;color:#bae6fd" // 員工：藍
		if s.Kind == "dependent" || s.Kind == "family" {
			cls = "background:#3b2f13;color:#fde68a" // 眷屬：黃
		}
		// 退保日有值 = 已退保，標示出來
		endCell := orDash(normDate(s.InsuranceEnd))
		if s.InsuranceEnd != "" {
			endCell = `<span style="color:#f87171">` + normDate(s.InsuranceEnd) + ` 已退保</span>`
		}
		depCell := `<span style="color:#64748b">本人</span>`
		if s.DependentOf > 0 {
			depCell = htmlEsc(s.DepOfName)
		}
		tb.WriteString(`<tr><td><b>` + htmlEsc(s.Name) + `</b></td><td><code>` + htmlEsc(s.Username) +
			`</code></td><td><span class="pill" style="` + cls + `">` + kindText +
			`</span></td><td style="font-size:12.5px;color:#94a3b8">` + orDash(branchLabel(s.Branch)) +
			`</td><td style="font-size:12.5px">` + depCell +
			`</td><td><code style="font-size:12px">` + orDash(htmlEsc(s.NationalID)) +
			`</code></td><td style="font-size:12.5px">` + orDash(normDate(s.Birthday)) +
			`</td><td style="font-size:12.5px">` + orDash(normDate(s.InsuranceStart)) +
			`</td><td style="font-size:12.5px">` + endCell + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有符合條件的資料</div>`
	}

	tab := func(label, key string) string {
		cls := "btn-s"
		if kind == key {
			cls = "btn-p"
		}
		u := url.Values{}
		if q != "" {
			u.Set("q", q)
		}
		if key != "" {
			u.Set("kind", key)
		}
		href := "/staff"
		if s := u.Encode(); s != "" {
			href += "?" + s
		}
		return `<a class="btn ` + cls + `" href="` + href + `">` + label + `</a>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>員工與健保資料</h2>
      <p style="color:#94a3b8;font-size:13.5px;margin:0 0 14px">
        舊系統把員工與健保眷屬放在同一張表（用編號後綴區分，如
        <code>E003</code> 王彩秀 / <code>E03-1</code> 嚴一舒）。<br>
        <b>健保眷屬不是技師</b>，不會出現在技師佣金表，也不會出現在技師指派選單；
        他們在這裡維護投保資料（身分證號、加保日、退保日）。
      </p>
      <div style="margin-bottom:14px">
        <div class="stat"><b>` + strconv.Itoa(empCount) + `</b><span>員工</span></div>
        <div class="stat"><b>` + strconv.Itoa(depCount) + `</b><span>健保眷屬</span></div>
        <div class="stat"><b>` + strconv.Itoa(insuredCount) + `</b><span>在保中</span></div>
      </div>
      <div style="margin-bottom:12px">` +
		tab("全部 "+strconv.Itoa(empCount+depCount), "") + ` ` +
		tab("員工 "+strconv.Itoa(empCount), "employee") + ` ` +
		tab("健保眷屬 "+strconv.Itoa(depCount), "dependent") + `
      </div>
      <form method="GET" action="/staff" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋姓名 / 編號 / 身分證號" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:280px">
        ` + hiddenInputs(qv, "q", "page", "per") + `
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/staff">清除</a>
        <a class="btn btn-s" href="/technicians/commissions">技師佣金</a>
      </form>` + table + pager("/staff", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/staff", qv, size) + `</div>
    </div>`

	renderPage(w, "員工與健保", "/staff", user.Username, user.Role, body)
}
