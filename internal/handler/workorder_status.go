package handler

import (
"errors"
"log"
"net/http"
"strconv"
"strings"

"autorepairos/internal/cron"
"autorepairos/internal/db"
"autorepairos/internal/model"
"autorepairos/internal/service"
)

// allowedTransitions moved to service layer.


// shouldCheckTransition determines if a status transition needs validation.
func shouldCheckTransition(current, target string) bool {
	return current != "" && current != target && WorkOrderStatus(target) != WorkOrderStatusPaid
}

func UpdateWorkOrderStatus(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	status := r.FormValue("status")
	if _, ok := workOrderStatusLabel[status]; !ok {
		fail(w, "無效狀態")
		return
	}

	// P1-5: 工單狀態機 — 只允許合法轉換
	currentStatus := countStatus(id)
	if shouldCheckTransition(currentStatus, status) {
		if err := service.ValidateWorkOrderTransition(currentStatus, status); err != nil {
			fail(w, "不允許從「"+workOrderStatusLabel[currentStatus]+"」轉換為「"+workOrderStatusLabel[status]+"」")
			return
		}
	}

	// 結帳扣料的異動紀錄需要操作人
	actorID := int64(0)
	if u, uerr := currentUser(r); uerr == nil {
		actorID = u.ID
	}

	// 已結帳的工單是財務憑證，不允許再改狀態：否則扣料與營收會與實物對不上。
	if WorkOrderStatus(currentStatus) == WorkOrderStatusPaid {
		if WorkOrderStatus(status) != WorkOrderStatusPaid {
			fail(w, "此工單已結帳，不可變更為「"+workOrderStatusLabel[status]+"」。如需更正請另行開立沖銷單。")
			return
		}
	}

	// T2.6：完工限制——所有必填 SOP 項目必須已完成才能進入 completed 或 paid。
	// 後端強制檢查，不能只靠前端禁用按鈕。
	if sopErr := CheckSOPCompletionForStatus(id, status); sopErr != nil {
		fail(w, sopErr.Error())
		return
	}

	// 未決的追加維修授權也會擋完工／結帳：客戶還沒同意就把車交出去，
	// 等於先修了再想辦法收錢。櫃檯可以走「客戶電話同意」或「取消授權」解套，
	// 所以這道關卡不會讓工單永遠卡住。
	if apprErr := CheckApprovalForStatus(id, status); apprErr != nil {
		fail(w, apprErr.Error())
		return
	}

	if WorkOrderStatus(status) == WorkOrderStatusPaid {
		if err := service.New(db.DB).Checkout(r.Context(), id, actorID); err != nil {
			if errors.Is(err, service.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			fail(w, "結帳失敗："+err.Error())
			return
		}
		if err := RecordPassbookForWorkOrder(id); err != nil {
			log.Printf("工單 #%d 結帳成功，但建立 Passbook 失敗: %v", id, err)
		}
		redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "狀態已更新為「"+workOrderStatusLabel[status]+"」")
		return
	}

	previousStatus := countStatus(id)
	// 一般狀態轉換仍以條件更新避免覆寫已結帳工單。
	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	cas, err := tx.Exec(`UPDATE work_orders SET status=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status != 'paid'`, status, id)
	if err != nil {
		fail(w, "更新狀態失敗")
		return
	}
	if n, _ := cas.RowsAffected(); n == 0 {
		if WorkOrderStatus(countStatus(id)) == WorkOrderStatusPaid {
			redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10),
				"此工單已是「已結帳」，未重複計算")
			return
		}
		http.NotFound(w, r)
		return
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}

	if WorkOrderStatus(status) == WorkOrderStatusCompleted && previousStatus != string(WorkOrderStatusCompleted) {
		var customerID int64
		if err := db.DB.QueryRow("SELECT customer_id FROM active_work_orders WHERE id = ?", id).Scan(&customerID); err != nil {
			log.Printf("工單 #%d 已完工，但讀取通知客戶失敗: %v", id, err)
		} else if err := cron.QueueWorkOrderCompletedNotification(id, customerID); err != nil {
			log.Printf("工單 #%d 已完工，但排入完工通知失敗: %v", id, err)
		}
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "狀態已更新為「"+workOrderStatusLabel[status]+"」")
}

