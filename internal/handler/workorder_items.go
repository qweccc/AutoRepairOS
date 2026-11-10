package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/service"
	"autorepairos/web/pages"
)

func AddWorkOrderItem(w http.ResponseWriter, r *http.Request, user model.User) {
	woID := mustInt(r.PathValue("id"))
	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
		return
	}
	desc := strings.TrimSpace(r.FormValue("description"))

	qty := atoiDefault(r.FormValue("quantity"), 0)
	if qty < 1 {
		formError(w, r, "新增工單項目", "/work-orders/"+strconv.FormatInt(woID, 10), "數量必須大於 0")
		return
	}

	itemType := r.FormValue("item_type")
	if _, ok := map[string]bool{"labor": true, "part": true, "subcontract": true}[itemType]; !ok {
		itemType = "labor"
	}

	invID := mustInt(r.FormValue("inventory_item_id"))
	if invID < 0 {
		invID = 0
	}

	unitPrice := atofDefault(r.FormValue("unit_price"), 0)
	if itemType == "part" && invID > 0 {
		var name string
		var sell float64
		if err := db.DB.QueryRow("SELECT name, COALESCE(selling_price,0) FROM active_inventory_items WHERE id=?", invID).
			Scan(&name, &sell); err == nil {
			if desc == "" {
				desc = name
			}
			unitPrice = sell
		} else {
			fail(w, "找不到該零件")
			return
		}
	}

	if desc == "" {
		formError(w, r, "新增工單項目", "/work-orders/"+strconv.FormatInt(woID, 10), "描述為必填")
		return
	}

	var invArg interface{}
	if itemType == "part" && invID > 0 {
		invArg = invID
	}

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	var pt, at interface{}
	if v := mustInt(r.FormValue("primary_technician_id")); v > 0 {
		pt = v
	}
	if v := mustInt(r.FormValue("assistant_technician_id")); v > 0 {
		at = v
	}

	if _, err := tx.Exec(`INSERT INTO work_order_items
		(work_order_id, item_type, inventory_item_id, description, quantity, unit_price, subtotal,
		 primary_technician_id, assistant_technician_id)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		woID, itemType, invArg, desc, qty, unitPrice, float64(qty)*unitPrice, pt, at); err != nil {
		fail(w, "加入項目失敗")
		return
	}

	// 零件預扣：工單加入零件時 reserved_quantity += qty，
	// 可用庫存 = stock_quantity - reserved_quantity，避免多工單並行搶料。
	// 實際扣料仍在結帳（status=paid）時執行（見 UpdateWorkOrderStatus）。
	if itemType == "part" && invID > 0 {
		res, err := tx.Exec(`UPDATE inventory_items
				SET reserved_quantity = COALESCE(reserved_quantity,0) + ?
				WHERE id = ? AND COALESCE(stock_quantity,0) - COALESCE(reserved_quantity,0) >= ?`, qty, invID, qty)
		if err != nil {
			fail(w, "更新預扣量失敗")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var stock, reserved int
			tx.QueryRow(`SELECT COALESCE(stock_quantity,0), COALESCE(reserved_quantity,0) FROM inventory_items WHERE id = ?`, invID).Scan(&stock, &reserved)
			if stock-reserved < qty {
				fail(w, "庫存不足")
				return
			}
			fail(w, "更新預扣量失敗")
			return
		}
	}

	if err := service.RecalculateWorkOrderTotal(r.Context(), tx, woID); err != nil {
		fail(w, "重算金額失敗")
		return
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}

	if renderWorkOrderItemsFragment(w, r, woID, user) {
		return
	}

	// AJAX 請求：回傳 JSON 供前端局部更新，不跳頁
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		var newTotal float64
		db.DB.QueryRow("SELECT COALESCE(total_amount,0) FROM active_work_orders WHERE id = ?", woID).Scan(&newTotal)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "wo_id": woID, "total": newTotal,
			"desc": desc, "qty": qty, "unit_price": unitPrice,
			"subtotal": float64(qty) * unitPrice, "item_type": itemType,
		})
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10), "已加入項目")
}

func DeleteWorkOrderItem(w http.ResponseWriter, r *http.Request, user model.User) {
	woID := mustInt(r.PathValue("id"))
	itemID := mustInt(r.PathValue("item_id"))

	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	// 讀取項目（用於校驗存在性與回補預扣量）
	var itemType string
	var invID int64
	var qty int
	err = tx.QueryRow("SELECT item_type, COALESCE(inventory_item_id,0), quantity FROM work_order_items WHERE id = ? AND work_order_id = ?", itemID, woID).
		Scan(&itemType, &invID, &qty)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 已結帳（paid）的工單不允許再改動項目，否則庫存與帳務會對不上。
	var status string
	if err := tx.QueryRow("SELECT status FROM active_work_orders WHERE id = ?", woID).Scan(&status); err == nil && WorkOrderStatus(status) == WorkOrderStatusPaid {
		fail(w, "工單已結帳，無法刪除項目")
		return
	}

	if _, err := tx.Exec("DELETE FROM work_order_items WHERE id = ? AND work_order_id = ?", itemID, woID); err != nil {
		fail(w, "刪除項目失敗")
		return
	}

	// 回補預扣量（刪除零件項目時）
	if itemType == "part" && invID > 0 {
		tx.Exec(`UPDATE inventory_items
			SET reserved_quantity = MAX(COALESCE(reserved_quantity,0) - ?, 0)
			WHERE id = ?`, qty, invID)
	}

	if err := service.RecalculateWorkOrderTotal(r.Context(), tx, woID); err != nil {
		fail(w, "重算金額失敗")
		return
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}

	if renderWorkOrderItemsFragment(w, r, woID, user) {
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10), "已刪除項目")
}

func renderWorkOrderItemsFragment(w http.ResponseWriter, r *http.Request, woID int64, user model.User) bool {
	if r.Header.Get("HX-Request") != "true" {
		return false
	}
	items, itemTotal := loadWorkOrderItems(woID)
	techNames := map[int64]string{}
	for _, it := range items {
		if it.PrimaryTechnicianID > 0 {
			techNames[it.PrimaryTechnicianID] = techName(it.PrimaryTechnicianID)
		}
		if it.AssistantTechID > 0 {
			techNames[it.AssistantTechID] = techName(it.AssistantTechID)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.WorkOrderItemsTable(woID, items, techNames, true, itemTotal, user).Render(r.Context(), w); err != nil {
		http.Error(w, "渲染工單項目失敗", http.StatusInternalServerError)
	}
	return true
}

func loadWorkOrderItems(woID int64) ([]model.WorkOrderItem, float64) {
	rows, err := db.DB.Query(`SELECT i.id, i.work_order_id, i.item_type, COALESCE(i.inventory_item_id,0),
		CASE
			WHEN i.item_type = 'part' AND COALESCE(ii.name,'') <> '' AND COALESCE(ii.part_number,'') <> ''
				THEN ii.name || ' (' || ii.part_number || ')'
			WHEN i.item_type = 'part' AND COALESCE(ii.name,'') <> '' THEN ii.name
			ELSE i.description
		END,
		COALESCE(i.quantity,0), COALESCE(i.unit_price,0), COALESCE(i.subtotal,0),
		COALESCE(i.primary_technician_id,0), COALESCE(i.assistant_technician_id,0), COALESCE(i.photo_url,'')
		FROM work_order_items i
		LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
		WHERE i.work_order_id = ? ORDER BY i.id`, woID)
	if err != nil {
		return nil, 0
	}
	defer rows.Close()

	var list []model.WorkOrderItem
	total := 0.0
	for rows.Next() {
		var it model.WorkOrderItem
		if err := rows.Scan(&it.ID, &it.WorkOrderID, &it.ItemType, &it.InventoryItemID,
			&it.Description, &it.Quantity, &it.UnitPrice, &it.Subtotal,
			&it.PrimaryTechnicianID, &it.AssistantTechID, &it.PhotoURL); err != nil {
			continue
		}
		total += it.Subtotal
		list = append(list, it)
	}
	return list, total
}

func technicianOptions() string {
	rows, err := db.DB.Query(`SELECT u.id, u.name, u.username, COALESCE(u.staff_branch,'')
		FROM users u
		WHERE u.role = 'technician'
		  AND COALESCE(u.staff_kind,'employee') = 'employee'
		  AND UPPER(COALESCE(u.staff_branch,'')) NOT IN ('E','G','H')
		  AND COALESCE(u.active,1) = 1
		ORDER BY u.staff_branch, u.name`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name, username, branch string
		if err := rows.Scan(&id, &name, &username, &branch); err != nil {
			continue
		}
		label := name + ` (` + username + `)`
		if dept := branchLabel(branch); dept != "" {
			label += `　` + dept
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(label) + `</option>`)
	}
	return b.String()
}

// branchLabel CAR35 部門代碼 → 中文（查 lookup_items 表，category='branch'）。
// 找不到時回傳空字串（由呼叫端決定降級顯示）。
