package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

func CreateVehicle(w http.ResponseWriter, r *http.Request) {
	customerID, errMsg := resolveCustomerID(r)
	plate := strings.TrimSpace(r.FormValue("plate_number"))
	model_ := normVehicleModel(r.FormValue("brand_model"))
	vehicleType := strings.TrimSpace(r.FormValue("vehicle_type"))

	if errMsg != "" {
		formError(w, r, "新增車輛", "/vehicles", errMsg)
		return
	}
	if plate == "" || model_ == "" {
		formError(w, r, "新增車輛", "/vehicles", "車牌、車型為必填（若選了「其他」請輸入車型）")
		return
	}

	_, err := db.DB.Exec(`INSERT INTO vehicles
		(customer_id, plate_number, brand_model, vehicle_type, driver_name, driver_phone,
		 contact_name, contact_phone, owner_name, owner_address, owner_id_number, license_expiry_date,
		 color, door_count, manufacture_year, manufacture_date, license_issue_date,
		 fuel_type, displacement, body_style, seating_capacity, curb_weight, gross_weight,
		 tire_size, wheelbase, vin, engine_number,
		 inspection_due_date, next_maintenance_date, next_maintenance_mileage,
		 last_maintenance_date, last_maintenance_mileage,
		 next_maintenance_interval_months, next_maintenance_interval_km)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		customerID, plate, model_, vehicleType,
		nullStr(r.FormValue("driver_name")), nullStr(r.FormValue("driver_phone")),
		nullStr(r.FormValue("contact_name")), nullStr(r.FormValue("contact_phone")),
		nullStr(r.FormValue("owner_name")), nullStr(r.FormValue("owner_address")),
		nullStr(r.FormValue("owner_id_number")), nullStr(r.FormValue("license_expiry_date")),
		nullStr(r.FormValue("color")), atoiDefault(r.FormValue("door_count"), 0),
		atoiDefault(r.FormValue("manufacture_year"), 0),
		nullStr(r.FormValue("manufacture_date")), nullStr(r.FormValue("license_issue_date")),
		nullStr(r.FormValue("fuel_type")), atoiDefault(r.FormValue("displacement"), 0),
		nullStr(r.FormValue("body_style")), atoiDefault(r.FormValue("seating_capacity"), 0),
		atoiDefault(r.FormValue("curb_weight"), 0), atoiDefault(r.FormValue("gross_weight"), 0),
		nullStr(r.FormValue("tire_size")), atoiDefault(r.FormValue("wheelbase"), 0),
		nullStr(r.FormValue("vin")), nullStr(r.FormValue("engine_number")),
		nullDate(r.FormValue("inspection_due_date")),
		nil, nil,
		nullDate(r.FormValue("last_maintenance_date")),
		atoiOrNil(r.FormValue("last_maintenance_mileage")),
		atoiDefault(r.FormValue("maint_interval_months"), 6),
		atoiDefault(r.FormValue("maint_interval_km"), 5000),
	)
	if err != nil {
		log.Printf("建立車輛失敗（plate=%s）: %v", plate, err)
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "車牌號碼已存在")
			return
		}
		fail(w, "建立車輛失敗")
		return
	}

	if vid := lastInsertID("vehicles"); vid > 0 {
		recalcNextMaintenance(vid)
	}

	redirectMsg(w, r, "/vehicles", "車輛已建立")
}

// atoiOrNil 空字串回 nil（讓 DB 存 NULL 而非 0）。
func atoiOrNil(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return nil
	}
	return n
}
func lastInsertID(table string) int64 {
	var id int64
	db.DB.QueryRow("SELECT COALESCE(MAX(id),0) FROM " + table).Scan(&id)
	return id
}

// recalcNextMaintenance 由「最後保養基準 + 間隔」推算下次保養日期與里程。
//
// 保養是兩條獨立條件（日期到 / 里程到），兩者都要算，先到者為準。
// 車行慣例：每 6 個月或每 5000 公里保養一次（可用 vehicles 的間隔欄位調整）。
func recalcNextMaintenance(vehicleID int64) {
	var lastDate string
	var lastKm, months, km int
	err := db.DB.QueryRow(`SELECT COALESCE(last_maintenance_date,''),
		COALESCE(last_maintenance_mileage,0),
		COALESCE(next_maintenance_interval_months,6),
		COALESCE(next_maintenance_interval_km,5000)
		FROM active_vehicles WHERE id = ?`, vehicleID).Scan(&lastDate, &lastKm, &months, &km)
	if err != nil {
		return
	}
	if months <= 0 {
		months = 6
	}
	if km <= 0 {
		km = 5000
	}

	var nextDate interface{}
	if d := normDate(lastDate); d != "" {
		if t, perr := time.Parse("2006-01-02", d); perr == nil {
			nextDate = t.AddDate(0, months, 0).Format("2006-01-02")
		}
	}
	var nextKm interface{}
	if lastKm > 0 {
		nextKm = lastKm + km
	}

	db.DB.Exec(`UPDATE vehicles SET next_maintenance_date=?, next_maintenance_mileage=? WHERE id=?`,
		nextDate, nextKm, vehicleID)
}
func EditVehicleForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+vehicleCols+` FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id WHERE v.id = ?`, id)
	v, err := scanVehicle(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pages.EditVehiclePage(v,
		vehicleModelOptions(v.BrandModel),
		lookupOptions("vehicle_type", v.VehicleType),
		lookupOptions("fuel_type", v.FuelType),
		lookupOptions("body_style", v.BodyStyle),
		user).Render(r.Context(), w)
}
func UpdateVehicle(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	customerID, errMsg := resolveCustomerID(r)
	plate := strings.TrimSpace(r.FormValue("plate_number"))
	model_ := normVehicleModel(r.FormValue("brand_model"))

	if errMsg != "" {
		formError(w, r, "編輯車輛", "/vehicles/"+strconv.FormatInt(id, 10)+"/edit", errMsg)
		return
	}
	if plate == "" || model_ == "" {
		formError(w, r, "編輯車輛", "/vehicles/"+strconv.FormatInt(id, 10)+"/edit", "車牌、車型為必填（若選了「其他」請輸入車型）")
		return
	}

	res, err := db.DB.Exec(`UPDATE vehicles SET customer_id=?, plate_number=?, brand_model=?,
		vehicle_type=?, driver_name=?, driver_phone=?, contact_name=?, contact_phone=?, owner_name=?,
		owner_address=?, owner_id_number=?, license_expiry_date=?,
		color=?, door_count=?, manufacture_year=?, manufacture_date=?, license_issue_date=?,
		fuel_type=?, displacement=?, body_style=?, seating_capacity=?, curb_weight=?, gross_weight=?,
		tire_size=?, wheelbase=?, vin=?, engine_number=?, inspection_due_date=?,
		last_maintenance_date=?, last_maintenance_mileage=?,
		next_maintenance_interval_months=?, next_maintenance_interval_km=? WHERE id=?`,
		customerID, plate, model_, strings.TrimSpace(r.FormValue("vehicle_type")),
		nullStr(r.FormValue("driver_name")), nullStr(r.FormValue("driver_phone")),
		nullStr(r.FormValue("contact_name")), nullStr(r.FormValue("contact_phone")),
		nullStr(r.FormValue("owner_name")), nullStr(r.FormValue("owner_address")),
		nullStr(r.FormValue("owner_id_number")), nullStr(r.FormValue("license_expiry_date")),
		nullStr(r.FormValue("color")), atoiDefault(r.FormValue("door_count"), 0),
		atoiDefault(r.FormValue("manufacture_year"), 0),
		nullStr(r.FormValue("manufacture_date")), nullStr(r.FormValue("license_issue_date")),
		nullStr(r.FormValue("fuel_type")), atoiDefault(r.FormValue("displacement"), 0),
		nullStr(r.FormValue("body_style")), atoiDefault(r.FormValue("seating_capacity"), 0),
		atoiDefault(r.FormValue("curb_weight"), 0), atoiDefault(r.FormValue("gross_weight"), 0),
		nullStr(r.FormValue("tire_size")), atoiDefault(r.FormValue("wheelbase"), 0),
		nullStr(r.FormValue("vin")), nullStr(r.FormValue("engine_number")),
		nullDate(r.FormValue("inspection_due_date")),
		nullDate(r.FormValue("last_maintenance_date")),
		atoiOrNil(r.FormValue("last_maintenance_mileage")),
		atoiDefault(r.FormValue("maint_interval_months"), 6),
		atoiDefault(r.FormValue("maint_interval_km"), 5000), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "車牌號碼已存在")
			return
		}
		fail(w, "更新車輛失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	// 基準或間隔改了 → 重算下次保養
	recalcNextMaintenance(id)

	redirectMsg(w, r, "/vehicles", "車輛已更新")
}

func DeleteVehicle(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	if countRows("SELECT COUNT(1) FROM active_work_orders WHERE vehicle_id = ?", id) > 0 {
		fail(w, "此車輛已有工單紀錄，無法刪除")
		return
	}

	res, err := db.DB.Exec("UPDATE vehicles SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		fail(w, "刪除車輛失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/vehicles", "車輛已刪除")
}

// resolveCustomerID 從表單解析客戶。
//
// 為什麼不用下拉選單：客戶有 6 千多筆（舊系統遷移進來後），
// 全部塞進 <select> 會讓頁面變成 400KB 以上，且實務上沒人捲得動。
// 改成輸入「客戶編號 / 姓名 / 電話」由伺服器解析，符合車行打單習慣。
//
// 回傳 (id, errorMsg)。errorMsg 非空代表解析失敗，呼叫端應直接回錯。
func resolveCustomerID(r *http.Request) (int64, string) {
	// 表單直接帶 id（例如從客戶列表點「開工單」帶過來）
	if id := mustInt(r.FormValue("customer_id")); id > 0 {
		if countRows("SELECT COUNT(1) FROM active_customers WHERE id = ?", id) > 0 {
			return id, ""
		}
		return 0, "找不到該客戶"
	}

	raw := strings.TrimSpace(r.FormValue("customer_query"))
	if raw == "" {
		return 0, "請輸入客戶（編號 / 姓名 / 電話）"
	}

	// 純數字 → 先當 id，再當電話
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		var id int64
		if db.DB.QueryRow("SELECT id FROM active_customers WHERE id = ?", n).Scan(&id) == nil {
			return id, ""
		}
		if db.DB.QueryRow("SELECT id FROM active_customers WHERE phone = ?", raw).Scan(&id) == nil {
			return id, ""
		}
	}

	// 姓名或電話精確比對
	var id int64
	if db.DB.QueryRow("SELECT id FROM active_customers WHERE name = ? OR phone = ? LIMIT 1", raw, raw).
		Scan(&id) == nil {
		return id, ""
	}
	// 模糊比對（取第一筆，並要求唯一以免誤選）
	like := "%" + raw + "%"
	rows, err := db.DB.Query(`SELECT id FROM active_customers
		WHERE name LIKE ? OR phone LIKE ? OR COALESCE(notes,'') LIKE ? LIMIT 5`, like, like, like)
	if err != nil {
		return 0, "查詢客戶失敗"
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var v int64
		if rows.Scan(&v) == nil {
			ids = append(ids, v)
		}
	}
	switch len(ids) {
	case 0:
		return 0, "找不到符合「" + raw + "」的客戶，請先到客戶頁建立"
	case 1:
		return ids[0], ""
	default:
		return 0, "「" + raw + "」符合多筆客戶，請輸入更完整的姓名或直接填客戶編號"
	}
}

// customerOptions 客戶下拉選單（僅用於小資料集，如工單篩選）。
// 大資料集請改用 resolveCustomerID 搭配文字輸入。
func customerOptions() string {
	rows, err := db.DB.Query("SELECT id, name, phone FROM active_customers ORDER BY name LIMIT 300")
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name, phone string
		if err := rows.Scan(&id, &name, &phone); err != nil {
			continue
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(name) + ` (` + htmlEsc(phone) + `)</option>`)
	}
	return b.String()
}

func vehicleOptions() string {
	rows, err := db.DB.Query(`SELECT v.id, v.plate_number, v.brand_model, COALESCE(c.name,'')
		FROM active_vehicles v JOIN active_customers c ON c.id = v.customer_id ORDER BY v.plate_number LIMIT 300`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var plate, model_, cname string
		if err := rows.Scan(&id, &plate, &model_, &cname); err != nil {
			continue
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(plate) + ` / ` +
			htmlEsc(model_) + ` / ` + htmlEsc(cname) + `</option>`)
	}
	return b.String()
}

func yearText(y int) string {
	if y == 0 {
		return "-"
	}
	return strconv.Itoa(y)
}

// vehicleModelOther 是下拉的「其他」哨兵值。
//
// 用 __OTHER__ 而非空字串或 "其他"：空字串會被 required 擋下，
// "其他" 則是 migration 裡真實存在的選項（label「其他（手動輸入）」），
// 兩者都會與真正的自由輸入混淆。前後加底劃線的常值不會是合法車型。
const vehicleModelOther = "__OTHER__"

// vehicleModelOptions 車輛品牌車型下拉（lookup category='vehicle_model'）。
//
// selected 為該車現有的 brand_model：
//   - 在清單內 → 標示 selected，使用者看到目前值
//   - 不在清單內（舊資料或自由輸入過）→ 額外附一個「（現有值）」選項並
//     設為 selected，避免一進編輯頁就被靜默改寫成清單第一項
//   - 最後固定附「其他（手動輸入）」
func vehicleModelOptions(selected string) string {
	selected = strings.TrimSpace(selected)
	rows, err := db.DB.Query(`SELECT code, label FROM lookup_items
		WHERE category = 'vehicle_model' AND active = 1
		ORDER BY sort_order, code`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	known := false
	for rows.Next() {
		var code, label string
		if rows.Scan(&code, &label) != nil {
			continue
		}
		sel := ""
		if code == selected {
			sel = " selected"
			known = true
		}
		b.WriteString(`<option value="` + htmlEsc(code) + `"` + sel + `>` + htmlEsc(label) + `</option>`)
	}
	// 現有值不在清單內：保留它，讓使用者自己決定是否正規化。
	if selected != "" && !known {
		b.WriteString(`<option value="` + htmlEsc(selected) + `" selected>` +
			htmlEsc(selected) + `（現有值）</option>`)
	}
	b.WriteString(`<option value="` + vehicleModelOther + `">其他（手動輸入）</option>`)
	return b.String()
}

// normVehicleModel 正規化車型輸入。
//
// 擋下 __OTHER__ 哨兵值：前端在選「其他」時會把 select 的 name 移除、
// 只送出文字欄位；但若使用者停用 JavaScript，哨兵值會直接送出而被存進
// brand_model，之後所有分析都會看到一台名為「__OTHER__」的車。
// 後端一律當成未填，讓必填檢查擋下。
func normVehicleModel(v string) string {
	v = strings.TrimSpace(v)
	if v == vehicleModelOther {
		return ""
	}
	return v
}
