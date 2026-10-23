package telegram

// getUpdates 輪詢（長輪詢）入站訊息的能力。
//
// 為什麼用輪詢而不是 webhook：
// LINE 的綁定靠 webhook（需 Cloudflare Tunnel 把 /webhook/line 對外公開）。
// Telegram 不需要——getUpdates 長輪詢是純出站連線，容器或 exe 都能直接跑，
// 不需要任何對外公開網址。一個 bot token 同時供「出站通知」與「入站輪詢」使用。
//
// offset 必須持續遞增：每次成功 getUpdates 後，把最大的 update_id+1 存起來，
// 下次從那裡續抓，Telegram 才會不再重送舊訊息。offset 寫進檔案（data 目錄下），
// 重啟後從檔讀回，避免重新處理停機期間積累的訊息。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Update 是 getUpdates 回傳的一筆更新。只取綁定流程需要的欄位。
type Update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
		From      struct {
			ID       int64  `json:"id"`
			IsBot    bool   `json:"is_bot"`
			Username string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
}

// GetUpdates 以長輪詢抓入站更新。offset=0 表示從「最近的 100 筆」開始（僅用於首次無紀錄時）。
// timeout 秒為長輪詢等待上限；回傳該次取得的所有更新（可能為空）。
func (c *Client) GetUpdates(offset int64, timeout int) ([]Update, error) {
	if c.cfg == nil || c.cfg.Mock {
		return nil, nil // Mock 模式不連網路
	}

	url := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=%d",
		strings.TrimRight(c.cfg.BaseURL, "/"), c.cfg.BotToken, offset, timeout)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram getUpdates 連線失敗: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB 上限，單次更新足夠
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram getUpdates 回應 %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var result struct {
		OK          bool     `json:"ok"`
		Description string   `json:"description"`
		Result      []Update `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("telegram getUpdates 解析失敗: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram getUpdates 拒絕: %s", result.Description)
	}
	return result.Result, nil
}

// ParseChatID 把客戶回報的 chat id 整理成可比對的字串。
// Telegram 私聊 chat id 是正整數；客戶通常從 @userinfobot 拿到。
func ParseChatID(s string) string {
	return strings.TrimSpace(s)
}

// chatIDFromInt 把 int64 chat id 轉成字串（存進 telegram_user_id 欄）。
func ChatIDFromInt(id int64) string {
	return strconv.FormatInt(id, 10)
}
