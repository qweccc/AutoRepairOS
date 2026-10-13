// Package vision 提供行照/車牌的視覺辨識（OCR）能力。
//
// 分層混合策略（對應 docs/DEVELOPMENT.md）：
//   - cloud 模式：呼叫外部 AI Vision API（預設 Gemini，也可接任何 OpenAI 相容
//     的 vision endpoint）。準確率最高、開發最快，但需連網。
//   - local 模式：呼叫區網本地 API 端點（車廠自架 Ollama+LLaVA、PaddleOCR 等）。
//     完全離線，API 形狀由本地服務決定，這裡只做「送 base64 圖、收 JSON」的轉發。
//
// 兩種模式都回傳相同的結構化結果 PlateReading，讓上層 handler 不用分支。
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Reading 行照/車牌辨識的結構化結果。
// 欄位對應台灣行車執照；辨識不到的空著，由上層決定要不要覆蓋既有值。
type Reading struct {
	PlateNumber    string `json:"plate_number"`
	BrandModel     string `json:"brand_model"`
	VIN            string `json:"vin"`
	EngineNumber   string `json:"engine_number"`
	OwnerName      string `json:"owner_name"`
	OwnerAddress   string `json:"owner_address"`
	OwnerIDNumber  string `json:"owner_id_number"`
	FuelType       string `json:"fuel_type"`
	Displacement   string `json:"displacement"`
	ManufactureDate string `json:"manufacture_date"`
	LicenseIssueDate string `json:"license_issue_date"`
	SeatingCapacity  string `json:"seating_capacity"`
	RawText        string `json:"raw_text,omitempty"` // 原始回傳，除錯用
}

// Client 依模式分派到 cloud 或 local。
type Client struct {
	Mode      string // "cloud" | "local"
	APIURL    string
	APIKey    string
	Model     string
	LocalURL  string
	HTTP      *http.Client
}

// NewClient 由 config 建立。若未設定（兩者皆空）回傳 nil，上層据此停用功能。
func NewClient(mode, apiURL, apiKey, model, localURL string) *Client {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "cloud"
	}
	// 未設定就沒有 client：cloud 缺 key，或 local 缺 url。
	if mode == "local" {
		if localURL == "" {
			return nil
		}
	} else {
		if apiURL == "" || apiKey == "" {
			return nil
		}
	}
	return &Client{
		Mode:     mode,
		APIURL:   apiURL,
		APIKey:   apiKey,
		Model:    model,
		LocalURL: localURL,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Recognize 辨識一張行照/車牌圖（JPEG/PNG base64，不含 data: 前綴）。
// 回傳結構化欄位；辨識失敗回傳 error（上層轉成 502 給前端）。
func (c *Client) Recognize(ctx context.Context, imageBase64 string) (*Reading, error) {
	if c.Mode == "local" {
		return c.recognizeLocal(ctx, imageBase64)
	}
	return c.recognizeCloud(ctx, imageBase64)
}

// cloudPrompt 要求模型只回 JSON，欄位對應 Reading。
const cloudPrompt = `You are reading a Taiwan vehicle registration document (行車執照) or license plate.
Extract these fields and return ONLY a JSON object (no markdown, no explanation):
{"plate_number":"","brand_model":"","vin":"","engine_number":"","owner_name":"",
"owner_address":"","owner_id_number":"","fuel_type":"","displacement":"",
"manufacture_date":"","license_issue_date":"","seating_capacity":""}
Use empty string for any field you cannot read. plate_number is the license plate like ABC-1234.`

// recognizeCloud 走 OpenAI 相容的 vision chat completions 端點。
// Gemini 的 OpenAI 相容端點、或任何 /v1/chat/completions 形狀的服務都適用。
func (c *Client) recognizeCloud(ctx context.Context, imageBase64 string) (*Reading, error) {
	model := c.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": cloudPrompt},
					{"type": "image_url", "image_url": map[string]string{
						"url": "data:image/jpeg;base64," + imageBase64,
					}},
				},
			},
		},
		"temperature": 0,
	}
	body, _ := json.Marshal(payload)

	url := strings.TrimRight(c.APIURL, "/")
	if !strings.Contains(url, "/chat/completions") {
		url += "/v1/chat/completions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vision api request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vision api status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	// 解析 OpenAI 形狀：choices[0].message.content 是模型回的文字（內含 JSON）。
	var chat struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &chat); err != nil {
		return nil, fmt.Errorf("vision api parse: %w", err)
	}
	if len(chat.Choices) == 0 {
		return nil, fmt.Errorf("vision api: no choices in response")
	}
	content := chat.Choices[0].Message.Content
	return parseReadingJSON(content), nil
}

// recognizeLocal 轉發到區網本地 API。
// 本地服務自訂 API 形狀，這裡約定：POST {image_base64} → 回傳 Reading 的 JSON。
// 車廠可把本地 Ollama/PaddleOCR 包一層成這個形狀，或改這裡的 payload 對接。
func (c *Client) recognizeLocal(ctx context.Context, imageBase64 string) (*Reading, error) {
	payload, _ := json.Marshal(map[string]string{"image_base64": imageBase64})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.LocalURL, "/")+"/recognize", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("local vision request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local vision status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var r Reading
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("local vision parse: %w", err)
	}
	r.RawText = string(raw)
	return &r, nil
}

// parseReadingJSON 從模型回傳文字中抽出 JSON 並解析成 Reading。
// 模型有時會包 ```json ... ``` 或加前言，這裡寬容地取第一個 { 到最後一個 }。
func parseReadingJSON(content string) *Reading {
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end < 0 || end <= start {
		return &Reading{RawText: content}
	}
	var r Reading
	if err := json.Unmarshal([]byte(content[start:end+1]), &r); err != nil {
		return &Reading{RawText: content}
	}
	r.RawText = content
	return &r
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// DecodeDataURL 把前端的 data:image/...;base64,XXXX 轉成純 base64。
// 前端 <input type=file> 用 FileReader.readAsDataURL 得到的就是這種格式。
func DecodeDataURL(dataURL string) string {
	if i := strings.Index(dataURL, ","); i >= 0 {
		return dataURL[i+1:]
	}
	return dataURL
}

// EncodeReader 把任意圖片 reader 編成 base64（給測試或非 dataURL 來源用）。
func EncodeReader(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 10<<20)) // 10MB 上限
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
