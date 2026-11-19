package handler

import (
	"log"
	"net/http"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// recordAudit 寫入一筆操作稽核（best-effort）。
// 用於沒有現成交易的操作點（軟刪、價格異動）；交易內的稽核（收款作廢、
// 庫存回沖）直接在 tx 內 INSERT 並讓錯誤中止交易，見 service/checkout.go。
func recordAudit(actorID int64, action, entityType string, entityID int64, detail string) {
	if detail == "" {
		detail = "{}"
	}
	if _, err := db.DB.Exec(`INSERT INTO audit_log
		(actor_id, action, entity_type, entity_id, detail_json) VALUES (?,?,?,?,?)`,
		actorID, action, entityType, entityID, detail); err != nil {
		log.Printf("audit_log 寫入失敗（%s %s #%d）: %v", action, entityType, entityID, err)
	}
}

// ListAuditLog 顯示 owner 限定的操作稽核軌跡（最近 200 筆）。
// 管理頁資料量小，依 scheduled-tasks 頁前例不做即時搜尋。
func ListAuditLog(w http.ResponseWriter, r *http.Request, user model.User) {
	rows, err := db.DB.Query(`SELECT a.created_at, COALESCE(u.name,u.username,'system'),
		a.action, a.entity_type, a.entity_id, a.detail_json
		FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
		ORDER BY a.created_at DESC, a.id DESC LIMIT 200`)
	if err != nil {
		http.Error(w, "讀取稽核紀錄失敗", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var list []pages.AuditLogRow
	for rows.Next() {
		var a pages.AuditLogRow
		if err := rows.Scan(&a.CreatedAt, &a.Actor, &a.Action, &a.EntityType, &a.EntityID, &a.Detail); err != nil {
			http.Error(w, "讀取稽核紀錄失敗", http.StatusInternalServerError)
			return
		}
		list = append(list, a)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "讀取稽核紀錄失敗", http.StatusInternalServerError)
		return
	}
	pages.AuditLogPage(list, user).Render(r.Context(), w)
}
