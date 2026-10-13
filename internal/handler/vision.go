package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"autorepairos/internal/integration/vision"
)

// visionClient 由 main 在啟動時依 config 設定。nil 表示未啟用視覺辨識。
var visionClient *vision.Client

// SetVisionClient 注入視覺辨識 client（main 啟動時呼叫一次）。
func SetVisionClient(c *vision.Client) {
	visionClient = c
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
