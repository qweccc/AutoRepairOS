package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// AddPhotoAnnotation 在照片上新增標記（line/rect/arrow/text）。
func AddPhotoAnnotation(w http.ResponseWriter, r *http.Request, user model.User) {
	photoID := mustInt(r.PathValue("photo_id"))

	var req struct {
		Type        string  `json:"type"`
		X1          float64 `json:"x1"`
		Y1          float64 `json:"y1"`
		X2          float64 `json:"x2"`
		Y2          float64 `json:"y2"`
		Color       string  `json:"color"`
		Note        string  `json:"note"`
		ItemID      *int64  `json:"item_id"`
		WorkOrderID int64   `json:"work_order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		formError(w, r, "新增照片標記", "/work-orders", "標記類型為必填")
		return
	}

	var workOrderID int64
	err := db.DB.QueryRow(`SELECT work_order_id
		FROM active_work_order_photos WHERE id = ?`, photoID).Scan(&workOrderID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("查詢照片所屬工單失敗: %v", err)
		fail(w, "查詢照片失敗")
		return
	}
	if req.WorkOrderID > 0 && req.WorkOrderID != workOrderID {
		http.Error(w, "照片不屬於此工單", http.StatusBadRequest)
		return
	}
	if req.ItemID != nil {
		if *req.ItemID <= 0 {
			http.Error(w, "無效的工單明細", http.StatusBadRequest)
			return
		}
		var found int
		err = db.DB.QueryRow(`SELECT 1 FROM work_order_items
			WHERE id = ? AND work_order_id = ?`, *req.ItemID, workOrderID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "明細不屬於此工單", http.StatusBadRequest)
			return
		}
		if err != nil {
			log.Printf("驗證照片標記明細失敗: %v", err)
			fail(w, "驗證工單明細失敗")
			return
		}
	}

	color := req.Color
	if color == "" {
		color = "#ff0000"
	}

	_, err = db.DB.Exec(`INSERT INTO photo_annotations
		(photo_id, work_order_id, annotation_type, x1, y1, x2, y2, color, note, created_by)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		photoID, workOrderID, req.Type, req.X1, req.Y1, req.X2, req.Y2, color, req.Note, user.ID)
	if err != nil {
		log.Printf("新增照片標記失敗: %v", err)
		fail(w, "新增標記失敗")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ListPhotoAnnotations 查詢照片的所有標記。
func ListPhotoAnnotations(w http.ResponseWriter, r *http.Request, user model.User) {
	photoID := mustInt(r.PathValue("photo_id"))
	var found int
	err := db.DB.QueryRow("SELECT 1 FROM active_work_order_photos WHERE id = ?", photoID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("查詢標記照片存在性失敗: %v", err)
		fail(w, "查詢照片失敗")
		return
	}

	rows, err := db.DB.Query(`SELECT id, photo_id, work_order_id, annotation_type,
		x1, y1, x2, y2, color, note, COALESCE(created_by,0), COALESCE(created_at,'')
		FROM active_photo_annotations WHERE photo_id = ? ORDER BY id`, photoID)
	if err != nil {
		log.Printf("查詢照片標記失敗: %v", err)
		fail(w, "查詢標記失敗")
		return
	}
	defer rows.Close()

	annotations := make([]model.PhotoAnnotation, 0)
	for rows.Next() {
		var a model.PhotoAnnotation
		if err := rows.Scan(&a.ID, &a.PhotoID, &a.WorkOrderID, &a.AnnotationType,
			&a.X1, &a.Y1, &a.X2, &a.Y2, &a.Color, &a.Note, &a.CreatedBy, &a.CreatedAt); err != nil {
			log.Printf("讀取照片標記失敗: %v", err)
			fail(w, "讀取標記失敗")
			return
		}
		annotations = append(annotations, a)
	}
	if err := rows.Err(); err != nil {
		log.Printf("列舉照片標記失敗: %v", err)
		fail(w, "讀取標記失敗")
		return
	}
	if err := json.NewEncoder(w).Encode(annotations); err != nil {
		log.Printf("輸出照片標記失敗: %v", err)
	}
}

// DeletePhotoAnnotation 軟刪除標記。
func DeletePhotoAnnotation(w http.ResponseWriter, r *http.Request, user model.User) {
	annotationID := mustInt(r.PathValue("annotation_id"))

	res, err := db.DB.Exec(`UPDATE photo_annotations SET deleted_at = datetime('now')
		WHERE id = ? AND deleted_at IS NULL AND photo_id IN
		(SELECT id FROM active_work_order_photos)`, annotationID)
	if err != nil {
		log.Printf("刪除照片標記失敗: %v", err)
		fail(w, "刪除標記失敗")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("檢查照片標記刪除結果失敗: %v", err)
		fail(w, "刪除標記失敗")
		return
	}
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// AddWalkaroundMark 在車身示意圖上新增舊傷標記。
func AddWalkaroundMark(w http.ResponseWriter, r *http.Request, user model.User) {
	woID := mustInt(r.PathValue("id"))

	var req struct {
		MarkType string  `json:"mark_type"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
		Note     string  `json:"note"`
		PhotoID  int64   `json:"photo_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.MarkType == "" {
		formError(w, r, "新增舊傷標記", "/work-orders/"+strconv.FormatInt(woID, 10), "舊傷類型為必填")
		return
	}

	var vehicleID int64
	err := db.DB.QueryRow("SELECT vehicle_id FROM active_work_orders WHERE id = ?", woID).Scan(&vehicleID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("查詢舊傷預檢工單失敗: %v", err)
		fail(w, "查詢工單失敗")
		return
	}

	_, err = db.DB.Exec(`INSERT INTO walkaround_marks
		(work_order_id, vehicle_id, mark_type, x, y, note, photo_id, created_by)
		VALUES (?,?,?,?,?,?,?,?)`,
		woID, vehicleID, req.MarkType, req.X, req.Y, req.Note, req.PhotoID, user.ID)
	if err != nil {
		log.Printf("新增舊傷標記失敗: %v", err)
		fail(w, "新增舊傷標記失敗")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ListWalkaroundMarks 查詢工單的所有舊傷標記。
func ListWalkaroundMarks(w http.ResponseWriter, r *http.Request, user model.User) {
	woID := mustInt(r.PathValue("id"))
	var found int
	err := db.DB.QueryRow("SELECT 1 FROM active_work_orders WHERE id = ?", woID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("查詢舊傷預檢工單存在性失敗: %v", err)
		fail(w, "查詢工單失敗")
		return
	}

	rows, err := db.DB.Query(`SELECT id, work_order_id, vehicle_id, mark_type,
		x, y, COALESCE(note,''), COALESCE(photo_id,0), COALESCE(created_by,0), COALESCE(created_at,'')
		FROM active_walkaround_marks WHERE work_order_id = ? ORDER BY id`, woID)
	if err != nil {
		log.Printf("查詢舊傷標記失敗: %v", err)
		fail(w, "查詢舊傷標記失敗")
		return
	}
	defer rows.Close()

	marks := make([]model.WalkaroundMark, 0)
	for rows.Next() {
		var m model.WalkaroundMark
		if err := rows.Scan(&m.ID, &m.WorkOrderID, &m.VehicleID, &m.MarkType,
			&m.X, &m.Y, &m.Note, &m.PhotoID, &m.CreatedBy, &m.CreatedAt); err != nil {
			log.Printf("讀取舊傷標記失敗: %v", err)
			fail(w, "讀取舊傷標記失敗")
			return
		}
		marks = append(marks, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("列舉舊傷標記失敗: %v", err)
		fail(w, "讀取舊傷標記失敗")
		return
	}
	if err := json.NewEncoder(w).Encode(marks); err != nil {
		log.Printf("輸出舊傷標記失敗: %v", err)
	}
}

// DeleteWalkaroundMark 軟刪除舊傷標記。
func DeleteWalkaroundMark(w http.ResponseWriter, r *http.Request, user model.User) {
	markID := mustInt(r.PathValue("mark_id"))

	res, err := db.DB.Exec(`UPDATE walkaround_marks SET deleted_at = datetime('now')
		WHERE id = ? AND deleted_at IS NULL`, markID)
	if err != nil {
		log.Printf("刪除舊傷標記失敗: %v", err)
		fail(w, "刪除舊傷標記失敗")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("檢查舊傷標記刪除結果失敗: %v", err)
		fail(w, "刪除舊傷標記失敗")
		return
	}
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}
