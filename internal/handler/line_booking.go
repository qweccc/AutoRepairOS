package handler

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"autorepairos/internal/db"
)

// LINE 預約掛號（FR-APP-06）：已綁定客戶傳「預約」進入多步驟對話，
// 依序收集日期→車輛→服務內容→確認，建立 booked 預約供櫃檯排時段。
// 對話狀態存記憶體（10 分鐘 TTL），重啟清零可接受——客戶重傳「預約」即可。

type bookingVehicle struct {
	ID    int64
	Plate string
}

type bookingSession struct {
	Step     string // date | vehicle | service | confirm
	Date     string
	Vehicles []bookingVehicle
	Vehicle  bookingVehicle
	Service  string
	Expires  time.Time
}

var (
	bookingMu       sync.Mutex
	bookingSessions = map[string]*bookingSession{}
)

const bookingTTL = 10 * time.Minute

func getBookingSession(userID string) *bookingSession {
	bookingMu.Lock()
	defer bookingMu.Unlock()
	s := bookingSessions[userID]
	if s == nil {
		return nil
	}
	if time.Now().After(s.Expires) {
		delete(bookingSessions, userID)
		return nil
	}
	return s
}

func dropBookingSession(userID string) {
	bookingMu.Lock()
	defer bookingMu.Unlock()
	delete(bookingSessions, userID)
}

// startBooking 客戶傳「預約」時進入流程。
func startBooking(lineUserID string) string {
	var customerID int64
	err := db.DB.QueryRow(`SELECT id FROM active_customers WHERE line_user_id = ?`, lineUserID).Scan(&customerID)
	if err != nil {
		return "請先完成綁定才能預約（輸入「綁定 綁定碼」，綁定碼請向櫃檯索取）。"
	}
	rows, err := db.DB.Query(`SELECT id, plate_number FROM active_vehicles WHERE customer_id = ? ORDER BY id`, customerID)
	if err != nil {
		return "系統忙碌，請稍後再試。"
	}
	defer rows.Close()
	var vehicles []bookingVehicle
	for rows.Next() {
		var v bookingVehicle
		if rows.Scan(&v.ID, &v.Plate) == nil {
			vehicles = append(vehicles, v)
		}
	}
	if len(vehicles) == 0 {
		return "您名下沒有車輛紀錄，請聯絡櫃檯建檔後再預約。"
	}
	bookingMu.Lock()
	bookingSessions[lineUserID] = &bookingSession{
		Step:     "date",
		Vehicles: vehicles,
		Expires:  time.Now().Add(bookingTTL),
	}
	bookingMu.Unlock()
	return "好的，請輸入預約日期（例如 2026-11-20、11/20，或「明天」「後天」）。\n回覆「取消」可隨時放棄。"
}

// handleBookingMessage 進行中的預約對話。回 (回覆, 是否已處理)。
func handleBookingMessage(lineUserID, q string) (string, bool) {
	s := getBookingSession(lineUserID)
	if s == nil {
		return "", false
	}
	if q == "取消" {
		dropBookingSession(lineUserID)
		return "已取消預約流程。如需預約請再傳「預約」。", true
	}
	s.Expires = time.Now().Add(bookingTTL)

	switch s.Step {
	case "date":
		date, ok := parseBookingDate(q)
		if !ok {
			return "日期格式看不懂，請輸入如 2026-11-20、11/20，或「明天」「後天」。回覆「取消」可放棄。", true
		}
		today := time.Now().Format("2006-01-02")
		if date < today {
			return "預約日期不能是過去，請重新輸入。", true
		}
		if date > time.Now().AddDate(0, 0, 60).Format("2006-01-02") {
			return "目前僅開放 60 天內的預約，請重新輸入日期。", true
		}
		s.Date = date
		if len(s.Vehicles) == 1 {
			s.Vehicle = s.Vehicles[0]
			s.Step = "service"
			return fmt.Sprintf("日期：%s\n車輛：%s\n請輸入服務內容（例如：定期保養、輪胎、檢修）。", date, s.Vehicle.Plate), true
		}
		s.Step = "vehicle"
		var b strings.Builder
		fmt.Fprintf(&b, "日期：%s\n您有多台車輛，請回覆編號選擇：\n", date)
		for i, v := range s.Vehicles {
			fmt.Fprintf(&b, "%d. %s\n", i+1, v.Plate)
		}
		return b.String(), true

	case "vehicle":
		n, err := strconv.Atoi(strings.TrimSpace(q))
		if err != nil || n < 1 || n > len(s.Vehicles) {
			return fmt.Sprintf("請回覆 1～%d 的編號選擇車輛，或「取消」放棄。", len(s.Vehicles)), true
		}
		s.Vehicle = s.Vehicles[n-1]
		s.Step = "service"
		return fmt.Sprintf("車輛：%s\n請輸入服務內容（例如：定期保養、輪胎、檢修）。", s.Vehicle.Plate), true

	case "service":
		if q == "" || len([]rune(q)) > 100 {
			return "請輸入服務內容（100 字以內），例如「定期保養」。", true
		}
		s.Service = q
		s.Step = "confirm"
		return bookingSummary(s) + "\n回覆「確認」送出預約，「取消」放棄。", true

	case "confirm":
		if q == "確認" {
			_, err := db.DB.Exec(`INSERT INTO appointments (customer_id, vehicle_id, appointment_date, service_type, status, notes)
				VALUES (?, ?, ?, ?, 'booked', 'LINE 預約')`,
				bookingCustomerID(lineUserID), s.Vehicle.ID, s.Date, s.Service)
			if err != nil {
				return "預約寫入失敗，請稍後再試或直接聯絡車行。", true
			}
			dropBookingSession(lineUserID)
			return bookingSummary(s) + "\n預約成功！櫃檯會為您安排時段，如需改期請聯絡車行。", true
		}
		return bookingSummary(s) + "\n回覆「確認」送出預約，「取消」放棄。", true
	}
	return "", false
}

func bookingCustomerID(lineUserID string) int64 {
	var id int64
	db.DB.QueryRow(`SELECT id FROM active_customers WHERE line_user_id = ?`, lineUserID).Scan(&id)
	return id
}

func bookingSummary(s *bookingSession) string {
	return fmt.Sprintf("預約內容：\n日期：%s\n車輛：%s\n服務：%s", s.Date, s.Vehicle.Plate, s.Service)
}

// parseBookingDate 寬容解析：ISO、斜線、M/D（自動補年）、明天/後天。
func parseBookingDate(q string) (string, bool) {
	q = strings.TrimSpace(q)
	now := time.Now()
	switch q {
	case "明天":
		return now.AddDate(0, 0, 1).Format("2006-01-02"), true
	case "後天":
		return now.AddDate(0, 0, 2).Format("2006-01-02"), true
	}
	for _, layout := range []string{"2006-01-02", "2006/1/2", "2006-1-2", "2006/01/02"} {
		if t, err := time.Parse(layout, q); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	// M/D 或 M-D：補今年；已過則明年。
	for _, sep := range []string{"/", "-"} {
		parts := strings.Split(q, sep)
		if len(parts) != 2 {
			continue
		}
		m, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		d, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || m < 1 || m > 12 || d < 1 || d > 31 {
			continue
		}
		t := time.Date(now.Year(), time.Month(m), d, 0, 0, 0, 0, time.Local)
		if t.Month() != time.Month(m) || t.Day() != d {
			continue // 無效日期（如 2/30）
		}
		if t.Format("2006-01-02") < now.Format("2006-01-02") {
			t = t.AddDate(1, 0, 0)
		}
		return t.Format("2006-01-02"), true
	}
	return "", false
}