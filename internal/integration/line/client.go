package line

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

// Client 封裝 LINE Messaging API 的連線與簽名。
type Client struct {
	cfg *Config
	httpClient *http.Client
	baseURL string
}

// NewClient 建立 Client。token 缺失時 cfg.Mock=true，仍回傳可用於 Mock 呼叫的 Client。
func NewClient(cfg *Config) *Client {
	if cfg == nil {
		cfg = &Config{BaseURL: "https://api.line.me"}
	}
	return &Client{cfg: cfg, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

// --- 簽名驗證（Webhook）---

// VerifyEventSignature 驗證 LINE 送來的 webhook 事件簽名，回傳 (是否合法, 原始內容)。
func (c *Client) VerifyEventSignature(rawBody []byte, signature string) (bool, []byte) {
	if c.cfg.ChannelID == "" || c.cfg.ChannelSecret == "" {
		return false, rawBody
	}
	h := hmac.New(sha256.New, []byte(c.cfg.ChannelSecret))
	h.Write(rawBody)
	expected := "signature=" + hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected)), rawBody
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
		"to": userID,
		"replyToken": "",
		"messages": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}
	resp, data, err := c.do(http.MethodPost, "/version2/bot/message/push", payload, map[string]string{"Authorization": c.authHeader()})
	if err != nil {
		return fmt.Errorf("推播失敗: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("推播狀態 %d: %s", resp.StatusCode, string(data))
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
					"type": "VBOX",
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
								"text": "總計：" + money(total),
								"color": "#008527",
								"size": "md",
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
	resp, data, err := c.do(http.MethodPost, "/version2/bot/message/push", payload, map[string]string{"Authorization": c.authHeader()})
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
	if !c.cfg.Valid() {
		return c.mock(func() error { return mockReplyLog(replyToken, text) })
	}
	payload := map[string]interface{}{
		"replyToken": replyToken,
		"messages": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}
	resp, data, err := c.do(http.MethodPost, "/version2/bot/message/reply", payload, map[string]string{"Authorization": c.authHeader()})
	if err != nil {
		return fmt.Errorf("回訊失敗: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("回訊狀態 %d: %s", resp.StatusCode, string(data))
	}
	return nil
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
	Desc string
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
