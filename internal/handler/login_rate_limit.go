package handler

import (
	"net"
	"net/http"
	"os"
	"strings"
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
	maxKeys  int
}

var loginLimiter = &loginRateLimiter{
	attempts: make(map[string][]time.Time),
	maxFails: 5,
	window:   15 * time.Minute,
	lockout:  15 * time.Minute,
	maxKeys:  10000,
}

func (l *loginRateLimiter) isLocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// 清理過期紀錄
	l.sweepLocked(now)
	return len(l.attempts[ip]) >= l.maxFails
}

func (l *loginRateLimiter) sweepLocked(now time.Time) {
	for key, times := range l.attempts {
		recent := times[:0]
		for _, t := range times {
			if now.Sub(t) < l.window {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(l.attempts, key)
		} else {
			l.attempts[key] = recent
		}
	}
}

func (l *loginRateLimiter) recordFail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweepLocked(now)
	if _, ok := l.attempts[ip]; !ok && len(l.attempts) >= l.maxKeys {
		var oldestKey string
		var oldest time.Time
		for key, times := range l.attempts {
			if len(times) == 0 || oldestKey == "" || times[0].Before(oldest) {
				oldestKey, oldest = key, times[0]
			}
		}
		if oldestKey != "" {
			delete(l.attempts, oldestKey)
		}
	}
	l.attempts[ip] = append(l.attempts[ip], now)
}

func (l *loginRateLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// clientIP 取得客戶端 IP。
func loginClientIP(r *http.Request) string {
	if os.Getenv("TRUSTED_PROXY") != "" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
