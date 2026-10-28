package handler

import (
	"net/http"
	"sync"
	"time"
)

// loginRateLimiter 簡單的記憶體登入速率限制。
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	maxFails int
	window   time.Duration
	lockout  time.Duration
}

var loginLimiter = &loginRateLimiter{
	attempts: make(map[string][]time.Time),
	maxFails: 5,
	window:   15 * time.Minute,
	lockout:  15 * time.Minute,
}

func (l *loginRateLimiter) isLocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// 清理過期紀錄
	var recent []time.Time
	for _, t := range l.attempts[ip] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	l.attempts[ip] = recent
	return len(recent) >= l.maxFails
}

func (l *loginRateLimiter) recordFail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[ip] = append(l.attempts[ip], time.Now())
}

func (l *loginRateLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// clientIP 取得客戶端 IP。
func loginClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
