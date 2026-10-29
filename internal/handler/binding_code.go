package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"autorepairos/internal/db"
)

// GenerateBindingCode 產生一次性綁定碼（POST /customers/{id}/generate-binding-code?platform=line|telegram）。
// 僅 owner/counter 可呼叫。碼顯示一次，10 分鐘有效。
func GenerateBindingCode(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	if platform != "line" && platform != "telegram" {
		platform = "telegram"
	}

	// 產生 6 位數碼
	code, err := randomDigits(6)
	if err != nil {
		http.Error(w, "產生碼失敗", http.StatusInternalServerError)
		return
	}

	// hash 儲存
	h := sha256.Sum256([]byte(code))
	codeHash := hex.EncodeToString(h[:])

	expiresAt := time.Now().Add(10 * time.Minute).Format("2006-01-02 15:04:05")

	actorID := int64(0)
	if u, uerr := currentUser(r); uerr == nil {
		actorID = u.ID
	}

	_, err = db.DB.Exec(`INSERT INTO binding_codes (platform, customer_id, code_hash, expires_at, created_by)
		VALUES (?, ?, ?, ?, ?)`, platform, id, codeHash, expiresAt, actorID)
	if err != nil {
		http.Error(w, "儲存碼失敗", http.StatusInternalServerError)
		return
	}

	// 回傳碼（僅此一次顯示）
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="msg msg-ok">綁定碼：<b>%s</b>（10 分鐘內有效，請客戶在 %s 傳送「綁定 %s」）</div>`,
		code, platform, code)
}

func randomDigits(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteByte('0' + b[i]%10)
	}
	return sb.String(), nil
}

// VerifyBindingCode 驗證綁定碼並綁定（由 webhook 呼叫）。
// 回傳 customerID, error。
func VerifyBindingCode(platform, code, chatOrLineID string) (int64, error) {
	h := sha256.Sum256([]byte(code))
	codeHash := hex.EncodeToString(h[:])

	var id, customerID int64
	var expiresAt string
	var usedAt *string
	var attempts int
	err := db.DB.QueryRow(`SELECT id, customer_id, expires_at, used_at, attempts
		FROM binding_codes WHERE platform = ? AND code_hash = ?`, platform, codeHash).
		Scan(&id, &customerID, &expiresAt, &usedAt, &attempts)
	if err != nil {
		return 0, fmt.Errorf("無效的綁定碼")
	}
	if usedAt != nil {
		return 0, fmt.Errorf("此碼已使用")
	}
	if attempts >= 5 {
		return 0, fmt.Errorf("嘗試次數過多")
	}
	// 檢查過期
	exp, err := time.Parse("2006-01-02 15:04:05", expiresAt)
	if err != nil || time.Now().After(exp) {
		db.DB.Exec(`UPDATE binding_codes SET attempts = attempts + 1 WHERE id = ?`, id)
		return 0, fmt.Errorf("綁定碼已過期")
	}

	// 遞增嘗試次數
	db.DB.Exec(`UPDATE binding_codes SET attempts = attempts + 1 WHERE id = ?`, id)

	// 綁定
	if platform == "telegram" {
		if err := bindTelegramUser(chatOrLineID, customerID); err != nil {
			return 0, err
		}
	} else if platform == "line" {
		if err := bindLineUser(chatOrLineID, customerID); err != nil {
			return 0, err
		}
	} else {
		return 0, fmt.Errorf("不支援的綁定平台")
	}

	// 標記已使用（使用交易避免競爭條件）
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("開始交易失敗")
	}
	res, err := tx.Exec(`UPDATE binding_codes SET used_at = CURRENT_TIMESTAMP WHERE id = ? AND used_at IS NULL`, id)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("標記綁定碼已使用失敗")
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		tx.Rollback()
		return 0, fmt.Errorf("此碼已使用")
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交交易失敗")
	}
	return customerID, nil
}
