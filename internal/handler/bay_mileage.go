// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/components"
	"github.com/a-h/templ"
)

// ============================================================
// 預約工位（FR-APP-02 / FR-APP-04）與車輛里程履歷（FR-VH-03）
//
// 工位：車廠的頂高機／鈑金校正機／烤漆房數量有限，同一時段只能服務一台車。
// 沒有工位欄位就無法偵測重複排程，只能靠櫃檯記憶——這是預約排程最常見的
// 人為失誤來源（兩台車約同一個時段搶同一台頂高機）。
//
// 里程履歷：里程也會在非進廠時更新（客戶電話告知、路邊回報），
// 且需要偵測回退（碼表更換或 key 錯數字）——回退的數據會污染
// 「下次保養里程」的推算。只靠 work_orders.current_mileage 做不到這些。
// ============================================================

// bayList 工位代碼。用固定清單而非自由輸入：只有固定清單能做衝突檢查
// 與「這個工位現在有沒有空」的調度查詢；自由輸入會讓同一台機器出現
// 「頂高機」「一號機」「B1」三種寫法而失去意義。
//
// 車行規模不同工位數量不同，因此這裡只定義「種類」而不定義數量——
// 同種類多台時在代碼後加編號（LIFT-1 / LIFT-2），仍屬於已知清單。
var bayList = []struct{ code, label string }{
	{"LIFT", "頂高機"},
	{"ALIGN", "四輪定位機"},
	{"BODY", "鈑金校正機"},
	{"PAINT", "烤漆房"},
	{"TIRE", "拆胎機／平衡機"},
	{"DIAG", "電腦診斷位"},
}

func bayLabel(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, b := range bayList {
		if b.code == code {
			return b.label
		}
	}
	return code
}

func bayOptions(selected string) templ.Component {
	options := make([]components.BayOption, 0, len(bayList))
	for _, bay := range bayList {
		options = append(options, components.BayOption{Code: bay.code, Label: bay.label})
	}
	legacy := selected != "" && bayLabel(selected) == strings.ToUpper(selected) && !bayKnown(selected)
	return components.BayOptions(selected, options, legacy)
}

func bayOptionsHTML(selected string) (string, error) {
	var out bytes.Buffer
	if err := bayOptions(selected).Render(context.Background(), &out); err != nil {
		return "", err
	}
	return out.String(), nil
}

func bayKnown(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, b := range bayList {
		if b.code == code {
			return true
		}
	}
	return false
}

func normBay(v string) string {
	return strings.ToUpper(strings.TrimSpace(v))
}

// bayConflict 檢查同一工位的 60 分鐘預約區間是否重疊（FR-APP-04）。
//
// 回傳衝突的預約資訊（車牌 / 客戶 / 時段），讓錯誤訊息可以直接告訴櫃檯
// 「跟誰撞了」而不是只說「有衝突」——後者會讓人反覆試錯。
//
// 已取消（cancelled）的預約不佔用工位：取消就是釋放資源。
// excludeID 讓「編輯自己的預約」不會被自己擋下。
func bayConflict(bay, date, timeSlot string, excludeID int64) (int64, string, string, string, bool) {
	if bay == "" || date == "" {
		return 0, "", "", "", false
	}
	// 未指定時段不佔工位；指定工位時則由建立／更新 handler 強制要求時間。
	if timeSlot == "" {
		return 0, "", "", "", false
	}
	if len(timeSlot) != 5 || !validAppointmentTime(timeSlot) {
		return 0, "", "", "", false
	}
	var id int64
	var plate, cust, t string
	var err error
	err = db.DB.QueryRow(`SELECT a.id, COALESCE(v.plate_number,''), COALESCE(c.name,''),
			COALESCE(a.appointment_time,'')
		FROM active_appointments a
		JOIN active_vehicles v ON v.id = a.vehicle_id
		JOIN active_customers c ON c.id = a.customer_id
		WHERE UPPER(TRIM(COALESCE(a.bay,''))) = ?
			AND a.appointment_date BETWEEN date(?, '-1 day') AND date(?, '+1 day')
			AND length(COALESCE(a.appointment_time,'')) = 5
			AND COALESCE(a.appointment_time,'') GLOB '[0-2][0-9]:[0-5][0-9]'
			AND CAST(substr(a.appointment_time,1,2) AS INTEGER) <= 23
			AND ABS(strftime('%s', a.appointment_date || ' ' || a.appointment_time) -
				strftime('%s', ? || ' ' || ?)) < 3600
			AND COALESCE(a.status,'booked') <> 'cancelled'
			AND a.id <> ?
		LIMIT 1`, bay, date, date, date, timeSlot, excludeID).
		Scan(&id, &plate, &cust, &t)
	if err != nil {
		return 0, "", "", "", false
	}
	return id, plate, cust, t, true
}

