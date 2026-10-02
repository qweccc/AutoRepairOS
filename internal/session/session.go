package session

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

const (
	cookieName   = "session_token"
	sessionTTL   = 7 * 24 * time.Hour
	tokenEntropy = 32 // bytes
)

// Start 為指定使用者建立新 session，回傳 token。
func Start(userID int64) (string, error) {
	raw := make([]byte, tokenEntropy)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("產生 session token 失敗: %v", err)
	}
	// base64.RawURLEncoding：只含 URL-safe 字元，可直接放 cookie
	token := base64.RawURLEncoding.EncodeToString(raw)

	if _, err := db.DB.Exec(
		"INSERT INTO sessions (id, user_id, created_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		token, userID,
	); err != nil {
		return "", fmt.Errorf("寫入 session 失敗: %v", err)
	}

	return token, nil
}

// Lookup 由 token 找出使用者的 session，未登入或過期回傳 error。
func Lookup(token string) (int64, error) {
	if strings.TrimSpace(token) == "" {
		return 0, sql.ErrNoRows
	}

	var userID int64
	var createdAt string
	err := db.DB.QueryRow(
		"SELECT user_id, created_at FROM sessions WHERE id = ?", token,
	).Scan(&userID, &createdAt)
	if err != nil {
		return 0, err
	}

	if expired(createdAt) {
		Delete(token)
		return 0, sql.ErrNoRows
	}

	return userID, nil
}

// Delete 移除指定 session（登出）。
func Delete(token string) {
	if token == "" {
		return
	}
	db.MustExec("DELETE FROM sessions WHERE id = ?", token)
}

// DeleteForUser 移除某使用者的所有 session（改密或停用帳號時用）。
func DeleteForUser(userID int64) {
	db.MustExec("DELETE FROM sessions WHERE user_id = ?", userID)
}

// expired 判斷 session 是否超過 TTL。created_at 可能是 SQL datetime 或 RFC3339。
func expired(createdAt string) bool {
	t, err := parseTime(createdAt)
	if err != nil {
		// 格式解析不了時保守視為有效，避免誤踢使用者；呼叫端可另外清理。
		return false
	}
	return time.Since(t) > sessionTTL
}

// parseTime 兩種格式都接受：driver 寫入的 SQL datetime 與 RFC3339。
func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
}

// SetCookie 把 token 寫進 HttpOnly cookie。
func SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // 本機 HTTP 部署；上線走 TLS 時應改為 true
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// ClearCookie 清除瀏覽器端的 cookie。
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// TokenFromRequest 取出請求中的 session token。
func TokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// ConstantTimeEqual 給需要比較 token 的地方用（例如 CSRF 類用途）。
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

var _ model.User
