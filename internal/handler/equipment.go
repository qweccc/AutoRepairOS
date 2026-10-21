package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// 設備管理（SRS FR-EQP-01）。
//
// 設備基本資料與保養排程分開：equipment 存設備本体，
// equipment_maintenance 存每次保養紀錄與下次保養日期。

const equipmentCols = `e.id, e.name, COALESCE(e.model,''), COALESCE(e.serial_number,''),
	COALESCE(e.location,''), COALESCE(e.purchase_date,''), COALESCE(e.warranty_expiry,''),
	e.status, COALESCE(e.notes,''), e.created_at`

var equipmentStatusLabel = map[string]string{
	"active":      "啟用",
	"maintenance": "維修中",
	"retired":     "報廢",
}

var equipmentStatusPill = map[string]string{
	"active":      "p-completed",
	"maintenance": "p-progress",
	"retired":     "p-apprno",
}

func ListEquipment(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	status := strings.TrimSpace(qv.Get("status"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (e.name LIKE ? OR COALESCE(e.model,'') LIKE ? OR COALESCE(e.serial_number,'') LIKE ? OR COALESCE(e.location,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	if status != "" {
		where += ` AND e.status = ?`
		args = append(args, status)
	}

	total := countRows(`SELECT COUNT(1) FROM active_equipment e`+where, args...)
	query := `SELECT ` + equipmentCols + ` FROM active_equipment e` + where +
		` ORDER BY e.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢設備失敗")
		return
	}
	defer rows.Close()

	var list []model.Equipment
	for rows.Next() {
		var e model.Equipment
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Name, &e.Model, &e.SerialNumber, &e.Location,
			&e.PurchaseDate, &e.WarrantyExpiry, &e.Status, &e.Notes, &createdAt); err != nil {
			continue
		}
		e.CreatedAt = parseFlexTime(createdAt)
		list = append(list, e)
	}

	var eqRows []pages.EquipmentRow
	for _, e := range list {
		eqRows = append(eqRows, pages.EquipmentRow{
			ID:           e.ID,
			Name:         e.Name,
			Model:        e.Model,
			SerialNumber: e.SerialNumber,
			Location:     e.Location,
			Status:       e.Status,
			StatusLabel:  equipmentStatusLabel[e.Status],
			StatusPill:   equipmentStatusPill[e.Status],
		})
	}

	pages.ListEquipmentPage(eqRows, total, page, size, q, status, user).Render(r.Context(), w)
}

// SearchEquipmentAPI 設備即時搜尋 API（HTMX 用）。
func SearchEquipmentAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (e.name LIKE ? OR COALESCE(e.model,'') LIKE ? OR COALESCE(e.serial_number,'') LIKE ? OR COALESCE(e.location,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	rows, err := db.DB.Query(`SELECT `+equipmentCols+` FROM active_equipment e`+where+
		` ORDER BY e.id DESC LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.EquipmentRow
	for rows.Next() {
		var e model.Equipment
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Name, &e.Model, &e.SerialNumber, &e.Location,
			&e.PurchaseDate, &e.WarrantyExpiry, &e.Status, &e.Notes, &createdAt); err != nil {
			continue
		}
		result = append(result, pages.EquipmentRow{
			ID:           e.ID,
			Name:         e.Name,
			Model:        e.Model,
			SerialNumber: e.SerialNumber,
			Location:     e.Location,
			Status:       e.Status,
			StatusLabel:  equipmentStatusLabel[e.Status],
			StatusPill:   equipmentStatusPill[e.Status],
		})
	}
	pages.EquipmentSearchRows(result).Render(r.Context(), w)
}

// CreateEquipment 新增設備（SRS FR-EQP-01）。
func CreateEquipment(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		fail(w, "設備名稱為必填")
		return
	}
	_, err := db.DB.Exec(`INSERT INTO equipment
		(name, model, serial_number, location, purchase_date, warranty_expiry, status, notes)
		VALUES (?,?,?,?,?,?,?,?)`,
		name, nullStr(r.FormValue("model")), nullStr(r.FormValue("serial_number")),
		nullStr(r.FormValue("location")), nullStr(r.FormValue("purchase_date")),
		nullStr(r.FormValue("warranty_expiry")), equipmentStatusArg(r.FormValue("status")),
		nullStr(r.FormValue("notes")))
	if err != nil {
		fail(w, "新增設備失敗")
		return
	}
	redirectMsg(w, r, "/equipment", "設備已新增")
}

// EditEquipmentForm 編輯設備表單。
func EditEquipmentForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	var e model.Equipment
	var createdAt string
	err := db.DB.QueryRow(`SELECT `+equipmentCols+` FROM active_equipment WHERE id = ?`, id).
		Scan(&e.ID, &e.Name, &e.Model, &e.SerialNumber, &e.Location,
			&e.PurchaseDate, &e.WarrantyExpiry, &e.Status, &e.Notes, &createdAt)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body := `
    <div class="card">
      <h2>編輯設備 ` + htmlEsc(e.Name) + `</h2>
      <form class="frm" method="POST" action="/equipment/` + strconv.FormatInt(e.ID, 10) + `">
        <label>設備名稱<input name="name" value="` + htmlEsc(e.Name) + `" required></label>
        <label>型號<input name="model" value="` + htmlEsc(e.Model) + `"></label>
        <label>序號<input name="serial_number" value="` + htmlEsc(e.SerialNumber) + `"></label>
        <label>位置<input name="location" value="` + htmlEsc(e.Location) + `"></label>
        <label>購買日期<input type="date" name="purchase_date" value="` + htmlEsc(e.PurchaseDate) + `"></label>
        <label>保固到期<input type="date" name="warranty_expiry" value="` + htmlEsc(e.WarrantyExpiry) + `"></label>
        <label>狀態
          <select name="status">
            <option value="active"` + selIf(e.Status == "active") + `>啟用</option>
            <option value="maintenance"` + selIf(e.Status == "maintenance") + `>維修中</option>
            <option value="retired"` + selIf(e.Status == "retired") + `>報廢</option>
          </select>
        </label>
        <label class="full">備註<textarea name="notes">` + htmlEsc(e.Notes) + `</textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">儲存</button> <a class="btn btn-s" href="/equipment">取消</a></div>
      </form>
    </div>`
	renderPage(w, "編輯設備", "/equipment", user.Username, user.Role, body)
}

// UpdateEquipment 更新設備。
func UpdateEquipment(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		fail(w, "設備名稱為必填")
		return
	}
	res, err := db.DB.Exec(`UPDATE equipment SET name=?, model=?, serial_number=?,
		location=?, purchase_date=?, warranty_expiry=?, status=?, notes=?,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, nullStr(r.FormValue("model")), nullStr(r.FormValue("serial_number")),
		nullStr(r.FormValue("location")), nullStr(r.FormValue("purchase_date")),
		nullStr(r.FormValue("warranty_expiry")), equipmentStatusArg(r.FormValue("status")),
		nullStr(r.FormValue("notes")), id)
	if err != nil {
		fail(w, "更新設備失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/equipment", "設備已更新")
}

// DeleteEquipment 刪除設備。
func DeleteEquipment(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	res, err := db.DB.Exec("UPDATE equipment SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		fail(w, "刪除設備失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/equipment", "設備已刪除")
}

// ListEquipmentMaintenance 顯示設備保養紀錄（SRS FR-EQP-01）。
func ListEquipmentMaintenance(w http.ResponseWriter, r *http.Request, user model.User) {
	equipID := mustInt(r.PathValue("id"))

	var equipName string
	err := db.DB.QueryRow(`SELECT name FROM active_equipment WHERE id = ?`, equipID).Scan(&equipName)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	rows, err := db.DB.Query(`SELECT em.id, em.equipment_id, COALESCE(e.name,''),
		em.maintenance_date, COALESCE(em.maintenance_type,''), COALESCE(em.description,''),
		COALESCE(em.cost,0), COALESCE(em.next_maintenance_date,''), COALESCE(em.technician_id,0),
		COALESCE(u.name,''), COALESCE(em.notes,''), em.created_at
		FROM active_equipment_maintenance em
		JOIN active_equipment e ON e.id = em.equipment_id
		LEFT JOIN users u ON u.id = em.technician_id
		WHERE em.equipment_id = ? ORDER BY em.maintenance_date DESC`, equipID)
	if err != nil {
		fail(w, "查詢保養紀錄失敗")
		return
	}
	defer rows.Close()

	var list []model.EquipmentMaintenance
	for rows.Next() {
		var m model.EquipmentMaintenance
		var createdAt string
		if err := rows.Scan(&m.ID, &m.EquipmentID, &m.EquipmentName, &m.MaintenanceDate,
			&m.MaintenanceType, &m.Description, &m.Cost, &m.NextMaintenanceDate,
			&m.TechnicianID, &m.TechnicianName, &m.Notes, &createdAt); err != nil {
			continue
		}
		m.CreatedAt = parseFlexTime(createdAt)
		list = append(list, m)
	}

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>ID</th><th>保養日期</th><th>類型</th><th>內容</th><th class="num">費用</th><th>下次保養</th><th>執行人員</th><th>操作</th></tr>`)
	for _, m := range list {
		tb.WriteString(`<tr><td>` + strconv.FormatInt(m.ID, 10) + `</td><td>` + htmlEsc(m.MaintenanceDate) +
			`</td><td>` + htmlEsc(m.MaintenanceType) + `</td><td>` + htmlEsc(m.Description) +
			`</td><td class="num">` + money(m.Cost) + `</td><td>` + htmlEsc(m.NextMaintenanceDate) +
			`</td><td>` + htmlEsc(m.TechnicianName) +
			`</td><td><a class="btn btn-d" href="/equipment/` + strconv.FormatInt(equipID, 10) + `/maintenance/` + strconv.FormatInt(m.ID, 10) + `/delete" onclick="return confirm('確定刪除這筆保養紀錄？')">刪除</a></td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">尚無保養紀錄</div>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>新增保養紀錄 - ` + htmlEsc(equipName) + `</h2>
      <form class="frm" method="POST" action="/equipment/` + strconv.FormatInt(equipID, 10) + `/maintenance">
        <label>保養日期<input type="date" name="maintenance_date" required></label>
        <label>保養類型<input name="maintenance_type" placeholder="例：日常保養"></label>
        <label class="full">保養內容<textarea name="description"></textarea></label>
        <label>費用<input type="number" step="0.01" name="cost" value="0"></label>
        <label>下次保養日期<input type="date" name="next_maintenance_date"></label>
        <label>執行人員
          <select name="technician_id">
            <option value="">（未指定）</option>` + technicianOptions() + `
          </select>
        </label>
        <label class="full">備註<textarea name="notes"></textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">新增保養</button></div>
      </form>
    </div>
    <div class="card">
      <h2>保養紀錄（共 ` + strconv.Itoa(len(list)) + ` 筆）</h2>` + table + `
    </div>`

	renderPage(w, "設備保養", "/equipment", user.Username, user.Role, body)
}

// CreateEquipmentMaintenance 新增保養紀錄。
func CreateEquipmentMaintenance(w http.ResponseWriter, r *http.Request) {
	equipID := mustInt(r.PathValue("id"))
	maintenanceDate := strings.TrimSpace(r.FormValue("maintenance_date"))
	if maintenanceDate == "" {
		fail(w, "保養日期為必填")
		return
	}
	_, err := db.DB.Exec(`INSERT INTO equipment_maintenance
		(equipment_id, maintenance_date, maintenance_type, description, cost, next_maintenance_date, technician_id, notes)
		VALUES (?,?,?,?,?,?,?,?)`,
		equipID, maintenanceDate, nullStr(r.FormValue("maintenance_type")),
		nullStr(r.FormValue("description")), atofDefault(r.FormValue("cost"), 0),
		nullStr(r.FormValue("next_maintenance_date")), technicianIDArg(r.FormValue("technician_id")),
		nullStr(r.FormValue("notes")))
	if err != nil {
		fail(w, "新增保養紀錄失敗")
		return
	}
	redirectMsg(w, r, "/equipment/"+strconv.FormatInt(equipID, 10)+"/maintenance", "保養紀錄已新增")
}

// DeleteEquipmentMaintenance 刪除保養紀錄。
func DeleteEquipmentMaintenance(w http.ResponseWriter, r *http.Request) {
	equipID := mustInt(r.PathValue("id"))
	maintID := mustInt(r.PathValue("maint_id"))
	res, err := db.DB.Exec(`UPDATE equipment_maintenance SET deleted_at = datetime('now')
		WHERE id = ? AND equipment_id = ? AND deleted_at IS NULL`, maintID, equipID)
	if err != nil {
		fail(w, "刪除失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/equipment/"+strconv.FormatInt(equipID, 10)+"/maintenance", "保養紀錄已刪除")
}

func technicianIDArg(v string) interface{} {
	n := mustInt(v)
	if n <= 0 {
		return nil
	}
	return n
}

func equipmentStatusArg(v string) string {
	switch v {
	case "active", "maintenance", "retired":
		return v
	default:
		return "active"
	}
}
