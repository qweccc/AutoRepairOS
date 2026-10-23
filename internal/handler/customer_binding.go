package handler

// 客戶的 LINE / Telegram 綁定管理。
//
// 為什麼要獨立成一支檔而不是放在 customers.go：
// 綁定不是「客戶資料的一個欄位」，它是一條有唯一性規則與跨客戶影響的關聯。
// 之前它被當成普通欄位塞進編輯表單（自由輸入 + 直接 UPDATE），造成兩個問題：
//
//  1. **跨客戶資料外洩**：表單的 UPDATE 繞過 bindLineUser 的唯一性檢查，
//     同一個 LINE 帳號可以綁到兩個客戶。履歷查詢
//     `SELECT id FROM customers WHERE line_user_id = ?` 用 QueryRow 任取一筆，
//     於是 A 客戶傳「履歷」會看到 B 客戶的維修項目與金額。
//     （migration 044 已補上 DB 層的 partial unique index 作為最後防線。）
//
//  2. **靜默失效**：櫃檯手打 Uxxxx 打錯字，系統照樣存下去，通知就永遠送不到，
//     而且沒有任何提示——因為沒有地方可以驗證這個 ID 對不對。
//
// 現在的設計：LINE User ID 唯讀（客戶自己都看不到自己的 ID，櫃檯更不可能知道），
// 只能透過「綁定 / 取消綁定」這兩個明確動作改變；Telegram 因為客戶查得到自己的
// chat id，仍可輸入，但加上格式與唯一性驗證。

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// lineUserIDPattern 是 LINE user ID 的寬鬆格式檢查。
//
// LINE 的 user ID 固定以 'U' 開頭、其後為 32 個十六進位字元，但格式曾調整過，
// 硬性要求 33 字元會在 LINE 改版時擋掉合法 ID。這裡只擋「明顯不是 ID」的輸入：
// 空白、含 @ 或斜線、長度差太多。目的是攔下打字錯誤與貼錯欄位，不是驗證真偽。
var lineUserIDPattern = regexp.MustCompile(`^U[0-9a-fA-F]{10,64}$`)

// telegramChatIDPattern 是 Telegram 私聊 chat id 的格式（純數字，負數為群組）。
var telegramChatIDPattern = regexp.MustCompile(`^-?[0-9]{5,20}$`)

func validateLineUserID(s string) error {
	if s == "" {
		return errors.New("請輸入 LINE User ID")
	}
	if !lineUserIDPattern.MatchString(s) {
		return errors.New("LINE User ID 格式不對：應以 U 開頭、後面接英數（例：U1234567890abcdef）。" +
			"請從「排程」頁的待綁定通知複製，不要手打")
	}
	return nil
}

func validateTelegramUserID(s string) error {
	if s == "" {
		return nil // 留空＝不綁 Telegram
	}
	if !telegramChatIDPattern.MatchString(s) {
		return errors.New("Telegram User ID 格式不對：應為一串數字（例：123456789）。" +
			"請客戶在 Telegram 搜尋 @userinfobot 查詢後回報")
	}
	return nil
}

// lineBindingTakenByOther 檢查該 LINE 帳號是否已被「其他」客戶綁定。
// 回傳佔用者的客戶 id 與名稱；沒被佔用則回 0。
func lineBindingTakenByOther(lineUserID string, selfID int64) (int64, string) {
	var otherID int64
	var otherName string
	err := db.DB.QueryRow(`SELECT id, COALESCE(name,'') FROM customers
		WHERE line_user_id = ? AND id <> ?`, lineUserID, selfID).Scan(&otherID, &otherName)
	if err != nil {
		return 0, ""
	}
	return otherID, otherName
}

// BindCustomerLine 把一個 LINE 帳號綁定到指定客戶（POST /customers/{id}/line-bind）。
//
// 允許覆蓋「該客戶自己」原有的綁定值（＝重新綁定），但拒絕綁到別的客戶已用的
// 帳號——這正是防止跨客戶資料外洩的關鍵一步。
func BindCustomerLine(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	editPath := "/customers/" + strconv.FormatInt(id, 10) + "/edit"

	lineUserID := strings.TrimSpace(r.FormValue("line_user_id"))
	if err := validateLineUserID(lineUserID); err != nil {
		redirectErrMsg(w, r, editPath, err.Error())
		return
	}
	if countRows("SELECT COUNT(1) FROM active_customers WHERE id = ?", id) == 0 {
		http.NotFound(w, r)
		return
	}
	if otherID, otherName := lineBindingTakenByOther(lineUserID, id); otherID != 0 {
		redirectErrMsg(w, r, editPath, fmt.Sprintf(
			"這個 LINE 帳號已綁定客戶 #%d（%s）。同一個帳號綁兩個客戶會讓對方看到不屬於他的維修紀錄，"+
				"請先到該客戶的頁面取消綁定", otherID, otherName))
		return
	}

	res, err := db.DB.Exec(`UPDATE customers SET line_user_id = ? WHERE id = ?`, lineUserID, id)
	if err != nil {
		logBindingError("綁定 LINE 失敗", id, err)
		redirectErrMsg(w, r, editPath, "綁定失敗，請稍後再試")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, editPath, "已綁定 LINE 帳號")
}

