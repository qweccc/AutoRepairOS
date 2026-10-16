package session

import (
	"context"
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

type csrfContextKey struct{}

// WithCSRFToken adds a session's CSRF token to the request context for rendering.
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfContextKey{}, token)
}

// CSRFTokenFromContext returns the CSRF token attached by CSRFProtection.
func CSRFTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}

// Start 為指定使用者建立新 session，回傳 token。
func Start(userID int64) (string, error) {
	raw := make([]byte, tokenEntropy)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("產生 session token 失敗: %v", err)
	}
	// base64.RawURLEncoding：只含 URL-safe 字元，可直接放 cookie
	token := base64.RawURLEncoding.EncodeToString(raw)

	csrfRaw := make([]byte, tokenEntropy)
	if _, err := rand.Read(csrfRaw); err != nil {
		return "", fmt.Errorf("產生 CSRF token 失敗: %v", err)
	}
	csrfToken := base64.RawURLEncoding.EncodeToString(csrfRaw)

	if _, err := db.DB.Exec(
		"INSERT INTO sessions (id, user_id, csrf_token, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)",
		token, userID, csrfToken,
	); err != nil {
		return "", fmt.Errorf("寫入 session 失敗: %v", err)
	}

	return token, nil
}

// CSRFToken returns the token stored with a session, initializing older sessions
// created before CSRF protection was installed.
func CSRFToken(sessionToken string) (string, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return "", sql.ErrNoRows
	}

	var token string
	err := db.DB.QueryRow("SELECT csrf_token FROM sessions WHERE id = ?", sessionToken).Scan(&token)
	if err != nil {
		return "", err
	}
	if token != "" {
		return token, nil
	}

	raw := make([]byte, tokenEntropy)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("產生 CSRF token 失敗: %v", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	if _, err := db.DB.Exec(
		"UPDATE sessions SET csrf_token = ? WHERE id = ? AND csrf_token = ''",
		token, sessionToken,
	); err != nil {
		return "", fmt.Errorf("儲存 CSRF token 失敗: %v", err)
	}

	if err := db.DB.QueryRow("SELECT csrf_token FROM sessions WHERE id = ?", sessionToken).Scan(&token); err != nil {
		return "", err
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
func SetCookie(w http.ResponseWriter, r *http.Request, token string) {
	secure := requestIsSecure(r)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// ClearCookie 清除瀏覽器端的 cookie。
func ClearCookie(w http.ResponseWriter, r *http.Request) {
	secure := requestIsSecure(r)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func requestIsSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
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
