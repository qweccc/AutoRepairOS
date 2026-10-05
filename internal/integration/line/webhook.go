package line

import (
	"encoding/json"
	"fmt"
	"strings"
)

// WebhookEvent 是 LINE Messaging API webhook 送來的封裝。
type WebhookEvent struct {
	// Destination 為發送此事件的 channel ID。
	Destination string `json:"destination"`
	// Events 為事件陣列，一次請求可能含多個事件。
	Events []Event `json:"events"`
}

// Event 單一事件。LINE 依 type 帶不同欄位，這裡用共用結構 + 巢狀解析。
type Event struct {
	Type       string        `json:"type"`
	ReplyToken string        `json:"replyToken"`
	Source     EventSource   `json:"source"`
	Timestamp  int64         `json:"timestamp"`
	Message    EventMessage  `json:"message"`
	Postback   EventPostback `json:"postback"`
}

// EventSource 事件的來源。
type EventSource struct {
	Type    string `json:"type"`   // user / group / room
	UserID  string `json:"userId"` // 客戶的 LINE user ID
	GroupID string `json:"groupId"`
	RoomID  string `json:"roomId"`
}

// EventMessage 文字訊息內容。
type EventMessage struct {
	ID   string `json:"id"`
	Type string `json:"type"` // text / image / sticker ...
	Text string `json:"text"`
}

// EventPostback 按鈕被點擊時帶的資料。
type EventPostback struct {
	Data string `json:"data"`
}

// ParseWebhook 解析 webhook 請求 body。
func ParseWebhook(raw []byte) (*WebhookEvent, error) {
	var ev WebhookEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("解析 webhook JSON 失敗: %w", err)
	}
	return &ev, nil
}

// FollowEvent 表示客戶把 bot 加為好友（或其他來源新增關注）。
type FollowEvent struct {
	UserID     string
	ReplyToken string
}

// FollowEvents 取出所有 follow 事件。
func (e *WebhookEvent) FollowEvents() []FollowEvent {
	var out []FollowEvent
	for _, ev := range e.Events {
		if ev.Type == "follow" && ev.Source.UserID != "" {
			out = append(out, FollowEvent{UserID: ev.Source.UserID, ReplyToken: ev.ReplyToken})
		}
	}
	return out
}

// MessageEvent 表示客戶傳來一則訊息。
type MessageEvent struct {
	UserID     string
	ReplyToken string
	Text       string
}

// MessageEvents 取出所有文字訊息事件（非文字如圖片會被略過）。
func (e *WebhookEvent) MessageEvents() []MessageEvent {
	var out []MessageEvent
	for _, ev := range e.Events {
		if ev.Type != "message" || ev.Message.Type != "text" {
			continue
		}
		text := strings.TrimSpace(ev.Message.Text)
		if text == "" {
			continue
		}
		out = append(out, MessageEvent{
			UserID:     ev.Source.UserID,
			ReplyToken: ev.ReplyToken,
			Text:       text,
		})
	}
	return out
}

// PostbackEvent 表示客戶點擊了模板訊息中的按鈕。
type PostbackEvent struct {
	UserID     string
	ReplyToken string
	Data       string
}

// PostbackEvents 取出所有 postback 事件。
func (e *WebhookEvent) PostbackEvents() []PostbackEvent {
	var out []PostbackEvent
	for _, ev := range e.Events {
		if ev.Type != "postback" {
			continue
		}
		data := strings.TrimSpace(ev.Postback.Data)
		if data == "" {
			continue
		}
		out = append(out, PostbackEvent{
			UserID:     ev.Source.UserID,
			ReplyToken: ev.ReplyToken,
			Data:       data,
		})
	}
	return out
}
