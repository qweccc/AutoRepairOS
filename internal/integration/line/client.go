package line

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client 封裝 LINE Messaging API 的連線與簽名。
type Client struct {
	cfg        *Config
	httpClient *http.Client
	baseURL    string
}

// APIError reports an HTTP rejection from the LINE API.
type APIError struct {
	StatusCode int
	Response   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("推播狀態 %d: %s", e.StatusCode, e.Response)
}

// NewClient 建立 Client。token 缺失時 cfg.Mock=true，仍回傳可用於 Mock 呼叫的 Client。
func NewClient(cfg *Config) *Client {
	if cfg == nil {
		// BaseURL 為 Messaging API 的根路徑。端點為 /v2/bot/...，
		// 不是 /version2/bot/...（那是另一套產品線，用錯會得到 404）。
		cfg = &Config{BaseURL: "https://api.line.me"}
	}
	return &Client{cfg: cfg, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

// --- 簽名驗證（Webhook）---

// VerifyEventSignature 驗證 LINE 送來的 webhook 事件簽名，回傳 (是否合法, 原始內容)。
//
// 實測發現 Cloudflare Tunnel 會改寫 X-Line-Signature：LINE 原始送出的是
// base64(base64url) 編碼的 HMAC，但經過 Cloudflare 後變成 "signature=" + hex，
// 長度從 54 變成 74。兩種格式都必須接受，否則每一筆合法請求都會被誤判為
// 不合法，而且 log 只會顯示「簽名不匹配」，難以察覺是編碼差異。
//
// 驗證順序：先算 HMAC，再分別以 hex 與 base64 兩種編碼比對，任一相符即通過。
// 比對用時間安全比較（hmac.Equal）以免洩漏資訊。
func (c *Client) VerifyEventSignature(rawBody []byte, signature string) (bool, []byte) {
	if c.cfg.ChannelID == "" || c.cfg.ChannelSecret == "" {
		return false, rawBody
	}

	sig := strings.TrimSpace(signature)
	sig = strings.TrimPrefix(sig, "signature=")
	if sig == "" {
		return false, rawBody
	}

	mac := hmac.New(sha256.New, []byte(c.cfg.ChannelSecret))
	mac.Write(rawBody)
	sum := mac.Sum(nil)

	// 格式一：hex（長度 64）
	if hmac.Equal([]byte(sig), []byte(hex.EncodeToString(sum))) {
		return true, rawBody
	}

	// 格式二：base64（長度 44，含 = 結尾）與 base64url（- _ 替換）
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding,
	} {
		if hmac.Equal([]byte(sig), []byte(enc.EncodeToString(sum))) {
			return true, rawBody
		}
	}

	return false, rawBody
}

// --- HTTP 工具 ---

func (c *Client) do(method, path string, body interface{}, headers map[string]string) (*http.Response, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("序列化請求失敗: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	u := c.cfg.BaseURL + path
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, nil, fmt.Errorf("建立請求失敗: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data, nil
}

// --- 推播（Push，主動發給單一使用者）---

// PushMessage 推播單則文本訊息給 LINE userID。無 token 時只 logging（Mock）。
func (c *Client) PushMessage(userID, text string) error {
	if !c.cfg.Valid() {
		return c.mock(func() error { return mockPushLog(userID, text) })
	}
	payload := map[string]interface{}{
		"to":         userID,
		"replyToken": "",
		"messages": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}
	resp, data, err := c.do(http.MethodPost, "/v2/bot/message/push", payload, map[string]string{"Authorization": c.authHeader()})
	if err != nil {
		return fmt.Errorf("推播失敗: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{StatusCode: resp.StatusCode, Response: string(data)}
	}
	return nil
}

// mockPushLog 記錄 Mock 推播。
func mockPushLog(userID, text string) error {
	log.Printf("[line][MOCK] 推播 to=%s text=%s", userID, text)
	return nil
}

// SendFlexMessage 推播 Flex Message（估價單用）：明細 + 金額 + 線上核准按鈕。
func (c *Client) SendFlexMessage(userID string, title, text string, items []ItemLine, total float64, approveURL string) error {
	if !c.cfg.Valid() {
		return c.mock(func() error { return mockFlexLog(userID, title, total, approveURL) })
	}
	contents := []map[string]interface{}{
		{
			"type": "box",
			"tag":  "fill",
			"spec": map[string]interface{}{
				"separator": true,
				"layout": map[string]interface{}{
					"type": "VBOX",
					"contents": []map[string]interface{}{
						{
							"type": "text",
							"tag":  "fill",
							"spec": map[string]interface{}{"text": title, "color": "#008527", "size": "md", "weight": "bold"},
						},
						{
							"type": "text",
							"tag":  "fill",
							"spec": map[string]interface{}{"text": text, "color": "#003838", "size": "sm"},
						},
					},
				},
			},
		},
		{
			"type": "separator",
			"tag":  "fill",
			"spec": map[string]interface{}{
				"layout": map[string]interface{}{
					"type":     "VBOX",
					"contents": flexRows(items),
				},
			},
		},
		{
			"type": "separator",
			"tag":  "fill",
			"spec": map[string]interface{}{
				"layout": map[string]interface{}{
					"type": "VBOX",
					"contents": []map[string]interface{}{
						{
							"type": "text",
							"tag":  "fill",
							"spec": map[string]interface{}{
								"text":   "總計：" + money(total),
								"color":  "#008527",
								"size":   "md",
								"weight": "bold",
							},
						},
					},
				},
			},
		},
		{
			"type": "button",
			"tag":  "fill",
			"spec": map[string]interface{}{
				"mode": "uri",
				"url":  approveURL,
				"text": "線上核准",
			},
		},
	}
	payload := map[string]interface{}{
		"to": userID,
		"messages": []map[string]interface{}{
			{"type": "flex", "altText": title, "contents": contents},
		},
	}
	resp, data, err := c.do(http.MethodPost, "/v2/bot/message/push", payload, map[string]string{"Authorization": c.authHeader()})
	if err != nil {
		return fmt.Errorf("推播 Flex 失敗: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("推播 Flex 狀態 %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

func (c *Client) authHeader() string {
	return "Bearer " + c.cfg.ChannelAccessToken
}

// --- 回訊（Reply，客戶回覆推播時用，不計費）---

// ReplyMessage 針對已推播訊息回傳回應（replyToken）。無 token 時只 logging。
func (c *Client) ReplyMessage(replyToken, text string) error {
	// 記錄嘗試與結果。replyToken 只能用一次且有效期限很短（約 30 秒內），
	// 失敗時若不記錄，外部完全看不出為什麼客戶沒收到回覆。
	log.Printf("[line] 回訊嘗試：token長度=%d 內容長度=%d", len(replyToken), len(text))

	if !c.cfg.Valid() {
		log.Printf("[line] MOCK 模式，未實際送出回訊")
		return c.mock(func() error { return mockReplyLog(replyToken, text) })
	}
	payload := map[string]interface{}{
		"replyToken": replyToken,
		"messages": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}
	resp, data, err := c.do(http.MethodPost, "/v2/bot/message/reply", payload, map[string]string{"Authorization": c.authHeader()})
	if err != nil {
		log.Printf("[line] 回訊請求失敗: %v", err)
		return fmt.Errorf("回訊失敗: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 常見原因：
		//   400 invalid reply token — token 已使用或過期。若官方帳號同時啟用
		//     「加入好友的歡迎訊息」，該功能會先消耗 token，這裡就會失敗。
		//   401 — token 無效。
		log.Printf("[line] 回訊被拒（狀態 %d）: %s", resp.StatusCode, truncate(string(data), 200))
		return fmt.Errorf("回訊狀態 %d: %s", resp.StatusCode, string(data))
	}
	log.Printf("[line] 回訊成功送達")
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// mock 在 Mock 模式下只 logging 並回空。
func (c *Client) mock(fn func() error) error {
	return fn()
}

// mockFlexLog 記錄 Mock Flex 推播。
func mockFlexLog(userID string, title string, total float64, approveURL string) error {
	log.Printf("[line][MOCK] Flex 推播 to=%s title=%s total=%.2f", userID, title, total)
	return nil
}

// mockReplyLog 記錄 Mock 回訊。
func mockReplyLog(replyToken string, text string) error {
	log.Printf("[line][MOCK] 回訊 replyToken=%s text=%s", replyToken, text)
	return nil
}

// --- 訊息類型 ---

// ItemLine 推播估價單的一行明細。
type ItemLine struct {
	Desc  string
	Total float64
}

// Money 格式化金額為 1,234.50 形式（供 Flex 顯示）。
func Money(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// --- 測試用：raw 請求回傳 ---

// DebugLastResp 回傳最近一次 raw 回應（供測試/排程）。
func (c *Client) DebugLastResp() (*http.Response, []byte) {
	return nil, nil
}

// URLParser 解析 BaseURL 的 scheme 與 host（供排程用）。
func (c *Client) URLParser() (scheme, host string, err error) {
	u, err := url.Parse(c.cfg.BaseURL)
	if err != nil {
		return "", "", err
	}
	return u.Scheme, u.Host, nil
}