// baySchedule 回傳指定日期的工位佔用表，供列表頁顯示「這個工位今天還有誰」。
// 用一次查詢取回整天，避免每列一次查詢（N+1）。
func baySchedule(date string) map[string][]string {
	out := map[string][]string{}
	if date == "" {
		return out
	}
	rows, err := db.DB.Query(`SELECT UPPER(COALESCE(a.bay,'')), COALESCE(a.appointment_time,''),
			COALESCE(v.plate_number,''), COALESCE(c.name,''), a.status
		FROM active_appointments a
		JOIN active_vehicles v ON v.id = a.vehicle_id
		JOIN active_customers c ON c.id = a.customer_id
		WHERE a.appointment_date = ? AND a.status <> 'cancelled'
		ORDER BY a.appointment_time`, date)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var bay, t, plate, cust, status string
		if rows.Scan(&bay, &t, &plate, &cust, &status) != nil {
			continue
		}
		if bay == "" {
			continue
		}
		label := t + " " + plate + "（" + cust + "）"
		if status == "completed" {
			label += " ✓"
		}
		out[bay] = append(out[bay], label)
	}
	return out
}

// ============ 里程履歷（FR-VH-03）============

// mileageSourceLabel 里程來源標籤。
var mileageSourceLabel = map[string]string{
	"work_order": "進廠工單",
	"manual":     "手動登錄",
	"import":     "舊系統匯入",
}

// RecordMileage 記錄一筆里程。
//
// source 標明數字從哪來，這是判斷可信度的依據：
//
//	work_order —— 進廠時從工單帶入，最可靠
//	manual     —— 手動登錄（客戶電話告知等），可能 key 錯
//	import     —— 舊系統匯入
//
// 不擋「回退」（比上一筆小）：碼表更換或 key 錯數字是事實，要留痕讓店長
// 事後判斷，靜默丟棄會讓使用者以為存进去了卻找不到。
// 回退的紀錄在顯示時標示提醒。
func RecordMileage(vehicleID int64, mileage int, source string, workOrderID int64, notes string, recordedBy int64) error {
	if vehicleID == 0 || mileage < 0 {
		return nil
	}
	switch source {
	case "work_order", "import":
	default:
		source = "manual"
	}
	var woArg interface{}
	if workOrderID > 0 {
		woArg = workOrderID
	}
	_, err := db.DB.Exec(`INSERT INTO vehicle_mileages
		(vehicle_id, mileage, source, work_order_id, notes, recorded_by)
		VALUES (?,?,?,?,?,?)`,
		vehicleID, mileage, source, woArg, nullStr(notes), zeroToNil64(recordedBy))
	return err
}

// lastMileageEntry 取該車最後一筆里程（用於顯示回退警示）。
func lastMileageEntry(vehicleID int64) (int, string, bool) {
	var m int
	var created string
	err := db.DB.QueryRow(`SELECT mileage, created_at FROM vehicle_mileages
		WHERE vehicle_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, vehicleID).
		Scan(&m, &created)
	if err != nil {
		return 0, "", false
	}
	return m, created, true
}

// mileageHistory 里程履歷列表（含回退標示）。
func mileageHistory(vehicleID int64, limit int) string {
	rows, err := db.DB.Query(`SELECT vm.mileage, COALESCE(vm.source,'manual'),
			COALESCE(vm.notes,''), COALESCE(vm.created_at,''), COALESCE(u.username,''),
			COALESCE(vm.work_order_id,0)
		FROM vehicle_mileages vm
		LEFT JOIN users u ON u.id = vm.recorded_by
		WHERE vm.vehicle_id = ?
		ORDER BY vm.created_at DESC, vm.id DESC LIMIT ?`, vehicleID, limit)
	if err != nil {
		return `<div class="empty">查詢里程履歷失敗</div>`
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString(`<table><tr><th>日期</th><th class="num">里程</th><th>來源</th><th>工單</th><th>操作人</th><th>備註</th></tr>`)
	n := 0
	type row struct {
		m    int
		src  string
		note string
		ts   string
		user string
		wo   int64
	}
	var all []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.m, &r.src, &r.note, &r.ts, &r.user, &r.wo) != nil {
			continue
		}
		all = append(all, r)
	}
	// 由新到舊迭代，但要比對「前一筆」（時間上更早的那一筆）才能判斷回退。
	for i := len(all) - 1; i >= 0; i-- {
		r := all[i]
		// 時間序：all[0] 最新 → all[len-1] 最舊。第 i 筆的「前一筆」是 all[i+1]。
		rollback := ""
		if i+1 < len(all) && r.m < all[i+1].m {
			rollback = ` <span class="pill" style="background:#7f1d1d;color:#fca5a5;font-size:11px">回退</span>`
		}
		srcLabel := mileageSourceLabel[r.src]
		if srcLabel == "" {
			srcLabel = r.src
		}
		woCell := `<span style="color:#64748b">—</span>`
		if r.wo > 0 {
			woCell = `<a href="/work-orders/` + strconv.FormatInt(r.wo, 10) + `">#` +
				strconv.FormatInt(r.wo, 10) + `</a>`
		}
		ts := r.ts
		if len(ts) > 16 {
			ts = ts[:16]
		}
		b.WriteString(`<tr><td style="font-size:12.5px;color:#94a3b8">` + htmlEsc(ts) +
			`</td><td class="num">` + strconv.Itoa(r.m) + rollback +
			`</td><td style="font-size:12.5px">` + srcLabel + `</td><td>` + woCell +
			`</td><td style="font-size:12.5px">` + htmlEsc(orDash(r.user)) +
			`</td><td style="font-size:12px;color:#94a3b8">` + htmlEsc(orDash(r.note)) + `</td></tr>`)
		n++
	}
	b.WriteString(`</table>`)
	if n == 0 {
		return `<div class="empty">尚無里程紀錄</div>`
	}
	return b.String()
}

