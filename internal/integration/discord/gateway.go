package discord

// Gateway WebSocket（純出站連線）入站訊息的能力。
//
// 為什麼用 Gateway 而不是 webhook：與 Telegram getUpdates 輪詢同理——
// 純出站連線，容器或 exe 都能直接跑，不需要任何對外公開網址。
//
// 協定要点（Discord Gateway v10）：
//   - op 10 HELLO：依 heartbeat_interval 啟動心跳 goroutine（週期送 op 1，帶最後 seq）
//   - op 2 IDENTIFY：token + intents（GUILDS|GUILD_MESSAGES|DIRECT_MESSAGES|MESSAGE_CONTENT）
//   - op 0 DISPATCH：追蹤 s（sequence）；MESSAGE_CREATE 回呼 onMessage（guild 與 DM 都收）
//   - op 11 HEARTBEAT_ACK：確認連線存活
//   - op 7 RECONNECT / op 9 INVALID_SESSION：關閉連線，外層迴圈 backoff 後
//     重新 IDENTIFY 開新 session（不做 RESUME，實作簡單且對通知場景足夠）
//
// 畸形帧只 log 不 panic；ctx 取消時關閉連線並結束迴圈。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// gatewayIntents：GUILDS(1) | GUILD_MESSAGES(512) | DIRECT_MESSAGES(4096) | MESSAGE_CONTENT(32768)。
const gatewayIntents = 1 | 512 | 4096 | 32768

var (
	// gatewayURL 與 reconnectBackoff 以變數宣告，讓測試可指向 httptest server
	// 並縮短 backoff（正式值勿動）。
	gatewayURL       = "wss://gateway.discord.gg/?v=10&encoding=json"
	reconnectBackoff = 3 * time.Second
)

// gatewayFrame 是 Gateway 帧的通用外層。S 用指標區分「null」與 0。
type gatewayFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
}

// handleGatewayPayload 解析單一帧並處理 DISPATCH（抽成函式以便單元測試）。
//
// 回傳：op（畸形帧回 -1）、HELLO 的 heartbeat_interval（ms，其余 op 為 0）、
// 是否需重新連線（op 7/9）。seq 以 atomic 寫入（心跳 goroutine 會讀）。
// 任何解析失敗只 log 不 panic。
func handleGatewayPayload(payload []byte, seq *int64, onMessage func(userID, text string)) (op int, heartbeatInterval int, reconnect bool) {
	var f gatewayFrame
	if err := json.Unmarshal(payload, &f); err != nil {
		fmt.Printf("[discord][gateway] 無法解析帧（忽略）: %v\n", err)
		return -1, 0, false
	}
	if f.S != nil && seq != nil {
		atomic.StoreInt64(seq, *f.S)
	}

	switch f.Op {
	case 10: // HELLO
		var d struct {
			HeartbeatInterval float64 `json:"heartbeat_interval"`
		}
		if err := json.Unmarshal(f.D, &d); err == nil {
			return 10, int(d.HeartbeatInterval), false
		}
		return 10, 0, false
	case 7: // RECONNECT
		fmt.Printf("[discord][gateway] 收到 RECONNECT (op 7)，準備重新連線\n")
		return 7, 0, true
	case 9: // INVALID_SESSION
		fmt.Printf("[discord][gateway] 收到 INVALID SESSION (op 9)，準備重新 IDENTIFY\n")
		return 9, 0, true
	case 0: // DISPATCH
		if f.T == "MESSAGE_CREATE" {
			dispatchMessageCreate(f.D, onMessage)
		}
		return 0, 0, false
	default: // 含 op 1（server 要求心跳）、op 11（ACK）
		return f.Op, 0, false
	}
}

