package handler

import (
	"html"
	"net/http"
	"strconv"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ListAuditLog shows the owner-only operational audit trail.
func ListAuditLog(w http.ResponseWriter, r *http.Request, _ model.User) {
	rows, err := db.DB.Query(`SELECT a.created_at, COALESCE(u.name,u.username,'system'),
		a.action, a.entity_type, a.entity_id, a.detail_json
		FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
		ORDER BY a.created_at DESC, a.id DESC LIMIT 200`)
	if err != nil {
		http.Error(w, "讀取稽核紀錄失敗", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<html lang="zh-Hant"><head><meta charset="utf-8"><title>稽核紀錄</title></head><body><h1>稽核紀錄</h1><table><thead><tr><th>時間</th><th>操作者</th><th>動作</th><th>實體</th><th>ID</th><th>明細</th></tr></thead><tbody>`))
	for rows.Next() {
		var created, actor, action, entity, detail string
		var id int64
		if err := rows.Scan(&created, &actor, &action, &entity, &id, &detail); err != nil {
			http.Error(w, "讀取稽核紀錄失敗", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("<tr><td>" + html.EscapeString(created) + "</td><td>" +
			html.EscapeString(actor) + "</td><td>" + html.EscapeString(action) +
			"</td><td>" + html.EscapeString(entity) + "</td><td>" +
			strconv.FormatInt(id, 10) + "</td><td><pre>" + html.EscapeString(detail) +
			"</pre></td></tr>"))
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "讀取稽核紀錄失敗", http.StatusInternalServerError)
		return
	}
	w.Write([]byte(`</tbody></table></body></html>`))
}
