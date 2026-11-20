package handler

// Discord 客戶自助（FR-CRM-06）：Gateway WebSocket 純出站，不需公開網址。
// 與 Telegram 綁定流程同構：綁定碼 → 唯一性驗證 → 歡迎詞；已綁定走共用指令層。

import (
	"context"
	"log"
	"strings"
	"sync"

	"autorepairos/internal/integration/discord"
)

var discordPrompted sync.Map

// StartDiscordBot 啟動 Gateway 長連線（token 未設定則不啟動）。
func StartDiscordBot(ctx context.Context, wg *sync.WaitGroup) {
	cfg, client := discord.Shared()
	if cfg == nil || !cfg.Valid() {
		log.Printf("[discord] 未設定 DISCORD_BOT_TOKEN，Discord 通道不啟動（MOCK）")
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[discord] Gateway 長連線啟動（純出站，不需公開網址）")
		if err := discord.RunGateway(ctx, cfg.BotToken, func(userID, text string) {
			handleDiscordMessage(client, userID, text)
		}); err != nil && ctx.Err() == nil {
			log.Printf("[discord] Gateway 結束: %v", err)
		}
	}()
}

func handleDiscordMessage(client *discord.Client, userID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if isChannelBound("discord", userID) {
		if reply := handleCustomerCommand("discord", userID, text); reply != "" {
			_ = client.SendDirectMessage(userID, reply)
		}
		return
	}
	if strings.HasPrefix(text, "綁定") {
		code := strings.TrimSpace(strings.TrimPrefix(text, "綁定"))
		customerID, err := VerifyBindingCode("discord", code, userID)
		if err != nil {
			_ = client.SendDirectMessage(userID, "綁定失敗："+err.Error())
			return
		}
		discordPrompted.Delete(userID)
		_ = client.SendDirectMessage(userID, channelBindWelcome("discord", customerID))
		return
	}
	if _, loaded := discordPrompted.LoadOrStore(userID, true); loaded {
		return
	}
	_ = client.SendDirectMessage(userID, "您好！請輸入「綁定 綁定碼」（6 位數字，由櫃檯提供）以開通查詢與通知服務。")
}