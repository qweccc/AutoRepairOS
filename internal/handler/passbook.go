package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// PassbookAPI 公開的 LINE 數位維修履歷與保養收據端點（SRS FR-VH-05）。
//
// 身分驗證不靠登入 session，而是靠 customers.line_user_id 綁定——與 T2.11 的
// 估價授權同款機制：只有綁定到該車輛所屬客戶的 LINE 帳號能查到此車輛的履歷。
//
// 驗收邊界（含枚舉防護設計）:
//   - 未提供 line 參數，或該 LINE 帳號未綁定任何客戶 → 404（不洩露任何資訊）
//   - 綁定客戶查自己的車輛 → 回傳該車所有 passbook 紀錄
//   - 車牌提供但該車不屬於此客戶（或車輛不存在）→ 404
//   - 綁定正確但該車無 passbook → 回 200 空陣列 [枚舉防護]：
//     若回 404 則攻擊者可逐一猜測車牌，判斷哪些車歸綁定客戶、哪些不歸，
//     故「有權查但無資料」一律回 200 空陣列，不洩露車輛歸屬。
//   - 只回傳必要資訊：客戶名、車牌、工單編號、服務類型、明細、金額、狀態
func PassbookAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	line := strings.TrimSpace(r.URL.Query().Get("line"))
	if line == "" {
		http.NotFound(w, r)
		return
	}

	vehicleIDStr := strings.TrimSpace(r.URL.Query().Get("vehicle_id"))
	vehicleID, err := strconv.ParseInt(vehicleIDStr, 10, 64)
	if err != nil || vehicleID <= 0 {
		// 車牌未提供時無法判斷車輛歸誰 → 一律 404，不洩露任何資訊。
		http.NotFound(w, r)
		return
	}

	// 一次查回 passbook + 客戶 + 車輛，並強制三關聯同時成立：
	//   passbook.customer_id = customers.id
	//   passbook.vehicle_id = vehicles.id
	//   customers.line_user_id = ?（綁定身份）
	// 這保證只有綁定客戶能看自己車輛的履歷。
	//
	// 枚舉防護：WHERE 條件用 line_user_id + vehicle_id，因此「非該客戶」
	// 「未綁定」「查無紀錄」三種情況都查不到列，最後一律回 200 + 空陣列。
	// 若對非該客戶改回 404，攻擊者就能用狀態碼差異枚舉「哪些車牌存在」；
	// 統一回 200 空陣列既達成「不得取得履歷」，又不洩露車輛是否存在。
	woNo := func(id int64) string { return fmt.Sprintf("WO-%05d", id) }

	// 明確列出欄位而非 pb.*：Scan 目標數與順序必須與 SELECT 一致，
	// 否則 SQLite driver 會把 customer_id 塞進 CustomerName 造成型別錯誤。
	rows, err := db.DB.Query(
		`SELECT pb.id, pb.customer_id, pb.vehicle_id, pb.work_order_id,
		        pb.service_type, pb.description,
		        pb.parts_cost, pb.labor_cost, pb.subcontract_cost,
		        pb.subtotal, pb.tax, pb.discount_amount, pb.total_amount,
		        pb.status, pb.notes, pb.created_at, pb.updated_at,
		        COALESCE(c.name, ''), COALESCE(v.plate_number, '')
		 FROM passbook pb
		 JOIN customers c ON c.id = pb.customer_id
		 JOIN vehicles v ON v.id = pb.vehicle_id
		 JOIN work_orders wo ON wo.id = pb.work_order_id
		  AND wo.vehicle_id = v.id
		 WHERE c.line_user_id = ?
		   AND v.id = ?
		 ORDER BY pb.id DESC`,
		line, vehicleID,
	)
	if err != nil {
		log.Printf("PassbookAPI 查詢失敗: %v", err)
		fail(w, "查詢履歷失敗")
		return
	}
	defer rows.Close()

	var passbooks []model.Passbook
	for rows.Next() {
		var p model.Passbook
		var customer, plate string
		if err := rows.Scan(
			&p.ID, &p.CustomerID, &p.VehicleID, &p.WorkOrderID,
			&p.ServiceType, &p.Description,
			&p.PartsCost, &p.LaborCost, &p.SubcontractCost,
			&p.Subtotal, &p.Tax, &p.DiscountAmount, &p.TotalAmount,
			&p.Status, &p.Notes, &p.CreatedAt, &p.UpdatedAt,
			&customer, &plate,
		); err != nil {
			log.Printf("PassbookAPI 讀列失敗: %v", err)
			fail(w, "讀取履歷失敗")
			return
		}
		p.CustomerName = customer
		p.VehiclePlate = plate
		p.WorkOrderNo = woNo(p.WorkOrderID)
		passbooks = append(passbooks, p)
	}
	if err := rows.Err(); err != nil {
		log.Printf("PassbookAPI 列舉失敗: %v", err)
		fail(w, "讀取履歷失敗")
		return
	}

	// 綁定正確但無資料時回空陣列（有權查、但無紀錄），
	// 避免用 404 洩露「此車牌歸誰」。
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(passbooks); err != nil {
		log.Printf("PassbookAPI 輸出失敗: %v", err)
	}
}