// countStatus 讀取工單目前狀態；查無回傳空字串。

func UpdateWorkOrderApproval(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	approval := r.FormValue("approval_status")
	if _, ok := approvalLabel[approval]; !ok {
		fail(w, "無效核准狀態")
		return
	}
	if err := requireOrderMutable(id); err != nil {
		fail(w, err.Error())
		return
	}

	res, err := db.DB.Exec("UPDATE work_orders SET approval_status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?", approval, id)
	if err != nil {
		fail(w, "更新核准狀態失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "核准狀態已更新為「"+approvalLabel[approval]+"」")
}


func ToggleWorkOrderSOP(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	if err := requireOrderMutable(id); err != nil {
		fail(w, err.Error())
		return
	}

	res, err := db.DB.Exec("UPDATE work_orders SET sop_completed = 1 - COALESCE(sop_completed,0), updated_at=CURRENT_TIMESTAMP WHERE id=?", id)
	if err != nil {
		fail(w, "更新 SOP 失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "SOP 狀態已切換")
}


func statusOptions(current string) string {
	var b strings.Builder
	for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid"} {
		b.WriteString(`<option value="` + s + `"` + selIf(current == s) + `>` + workOrderStatusLabel[s] + `</option>`)
	}
	return b.String()
}


func approvalOptions(current string) string {
	var b strings.Builder
	for _, s := range []string{"not_required", "pending", "approved", "rejected"} {
		b.WriteString(`<option value="` + s + `"` + selIf(current == s) + `>` + approvalLabel[s] + `</option>`)
	}
	return b.String()
}

// technicianOptions 技師下拉選單。
//
// 只列「真的會動手的人」，三層過濾缺一不可：
//
//	staff_kind='employee' —— 舊系統的 Staff 表混了協力廠商（上上汽車、元發電機）
//	                         與眷屬（車行家族），協力廠商請到「廠商」頁。
//	staff_branch NOT IN ('E','G','H') —— E 是行政、G 是眷屬、H 是廠商。
//	                         舊資料有 40 個眷屬編號（G001…G038）在遷移時被判成
//	                         employee，只靠 staff_kind 擋不掉，會讓行政與眷屬
//	                         出現在技師選單裡，污染技師產值與佣金統計。
//	active=1 —— 離職技師不該出現在指派選單（migration 014 加入）。

func statusControls(wo model.WorkOrder, canEdit bool) string {
	if !canEdit {
		return `<div style="padding:10px 14px;border-radius:8px;background:#052e16;
          color:#86efac;border:1px solid #166534;font-size:13.5px">
          此工單已結帳，內容唯讀。如需更正請另行開立沖銷單。
        </div>`
	}

	return `
        <form class="inline" method="POST" action="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/status">
          <label style="font-size:12px;color:#94a3b8">變更狀態</label>
          <select name="status" style="padding:6px 9px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;margin:0 8px">` +
		statusOptions(wo.Status) + `</select>
          <button class="btn btn-p" type="submit">更新狀態</button>
        </form>
        <form class="inline" method="POST" action="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/approval">
          <label style="font-size:12px;color:#94a3b8;margin-left:16px">核准</label>
          <select name="approval_status" style="padding:6px 9px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;margin:0 8px">` +
		approvalOptions(wo.ApprovalStatus) + `</select>
          <button class="btn btn-p" type="submit">更新核准</button>
        </form>
        <form class="inline" method="POST" action="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/sop">
          <button class="btn btn-s" type="submit">` + sopToggleLabel(wo.SOPCompleted) + `</button>
        </form>`
}


func sopToggleLabel(done bool) string {
	if done {
		return "取消 SOP 完成"
	}
	return "標記 SOP 完成"
}


