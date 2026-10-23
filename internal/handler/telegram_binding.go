package handler

// Telegram 客戶自助綁定（鏡射 LINE 的 webhook 綁定，改用 getUpdates 長輪詢）。
//
// 流程與 LINE 完全一致，因為車行情境相同：
//   - 一位車主可能有多台車、也可能多位車主共用門號 → 不猜測，請客戶傳手機號碼
//   - 比對 customers.phone（用與 LINE 相同的 phoneMatchSQL 正規化舊資料）
//   - 只有唯一命中才自動綁定；0 筆回「查無此號碼」、多筆回「需櫃檯協助」
//
// 差異只在傳輸層：LINE 靠 webhook（需對外公開網址），Telegram 靠 getUpdates
// 長輪詢（純出站，容器/exe 都能跑，不需 Cloudflare）。一個 bot token 同時供
// 出站通知（internal/integration/telegram）與此處入站輪詢使用。

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/telegram"
)

// bindingOffsetPath 存 getUpdates offset 的檔案（data 目錄下）。
func bindingOffsetPath() string {
	// 與 DB 同目錄，確保 exe 與 docker 都寫得到。
	dbPath := db.Path()
	return filepath.Join(filepath.Dir(dbPath), "telegram_binding_offset.txt")
}

func loadBindingOffset() int64 {
	b, err := os.ReadFile(bindingOffsetPath())
	if err != nil {
		return 0 // 無紀錄：首次從最近 100 筆開始
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func saveBindingOffset(offset int64) {
	path := bindingOffsetPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[telegram-bind] 建立 offset 目錄失敗: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatInt(offset, 10)), 0o644); err != nil {
		log.Printf("[telegram-bind] 寫入 offset 失敗: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("[telegram-bind] 更新 offset 失敗: %v", err)
		_ = os.Remove(tmp)
	}
}

// promptedChats 記錄「已提示過要傳手機號碼」的 chat，避免每次訊息都重複洗訊息。
// 僅記憶體狀態：重啟後會重新提示一次，可接受。
var (
	promptedMu    sync.Mutex
	promptedChats = map[int64]bool{}
)

func markPrompted(chatID int64) {
	promptedMu.Lock()
	promptedChats[chatID] = true
	promptedMu.Unlock()
}

func wasPrompted(chatID int64) bool {
	promptedMu.Lock()
	defer promptedMu.Unlock()
	return promptedChats[chatID]
}

// isTelegramBound 該 chat id 是否已綁到某客戶。
func isTelegramBound(chatID string) bool {
	return countRows("SELECT COUNT(1) FROM active_customers WHERE telegram_user_id = ?", chatID) > 0
}

// bindTelegramUser 把一個 Telegram chat id 綁到指定客戶（寫入 telegram_user_id）。
//
// 唯一性規則與 LINE 對稱：
//   - 同一 chat id 不能綁兩個客戶（否則通知送錯人、且可能洩漏維修紀錄）
//   - 同一客戶不能被兩個 chat id 綁定（否則通知重複送出）
func bindTelegramUser(chatID string, customerID int64) error {
	if strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("Telegram chat id 為空")
	}

	tx, err := db.BeginImmediate()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRow("SELECT COUNT(1) FROM active_customers WHERE telegram_user_id = ?", chatID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("此 Telegram 帳號已綁定其他客戶")
	}
	if err := tx.QueryRow("SELECT COUNT(1) FROM active_customers WHERE id = ? AND COALESCE(telegram_user_id,'') <> ''", customerID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("此客戶已綁定其他 Telegram 帳號")
	}
	res, err := tx.Exec("UPDATE customers SET telegram_user_id = ? WHERE id = ?", chatID, customerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("找不到該客戶")
	}
	return tx.Commit()
}

