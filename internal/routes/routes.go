// Package routes 組裝 HTTP 路由（含 middleware）。
// Phase 0：先提供 New() 以利測試直接用 httptest.NewServer(routes.New())，
// 後續逐步把 main.go 的路由遷移過來。
package routes

import (
	"net/http"

	"autorepairos/internal/handler"
)

// Deps 預留給日後注入設定、服務等相依。
type Deps struct{}

// New 建立應用路由，外層需加上 middleware（如 CSRFProtection）。
// 目前暫時回傳空 mux，實際路由仍由 main 註冊；等遷移完成再整併。
func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	// TODO: 將 main.go 的路由逐步遷移至此
	_ = handler.CSRFProtection(mux)
	return mux
}
