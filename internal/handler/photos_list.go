package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// photos_list.go — photo list / serve / upload feedback views.

// ListWorkOrderPhotos 顯示工單照片並提供上傳表單。
func ListWorkOrderPhotos(w http.ResponseWriter, r *http.Request, user model.User) {
	woID := mustInt(r.PathValue("id"))
	allowed, err := canViewWorkOrder(user, woID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			log.Printf("檢查工單照片頁權限失敗（工單 #%d）: %v", woID, err)
			fail(w, "無法確認工單權限")
		}
		return
	}
	if !allowed {
		http.Error(w, "沒有查看此工單照片的權限", http.StatusForbidden)
		return
	}

	rows, err := db.DB.Query(`SELECT p.id, p.work_order_id, COALESCE(p.item_id,0),
		p.filename, COALESCE(p.original_name,''), COALESCE(p.mime_type,''),
		COALESCE(p.file_size,0), COALESCE(p.photo_type,'damage'), COALESCE(p.note,''),
		COALESCE(p.created_at,''), COALESCE(u.name,'-')
		FROM active_work_order_photos p
		LEFT JOIN users u ON u.id = p.uploaded_by
		WHERE p.work_order_id = ? ORDER BY p.id DESC`, woID)
	if err != nil {
		fail(w, "查詢照片失敗")
		return
	}
	defer rows.Close()

	var photoCards []pages.PhotoCard
	for rows.Next() {
		var id, woIDv, itemID int64
		var filename, origName, mime, photoType, note, createdAt, uploader string
		var size int64
		if err := rows.Scan(&id, &woIDv, &itemID, &filename, &origName, &mime,
			&size, &photoType, &note, &createdAt, &uploader); err != nil {
			log.Printf("讀取工單照片失敗: %v", err)
			fail(w, "讀取照片失敗")
			return
		}
		typeLabel := map[string]string{"damage": "車損", "repair": "維修", "part": "零件", "other": "其他"}[photoType]
		photoCards = append(photoCards, pages.PhotoCard{
			WorkOrderID: woIDv, ID: id, TypeLabel: typeLabel,
			OriginalName: orDash(origName), Uploader: uploader, CreatedAt: createdAt,
			Size: sizeKB(size), Note: note,
			Marker: pages.PhotoMarkerOverlay("/work-orders/"+strconv.FormatInt(woIDv, 10)+"/photos/"+strconv.FormatInt(id, 10), id),
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("列舉工單照片失敗: %v", err)
		fail(w, "讀取照片失敗")
		return
	}
	pages.WorkOrderPhotosPage(woID, photoCards, photoDir, user, r.URL.Query().Get("msg")).Render(r.Context(), w)
}

func photoUploadSuccess(w http.ResponseWriter, r *http.Request, woID int64, message string) {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "done", "work_order_id": woID, "message": message,
		}); err != nil {
			log.Printf("回傳照片上傳結果失敗: %v", err)
		}
		return
	}
	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10)+"/photos", message)
}

type photoRetryResult struct {
	Processed int `json:"processed"`
	Failed    int `json:"failed"`
}

func retryPhotoUploads(w http.ResponseWriter, r *http.Request, woID int64) {
	photoUploadMu.Lock()
	defer photoUploadMu.Unlock()

	// 程序重啟可能留下永久占用的記錄，只回收已逾時的上傳狀態。
	if _, err := db.DB.Exec(`UPDATE photo_upload_queue SET status='pending',
		updated_at=datetime('now','localtime')
		WHERE work_order_id=? AND status='uploading'
			AND updated_at < datetime('now','localtime','-10 minutes')`, woID); err != nil {
		http.Error(w, "重設逾時照片佇列失敗", http.StatusInternalServerError)
		return
	}
	rows, err := db.DB.Query(`SELECT id FROM photo_upload_queue
		WHERE work_order_id = ? AND status IN ('pending','failed') ORDER BY id`, woID)
	if err != nil {
		http.Error(w, "讀取照片重試佇列失敗", http.StatusInternalServerError)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			http.Error(w, "讀取照片重試佇列失敗", http.StatusInternalServerError)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		http.Error(w, "讀取照片重試佇列失敗", http.StatusInternalServerError)
		return
	}
	rows.Close()

	result := photoRetryResult{}
	for _, id := range ids {
		if err := processPhotoUpload(id); err != nil {
			result.Failed++
			log.Printf("照片佇列重試失敗 #%d: %v", id, err)
		} else {
			result.Processed++
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if result.Failed > 0 {
		w.WriteHeader(http.StatusMultiStatus)
	}
	_ = json.NewEncoder(w).Encode(result)
}

// ServePhoto 依工單與 DB 照片紀錄提供照片檔案，避免以可預測檔名跨工單讀取。
func ServePhoto(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	photoID := mustInt(r.PathValue("n"))
	user, err := currentUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	allowed, err := canViewWorkOrder(user, woID)
	if err != nil {
		log.Printf("檢查照片工單權限失敗（工單 #%d）: %v", woID, err)
		http.Error(w, "無法確認工單權限", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "沒有查看此工單照片的權限", http.StatusForbidden)
		return
	}

	var name string
	err = db.DB.QueryRow(`SELECT filename
		FROM active_work_order_photos WHERE id = ? AND work_order_id = ?`,
		photoID, woID).Scan(&name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	fullPath := filepath.Join(photoDir, name)
	if _, err := os.Stat(fullPath); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, fullPath)
}

func noteLine(note string) string {
	if strings.TrimSpace(note) == "" {
		return ""
	}
	return `<div class="pnote">` + htmlEsc(note) + `</div>`
}

func sizeKB(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	return strconv.FormatInt(n/1024, 10) + " KB"
}
