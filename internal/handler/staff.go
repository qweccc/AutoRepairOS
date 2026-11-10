package handler

import (
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/session"
	"autorepairos/web/pages"
)

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
		formError(w, r, "新增員工", "/staff", "帳號、密碼、姓名為必填")
		return
	}
	validRoles := map[string]bool{"owner": true, "counter": true, "technician": true}
	if !validRoles[role] {
		role = "technician"
	}
	// 只有系統管理員能建立管理員帳號（櫃檯自建 owner = 自己升權）。
	if role == "owner" && user.Role != "owner" {
		fail(w, "權限不足：只有系統管理員能建立管理員帳號")
		return
	}
	if !isValidStaffKind(staffKind) {
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
		ID: s.ID, Name: s.Name, Username: s.Username, Error: r.URL.Query().Get("err"), NationalID: s.NationalID,
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
	} else {
		// 非 owner 不可修改個資/保險，保留既有值避免被清空
		db.DB.QueryRow(`SELECT COALESCE(national_id,''), COALESCE(birthday,''), COALESCE(insurance_start,''), COALESCE(insurance_end,'') FROM users WHERE id=?`, id).
			Scan(&nationalID, &birthday, &insuranceStart, &insuranceEnd)
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
			log.Printf("員工 #%d 密碼雜湊失敗: %v", id, err)
			renderStaffUpdateError(w, r, user, "密碼更新失敗，請稍後再試")
			return
		}
		if _, err := db.DB.Exec(`UPDATE users SET password = ? WHERE id = ?`, string(hashed), id); err != nil {
			log.Printf("員工 #%d 密碼更新失敗: %v", id, err)
			renderStaffUpdateError(w, r, user, "密碼更新失敗，請確認資料庫可寫入後再試")
			return
		}
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
		// 優先序：若同時新設加保與退保，以「enroll」為主（最後覆寫）
		if oldEnd == "" && insuranceEnd != "" {
			eventType = "withdraw"
		}
		if oldStart == "" && insuranceStart != "" {
			eventType = "enroll"
		}
	}
	if !isValidStaffKind(staffKind) {
		staffKind = "employee"
	}

	// 驗證部門
	validBranches := map[string]bool{"A": true, "B": true, "C": true, "D": true, "E": true, "F": true, "G": true, "H": true}
	if !validBranches[staffBranch] {
		staffBranch = ""
	}

	_, err = db.DB.Exec(`UPDATE users SET
		national_id = ?, birthday = ?, insurance_start = ?, insurance_end = ?,
		staff_kind = ?, staff_branch = ?, active = ?
		WHERE id = ?`,
		nullStr(nationalID), nullStr(birthday), nullStr(insuranceStart), nullStr(insuranceEnd),
		staffKind, staffBranch, active, id)
	if err != nil {
		log.Printf("員工 #%d 資料更新失敗: %v", id, err)
		renderStaffUpdateError(w, r, user, "更新失敗，請確認資料庫可寫入後再試")
		return
	}
	if changed {
		if _, err := db.DB.Exec(`INSERT INTO insurance_history (user_id, event_type, event_date, note, created_by) VALUES (?,?,?,?,?)`,
			id, eventType, eventDate, "由員工編輯頁更新", user.ID); err != nil {
			log.Printf("insurance_history insert failed: %v", err)
		}
	}

	redirectMsg(w, r, "/staff", "員工資料已更新")
}

func renderStaffUpdateError(w http.ResponseWriter, r *http.Request, user model.User, message string) {
	query := r.URL.Query()
	query.Set("err", message)
	r.URL.RawQuery = query.Encode()
	EditStaffForm(w, r, user)
}

// DeactivateStaff 停用員工（軟刪除，標記為離職）。
func DeactivateStaff(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	_, err := db.DB.Exec(`UPDATE users SET active = 0 WHERE id = ?`, id)
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

	_, err := db.DB.Exec(`UPDATE users SET active = 1 WHERE id = ?`, id)
	if err != nil {
		fail(w, "復職失敗")
		return
	}

	redirectMsg(w, r, "/staff", "員工已復職")
}
