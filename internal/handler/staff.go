package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/session"
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
			CanEdit:      true,
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
			CanEdit:      true,
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

// CreateStaff 新增員工（或健保眷屬）。
//
// 權限：櫃檯以上可新增，但只有系統管理員能建立 owner 帳號
// （否則櫃檯可自建管理員，等於自己升權）。
func CreateStaff(w http.ResponseWriter, r *http.Request, user model.User) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))
	name := strings.TrimSpace(r.FormValue("name"))
	role := strings.TrimSpace(r.FormValue("role"))
	staffKind := strings.TrimSpace(r.FormValue("staff_kind"))
	staffBranch := strings.TrimSpace(r.FormValue("staff_branch"))

	if username == "" || password == "" || name == "" {
		fail(w, "帳號、密碼、姓名為必填")
		return
	}
	if role != "owner" && role != "counter" && role != "technician" {
		role = "technician"
	}
	// 只有系統管理員能建立管理員帳號（櫃檯自建 owner = 自己升權）。
	if role == "owner" && user.Role != "owner" {
		fail(w, "權限不足：只有系統管理員能建立管理員帳號")
		return
	}
	if staffKind != "employee" && staffKind != "dependent" {
		staffKind = "employee"
	}

	// 驗證部門（查 lookup_items 表）
	if staffBranch != "" {
		var validBranch int
		db.DB.QueryRow(`SELECT COUNT(1) FROM lookup_items WHERE category = 'branch' AND code = ? AND active = 1`, staffBranch).Scan(&validBranch)
		if validBranch == 0 {
			staffBranch = ""
		}
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, "密碼加密失敗")
		return
	}

	_, err = db.DB.Exec(`INSERT INTO users (username, password, role, name, staff_kind, staff_branch, active)
		VALUES (?,?,?,?,?,?,1)`,
		username, string(hashedPassword), role, name, staffKind, staffBranch)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "帳號已存在")
			return
		}
		fail(w, "新增員工失敗")
		return
	}

	redirectMsg(w, r, "/staff", "員工已新增")
}

// EditStaffForm 編輯員工資料頁。
//
// 可編輯欄位：
//   - national_id（身分證號）
//   - birthday（生日）
//   - insurance_start（加保日）
//   - insurance_end（退保日）
//   - staff_kind（身分：員工/眷屬）
//   - staff_branch（部門：A~H）
//   - active（在职/離職）
//
// 不可編輯：
//   - username（帳號，改了會斷登入）
//   - name（姓名，改了會影響歷史工單顯示）
//   - role（角色，改了會變權限）
func EditStaffForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	var s staffRow
	err := db.DB.QueryRow(`SELECT u.id, u.username, COALESCE(u.name,''), u.role,
		COALESCE(u.staff_branch,''), COALESCE(u.staff_kind,'employee'),
		COALESCE(u.national_id,''), COALESCE(u.birthday,''),
		COALESCE(u.insurance_start,''), COALESCE(u.insurance_end,''),
		COALESCE(u.dependent_of,0), COALESCE(p.name,''),
		COALESCE(u.active,1)
		FROM users u
		LEFT JOIN users p ON p.id = u.dependent_of
		WHERE u.id = ?`, id).Scan(
		&s.ID, &s.Username, &s.Name, &s.Role, &s.Branch, &s.Kind,
		&s.NationalID, &s.Birthday, &s.InsuranceStart, &s.InsuranceEnd,
		&s.DependentOf, &s.DepOfName, &s.Active)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 載入健保異動歷史
	var history []pages.InsuranceHistoryItem
	hRows, _ := db.DB.Query(`SELECT id, event_type, event_date, COALESCE(note,'') FROM insurance_history WHERE user_id = ? ORDER BY event_date DESC, id DESC`, id)
	if hRows != nil {
		defer hRows.Close()
		for hRows.Next() {
			var it pages.InsuranceHistoryItem
			if err := hRows.Scan(&it.ID, &it.EventType, &it.EventDate, &it.Note); err == nil {
				history = append(history, it)
			}
		}
	}
	if user.Role != "owner" {
		s.NationalID = ""
		s.Birthday = ""
		s.InsuranceStart = ""
		s.InsuranceEnd = ""
	}

	pages.EditStaffPage(pages.StaffEditData{
		ID: s.ID, Name: s.Name, Username: s.Username, NationalID: s.NationalID,
		Birthday: s.Birthday, InsuranceStart: s.InsuranceStart, InsuranceEnd: s.InsuranceEnd,
		Kind: s.Kind, Branch: s.Branch, Active: s.Active,
		BranchOpts: lookupSelectOptions("branch", s.Branch), User: user,
		InsuranceHistory: history,
	}).Render(r.Context(), w)
}

