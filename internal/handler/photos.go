package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

const (
	maxPhotoSize = 8 << 20 // 8MB
	photoDir     = "data/photos"
)

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

	var b strings.Builder
	b.WriteString(`<div class="pgrid">`)
	n := 0
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
		var marker strings.Builder
		if err := pages.PhotoMarkerOverlay("/photos/"+filename, id).Render(r.Context(), &marker); err != nil {
			log.Printf("產生照片標記介面失敗: %v", err)
			fail(w, "產生照片標記介面失敗")
			return
		}
		typeLabel := map[string]string{"damage": "車損", "repair": "維修", "part": "零件", "other": "其他"}[photoType]
		b.WriteString(`<figure class="pcard" data-work-order-id="` + strconv.FormatInt(woIDv, 10) + `">
          ` + marker.String() + `
          <figcaption>
            <span class="pill p-progress">` + typeLabel + `</span>
            <div class="pname">` + htmlEsc(orDash(origName)) + `</div>
            <div class="pmeta">` + htmlEsc(uploader) + ` · ` + htmlEsc(createdAt) + ` · ` + sizeKB(size) + `</div>
            ` + noteLine(note) + `
            <form method="POST" action="/work-orders/` + strconv.FormatInt(woID, 10) +
			`/photos/` + strconv.FormatInt(id, 10) + `/delete" style="display:inline" onsubmit="return confirm('刪除這張照片？')"><button class="btn btn-d" type="submit">刪除</button></form>
          </figcaption>
        </figure>`)
		n++
	}
	if err := rows.Err(); err != nil {
		log.Printf("列舉工單照片失敗: %v", err)
		fail(w, "讀取照片失敗")
		return
	}
	b.WriteString(`</div>`)

	grid := b.String()
	if n == 0 {
		grid = `<div class="empty">尚未上傳任何照片</div>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>工單照片（` + strconv.Itoa(n) + ` 張）</h2>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">
        離線優先：照片存在本機 <code>` + htmlEsc(photoDir) + `</code>，不依賴外部網路。單檔上限 8MB。
      </p>` + grid + `
    </div>

    <div class="card">
      <h2>上傳照片</h2>
      <form class="frm" method="POST" action="/work-orders/` + strconv.FormatInt(woID, 10) + `/photos" enctype="multipart/form-data">
        <label class="full">選擇照片<input name="photo" type="file" accept="image/*" required></label>
        <label>照片類型<select name="photo_type">
          <option value="damage">車損</option>
          <option value="repair">維修</option>
          <option value="part">零件</option>
          <option value="other">其他</option>
        </select></label>
        <label>備註<input name="note"></label>
        <div class="full"><button class="btn btn-p" type="submit">上傳</button>
          <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(woID, 10) + `">返回工單</a></div>
      </form>
    </div>
    <script src="/static/photo-markup.js"></script>`

	renderPage(w, "工單照片", "/work-orders", user.Username, user.Role, body)
}

// UploadWorkOrderPhoto 接收 multipart 上傳並存檔。
func UploadWorkOrderPhoto(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))

	if countRows("SELECT COUNT(1) FROM active_work_orders WHERE id = ?", woID) == 0 {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseMultipartForm(maxPhotoSize); err != nil {
		fail(w, "上傳失敗：檔案過大或格式錯誤（上限 8MB）")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("photo")
	if err != nil {
		fail(w, "請選擇要上傳的檔案")
		return
	}
	defer file.Close()

	if header.Size > maxPhotoSize {
		fail(w, "檔案超過 8MB 上限")
		return
	}

	mime := header.Header.Get("Content-Type")
	ext, ok := allowedPhotoTypes[strings.ToLower(mime)]
	if !ok {
		ext = strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" && ext != ".gif" {
			fail(w, "只支援 JPG / PNG / WEBP / GIF 格式")
			return
		}
	}

	if err := os.MkdirAll(photoDir, 0755); err != nil {
		fail(w, "無法建立照片目錄")
		return
	}

	filename := fmt.Sprintf("wo%d_%d%s", woID, time.Now().UnixNano(), ext)
	fullPath := filepath.Join(photoDir, filename)

	dst, err := os.Create(fullPath)
	if err != nil {
		fail(w, "無法寫入照片檔")
		return
	}
	written, copyErr := io.Copy(dst, io.LimitReader(file, maxPhotoSize))
	dst.Close()
	if copyErr != nil {
		os.Remove(fullPath)
		fail(w, "寫入照片失敗")
		return
	}

	photoType := r.FormValue("photo_type")
	if _, ok := map[string]bool{"damage": true, "repair": true, "part": true, "other": true}[photoType]; !ok {
		photoType = "damage"
	}

	if _, err := db.DB.Exec(`INSERT INTO work_order_photos
		(work_order_id, filename, original_name, mime_type, file_size, photo_type, note)
		VALUES (?,?,?,?,?,?,?)`,
		woID, filename, header.Filename, mime, written, photoType,
		nullStr(r.FormValue("note"))); err != nil {
		os.Remove(fullPath)
		fail(w, "照片紀錄寫入失敗")
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10)+"/photos", "照片已上傳")
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

// ServePhoto 提供照片檔案給瀏覽器預覽。
func ServePhoto(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("filename"))
	if name == "." || name == string(filepath.Separator) || name == "" {
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