// RecordPassbookForWorkOrder 為結帳工單建立車主維修履歷；已有紀錄時冪等略過。
//
// parts_cost、labor_cost、subcontract_cost 雖沿用既有欄位名稱，實際記錄的是
// 對客戶收取的各類維修收入（work_order_items.subtotal），不是車廠成本；
// total_amount 則是工單對客戶顯示的最終維修金額。
func RecordPassbookForWorkOrder(woID int64) error {
	var customerID, vehicleID int64
	var totalAmount float64
	if err := db.DB.QueryRow(`SELECT customer_id, vehicle_id, COALESCE(total_amount,0)
		FROM active_work_orders WHERE id = ?`, woID).
		Scan(&customerID, &vehicleID, &totalAmount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("工單 #%d 不存在", woID)
		}
		log.Printf("RecordPassbookForWorkOrder 讀取工單失敗（工單 #%d）: %v", woID, err)
		return fmt.Errorf("讀取工單 #%d 失敗: %w", woID, err)
	}

	// 條件式 INSERT 在同一條 SQL 內檢查既有紀錄，避免重放或併發結帳產生重複履歷。
	_, err := db.DB.Exec(`INSERT INTO passbook
		(customer_id, vehicle_id, work_order_id, service_type, description,
		 parts_cost, labor_cost, subcontract_cost, subtotal, tax,
		 discount_amount, total_amount, status)
		SELECT wo.customer_id, wo.vehicle_id, wo.id, 'repair',
			COALESCE((SELECT GROUP_CONCAT(description || ' x' || quantity, '、')
				FROM work_order_items WHERE work_order_id = wo.id), ''),
			COALESCE((SELECT SUM(subtotal) FROM work_order_items
				WHERE work_order_id = wo.id AND item_type = 'part'), 0),
			COALESCE((SELECT SUM(subtotal) FROM work_order_items
				WHERE work_order_id = wo.id AND item_type = 'labor'), 0),
			COALESCE((SELECT SUM(subtotal) FROM work_order_items
				WHERE work_order_id = wo.id AND item_type = 'subcontract'), 0),
			COALESCE((SELECT SUM(subtotal) FROM work_order_items
				WHERE work_order_id = wo.id), 0),
			0, 0, ?, 'paid'
		FROM active_work_orders wo
		WHERE wo.id = ?
		  AND NOT EXISTS (SELECT 1 FROM passbook WHERE work_order_id = wo.id)`,
		totalAmount, woID)
	if err != nil {
		log.Printf("RecordPassbookForWorkOrder 寫入失敗（工單 #%d）: %v", woID, err)
		return fmt.Errorf("建立工單 #%d 的維修履歷失敗: %w", woID, err)
	}
	return nil
}
