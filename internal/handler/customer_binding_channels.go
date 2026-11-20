package handler

// Discord／Slack 綁定管理（FR-CRM-06）：與 LINE/Telegram 相同的唯一性規則——
// 同一個帳號只能綁一個客戶，避免跨客戶資料外洩。綁定來源：
//  1. 客戶聊天傳「綁定 綁定碼」→ VerifyBindingCode → bindChannelUserByID
//  2. 櫃檯編輯頁手動輸入 ID → BindCustomerDiscord/Slack（格式＋唯一性驗證）

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"autorepairos/internal/db"
)

var discordUserIDPattern = regexp.MustCompile(`^[0-9]{17,20}$`) // Discord snowflake
var slackUserIDPattern = regexp.MustCompile(`^[UW][A-Z0-9]{8,}$`)

func validateChannelUserID(platform, s string) error {
	if s == "" {
		return nil // 留空＝不綁
	}
	switch platform {
	case "discord":
		if !discordUserIDPattern.MatchString(s) {
			return fmt.Errorf("Discord 用戶 ID 格式不對：應為 17–20 位數字（開發者模式→右鍵用戶→複製用戶 ID）")
		}
	case "slack":
		if !slackUserIDPattern.MatchString(s) {
			return fmt.Errorf("Slack 會員 ID 格式不對：應為 U 開頭的英數（個人資料→⋯→複製會員 ID）")
		}
	}
	return nil
}

func isChannelBound(platform, userID string) bool {
	var n int
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_customers WHERE `+channelColumn(platform)+` = ?`, userID).Scan(&n)
	return n > 0
}

// bindChannelUserByID 聊天綁定碼驗證成功後寫入綁定（拒絕已被其他客戶佔用的帳號）。
func bindChannelUserByID(platform, userID string, customerID int64) error {
	col := channelColumn(platform)
	var otherID int64
	if err := db.DB.QueryRow(`SELECT id FROM customers WHERE `+col+` = ? AND id <> ?`, userID, customerID).Scan(&otherID); err == nil {
		return fmt.Errorf("此帳號已綁定客戶 #%d，請先由櫃檯解除綁定", otherID)
	}
	_, err := db.DB.Exec(`UPDATE customers SET `+col+` = ? WHERE id = ?`, userID, customerID)
	return err
}

func channelDisplayName(platform string) string {
	if platform == "discord" {
		return "Discord"
	}
	return "Slack"
}

func channelBindingTakenByOther(platform, userID string, selfID int64) (int64, string) {
	var otherID int64
	var otherName string
	err := db.DB.QueryRow(`SELECT id, COALESCE(name,'') FROM customers
		WHERE `+channelColumn(platform)+` = ? AND id <> ?`, userID, selfID).Scan(&otherID, &otherName)
	if err != nil {
		return 0, ""
	}
	return otherID, otherName
}

func bindChannelCustomer(w http.ResponseWriter, r *http.Request, platform string) {
	id := mustInt(r.PathValue("id"))
	editPath := "/customers/" + strconv.FormatInt(id, 10) + "/edit"

	uid := strings.TrimSpace(r.FormValue(platform + "_user_id"))
	if err := validateChannelUserID(platform, uid); err != nil {
		redirectErrMsg(w, r, editPath, err.Error())
		return
	}
	if countRows("SELECT COUNT(1) FROM active_customers WHERE id = ?", id) == 0 {
		http.NotFound(w, r)
		return
	}
	if uid != "" {
		if otherID, otherName := channelBindingTakenByOther(platform, uid, id); otherID != 0 {
			redirectErrMsg(w, r, editPath, fmt.Sprintf(
				"這個帳號已綁定客戶 #%d（%s）。同一個帳號綁兩個客戶會讓對方看到不屬於他的維修紀錄，請先到該客戶的頁面取消綁定",
				otherID, otherName))
			return
		}
	}
	res, err := db.DB.Exec(`UPDATE customers SET `+channelColumn(platform)+` = ? WHERE id = ?`, uid, id)
	if err != nil {
		logBindingError("綁定 "+platform+" 失敗", id, err)
		redirectErrMsg(w, r, editPath, "綁定失敗，請稍後再試")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	if uid == "" {
		redirectMsg(w, r, editPath, "已取消 "+channelDisplayName(platform)+" 綁定")
		return
	}
	redirectMsg(w, r, editPath, "已綁定 "+channelDisplayName(platform)+" 帳號")
}

func unbindChannelCustomer(w http.ResponseWriter, r *http.Request, platform string) {
	id := mustInt(r.PathValue("id"))
	editPath := "/customers/" + strconv.FormatInt(id, 10) + "/edit"
	res, err := db.DB.Exec(`UPDATE customers SET `+channelColumn(platform)+` = NULL WHERE id = ?`, id)
	if err != nil {
		logBindingError("取消 "+platform+" 綁定失敗", id, err)
		redirectErrMsg(w, r, editPath, "取消綁定失敗，請稍後再試")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, editPath, "已取消 "+channelDisplayName(platform)+" 綁定")
}

// BindCustomerDiscord／UnbindCustomerDiscord／BindCustomerSlack／UnbindCustomerSlack
// 是編輯頁手動綁定／取消的端點（routes.go 註冊）。
func BindCustomerDiscord(w http.ResponseWriter, r *http.Request)   { bindChannelCustomer(w, r, "discord") }
func UnbindCustomerDiscord(w http.ResponseWriter, r *http.Request) { unbindChannelCustomer(w, r, "discord") }
func BindCustomerSlack(w http.ResponseWriter, r *http.Request)     { bindChannelCustomer(w, r, "slack") }
func UnbindCustomerSlack(w http.ResponseWriter, r *http.Request)   { unbindChannelCustomer(w, r, "slack") }

// channelBindWelcome 綁定成功歡迎詞（各通道共用）。
func channelBindWelcome(platform string, customerID int64) string {
	var vehicleCount int
	db.DB.QueryRow(`SELECT COUNT(DISTINCT v.id) FROM active_vehicles v
		JOIN active_work_orders wo ON wo.vehicle_id = v.id
		WHERE v.customer_id = ?`, customerID).Scan(&vehicleCount)
	return "綁定成功！您已連結 " + strconv.Itoa(vehicleCount) + " 台有維修紀錄的車輛。\n\n" +
		"傳「進度」查工單狀態、「履歷」查維修紀錄、「預約」掛號預約。"
}