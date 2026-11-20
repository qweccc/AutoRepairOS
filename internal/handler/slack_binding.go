package handler

// Slack 客戶自助（FR-CRM-06）：Socket Mode 純出站，不需公開網址。
// 與 Telegram 綁定流程同構；已綁定走共用指令層。Slack 的 chat.postMessage
// 直接對 user ID 發送即會開啟／使用 IM 頻道，不需另建 DM channel。

import (
	"context"
	"log"
	"strings"
	"sync"

	"autorepairos/internal/integration/slack"
)

var slackPrompted sync.Map

// StartSlackBot 啟動 Socket Mode（bot＋app token 未設定則不啟動）。
func StartSlackBot(ctx context.Context, wg *sync.WaitGroup) {
	cfg, client := slack.Shared()
	if cfg == nil || !cfg.SocketReady() {
		log.Printf("[slack] 未設定 SLACK_BOT_TOKEN＋SLACK_APP_TOKEN，Slack 通道不啟動（MOCK）")
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[slack] Socket Mode 長連線啟動（純出站，不需公開網址）")
		if err := slack.RunSocketMode(ctx, cfg.AppToken, func(userID, text string) {
			handleSlackMessage(client, userID, text)
		}); err != nil && ctx.Err() == nil {
			log.Printf("[slack] Socket Mode 結束: %v", err)
		}
	}()
}

func handleSlackMessage(client *slack.Client, userID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if isChannelBound("slack", userID) {
		if reply := handleCustomerCommand("slack", userID, text); reply != "" {
			_ = client.SendMessage(userID, reply)
		}
		return
	}
	if strings.HasPrefix(text, "綁定") {
		code := strings.TrimSpace(strings.TrimPrefix(text, "綁定"))
		customerID, err := VerifyBindingCode("slack", code, userID)
		if err != nil {
			_ = client.SendMessage(userID, "綁定失敗："+err.Error())
			return
		}
		slackPrompted.Delete(userID)
		_ = client.SendMessage(userID, channelBindWelcome("slack", customerID))
		return
	}
	if _, loaded := slackPrompted.LoadOrStore(userID, true); loaded {
		return
	}
	_ = client.SendMessage(userID, "您好！請輸入「綁定 綁定碼」（6 位數字，由櫃檯提供）以開通查詢與通知服務。")
}