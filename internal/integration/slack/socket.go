package slack

// Socket Mode 入站訊息：純出站 WebSocket 連線，不需公開 endpoint。
//
// 流程：
//  1. POST /api/apps.connections.open（Bearer xapp- app token）取得 wss URL
//  2. gorilla/websocket dial 該 URL，讀 Slack 推送的 envelope
//  3. events_api envelope 必須「先 ack 再處理」，否則 Slack 會重送
//  4. disconnect envelope 或連線中斷 → 外層迴圈重新 open＋dial（3 秒 backoff）
//
// 只處理 DM（channel_type=="im"）的一般使用者訊息：
// bot 訊息（bot_id 非空）、subtype 非空（編輯/刪除/message_replied 等）一律跳過，
// 避免機器人自我回覆迴圈。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// socketAPIBaseURL 與 socketBackoff 可覆寫，供測試指向 httptest 伺服器。
var (
	socketAPIBaseURL = "https://slack.com"
	socketBackoff    = 3 * time.Second
)

// socketEnvelope 是 Socket Mode 推送的封包。只取需要的欄位。
type socketEnvelope struct {
	Type       string `json:"type"`
	EnvelopeID string `json:"envelope_id"`
	Payload    struct {
		Event struct {
			Type        string `json:"type"`
			ChannelType string `json:"channel_type"`
			User        string `json:"user"`
			Text        string `json:"text"`
			BotID       string `json:"bot_id"`
			Subtype     string `json:"subtype"`
		} `json:"event"`
	} `json:"payload"`
}

// handleSocketEnvelope 處理單一 envelope（抽成函式以便用合成封包測試）。
//
// 順序很重要：events_api 一律先 ack（Slack 未收到 ack 會重送），
// 之後才判斷是否為要交給 onMessage 的客戶 DM 訊息。
// 回傳 true 表示收到 disconnect，呼叫端應重新連線。
//  malformed 封包只 log，不 panic。
func handleSocketEnvelope(raw []byte, ack func(string), onMessage func(string, string)) bool {
	var env socketEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		fmt.Printf("[slack][socket] 無法解析 envelope: %v body=%s\n", err, truncate(string(raw), 200))
		return false
	}

	switch env.Type {
	case "hello":
		fmt.Printf("[slack][socket] 已連線（hello）\n")
		return false

	case "events_api":
		// 先 ack，再處理——順序不可對調
		if env.EnvelopeID != "" && ack != nil {
			ack(env.EnvelopeID)
		}
		ev := env.Payload.Event
		if ev.Type != "message" {
			return false
		}
		if ev.BotID != "" || ev.Subtype != "" {
			// bot 訊息、編輯/刪除/message_replied 等 subtype 一律跳過
			return false
		}
		if ev.ChannelType != "im" {
			// 只回 DM；頻道訊息不處理
			return false
		}
		if strings.TrimSpace(ev.Text) == "" || ev.User == "" {
			return false
		}
		if onMessage != nil {
			onMessage(ev.User, ev.Text)
		}
		return false

	case "disconnect":
		fmt.Printf("[slack][socket] 收到 disconnect，準備重新連線\n")
		return true

	default:
		// 其他 type（如 interactive）目前不處理，只記錄
		fmt.Printf("[slack][socket] 未處理的 envelope type=%s\n", env.Type)
		return false
	}
}

// openSocketConnection 以 app token 呼叫 apps.connections.open 取得 wss URL。
func openSocketConnection(ctx context.Context, appToken string) (string, error) {
	url := strings.TrimRight(socketAPIBaseURL, "/") + "/api/apps.connections.open"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+appToken)

	hc := &http.Client{Timeout: 30 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack apps.connections.open 連線失敗: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slack apps.connections.open 回應 %d: %s",
			resp.StatusCode, truncate(string(body), 200))
	}

	var result struct {
		OK    bool   `json:"ok"`
		URL   string `json:"url"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("slack apps.connections.open 解析失敗: %w", err)
	}
	if !result.OK || result.URL == "" {
		return "", fmt.Errorf("slack apps.connections.open 拒絕: %s", result.Error)
	}
	return result.URL, nil
}

// runSocketOnce 建立一條 Socket Mode 連線並讀到斷線為止。
// 回傳 nil 表示正常結束（disconnect 或讀取錯誤），由外層決定重連。
func runSocketOnce(ctx context.Context, appToken string, onMessage func(string, string)) error {
	wsURL, err := openSocketConnection(ctx, appToken)
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("slack socket dial 失敗: %w", err)
	}
	defer conn.Close()

	// ctx 取消時主動關連線，讓阻塞中的 ReadMessage 立刻返回
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	ack := func(envelopeID string) {
		payload, _ := json.Marshal(map[string]string{"envelope_id": envelopeID})
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			fmt.Printf("[slack][socket] ack 送出失敗: %v\n", err)
		}
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("slack socket 讀取中斷: %w", err)
		}
		if reconnect := handleSocketEnvelope(raw, ack, onMessage); reconnect {
			return nil // 外層迴圈重新 open＋dial
		}
	}
}

// RunSocketMode 啟動 Socket Mode 入站監聽，直到 ctx 取消。
//
// appToken 為 xapp- app-level token（需 connections:write scope）。
// onMessage(userID, text) 在收到客戶 DM 時被呼叫（已先 ack）。
// 斷線自動重連，backoff 3 秒；ctx 取消時回傳 ctx.Err()。
func RunSocketMode(ctx context.Context, appToken string, onMessage func(userID, text string)) error {
	if strings.TrimSpace(appToken) == "" {
		return fmt.Errorf("slack Socket Mode 需要 SLACK_APP_TOKEN（xapp-）")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := runSocketOnce(ctx, appToken, onMessage)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			fmt.Printf("[slack][socket] 連線錯誤，%.0f 秒後重連: %v\n", socketBackoff.Seconds(), err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(socketBackoff):
		}
	}
}
