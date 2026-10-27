package handler

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/components"
	"autorepairos/web/pages"
)

// NewWorkOrder 開立工單頁面。
//
// 支援三種進入方式，對應櫃檯的三種常見情境：
//   - /work-orders/new?vehicle=21   從車輛頁點「開工單」
//   - /work-orders/new?plate=ABC-1234 客戶直接報車牌
//   - /work-orders/new?customer=3    從客戶頁點「為此客戶開工單」
//   - /work-orders/new?q=keyword     搜尋車牌 / 車主 / 電話後挑車
func NewWorkOrder(w http.ResponseWriter, r *http.Request, user model.User) {
	preVehicle := mustInt(r.URL.Query().Get("vehicle"))
	prePlate := strings.TrimSpace(r.URL.Query().Get("plate"))
	preCustomer := mustInt(r.URL.Query().Get("customer"))
	kw := strings.TrimSpace(r.URL.Query().Get("q"))

	// 從預約進廠開單：把預約的客戶、車輛帶進來。
	// 為什麼需要：櫃檯已經知道車到了、預約資料也填好了，卻要在開單時重打一次
	// 車牌與客戶——既慢又容易選錯車。
	preAppointment := mustInt(r.URL.Query().Get("appointment_id"))
	apptBanner := ""
	if preAppointment > 0 {
		var apptVehicle, apptCustomer int64
		var apptService, apptStatus, apptDate, apptTime string
		err := db.DB.QueryRow(`SELECT vehicle_id, customer_id, COALESCE(service_type,''),
				status, COALESCE(appointment_date,''), COALESCE(appointment_time,'')
			FROM appointments WHERE id = ?`, preAppointment).
			Scan(&apptVehicle, &apptCustomer, &apptService, &apptStatus, &apptDate, &apptTime)
		switch {
		case err != nil || apptVehicle <= 0:
			apptBanner = `<div class="msg msg-err">找不到預約紀錄，已改為一般開單。</div>`
			preAppointment = 0
		case apptStatus == "cancelled" || apptStatus == "completed":
			// 已取消／已完成的預約不該再開單：開了也無法回寫狀態，
			// 而且車主可能根本沒來。明確告知而不是靜默開一張空單。
			apptBanner = `<div class="msg msg-err">這筆預約已` +
				map[string]string{"cancelled": "取消", "completed": "結案"}[apptStatus] +
				`，不能據以開單。</div>`
			preAppointment = 0
		default:
			preVehicle = apptVehicle
			preCustomer = apptCustomer
			label := map[string]string{"booked": "已預約", "confirmed": "已確認", "arrived": "已進廠"}[apptStatus]
			if label == "" {
				label = apptStatus
			}
			apptBanner = `<div class="msg msg-ok">由預約進廠開單（` + htmlEsc(label) +
				" · " + htmlEsc(apptDate)
			if apptTime != "" {
				apptBanner += " " + htmlEsc(apptTime)
			}
			apptBanner += "）"
			if apptService != "" {
				apptBanner += "，預約服務：" + htmlEsc(apptService)
			}
			apptBanner += `。客戶與車輛已帶入，請確認無誤後再送出。</div>`
		}
	}

	// 指定了車牌就先查出車輛 id，讓表單預選
	if preVehicle <= 0 && prePlate != "" {
		var vid int64
		if err := db.DB.QueryRow(
			"SELECT id FROM active_vehicles WHERE UPPER(plate_number) = UPPER(?)", prePlate).Scan(&vid); err == nil {
			preVehicle = vid
		}
	}

	var vehicleList []model.Vehicle

	if preCustomer > 0 {
		// 指定客戶 → 只顯示該客戶的車輛
		rows, err := db.DB.Query(`SELECT `+vehicleCols+` FROM active_vehicles v
			JOIN active_customers c ON c.id = v.customer_id
			WHERE v.customer_id = ? ORDER BY v.plate_number`, preCustomer)
		if err == nil {
			for rows.Next() {
				v, serr := scanVehicle(rows)
				if serr == nil {
					vehicleList = append(vehicleList, v)
				}
			}
			rows.Close()
		}
	} else {
		// 一般情況：列出可選車輛。
		// 篩選交給前端的即時搜尋處理，這裡給完整清單；
		// 只在明確帶入關鍵字（?q=）時才用 SQL 先篩一次，減少傳輸量。
		query := `SELECT ` + vehicleCols + ` FROM active_vehicles v
			JOIN active_customers c ON c.id = v.customer_id WHERE 1=1`
		var args []interface{}
		if kw != "" {
			query += ` AND (v.plate_number LIKE ? OR v.brand_model LIKE ?
				OR c.name LIKE ? OR c.phone LIKE ? OR COALESCE(v.vin,'') LIKE ?)`
			like := "%" + kw + "%"
			args = append(args, like, like, like, like, like)
		}
		// 上限 500：超過代表資料異常，寧可請櫃檯用搜尋也不要塞爆瀏覽器
		query += ` ORDER BY v.plate_number LIMIT 500`

		rows, err := db.DB.Query(query, args...)
		if err == nil {
			for rows.Next() {
				v, serr := scanVehicle(rows)
				if serr == nil {
					vehicleList = append(vehicleList, v)
				}
			}
			rows.Close()
		}
	}

	// 選了車輛就帶出該車主，讓櫃檯確認身分
	var linkedCustomer string
	if preVehicle > 0 {
		rows, err := db.DB.Query(`SELECT c.id, c.name, c.phone FROM active_vehicles v
			JOIN active_customers c ON c.id = v.customer_id WHERE v.id = ?`, preVehicle)
		if err == nil && rows.Next() {
			var cid int64
			var name, phone string
			if rows.Scan(&cid, &name, &phone) == nil {
				preCustomer = cid
				linkedCustomer = name + " / " + phone
			}
		}
		rows.Close()
	}

	// 車輛選項交給前端做即時篩選，因此這裡給完整清單（不分頁）。
	// 車輛數是「店內車主的車」，一間車行數百台，JSON 幾十 KB 可接受；
	// 真的太多時會退化成純文字輸入 + 送出後重整頁。
	optsJSON := vehicleOptionsJSON(vehicleList)

	// 預選車輛的顯示文字
	presetLabel := ""
	if preVehicle > 0 {
		for _, v := range vehicleList {
			if v.ID == preVehicle {
				presetLabel = v.PlateNumber + " / " + v.BrandModel + " / " + v.CustomerName
			}
		}
	}

	// 客戶提示
	customerHint := `<span style="color:#64748b;font-size:12px">建立後會依所選車輛自動帶入車主</span>`
	if linkedCustomer != "" {
		customerHint = `<span class="pill p-completed">` + htmlEsc(linkedCustomer) + `</span>`
	}

	// 預填里程：帶入該車上次進廠里程
	mileage := ""
	if preVehicle > 0 {
		var last sql.NullInt64
		if err := db.DB.QueryRow(`SELECT current_mileage FROM active_work_orders
			WHERE vehicle_id = ? ORDER BY id DESC LIMIT 1`, preVehicle).Scan(&last); err == nil && last.Valid {
			mileage = strconv.FormatInt(last.Int64, 10)
		}
	}

	notFound := ""
	if kw != "" && len(vehicleList) == 0 {
		notFound = `<div class="msg msg-err">查無符合「` + htmlEsc(kw) + `」的車輛。
			請確認車牌是否正確，或先到「車輛」頁為這位客戶新增車輛。</div>`
	}
	if prePlate != "" && preVehicle <= 0 {
		notFound = `<div class="msg msg-err">查無車牌「` + htmlEsc(prePlate) + `」。
			請確認車牌是否正確，或先到「車輛」頁新增。</div>`
	}

	// 表單裡的 appointment_id 由預約開單時帶回，建立工單時一併把預約標記為已進廠。
	// 之所以留在表單裡而不是只看 URL：櫃檯可能中途改了其他欄位後送出，
	// 若以重新帶入的參數為準會失去來源。
	data := pages.NewWorkOrderPageData{
		FlashHTML:          flash(r),
		NotFoundHTML:       notFound,
		ApptBannerHTML:     apptBanner,
		PreAppointment:     int64(preAppointment),
		PreVehicle:         preVehicle,
		VehicleOptionsJSON: optsJSON,
		Vehicles:           vehicleList,
		PresetLabel:        presetLabel,
		Mileage:            mileage,
		TechnicianOptions:  technicianOptions(),
		InsuranceOptions:   insuranceOptionsSel(0),
		CustomerHintHTML:   customerHint,
	}
	_ = pages.NewWorkOrderPage(data, user).Render(r.Context(), w)
}