// UnbindCustomerLine 取消指定客戶的 LINE 綁定（POST /customers/{id}/line-unbind）。
//
// 刻意允許「取消後重新綁定」：客戶換 LINE 帳號、或原本綁錯人時，
// 先取消再綁是最容易稽核的流程（兩步都有明確紀錄），
// 不要在一個動作裡同時做「解除別人的綁定 + 綁給自己」。
func UnbindCustomerLine(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	editPath := "/customers/" + strconv.FormatInt(id, 10) + "/edit"

	res, err := db.DB.Exec(`UPDATE customers SET line_user_id = NULL WHERE id = ?`, id)
	if err != nil {
		logBindingError("取消 LINE 綁定失敗", id, err)
		redirectErrMsg(w, r, editPath, "取消綁定失敗，請稍後再試")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, editPath, "已取消 LINE 綁定，客戶可重新加好友或報手機號碼再綁一次")
}

// telegramBindingTakenByOther 檢查該 Telegram ID 是否已被其他客戶使用。
func telegramBindingTakenByOther(telegramID string, selfID int64) (int64, string) {
	var otherID int64
	var otherName string
	err := db.DB.QueryRow(`SELECT id, COALESCE(name,'') FROM customers
		WHERE telegram_user_id = ? AND id <> ?`, telegramID, selfID).Scan(&otherID, &otherName)
	if err != nil {
		return 0, ""
	}
	return otherID, otherName
}

// UnbindCustomerTelegram 取消 Telegram 綁定（POST /customers/{id}/telegram-unbind）。
func UnbindCustomerTelegram(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	editPath := "/customers/" + strconv.FormatInt(id, 10) + "/edit"

	res, err := db.DB.Exec(`UPDATE customers SET telegram_user_id = NULL WHERE id = ?`, id)
	if err != nil {
		logBindingError("取消 Telegram 綁定失敗", id, err)
		redirectErrMsg(w, r, editPath, "取消綁定失敗，請稍後再試")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, editPath, "已取消 Telegram 綁定")
}

