package handler

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
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

	// 指定了車牌就先查出車輛 id，讓表單預選
	if preVehicle <= 0 && prePlate != "" {
		var vid int64
		if err := db.DB.QueryRow(
			"SELECT id FROM vehicles WHERE UPPER(plate_number) = UPPER(?)", prePlate).Scan(&vid); err == nil {
			preVehicle = vid
		}
	}

	var vehicleList []model.Vehicle

	if preCustomer > 0 {
		// 指定客戶 → 只顯示該客戶的車輛
		rows, err := db.DB.Query(`SELECT `+vehicleCols+` FROM vehicles v
			JOIN customers c ON c.id = v.customer_id
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
		query := `SELECT ` + vehicleCols + ` FROM vehicles v
			JOIN customers c ON c.id = v.customer_id WHERE 1=1`
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
		rows, err := db.DB.Query(`SELECT c.id, c.name, c.phone FROM vehicles v
			JOIN customers c ON c.id = v.customer_id WHERE v.id = ?`, preVehicle)
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

	// 無 JS 時的降級選單（同一份資料）
	var fallback strings.Builder
	fallback.WriteString(`<option value="">— 請選擇車輛 —</option>`)
	for _, v := range vehicleList {
		fallback.WriteString(`<option value="` + strconv.FormatInt(v.ID, 10) + `"` +
			selIf(v.ID == preVehicle) + `>` +
			htmlEsc(v.PlateNumber) + " / " + htmlEsc(v.BrandModel) + " / " +
			htmlEsc(v.CustomerName) + `</option>`)
	}
	if len(vehicleList) == 0 {
		fallback.WriteString(`<option value="0">— 查無車輛，請先至車輛頁新增 —</option>`)
	}

	// 預選車輛的顯示文字
	presetLabel := ""
	if preVehicle > 0 {
		for _, v := range vehicleList {
			if v.ID == preVehicle {
				presetLabel = htmlEsc(v.PlateNumber) + " / " + htmlEsc(v.BrandModel) + " / " + htmlEsc(v.CustomerName)
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
		if err := db.DB.QueryRow(`SELECT current_mileage FROM work_orders
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

	body := flash(r) + notFound + `
    <div class="card">
      <h2>開立工單</h2>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">
        在下方輸入車牌、車型或車主姓名即可即時篩選，點選後確認車主再填里程。
      </p>
      <form class="frm" method="POST" action="/work-orders" id="woForm">
        <label class="full">車輛
          <div class="vsearch" data-vsearch data-options="` + optsJSON + `"` +
			selAttr(preVehicle > 0) + `>
            <input type="hidden" name="vehicle_id" id="vehicle_id" value="` +
			strconv.FormatInt(preVehicle, 10) + `" required>
            <input class="vs-input" id="vsInput" autocomplete="off"
                   placeholder="輸入車牌 / 車型 / 車主 / 電話"
                   value="` + presetLabel + `">
            <div class="vs-list hidden"></div>
          </div>
          <noscript>
            <select name="vehicle_id_fallback" required style="margin-top:8px">` + fallback.String() + `</select>
          </noscript>
          <button type="button" class="btn btn-s" style="margin-top:8px" onclick="openQuickModal()">⚡ 查無車牌？快速建立客戶與車輛</button>
        </label>
        <label>進廠里程<input name="current_mileage" type="number" min="0" value="` + htmlEsc(mileage) + `" required></label>
        <label>技師<select name="technician_id"><option value="0">未指派</option>` + technicianOptions() + `</select></label>
        <label>需要核准<select name="approval_status">
          <option value="not_required">不需要</option>
          <option value="pending">需客戶核准</option>
        </select></label>
        <label>車主` + customerHint + `</label>
        <label class="full">客戶反映問題<textarea name="customer_complaint" placeholder="例：引擎異音、煞車踏板偏軟"></textarea></label>
        <div class="full" style="border-top:1px solid #263449;padding-top:16px;margin-top:4px">
          <p style="color:#64748b;font-size:12.5px;margin:0 0 12px">
            保險理賠（選填）：勾選後需指定保險公司，報表才能區分保險件與自費件。
          </p>
        </div>
        <label>件別<select name="is_insurance" id="isInsurance">
          <option value="0">自費</option>
          <option value="1">保險理賠</option>
        </select></label>
        <label>保險公司<select name="insurance_company_id" id="insuranceCompany">` + insuranceOptionsSel(0) + `</select>
          <input type="hidden" name="insurance_company_id_mirror" id="insuranceCompanyMirror" value="">
        </label>
        <label>理賠案件編號<input name="claim_no" placeholder="保險公司給的報案號"></label>
        <label>事故日期<input name="accident_date" type="date"></label>
        <label>自負額<input name="deductible_amount" type="number" min="0" step="1" placeholder="0"></label>
        <div class="full">
          <button class="btn btn-p" type="submit">建立工單</button>
          <a class="btn btn-s" href="/work-orders">取消</a>
        </div>
      </form>
    </div>

    <script>
      // 未勾選保險件時把保險欄位停用，避免誤填。
      // 注意：disabled 的 input 不會隨表單送出，所以保險公司另以 hidden 鏡像保存 id，
      // 否則「選了保險件但沒改公司」時後端會收不到 insurance_company_id。
      (function () {
        var sel = document.getElementById('isInsurance');
        var mirror = document.getElementById('insuranceCompanyMirror');
        var company = document.querySelector('[name="insurance_company_id"]');
        var others = ['claim_no', 'accident_date', 'deductible_amount'];
        function sync() {
          var on = sel && sel.value === '1';
          others.forEach(function (n) {
            var el = document.querySelector('[name="' + n + '"]');
            if (el) el.disabled = !on;
          });
          if (company) company.disabled = !on;
          if (mirror) mirror.value = (on && company) ? company.value : '';
        }
        if (sel) {
          sel.addEventListener('change', sync);
          if (company) company.addEventListener('change', sync);
          sync();
        }
      })();
    </script>

    <link rel="stylesheet" href="/static/vehicle-search.css">
    <script src="/static/vehicle-search.js" defer></script>
    <div id="quickModal" class="modal-backdrop hidden">
      <div class="modal-card">
        <div class="modal-header">
          <h3>⚡ 快速建立客戶與車輛</h3>
          <button type="button" class="close-btn" onclick="closeQuickModal()">✕</button>
        </div>
        <form id="quickForm" onsubmit="submitQuickVehicle(event)">
          <div class="frm">
            <label>車牌號碼<input name="plate_number" id="quickPlate" required></label>
            <label>廠牌車型<input name="brand_model" placeholder="例：Toyota RAV4" required></label>
            <label>車主姓名<input name="customer_name" required></label>
            <label>聯絡電話<input name="customer_phone" required></label>
            <label>目前里程<input name="current_mileage" type="number" min="0" required></label>
          </div>
          <div class="modal-actions">
            <button type="submit" class="btn btn-p">建立並帶入工單</button>
            <button type="button" class="btn btn-s" onclick="closeQuickModal()">取消</button>
          </div>
        </form>
      </div>
    </div>
    <script>
      function openQuickModal() {
        document.getElementById('quickModal').classList.remove('hidden');
        document.getElementById('quickPlate').focus();
      }
      function closeQuickModal() {
        document.getElementById('quickModal').classList.add('hidden');
        document.getElementById('quickForm').reset();
      }
      function submitQuickVehicle(e) {
        e.preventDefault();
        var f = document.getElementById('quickForm');
        var data = new FormData(f);
        fetch('/api/quick-vehicle', {method:'POST', body:data})
          .then(function(r){ return r.json(); })
          .then(function(j){
            if (j.ok) {
              closeQuickModal();
              var input = document.getElementById('vehicle_id');
              input.value = j.vehicle_id;
              var vs = document.querySelector('.vs-input');
              if (vs) vs.value = j.label;
              var hint = document.querySelector('[name="current_mileage"]');
              if (hint) hint.value = j.mileage || '';
              var btn = document.querySelector('.btn-p');
              if (btn) btn.focus();
            } else {
              alert(j.error || '建立失敗');
            }
          })
          .catch(function(){ alert('網路錯誤'); });
      }
      document.addEventListener('keydown', function(e){ if(e.key==='Escape') closeQuickModal(); });
    </script>`

	renderPage(w, "開立工單", "/work-orders", user.Username, user.Role, body)
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
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "未登入"})
		return
	}

	plate := strings.TrimSpace(r.FormValue("plate_number"))
	brand := strings.TrimSpace(r.FormValue("brand_model"))
	name := strings.TrimSpace(r.FormValue("customer_name"))
	phone := strings.TrimSpace(r.FormValue("customer_phone"))
	mileage := strings.TrimSpace(r.FormValue("current_mileage"))

	if plate == "" || brand == "" || name == "" || phone == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "車牌、廠牌、姓名、電話為必填"})
		return
	}

	// 同車牌已存在：直接回傳既有 vehicle，不重複建立
	var existingID int64
	if err := db.DB.QueryRow(`SELECT id FROM vehicles WHERE UPPER(plate_number) = UPPER(?)`, plate).Scan(&existingID); err == nil {
		var label string
		db.DB.QueryRow(`SELECT v.plate_number || ' / ' || COALESCE(v.brand_model,'') || ' / ' || COALESCE(c.name,'')
			FROM vehicles v JOIN customers c ON c.id = v.customer_id WHERE v.id = ?`, existingID).Scan(&label)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "vehicle_id": existingID, "label": label, "mileage": ""})
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
	err = tx.QueryRow(`SELECT id FROM customers WHERE phone = ?`, phone).Scan(&customerID)
	if err != nil {
		res, cerr := tx.Exec(`INSERT INTO customers (name, phone) VALUES (?, ?)`, name, phone)
		if cerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "建立客戶失敗"})
			return
		}
		customerID, _ = res.LastInsertId()
	}

	res, err := tx.Exec(`INSERT INTO vehicles (customer_id, plate_number, brand_model, current_mileage)
		VALUES (?, ?, ?, ?)`, customerID, plate, brand, atoiDefault(mileage, 0))
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "vehicle_id": vehicleID, "label": label, "mileage": mileage})
}

// selAttr 產生 data-picked 屬性，讓 JS 知道已經有預選車輛。
func selAttr(picked bool) string {
	if picked {
		return ` data-picked="1"`
	}
	return ""
}