// SearchWorkOrderVehiclesAPI 回傳開工單選擇器的車輛選項。
// 開工單頁不再依賴前端載入的前 500 台車，避免舊車因排序靠後而查不到。
func SearchWorkOrderVehiclesAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	like := "%" + q + "%"
	rows, err := db.DB.Query(`SELECT v.id, v.plate_number, v.brand_model, c.name
		FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id
		WHERE v.id = ? OR v.plate_number LIKE ? OR v.brand_model LIKE ?
			OR c.name LIKE ? OR c.phone LIKE ? OR COALESCE(v.vin,'') LIKE ?
		ORDER BY
			CASE WHEN UPPER(v.plate_number) = UPPER(?) THEN 0
				WHEN v.plate_number LIKE ? THEN 1 ELSE 2 END,
			v.plate_number
		LIMIT 60`,
		mustInt(q), like, like, like, like, like, q, q+"%")
	if err != nil {
		http.Error(w, "搜尋車輛失敗", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type vehicleOption struct {
		V     string `json:"v"`
		Label string `json:"label"`
	}
	result := make([]vehicleOption, 0)
	for rows.Next() {
		var id int64
		var plate, brand, customer string
		if err := rows.Scan(&id, &plate, &brand, &customer); err != nil {
			http.Error(w, "讀取車輛搜尋結果失敗", http.StatusInternalServerError)
			return
		}
		result = append(result, vehicleOption{
			V: strconv.FormatInt(id, 10),
			Label: strings.Join([]string{
				strings.TrimSpace(plate),
				strings.TrimSpace(brand),
				strings.TrimSpace(customer),
			}, " / "),
		})
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "列舉車輛搜尋結果失敗", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// QuickVehicle 開單頁快速建立客戶與車輛（AJAX）。
//
// 為什麼需要：櫃檯開單時若查無車牌，傳統流程要跳出頁面先到 /customers
// 與 /vehicles 建檔再回來，新客新車被迫中斷。此 API 讓 Modal 在同一
// 頁面完成建立並回傳 vehicle id，前端直接帶入工單表單。
func QuickVehicle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := currentUser(r); err != nil {
		if r.Header.Get("HX-Request") == "true" {
			writeQuickVehicleFragment(w, r, http.StatusUnauthorized, components.QuickVehicleResultData{Error: "未登入"})
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "未登入"})
		return
	}

	plate := strings.TrimSpace(r.FormValue("plate_number"))
	brand := strings.TrimSpace(r.FormValue("brand_model"))
	name := strings.TrimSpace(r.FormValue("customer_name"))
	phone := strings.TrimSpace(r.FormValue("customer_phone"))
	mileage := strings.TrimSpace(r.FormValue("current_mileage"))

	if plate == "" || brand == "" || name == "" || phone == "" {
		if r.Header.Get("HX-Request") == "true" {
			writeQuickVehicleFragment(w, r, http.StatusBadRequest, components.QuickVehicleResultData{Error: "車牌、廠牌、姓名、電話為必填"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "車牌、廠牌、姓名、電話為必填"})
		return
	}

	// 同車牌已存在：不重複建立，但**必須讓櫃檯知道這台車已經有車主**。
	//
	// 原本直接回傳既有 vehicle 並 ok:true，前端只看到「車輛已帶入」——
	// 櫃檯不會發現自己可能打錯車牌，或正要把工單開到另一台車上。
	// 車牌是選車的唯一依據，選錯車等於整張工單（含收款）都掛錯人。
	var existingID int64
	if err := db.DB.QueryRow(`SELECT id FROM active_vehicles WHERE UPPER(plate_number) = UPPER(?)`, plate).Scan(&existingID); err == nil {
		var label, ownerName, ownerPhone string
		db.DB.QueryRow(`SELECT v.plate_number || ' / ' || COALESCE(v.brand_model,'') || ' / ' || COALESCE(c.name,''),
			COALESCE(c.name,''), COALESCE(c.phone,'')
			FROM active_vehicles v JOIN active_customers c ON c.id = v.customer_id WHERE v.id = ?`, existingID).
			Scan(&label, &ownerName, &ownerPhone)
		if r.Header.Get("HX-Request") == "true" {
			writeQuickVehicleFragment(w, r, http.StatusOK, components.QuickVehicleResultData{
				OK: true, VehicleID: strconv.FormatInt(existingID, 10), Label: label,
				Duplicate: true, OwnerName: ownerName, OwnerPhone: ownerPhone,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "vehicle_id": existingID, "label": label, "mileage": "",
			"duplicate":   true,
			"owner_name":  ownerName,
			"owner_phone": ownerPhone,
		})
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "建立失敗"})
		return
	}
	defer tx.Rollback()

	// 客戶：同電話不重複建立
	var customerID int64
	err = tx.QueryRow(`SELECT id FROM active_customers WHERE phone = ?`, phone).Scan(&customerID)
	if err != nil {
		res, cerr := tx.Exec(`INSERT INTO customers (name, phone) VALUES (?, ?)`, name, phone)
		if cerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "建立客戶失敗"})
			return
		}
		customerID, _ = res.LastInsertId()
	}

	res, err := tx.Exec(`INSERT INTO vehicles (customer_id, plate_number, brand_model)
		VALUES (?, ?, ?)`, customerID, plate, brand)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "建立車輛失敗"})
		return
	}
	vehicleID, _ := res.LastInsertId()

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "建立失敗"})
		return
	}

	label := plate + " / " + brand + " / " + name
	if r.Header.Get("HX-Request") == "true" {
		writeQuickVehicleFragment(w, r, http.StatusOK, components.QuickVehicleResultData{
			OK: true, VehicleID: strconv.FormatInt(vehicleID, 10), Label: label, Mileage: mileage,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "vehicle_id": vehicleID, "label": label, "mileage": mileage})
}

func writeQuickVehicleFragment(w http.ResponseWriter, r *http.Request, status int, data components.QuickVehicleResultData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := components.QuickVehicleResult(data).Render(r.Context(), w); err != nil {
		http.Error(w, "渲染快速建車結果失敗", http.StatusInternalServerError)
	}
}
