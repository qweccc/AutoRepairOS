package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/vision"
)

// visionClient 由 main 在啟動時依 config 設定。nil 表示未啟用視覺辨識。
var visionClient *vision.Client

// SetVisionClient 由 main 在啟動時呼叫一次。
func SetVisionClient(c *vision.Client) {
	visionClient = c
}

// StopVisionClient 由 main defer 呼叫：關閉自動管理的 llama-server。
func StopVisionClient() {
	if visionClient != nil {
		visionClient.Stop()
	}
}

// RecognizePlate 拍照辨識行照/車牌，回傳結構化 JSON 給前端自動填入表單。
//
// POST body: {"image": "data:image/jpeg;base64,...."} 或 {"image": "<純base64>"}
// 回傳: {"ok":true, "reading":{...}} 或 {"error":"..."}
//
// 前端用 <input type=file accept=image/* capture=environment> + FileReader
// 讀成 dataURL 再 POST 到這裡，拿到欄位後直接填進車輛表單。
func RecognizePlate(w http.ResponseWriter, r *http.Request) {
	if visionClient == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]interface{}{"ok": false, "error": "視覺辨識未啟用（請在 .env 設定 VISION_API_URL/VISION_API_KEY 或 VISION_LOCAL_URL）"})
		return
	}

	var body struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "請求格式錯誤"})
		return
	}
	body.Image = strings.TrimSpace(body.Image)
	if body.Image == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "缺少圖片"})
		return
	}

	b64 := vision.DecodeDataURL(body.Image)
	reading, err := visionClient.Recognize(r.Context(), b64)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "reading": reading})
}

// RecognizeInvoice 拍照辨識供應商進貨單據，回傳每一行品項給前端帶入。
//
// POST body: {"image": "data:image/jpeg;base64,...."}
// 回傳: {"ok":true, "items":[{description,quantity,unit_price,part_number}, ...]}
//       （items 可能為空陣列 = 圖裡讀不到品項，這不是錯誤）
func RecognizeInvoice(w http.ResponseWriter, r *http.Request) {
	if visionClient == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]interface{}{"ok": false, "error": "視覺辨識未啟用（請在 .env 設定 VISION_API_URL/VISION_API_KEY 或 VISION_LOCAL_URL）"})
		return
	}

	var body struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "請求格式錯誤"})
		return
	}
	body.Image = strings.TrimSpace(body.Image)
	if body.Image == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "缺少圖片"})
		return
	}

	b64 := vision.DecodeDataURL(body.Image)
	doc, err := visionClient.RecognizeInvoice(r.Context(), b64)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if doc.Items == nil {
		doc.Items = []vision.InvoiceLine{} // 空陣列而不是 null，前端好判斷
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":            true,
		"supplier_name": doc.SupplierName,
		"order_date":    doc.OrderDate,
		"items":         doc.Items,
	})
}

// SupplierOptionsAPI 供應商 id/name 清單（JSON），給前端把辨識出的供應商名
// 比對成 select 的 value 用。「先拍單據再開單」流程需要它：
// 辨識回傳的是名稱字串，要轉成下拉選單的 id 才能帶入表單。
func SupplierOptionsAPI(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query("SELECT id, name FROM suppliers ORDER BY name LIMIT 300")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "查詢供應商失敗"})
		return
	}
	defer rows.Close()
	type sup struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	var list []sup
	for rows.Next() {
		var s sup
		if err := rows.Scan(&s.ID, &s.Name); err != nil {
			continue
		}
		list = append(list, s)
	}
	if list == nil {
		list = []sup{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "suppliers": list})
}

// VisionStatus 回傳視覺辨識是否啟用與模式（前端用來決定要不要顯示拍照鈕）。
func VisionStatus(w http.ResponseWriter, r *http.Request) {
	enabled := visionClient != nil
	mode := ""
	if visionClient != nil {
		mode = visionClient.Mode
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"enabled": enabled,
		"mode":    mode,
	})
}