// UpdateStaff 更新員工資料。
func UpdateStaff(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	user, err := currentUser(r)
	if err != nil {
		http.Error(w, "未登入", http.StatusUnauthorized)
		return
	}
	isOwner := user.Role == "owner"

	nationalID := ""
	birthday := ""
	insuranceStart := ""
	insuranceEnd := ""
	if isOwner {
		nationalID = strings.TrimSpace(r.FormValue("national_id"))
		birthday = strings.TrimSpace(r.FormValue("birthday"))
		insuranceStart = strings.TrimSpace(r.FormValue("insurance_start"))
		insuranceEnd = strings.TrimSpace(r.FormValue("insurance_end"))
	}
	staffKind := strings.TrimSpace(r.FormValue("staff_kind"))
	staffBranch := strings.TrimSpace(r.FormValue("staff_branch"))
	active := 1
	if r.FormValue("active") == "0" {
		active = 0
	}
	password := strings.TrimSpace(r.FormValue("password"))
	if password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			fail(w, "密碼雜湊失敗")
			return
		}
		db.DB.Exec(`UPDATE users SET password = ? WHERE id = ?`, string(hashed), id)
	}

	// 讀取舊值以判斷是否異動
	var oldStart, oldEnd string
	if err := db.DB.QueryRow(`SELECT COALESCE(insurance_start,''), COALESCE(insurance_end,'') FROM users WHERE id=?`, id).Scan(&oldStart, &oldEnd); err != nil {
		oldStart, oldEnd = "", ""
	}

	changed := isOwner && (insuranceStart != oldStart || insuranceEnd != oldEnd)
	var eventType, eventDate string
	if changed {
		eventType = "change"
		eventDate = insuranceStart
		if eventDate == "" {
			eventDate = insuranceEnd
		}
		if eventDate == "" {
			eventDate = time.Now().Format("2006-01-02")
		}
		if oldEnd == "" && insuranceEnd != "" {
			eventType = "withdraw"
		}
		if oldStart == "" && insuranceStart != "" {
			eventType = "enroll"
		}
	}
	if staffKind != "employee" && staffKind != "dependent" {
		staffKind = "employee"
	}

	// 驗證部門
	validBranches := map[string]bool{"A": true, "B": true, "C": true, "D": true, "E": true, "F": true, "G": true, "H": true}
	if !validBranches[staffBranch] {
		staffBranch = ""
	}

	_, err = db.DB.Exec(`UPDATE users SET
		national_id = ?, birthday = ?, insurance_start = ?, insurance_end = ?,
		staff_kind = ?, staff_branch = ?, active = ?, updated_at = datetime('now','localtime')
		WHERE id = ?`,
		nullStr(nationalID), nullStr(birthday), nullStr(insuranceStart), nullStr(insuranceEnd),
		staffKind, staffBranch, active, id)
	if err != nil {
		fail(w, "更新失敗")
		return
	}
	if changed {
		if _, err := db.DB.Exec(`INSERT INTO insurance_history (user_id, event_type, event_date, note, created_by) VALUES (?,?,?,?,?)`,
			id, eventType, eventDate, "由員工編輯頁更新", user.ID); err != nil {
			fmt.Println("insurance_history insert failed:", err)
		}
	}

	redirectMsg(w, r, "/staff", "員工資料已更新")
}

// DeactivateStaff 停用員工（軟刪除，標記為離職）。
func DeactivateStaff(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	_, err := db.DB.Exec(`UPDATE users SET active = 0, updated_at = datetime('now','localtime') WHERE id = ?`, id)
	if err != nil {
		fail(w, "停用失敗")
		return
	}

	// 停用後立刻讓所有已開啟的 session 失效，否則被停用者手上的瀏覽器分頁仍可操作。
	session.DeleteForUser(id)

	redirectMsg(w, r, "/staff", "員工已標記為離職")
}

// ReactivateStaff 復職（active=1）。
func ReactivateStaff(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	_, err := db.DB.Exec(`UPDATE users SET active = 1, updated_at = datetime('now','localtime') WHERE id = ?`, id)
	if err != nil {
		fail(w, "復職失敗")
		return
	}

	redirectMsg(w, r, "/staff", "員工已復職")
}
