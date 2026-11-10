package handler

import (
	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func ShowWorkOrder(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	allowed, err := canViewWorkOrder(user, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			log.Printf("檢查工單權限失敗（工單 #%d）: %v", id, err)
			fail(w, "無法確認工單權限")
		}
		return
	}
	if !allowed {
		http.Error(w, "沒有查看此工單的權限", http.StatusForbidden)
		return
	}

	row := db.DB.QueryRow(`SELECT `+workOrderCols+workOrderFrom+` WHERE wo.id = ?`, id)
	wo, err := scanWorkOrder(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	items, itemTotal := loadWorkOrderItems(id)

	// 已結帳工單唯讀：隱藏所有編輯與刪除控件（後端也擋，見 requireOrderMutable）。
	locked := WorkOrderStatus(wo.Status) == WorkOrderStatusPaid
	canEdit := !locked

	// 追加維修授權僅限櫃檯以上（POST /work-orders/{id}/approval-request 是
	// CounterUp），且已結帳的工單不能再追加。兩者都符合才顯示表單。
	canCreateApproval := canEdit && (user.Role == "owner" || user.Role == "counter")

	// 技師名稱對照表（供項目表格顯示）。
	techNames := map[int64]string{}
	for _, it := range items {
		if it.PrimaryTechnicianID > 0 {
			if _, ok := techNames[it.PrimaryTechnicianID]; !ok {
				techNames[it.PrimaryTechnicianID] = techName(it.PrimaryTechnicianID)
			}
		}
		if it.AssistantTechID > 0 {
			if _, ok := techNames[it.AssistantTechID]; !ok {
				techNames[it.AssistantTechID] = techName(it.AssistantTechID)
			}
		}
	}

	// T2.6：載入工單的 SOP 檢查紀錄與已掛模板。
	sopRecords := LoadSOPRecords(id)
	attachedTemplates := LoadAttachedTemplates(id)
	sopTemplateOpts := SOPTemplateOptions(attachedTemplates)

	pages.ShowWorkOrderPage(wo, items, itemTotal,
		workOrderStatusLabel, statusPill, approvalLabel, approvalPill,
		techNames, canEdit, flashText(r),
		sopRecords, sopTemplateOpts, attachedTemplates,
		canCreateApproval, pendingApprovalRows(id), user).Render(r.Context(), w)
}

// woTechFilterOptions 工單列表的技師篩選下拉。
//
// 與 technicianOptions() 分開，因為用途不同：
//
//	technicianOptions()     指派技師 —— 只列「現在會動手的人」（排除行政/眷屬/廠商）
//	woTechFilterOptions()   查歷史  —— 要能查到已離職或轉行政者做的舊單，
//	                                   否則 2016 年那張單會永遠找不到人。
//
// 兩者都排除 role='technician' 以外的帳號。

func woTechFilterOptions(selected string) string {
	rows, err := db.DB.Query(`SELECT u.id, u.name, COALESCE(u.username,''), COALESCE(u.staff_branch,'')
		FROM users u
		WHERE u.role = 'technician' AND COALESCE(u.staff_kind,'employee') = 'employee'
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
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `"` +
			selIf(strconv.FormatInt(id, 10) == selected) + `>` + htmlEsc(label) + `</option>`)
	}
	return b.String()
}

func techName(id int64) string {
	if id == 0 {
		return "-"
	}
	var name string
	if err := db.DB.QueryRow("SELECT name FROM users WHERE id = ?", id).Scan(&name); err != nil {
		return "-"
	}
	return name
}
