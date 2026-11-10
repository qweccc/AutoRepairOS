package handler

import (
	"net/http"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
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
	Active         int
}

var staffKindLabel = map[string]string{
	"employee":  "員工",
	"dependent": "健保眷屬",
	"family":    "健保眷屬", // 舊值，相容用
	"vendor":    "協力廠商",
}

// ListStaff 員工與健保資料頁。
// isValidStaffKind checks if the staff kind is employee or dependent.
func isValidStaffKind(kind string) bool {
	return kind == "employee" || kind == "dependent"
}

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
		COALESCE(u.dependent_of,0), COALESCE(p.name,''),
		COALESCE(u.active,1)
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
			&s.DependentOf, &s.DepOfName, &s.Active); err != nil {
			continue
		}
		list = append(list, s)
	}

	empCount := countRows(`SELECT COUNT(1) FROM users WHERE COALESCE(staff_kind,'employee')='employee'`)
	depCount := countRows(`SELECT COUNT(1) FROM users WHERE staff_kind='dependent'`)
	insuredCount := countRows(`SELECT COUNT(1) FROM users WHERE COALESCE(staff_kind,'employee')<>'vendor'
		AND COALESCE(insurance_end,'')='' AND COALESCE(insurance_start,'')<>''`)

	var staffRows []pages.StaffRow
	for _, s := range list {
		kindCls := "background:#1e3a5f;color:#bae6fd"
		if s.Kind == "dependent" || s.Kind == "family" {
			kindCls = "background:#3b2f13;color:#fde68a"
		}
		endLabel := s.InsuranceEnd
		endRed := false
		if s.InsuranceEnd != "" {
			endLabel = s.InsuranceEnd + " 已退保"
			endRed = true
		}
		staffRows = append(staffRows, pages.StaffRow{
			ID:          s.ID,
			Username:    s.Username,
			Name:        s.Name,
			Role:        s.Role,
			Branch:      branchLabel(s.Branch),
			Kind:        s.Kind,
			KindLabel:   staffKindLabel[s.Kind],
			KindCls:     kindCls,
			NationalID:  s.NationalID,
			Birthday:    s.Birthday,
			InsStart:    s.InsuranceStart,
			InsEnd:      s.InsuranceEnd,
			InsEndLabel: endLabel,
			InsEndRed:   endRed,
			DepOf:       s.DependentOf,
			DepOfName:   s.DepOfName,
			Active:      s.Active,
			CanEdit:     true,
		})
	}

	roleOpts := `<option value="">—</option><option value="technician">技師</option><option value="counter">櫃檯人員</option>`
	if user.Role == "owner" {
		roleOpts = roleOpts + `<option value="owner">系統管理員</option>`
	}

	pages.ListStaffPage(staffRows, total, page, size, q, kind, empCount, depCount, insuredCount,
		user.Role == "owner" || user.Role == "counter", roleOpts, lookupOptions("branch", ""),
		user).Render(r.Context(), w)
}

// SearchStaffAPI 員工即時搜尋 API（HTMX 用）。
func SearchStaffAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if r.URL.Query().Get("format") == "picker" {
		searchStaffPicker(w, r, q)
		return
	}
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
	rows, err := db.DB.Query(`SELECT u.id, u.username, COALESCE(u.name,''), u.role,
		COALESCE(u.staff_branch,''), COALESCE(u.staff_kind,'employee'),
		COALESCE(u.national_id,''), COALESCE(u.birthday,''),
		COALESCE(u.insurance_start,''), COALESCE(u.insurance_end,''),
		COALESCE(u.dependent_of,0), COALESCE(p.name,''),
		COALESCE(u.active,1)
		FROM users u
		LEFT JOIN users p ON p.id = u.dependent_of`+where+
		` ORDER BY COALESCE(u.staff_kind,'employee'), u.staff_branch, u.username
		LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.StaffRow
	for rows.Next() {
		var s staffRow
		if err := rows.Scan(&s.ID, &s.Username, &s.Name, &s.Role, &s.Branch, &s.Kind,
			&s.NationalID, &s.Birthday, &s.InsuranceStart, &s.InsuranceEnd,
			&s.DependentOf, &s.DepOfName, &s.Active); err != nil {
			continue
		}
		kindCls := "background:#1e3a5f;color:#bae6fd"
		if s.Kind == "dependent" || s.Kind == "family" {
			kindCls = "background:#3b2f13;color:#fde68a"
		}
		endLabel := s.InsuranceEnd
		endRed := false
		if s.InsuranceEnd != "" {
			endLabel = s.InsuranceEnd + " 已退保"
			endRed = true
		}
		result = append(result, pages.StaffRow{
			ID:          s.ID,
			Username:    s.Username,
			Name:        s.Name,
			Role:        s.Role,
			Branch:      branchLabel(s.Branch),
			Kind:        s.Kind,
			KindLabel:   staffKindLabel[s.Kind],
			KindCls:     kindCls,
			NationalID:  s.NationalID,
			Birthday:    s.Birthday,
			InsStart:    s.InsuranceStart,
			InsEnd:      s.InsuranceEnd,
			InsEndLabel: endLabel,
			InsEndRed:   endRed,
			DepOf:       s.DependentOf,
			DepOfName:   s.DepOfName,
			Active:      s.Active,
			CanEdit:     true,
		})
	}
	pages.StaffSearchRows(result).Render(r.Context(), w)
}

func searchStaffPicker(w http.ResponseWriter, r *http.Request, q string) {
	where := ` WHERE u.role = 'technician' AND COALESCE(u.staff_kind,'employee') = 'employee' AND COALESCE(u.active,1) <> 0`
	var args []interface{}
	if q != "" {
		where += ` AND (u.name LIKE ? OR u.username LIKE ? OR COALESCE(u.staff_branch,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	rows, err := db.DB.Query(`SELECT u.id, COALESCE(u.name,''), COALESCE(u.staff_branch,'')
		FROM users u`+where+` ORDER BY u.name LIMIT 20`, args...)
	if err != nil {
		http.Error(w, "技師搜尋失敗，請稍後重試", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var result []pages.StaffPickerRow
	for rows.Next() {
		var row pages.StaffPickerRow
		var branch string
		if err := rows.Scan(&row.ID, &row.Name, &branch); err != nil {
			http.Error(w, "技師搜尋失敗，請稍後重試", http.StatusInternalServerError)
			return
		}
		row.Branch = branchLabel(branch)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "技師搜尋失敗，請稍後重試", http.StatusInternalServerError)
		return
	}
	pages.StaffPickerResults(result).Render(r.Context(), w)
}