// BindCustomerTelegram 把一個 Telegram chat id 綁定到指定客戶（POST /customers/{id}/telegram-bind）。
//
// 與 BindCustomerLine 對稱：允許覆蓋「該客戶自己」原有的綁定值（重新綁定），
// 但拒絕綁到別的客戶已用的 chat id。客戶可在 Telegram 搜尋 @userinfobot 拿到
// 自己的 chat id 後回報，櫃檯在此輸入；格式與唯一性在此驗證。
func BindCustomerTelegram(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	editPath := "/customers/" + strconv.FormatInt(id, 10) + "/edit"

	chatID := strings.TrimSpace(r.FormValue("telegram_user_id"))
	if err := validateTelegramUserID(chatID); err != nil {
		redirectErrMsg(w, r, editPath, err.Error())
		return
	}
	if countRows("SELECT COUNT(1) FROM active_customers WHERE id = ?", id) == 0 {
		http.NotFound(w, r)
		return
	}
	if chatID != "" {
		if otherID, otherName := telegramBindingTakenByOther(chatID, id); otherID != 0 {
			redirectErrMsg(w, r, editPath, fmt.Sprintf(
				"這個 Telegram 帳號已綁定客戶 #%d（%s）。同一個帳號綁兩個客戶會讓對方看到不屬於他的維修紀錄，"+
					"請先到該客戶的頁面取消綁定", otherID, otherName))
			return
		}
	}

	res, err := db.DB.Exec(`UPDATE customers SET telegram_user_id = ? WHERE id = ?`, chatID, id)
	if err != nil {
		logBindingError("綁定 Telegram 失敗", id, err)
		redirectErrMsg(w, r, editPath, "綁定失敗，請稍後再試")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	if chatID == "" {
		redirectMsg(w, r, editPath, "已取消 Telegram 綁定")
		return
	}
	redirectMsg(w, r, editPath, "已綁定 Telegram 帳號")
}

func logBindingError(what string, customerID int64, err error) {
	// 客戶資料相關的錯誤一律留原始 err：UNIQUE 約束觸發時只顯示「失敗」
	// 會讓維護者完全無法判斷是格式、權限還是併發問題。
	log.Printf("%s（客戶 #%d）: %v", what, customerID, err)
}

// lineBindingBlock 產生編輯頁的 LINE 綁定區塊（唯讀狀態 + 取消 / 重新綁定）。
//
// 表單不能巢狀（HTML 不允許），所以這一塊必須放在客戶資料表單「之外」，
// 用獨立的 form 指向綁定 / 取消端點。
func lineBindingBlock(c model.Customer) string {
	var b strings.Builder
	b.WriteString(`<div class="card">
      <h2>LINE 綁定</h2>`)

	if strings.TrimSpace(c.LineUserID) != "" {
		b.WriteString(`<p style="margin:0 0 10px">
        <span class="pill p-done">已綁定</span>
        <code style="margin-left:8px">` + htmlEsc(c.LineUserID) + `</code>
      </p>
      <p style="color:#94a3b8;font-size:12.5px;margin:0 0 12px">
        此客戶已可在 LINE 傳「進度」查維修狀態、傳「履歷」查維修紀錄。
      </p>
      <form method="POST" action="/customers/` + strconv.FormatInt(c.ID, 10) + `/line-unbind"
            onsubmit="return confirm('確定取消綁定？取消後這位客戶將無法在 LINE 查到自己的維修紀錄。')">
        <button class="btn btn-d" type="submit">取消綁定</button>
      </form>`)
	} else {
		b.WriteString(`<p style="margin:0 0 10px"><span class="pill">未綁定</span></p>
      <p style="color:#94a3b8;font-size:12.5px;margin:0 0 12px">
        <b>建議讓客戶自己綁</b>：請客戶加官方帳號好友並傳送手機號碼，系統會自動連結（比對的是客戶資料上的電話，
        所以請先確認電話已建檔且正確）。<br>
        客戶加好友時，系統會在「排程」頁留下一則「新的 LINE 好友（待綁定）」通知，內含他的 LINE User ID。
      </p>`)
	}

	// 手動綁定：LINE User ID 客戶自己也看不到，所以這個欄位只能由系統通知複製過來。
	// 保留入口是為了「電話對不到」「同號多筆」等無法自動綁定的情境。
	b.WriteString(`
      <details style="margin-top:6px">
        <summary style="cursor:pointer;color:#93c5fd;font-size:13px">手動綁定（從排程頁的通知複製 LINE User ID）</summary>
        <form method="POST" action="/customers/` + strconv.FormatInt(c.ID, 10) + `/line-bind"
              style="margin-top:10px;display:flex;gap:8px;align-items:center;flex-wrap:wrap">
          <input name="line_user_id" placeholder="U1234567890abcdef" required
                 style="flex:1;min-width:240px;padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0">
          <button class="btn btn-p" type="submit">` + lineBindButtonLabel(c) + `</button>
        </form>
        <p style="color:#64748b;font-size:12px;margin:8px 0 0">
          同一個 LINE 帳號不能綁兩個客戶——那會讓對方看到不屬於他的維修紀錄，系統會直接擋下。
        </p>
      </details>
    </div>`)
	return b.String()
}

func lineBindButtonLabel(c model.Customer) string {
	if strings.TrimSpace(c.LineUserID) != "" {
		return "重新綁定"
	}
	return "綁定"
}

// telegramBindingBlock 產生編輯頁的 Telegram 綁定區塊。
func telegramBindingBlock(c model.Customer) string {
	var b strings.Builder
	b.WriteString(`<div class="card">
      <h2>Telegram 綁定</h2>`)

	if strings.TrimSpace(c.TelegramUserID) != "" {
		b.WriteString(`<p style="margin:0 0 10px">
        <span class="pill p-done">已綁定</span>
        <code style="margin-left:8px">` + htmlEsc(c.TelegramUserID) + `</code>
      </p>
      <p style="color:#94a3b8;font-size:12.5px;margin:0 0 12px">
        此客戶已可在 Telegram 傳訊息查詢維修服務。
      </p>
      <form method="POST" action="/customers/` + strconv.FormatInt(c.ID, 10) + `/telegram-unbind"
            onsubmit="return confirm('確定取消 Telegram 綁定？')">
        <button class="btn btn-d" type="submit">取消綁定</button>
      </form>`)
	} else {
		b.WriteString(`<p style="margin:0 0 10px"><span class="pill">未綁定</span></p>`)
	}

	b.WriteString(`
      <p style="color:#94a3b8;font-size:12.5px;margin:12px 0">
        <b>建議讓客戶自己綁</b>：請客戶在 Telegram 傳送手機號碼，系統會比對客戶資料上的電話並自動連結。
        若電話對不到或同號多筆，再由櫃檯手動綁定。
      </p>
      <details style="margin-top:6px">
        <summary style="cursor:pointer;color:#93c5fd;font-size:13px">手動綁定</summary>
        <form method="POST" action="/customers/` + strconv.FormatInt(c.ID, 10) + `/telegram-bind"
              style="margin-top:10px;display:flex;gap:8px;align-items:center;flex-wrap:wrap">
          <input name="telegram_user_id" placeholder="123456789" required
                 style="flex:1;min-width:240px;padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0">
          <button class="btn btn-p" type="submit">` + telegramBindButtonLabel(c) + `</button>
        </form>
        <p style="color:#64748b;font-size:12px;margin:8px 0 0">
          請客戶在 Telegram 搜尋 @userinfobot 查詢 chat ID。同一個 Telegram 帳號不能綁兩個客戶。
        </p>
      </details>
    </div>`)
	return b.String()
}

func telegramBindButtonLabel(c model.Customer) string {
	if strings.TrimSpace(c.TelegramUserID) != "" {
		return "重新綁定"
	}
	return "綁定"
}
