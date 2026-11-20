package discord

// REST API v10 出站訊息：
//   - SendMessage：送到指定頻道（內部通知的常見情境：全部告警都送到固定 ChannelID）
//   - SendDirectMessage：先建 DM channel 再送訊息（客戶自助回覆走使用者私訊）
//
// 與 telegram 相同：缺 token 一律 Mock（只 log、不連網路），
// log 只記 channel/user id 與長度，绝不記錄 token。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxContentLen 是 Discord 單則訊息 content 的 rune 上限。
const maxContentLen = 2000

// Client Discord Bot API 客戶端。
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

// SendMessage 送出純文字訊息到指定頻道。
//
// channelID 為空時用設定中的預設 ChannelID（內部通知的常見情境：
// 全部告警都送到同一個頻道）。超過 2000 字的內容會截斷。
func (c *Client) SendMessage(channelID, text string) error {
	if c.cfg == nil || !c.cfg.Valid() {
		return mockLog(channelID, text)
	}
	if channelID == "" {
		channelID = c.cfg.ChannelID
	}
	if channelID == "" {
		return fmt.Errorf("未指定 channel id，且未設定 DISCORD_CHANNEL_ID")
	}

	text = clampContent(text)
	fmt.Printf("[discord] 嘗試發送訊息 channel=%s len=%d\n", channelID, len([]rune(text)))

	status, respBody, err := c.apiPost("/api/v10/channels/"+channelID+"/messages",
		map[string]string{"content": text})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		fmt.Printf("[discord] 發送失敗 channel=%s status=%d body=%s\n",
			channelID, status, truncate(string(respBody), 200))
		return fmt.Errorf("discord 回應 %d: %s", status, truncate(string(respBody), 200))
	}
	fmt.Printf("[discord] 發送成功 channel=%s status=%d\n", channelID, status)
	return nil
}

// SendDirectMessage 送私訊給指定使用者。
//
// Discord 的 DM 必須先建 channel：POST /users/@me/channels 帶 recipient_id，
// 取得 DM channel id 後再走與 SendMessage 完全相同的送訊邏輯。
func (c *Client) SendDirectMessage(userID, text string) error {
	if c.cfg == nil || !c.cfg.Valid() {
		return mockLog("DM "+userID, text)
	}
	if userID == "" {
		return fmt.Errorf("未指定 Discord user id")
	}
	channelID, err := c.createDMChannel(userID)
	if err != nil {
		return err
	}
	return c.SendMessage(channelID, text)
}

// createDMChannel 建立（或取回既有）與使用者的 DM channel。
func (c *Client) createDMChannel(userID string) (string, error) {
	fmt.Printf("[discord] 嘗試建立 DM channel user=%s\n", userID)
	status, respBody, err := c.apiPost("/api/v10/users/@me/channels",
		map[string]string{"recipient_id": userID})
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		fmt.Printf("[discord] 建立 DM channel 失敗 user=%s status=%d body=%s\n",
			userID, status, truncate(string(respBody), 200))
		return "", fmt.Errorf("discord 建立 DM channel 回應 %d: %s",
			status, truncate(string(respBody), 200))
	}
	var ch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &ch); err != nil {
		return "", fmt.Errorf("discord DM channel 回應解析失敗: %w", err)
	}
	if ch.ID == "" {
		return "", fmt.Errorf("discord DM channel 回應缺 id: %s", truncate(string(respBody), 200))
	}
	return ch.ID, nil
}

// apiPost 送授權 JSON POST 到 Discord API，回傳狀態碼與回應 body。
// Authorization 用 "Bot <token>"；token 只進 header，不進 log。
func (c *Client) apiPost(path string, payload interface{}) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.cfg.BotToken)

	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("discord 連線失敗: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, respBody, nil
}

// clampContent 把內容截到 Discord 的 2000 字上限（含結尾 "..."）。
func clampContent(s string) string {
	r := []rune(s)
	if len(r) <= maxContentLen {
		return s
	}
	return string(r[:maxContentLen-3]) + "..."
}

// mockLog Mock 模式：只寫 log，不實際外送。
func mockLog(to, text string) error {
	if to == "" {
		to = "(預設)"
	}
	fmt.Printf("[discord][MOCK] to=%s len=%d text=%s\n", to, len(text), truncate(text, 120))
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
