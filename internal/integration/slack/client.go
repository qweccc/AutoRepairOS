package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client Slack Web API 客戶端（出站通知）。
type Client struct {
	cfg *Config
	hc  *http.Client
}

// NewClient 建立客戶端。
func NewClient(cfg *Config) *Client {
	return &Client{
		cfg: cfg,
		hc:  &http.Client{Timeout: 30 * time.Second},
	}
}

// SendMessage 送出純文字訊息到指定頻道／DM。
//
// channelID 為空時用設定中的預設 ChannelID（內部通知的常見情境：
// 全部告警都送到老闆指定的頻道）。
//
// 注意：Slack 的錯誤是 HTTP 200 + {"ok":false,"error":"..."}，
// 必須檢查 ok 欄位，不能只看 HTTP status。
func (c *Client) SendMessage(channelID, text string) error {
	if c.cfg == nil || !c.cfg.Valid() {
		return mockLog(channelID, text)
	}
	if channelID == "" {
		channelID = c.cfg.ChannelID
	}
	if channelID == "" {
		return fmt.Errorf("未指定 channel id，且未設定 SLACK_CHANNEL_ID")
	}

	payload := map[string]interface{}{
		"channel": channelID,
		"text":    text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/api/chat.postMessage"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+c.cfg.BotToken)

	resp, err := c.hc.Do(req)
	if err != nil {
		// err 可能含 URL，但 token 在 header 不在 URL，不會洩漏
		return fmt.Errorf("slack 連線失敗: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[slack] chat.postMessage 失敗 status=%d body=%s\n",
			resp.StatusCode, truncate(string(respBody), 200))
		return fmt.Errorf("slack 回應 %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && !result.OK {
		fmt.Printf("[slack] chat.postMessage 拒絕 status=%d error=%s body=%s\n",
			resp.StatusCode, result.Error, truncate(string(respBody), 200))
		return fmt.Errorf("slack 拒絕: %s", result.Error)
	}
	return nil
}

// mockLog Mock 模式：只寫 log，不實際外送。
func mockLog(channelID, text string) error {
	if channelID == "" {
		channelID = "(預設)"
	}
	fmt.Printf("[slack][MOCK] to=%s len=%d text=%s\n", channelID, len(text), truncate(text, 120))
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