// dispatchMessageCreate 處理 MESSAGE_CREATE：跳過 bot 作者與空內容，
// guild 訊息與 DM 都回呼（由呼叫端決定怎麼分流）。
func dispatchMessageCreate(d json.RawMessage, onMessage func(userID, text string)) {
	var msg struct {
		Author struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(d, &msg); err != nil {
		fmt.Printf("[discord][gateway] MESSAGE_CREATE 解析失敗（忽略）: %v\n", err)
		return
	}
	if msg.Author.Bot || msg.Author.ID == "" {
		return
	}
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	fmt.Printf("[discord][gateway] 收到訊息 user=%s len=%d\n", msg.Author.ID, len([]rune(msg.Content)))
	if onMessage != nil {
		onMessage(msg.Author.ID, msg.Content)
	}
}

// RunGateway 維持 Discord Gateway 連線直到 ctx 取消。
//
// 斷線（讀取失敗、op 7、op 9）一律以固定 backoff 重新連線並重新 IDENTIFY
// （新 session，不做 RESUME）。只有 ctx 取消是正常結束（回 ctx.Err()）。
func RunGateway(ctx context.Context, botToken string, onMessage func(userID, text string)) error {
	if strings.TrimSpace(botToken) == "" {
		return fmt.Errorf("discord gateway 需要 bot token（未設定 DISCORD_BOT_TOKEN）")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := runGatewaySession(ctx, botToken, onMessage)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			fmt.Printf("[discord][gateway] 連線中斷: %v\n", err)
		}
		fmt.Printf("[discord][gateway] %s 後重新連線\n", reconnectBackoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectBackoff):
		}
	}
}

// runGatewaySession 跑單一連線 session：HELLO → 心跳 + IDENTIFY → 讀取迴圈。
// 回 nil 表示需重新連線（op 7/9）；回 error 表示連線異常。
func runGatewaySession(ctx context.Context, botToken string, onMessage func(userID, text string)) error {
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, gatewayURL, nil)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return fmt.Errorf("discord gateway 連線失敗: %w", err)
	}
	defer conn.Close()

	// ctx 取消時關閉連線，讓阻塞中的 ReadMessage 立刻返回。
	sessionDone := make(chan struct{})
	defer close(sessionDone)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-sessionDone:
		}
	}()

	var (
		writeMu   sync.Mutex // 心跳 goroutine 與主迴圈共用寫入
		seq       int64      // atomic：最後收到的 dispatch sequence
		hbStop    = make(chan struct{})
		hbOnce    sync.Once
		startOnce sync.Once
	)
	writeJSON := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}
	stopHeartbeat := func() { hbOnce.Do(func() { close(hbStop) }) }
	defer stopHeartbeat()

	sendHeartbeat := func() {
		var d interface{} // 尚无 seq 時送 null
		if s := atomic.LoadInt64(&seq); s != 0 {
			d = s
		}
		if err := writeJSON(map[string]interface{}{"op": 1, "d": d}); err != nil {
			fmt.Printf("[discord][gateway] 心跳寫入失敗: %v\n", err)
		}
	}

	// 讀取逾時：HELLO 後設為 2 倍心跳週期 + 10s，每收到任何帧就展延。
	// 對端失聯（連 ACK 都不回）時 ReadMessage 會逾時 → 外層重連。
	readTimeout := 90 * time.Second

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("discord gateway 讀取失敗: %w", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))

		op, interval, reconnect := handleGatewayPayload(payload, &seq, onMessage)
		if reconnect {
			return nil // op 7/9：外層 backoff 後重新 IDENTIFY
		}
		switch op {
		case 10: // HELLO
			if interval <= 0 {
				interval = 45000
			}
			readTimeout = time.Duration(interval)*time.Millisecond*2 + 10*time.Second
			_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
			startOnce.Do(func() {
				go func() {
					t := time.NewTicker(time.Duration(interval) * time.Millisecond)
					defer t.Stop()
					for {
						select {
						case <-hbStop:
							return
						case <-ctx.Done():
							return
						case <-t.C:
							sendHeartbeat()
						}
					}
				}()
			})
			identify := map[string]interface{}{
				"op": 2,
				"d": map[string]interface{}{
					"token":   botToken,
					"intents": gatewayIntents,
					"properties": map[string]string{
						"os":      "linux",
						"browser": "discordgo",
						"device":  "discordgo",
					},
				},
			}
			if err := writeJSON(identify); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("discord gateway IDENTIFY 寫入失敗: %w", err)
			}
			fmt.Printf("[discord][gateway] 已送出 IDENTIFY（intents=%d）\n", gatewayIntents)
		case 1: // server 要求立即心跳
			sendHeartbeat()
		case 11: // HEARTBEAT_ACK：讀取逾時已在上方展延，連線存活確認
		}
	}
}