// AddVehicleMileageForm 手動新增里程。
func AddVehicleMileageForm(w http.ResponseWriter, r *http.Request, user model.User) {
	vehicleID := mustInt(r.PathValue("id"))
	var plate, brand string
	if err := db.DB.QueryRow(`SELECT COALESCE(plate_number,''), COALESCE(brand_model,'')
		FROM active_vehicles WHERE id = ?`, vehicleID).Scan(&plate, &brand); err != nil {
		http.NotFound(w, r)
		return
	}

	last, _, ok := lastMileageEntry(vehicleID)
	lastHint := ""
	if ok {
		lastHint = `　<span style="color:#64748b;font-size:12.5px">（最後一筆：` +
			strconv.Itoa(last) + ` km）</span>`
	}

	body := `
    <div class="card">
      <h2>新增里程紀錄 — ` + htmlEsc(plate) + `</h2>
      <p style="color:#94a3b8;font-size:13.5px;margin:0 0 14px">
        ` + htmlEsc(brand) + lastHint + `
      </p>
      <form class="frm" method="POST" action="/vehicles/` + strconv.FormatInt(vehicleID, 10) + `/mileage">
        <label>里程（km）<input name="mileage" type="number" min="0" step="1" required></label>
        <label class="full">備註<textarea name="notes" placeholder="例：客戶來電告知已跑 8 萬公里"></textarea></label>
        <div class="full">
          <button class="btn btn-p" type="submit">記錄</button>
          <a class="btn btn-s" href="/vehicles/` + strconv.FormatInt(vehicleID, 10) + `">取消</a>
        </div>
      </form>
      <p style="color:#64748b;font-size:12.5px;margin-top:10px">
        進廠工單會自動帶入里程（來源標示「進廠工單」），這裡是給非進廠時
        的更新使用。若新里程比上一筆小，系統仍會記錄但標示「回退」，
        請在備註說明原因（如更換碼表）。
      </p>
    </div>`

	renderPage(w, "新增里程", "/vehicles", user.Username, user.Role, body)
}

// CreateVehicleMileage 手動記錄里程。
func CreateVehicleMileage(w http.ResponseWriter, r *http.Request) {
	vehicleID := mustInt(r.PathValue("id"))
	mileage := atoiDefault(r.FormValue("mileage"), -1)
	if mileage < 0 {
		fail(w, "請填寫有效里程（不可為負數）")
		return
	}
	if countRows("SELECT COUNT(1) FROM active_vehicles WHERE id = ?", vehicleID) == 0 {
		http.NotFound(w, r)
		return
	}
	var actorID int64
	if u, err := currentUser(r); err == nil {
		actorID = u.ID
	}
	if err := RecordMileage(vehicleID, mileage, "manual", 0,
		r.FormValue("notes"), actorID); err != nil {
		fail(w, "記錄里程失敗")
		return
	}
	redirectMsg(w, r, "/vehicles/"+strconv.FormatInt(vehicleID, 10), "里程已記錄："+
		strconv.Itoa(mileage)+" km")
}

// zeroToNil64 把 0 轉成 nil（recorded_by 允許 NULL）。
func zeroToNil64(n int64) interface{} {
	if n == 0 {
		return nil
	}
	return n
}