// StartTelegramBinding 啟動 getUpdates 長輪詢背景迴圈（main.go 啟動）。
// 停止訊號關閉後迴圈退出。
func StartTelegramBinding(stop <-chan struct{}) {
	cfg, client := telegram.Shared()
	if cfg == nil || !cfg.Valid() {
		log.Printf("[telegram-bind] 未設定 TELEGRAM_BOT_TOKEN，客戶自助綁定輪詢不啟動")
		return
	}

	offset := loadBindingOffset()
	log.Printf("[telegram-bind] 輪詢啟動，offset=%d", offset)

	pollTimeout := 30
	for {
		select {
		case <-stop:
			log.Printf("[telegram-bind] 停止訊號收到，輪詢退出")
			return
		default:
		}

		updates, err := client.GetUpdates(offset, pollTimeout)
		if err != nil {
			// 長輪詢超時（無新訊息）是正常情況，不當錯誤打。
			if !strings.Contains(err.Error(), "context deadline") && !strings.Contains(err.Error(), "timeout") {
				log.Printf("[telegram-bind] getUpdates 錯誤: %v", err)
			}
			select {
			case <-stop:
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			if u.Message == nil || u.Message.From.IsBot {
				continue
			}
			if u.Message.Chat.Type != "private" {
				continue // 只處理私聊
			}
			chatID := u.Message.Chat.ID
			chatStr := strconv.FormatInt(chatID, 10)
			text := strings.TrimSpace(u.Message.Text)

			handleTelegramBindMessage(client, chatID, chatStr, text)

			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
				saveBindingOffset(offset)
			}
		}
	}
}

// handleTelegramBindMessage 處理一則來自未綁定客戶的訊息。
func handleTelegramBindMessage(client *telegram.Client, chatID int64, chatStr, text string) {
	if isTelegramBound(chatStr) {
		// 已綁定的客戶再傳訊息：目前只簡單回應，其他指令（進度/履歷）後續擴充。
		return
	}

	phone := normalizePhone(text)
	if phone == "" {
		if !wasPrompted(chatID) {
			_ = client.SendMessage(chatStr,
				"您好，歡迎加入汽車修 🔧\n\n請傳送您的<b>手機號碼</b>（例：0912345678），\n我們會為您連結車輛紀錄。\n連結後即可查詢工單進度。")
			markPrompted(chatID)
		}
		return
	}

	n := customerCountByPhone(phone)
	switch {
	case n == 0:
		_ = client.SendMessage(chatStr,
			"查無此號碼「"+phone+"」的客戶紀錄。\n請確認號碼是否正確，或撥打店內電話由櫃檯協助。")

	case n > 1:
		log.Printf("[telegram-bind] 號碼 %s 對應 %d 個客戶，需人工處理", phone, n)
		db.MustExec(`INSERT INTO notifications
			(customer_id, channel, kind, subject, body, scheduled_at, status)
			VALUES (NULL, 'internal', 'telegram_follow', 'Telegram 綁定需人工處理',
				?, CURRENT_TIMESTAMP, 'pending')`,
			"號碼 "+phone+" 對應 "+strconv.Itoa(n)+" 個客戶，Telegram chat "+chatStr+" 無法自動綁定")
		_ = client.SendMessage(chatStr,
			"此號碼對應 "+strconv.Itoa(n)+" 筆客戶紀錄，為避免通知送錯人，需由櫃檯協助綁定。\n請撥打店內電話告知。")

	default:
		var customerID int64
		if err := db.DB.QueryRow("SELECT id FROM active_customers WHERE "+phoneMatchSQL("phone"), phone).Scan(&customerID); err != nil {
			_ = client.SendMessage(chatStr, "系統忙碌中，請稍後再試。")
			return
		}
		if err := bindTelegramUser(chatStr, customerID); err != nil {
			log.Printf("[telegram-bind] 綁定 chat %s 到客戶 #%d 失敗: %v", chatStr, customerID, err)
			_ = client.SendMessage(chatStr, "綁定失敗："+err.Error()+"。請撥打店內電話由櫃檯協助。")
			return
		}

		var cname string
		db.DB.QueryRow("SELECT name FROM active_customers WHERE id = ?", customerID).Scan(&cname)
		log.Printf("[telegram-bind] chat %s 已自動綁定客戶 #%d（%s）", chatStr, customerID, cname)

		var vehicleCount int
		db.DB.QueryRow(`SELECT COUNT(1) FROM active_vehicles v
			JOIN active_work_orders w ON w.vehicle_id = v.id
			WHERE v.customer_id = ?`, customerID).Scan(&vehicleCount)

		_ = client.SendMessage(chatStr,
			"✅ 已為您連結，客戶："+cname+
				"\n共 "+strconv.Itoa(vehicleCount)+" 台有維修紀錄的車輛。\n\n傳「進度」查工單狀態、傳「履歷」查維修紀錄（功能陸續上線）。")
	}
}
