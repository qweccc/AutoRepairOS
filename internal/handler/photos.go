package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

const maxPhotoSize = 8 << 20 // 8MB

var photoDir = "data/photos"

var photoUploadMu sync.Mutex

var allowedPhotoTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/jpg":  ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

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

// UploadWorkOrderPhoto 接收 multipart 上傳並先持久化至照片佇列。
func UploadWorkOrderPhoto(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))

	if countRows("SELECT COUNT(1) FROM active_work_orders WHERE id = ?", woID) == 0 {
		http.NotFound(w, r)
		return
	}

	if r.URL.Query().Get("retry") == "1" {
		retryPhotoUploads(w, r, woID)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoSize+(1<<20))
	if err := r.ParseMultipartForm(maxPhotoSize); err != nil {
		http.Error(w, "上傳失敗：檔案過大或格式錯誤（上限 8MB）", http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		http.Error(w, "請選擇要上傳的檔案", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if header.Size > maxPhotoSize {
		http.Error(w, "檔案超過 8MB 上限", http.StatusBadRequest)
		return
	}

	itemID := int64(0)
	if rawItemID := strings.TrimSpace(r.FormValue("item_id")); rawItemID != "" {
		itemID, err = strconv.ParseInt(rawItemID, 10, 64)
	}
	if err != nil || itemID < 0 {
		http.Error(w, "工單項目編號格式錯誤", http.StatusBadRequest)
		return
	}
	if itemID > 0 {
		var found int
		if err := db.DB.QueryRow(`SELECT COUNT(1) FROM work_order_items
			WHERE id = ? AND work_order_id = ?`, itemID, woID).Scan(&found); err != nil {
			http.Error(w, "驗證工單項目失敗", http.StatusInternalServerError)
			return
		}
		if found == 0 {
			http.Error(w, "工單項目不存在或不屬於此工單", http.StatusBadRequest)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(file, maxPhotoSize+1))
	if err != nil {
		http.Error(w, "讀取照片失敗", http.StatusBadRequest)
		return
	}
	if len(body) == 0 || len(body) > maxPhotoSize {
		http.Error(w, "檔案為空或超過 8MB 上限", http.StatusBadRequest)
		return
	}

	mime := http.DetectContentType(body)
	if _, ok := allowedPhotoTypes[mime]; !ok {
		http.Error(w, "只支援 JPG / PNG / WEBP / GIF 格式", http.StatusBadRequest)
		return
	}
	ext := allowedPhotoTypes[mime]
	originalName := filepath.Base(strings.TrimSpace(header.Filename))
	if originalName == "." || originalName == "" {
		originalName = "photo" + ext
	}
	photoType := r.FormValue("photo_type")
	if _, ok := map[string]bool{"damage": true, "repair": true, "part": true, "other": true}[photoType]; !ok {
		photoType = "damage"
	}
	note := r.FormValue("note")

	photoUploadMu.Lock()
	defer photoUploadMu.Unlock()

	var queueID int64
	var status string
	err = db.DB.QueryRow(`SELECT id, status FROM photo_upload_queue
		WHERE work_order_id = ? AND COALESCE(item_id, 0) = ? AND file_name = ?`,
		woID, itemID, originalName).Scan(&queueID, &status)
	if err == nil {
		if status == "done" {
			photoUploadSuccess(w, r, woID, "照片已存在，未重複建立")
			return
		}
		if processPhotoUpload(queueID) != nil {
			http.Error(w, "照片已暫存，稍後可重試上傳", http.StatusServiceUnavailable)
			return
		}
		photoUploadSuccess(w, r, woID, "照片已上傳")
		return
	}
	if err != sql.ErrNoRows {
		http.Error(w, "查詢照片佇列失敗", http.StatusInternalServerError)
		return
	}

	var duplicate int
	if err := db.DB.QueryRow(`SELECT COUNT(1) FROM active_work_order_photos
		WHERE work_order_id = ? AND COALESCE(item_id, 0) = ? AND original_name = ?`,
		woID, itemID, originalName).Scan(&duplicate); err != nil {
		http.Error(w, "檢查重複照片失敗", http.StatusInternalServerError)
		return
	}
	if duplicate > 0 {
		http.Error(w, "相同工單、項目及檔名的照片已存在", http.StatusConflict)
		return
	}

	pendingDir := filepath.Join(photoDir, "pending")
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		http.Error(w, "無法建立照片暫存目錄", http.StatusInternalServerError)
		return
	}
	tmp, err := os.CreateTemp(pendingDir, "upload-*.pending")
	if err != nil {
		http.Error(w, "無法暫存照片檔", http.StatusInternalServerError)
		return
	}
	storedName := filepath.Base(tmp.Name())
	if _, err = tmp.Write(body); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		http.Error(w, "暫存照片失敗", http.StatusInternalServerError)
		return
	}

	result, err := db.DB.Exec(`INSERT INTO photo_upload_queue
		(work_order_id, item_id, file_name, mime_type, file_size, stored_name, photo_type, note)
		VALUES (?, NULLIF(?, 0), ?, ?, ?, ?, ?, ?)`,
		woID, itemID, originalName, mime, len(body), storedName, photoType, note)
	if err != nil {
		_ = os.Remove(tmp.Name())
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			http.Error(w, "相同工單、項目及檔名的照片已在佇列中", http.StatusConflict)
			return
		}
		http.Error(w, "建立照片上傳佇列失敗", http.StatusInternalServerError)
		return
	}
	queueID, err = result.LastInsertId()
	if err != nil {
		http.Error(w, "讀取照片佇列編號失敗", http.StatusInternalServerError)
		return
	}
	if err := processPhotoUpload(queueID); err != nil {
		log.Printf("照片上傳已留在佇列 #%d: %v", queueID, err)
		http.Error(w, "照片已暫存，稍後可重試上傳", http.StatusServiceUnavailable)
		return
	}
	photoUploadSuccess(w, r, woID, "照片已上傳")
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

func processPhotoUpload(queueID int64) error {
	var woID int64
	var itemID sql.NullInt64
	var fileName, mime, storedName, photoType, note string
	var size int64
	if err := db.DB.QueryRow(`SELECT work_order_id, item_id, file_name, mime_type,
		file_size, stored_name, photo_type, COALESCE(note, '')
		FROM photo_upload_queue WHERE id = ?`, queueID).Scan(
		&woID, &itemID, &fileName, &mime, &size, &storedName, &photoType, &note); err != nil {
		return fmt.Errorf("讀取佇列記錄失敗: %w", err)
	}
	res, err := db.DB.Exec(`UPDATE photo_upload_queue SET status='uploading',
		updated_at=datetime('now','localtime')
		WHERE id=? AND status IN ('pending','failed')`, queueID)
	if err != nil {
		return fmt.Errorf("領取照片佇列失敗: %w", err)
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		var status string
		if err := db.DB.QueryRow("SELECT status FROM photo_upload_queue WHERE id=?", queueID).Scan(&status); err != nil {
			return err
		}
		if status == "done" {
			return nil
		}
		return fmt.Errorf("照片佇列目前狀態為 %s", status)
	}
	fail := func(cause error) error {
		_, updateErr := db.DB.Exec(`UPDATE photo_upload_queue SET status='failed',
			retry_count=retry_count+1, error_message=?, updated_at=datetime('now','localtime') WHERE id=?`,
			cause.Error(), queueID)
		if updateErr != nil {
			return fmt.Errorf("%v; 更新失敗狀態也失敗: %w", cause, updateErr)
		}
		return cause
	}

	pendingPath := filepath.Join(photoDir, "pending", filepath.Base(storedName))
	body, err := os.ReadFile(pendingPath)
	if err != nil {
		return fail(fmt.Errorf("讀取暫存照片失敗: %w", err))
	}
	if int64(len(body)) != size {
		return fail(fmt.Errorf("暫存照片大小不符，預期 %d bytes，實際 %d bytes", size, len(body)))
	}
	ext := allowedPhotoTypes[mime]
	if ext == "" {
		return fail(fmt.Errorf("不支援的照片 MIME 類型 %q", mime))
	}
	if err := os.MkdirAll(photoDir, 0755); err != nil {
		return fail(fmt.Errorf("建立照片目錄失敗: %w", err))
	}
	filename := fmt.Sprintf("wo%d_q%d%s", woID, queueID, ext)
	finalPath := filepath.Join(photoDir, filename)

	tx, err := db.DB.Begin()
	if err != nil {
		return fail(fmt.Errorf("開始照片交易失敗: %w", err))
	}
	var existingPhotoID int64
	err = tx.QueryRow(`SELECT id FROM work_order_photos
		WHERE work_order_id=? AND COALESCE(item_id,0)=? AND original_name=?
			AND deleted_at IS NULL`, woID, itemID.Int64, fileName).Scan(&existingPhotoID)
	if err == nil {
		_, err = tx.Exec(`UPDATE photo_upload_queue SET status='done', photo_id=?,
			error_message='', updated_at=datetime('now','localtime') WHERE id=?`, existingPhotoID, queueID)
		if err != nil {
			_ = tx.Rollback()
			return fail(fmt.Errorf("連結已存在照片失敗: %w", err))
		}
		if err := tx.Commit(); err != nil {
			return fail(fmt.Errorf("提交既有照片狀態失敗: %w", err))
		}
		if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
			log.Printf("刪除已完成照片暫存檔 %s 失敗: %v", pendingPath, err)
		}
		return nil
	}
	if err != sql.ErrNoRows {
		_ = tx.Rollback()
		return fail(fmt.Errorf("檢查既有照片失敗: %w", err))
	}
	if err := os.WriteFile(finalPath, body, 0644); err != nil {
		_ = tx.Rollback()
		return fail(fmt.Errorf("寫入照片檔失敗: %w", err))
	}
	result, err := tx.Exec(`INSERT INTO work_order_photos
		(work_order_id, item_id, filename, original_name, mime_type, file_size, photo_type, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		woID, itemID, filename, fileName, mime, size, photoType, nullStr(note))
	if err != nil {
		_ = tx.Rollback()
		return fail(fmt.Errorf("寫入照片紀錄失敗: %w", err))
	}
	photoID, err := result.LastInsertId()
	if err == nil {
		_, err = tx.Exec(`UPDATE photo_upload_queue SET status='done', photo_id=?,
			error_message='', updated_at=datetime('now','localtime') WHERE id=?`, photoID, queueID)
	}
	if err != nil {
		_ = tx.Rollback()
		return fail(fmt.Errorf("更新照片佇列完成狀態失敗: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fail(fmt.Errorf("提交照片交易失敗: %w", err))
	}
	if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
		log.Printf("刪除已完成照片暫存檔 %s 失敗: %v", pendingPath, err)
	}
	return nil
}

// DeleteWorkOrderPhoto 隱藏照片紀錄並保留原始檔供稽核追溯。
func DeleteWorkOrderPhoto(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	photoID := mustInt(r.PathValue("photo_id"))

	var found int
	err := db.DB.QueryRow("SELECT 1 FROM active_work_order_photos WHERE id = ? AND work_order_id = ?",
		photoID, woID).Scan(&found)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	res, err := db.DB.Exec(`UPDATE work_order_photos SET deleted_at = datetime('now')
		WHERE id = ? AND work_order_id = ? AND deleted_at IS NULL`, photoID, woID)
	if err != nil {
		fail(w, "刪除照片紀錄失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10)+"/photos", "照片已刪除")
}

func canViewWorkOrder(user model.User, workOrderID int64) (bool, error) {
	if user.Role != "owner" && user.Role != "counter" && user.Role != "technician" {
		return false, nil
	}
	var technicianID int64
	err := db.DB.QueryRow(`SELECT COALESCE(technician_id,0) FROM active_work_orders WHERE id = ?`, workOrderID).Scan(&technicianID)
	if err != nil {
		return false, err
	}
	return user.Role != "technician" || technicianID == user.ID, nil
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
