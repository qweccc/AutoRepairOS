package handler

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"autorepairos/internal/model"
)

//go:embed all:static
var staticFS embed.FS

// RegisterStatic 掛載靜態資源。唯一來源是 internal/handler/static/ 這份
func RegisterStatic(mux http.Handler) {
	if r, ok := mux.(*chi.Mux); ok {
		registerStaticChi(r)
		return
	}
	if sm, ok := mux.(*http.ServeMux); ok {
		registerStaticServeMux(sm)
		return
	}
	// For other routers with chi-like methods
	if h, ok := mux.(interface {
		Get(string, ...interface{})
	}); ok {
		sub, err := fs.Sub(staticFS, "static")
		if err != nil {
			return
		}
		for _, name := range []string{"sw.js", "offline.js", "offline.css"} {
			n := name
			h.Get("/"+n, RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				serveStatic(w, req, sub, n)
			})).ServeHTTP)
		}
		h.Get("/static/app.css", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			serveStatic(w, req, sub, "app.css")
		}).ServeHTTP)
		h.Get("/static/htmx.min.js", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			serveStatic(w, req, sub, "htmx.min.js")
		}).ServeHTTP)
		// Try to register wildcard
		if h2, ok2 := mux.(interface {
			HandleFunc(string, func(http.ResponseWriter, *http.Request))
		}); ok2 {
			h2.HandleFunc("/static/*", RequireLogin(func(w http.ResponseWriter, req *http.Request) {
				serveSub(w, req, sub)
			}))
			return
		}
		return
	}
}

func registerStaticChi(r *chi.Mux) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return
	}
	for _, name := range []string{"sw.js", "offline.js", "offline.css"} {
		n := name
		r.Get("/"+n, RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			serveStatic(w, req, sub, n)
		})))
	}
	r.Get("/static/app.css", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		serveStatic(w, req, sub, "app.css")
	}))
	r.Get("/static/htmx.min.js", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		serveStatic(w, req, sub, "htmx.min.js")
	}))
	r.Get("/static/*", RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		serveSub(w, req, sub)
	})))
}

func registerStaticServeMux(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return
	}

	// 離線資產：sw.js、offline.js、offline.css
	// 路徑前綴必須用完整路徑（Go 1.22+ ServeMux 不接受裸檔名，例如 "GET sw.js" 會 panic）。
	for _, name := range []string{"sw.js", "offline.js", "offline.css"} {
		mux.Handle("GET "+"/"+name, RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveStatic(w, r, sub, name)
		})))
	}
	// 公開靜態資源（登入頁就要載入，不可 RequireLogin）：
	// app.css 全站樣式、htmx.min.js 互動函式庫。二者無敏感資料，公開安全。
	for _, name := range []string{"app.css", "htmx.min.js"} {
		mux.Handle("GET /static/"+name, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveStatic(w, r, sub, name)
		}))
	}
	// 其餘靜態資源由 sub 直接掛載完整路徑（皆來自 embed 的 static 目錄）。
	// 需包成 http.HandlerFunc：sub 是 fs.FS，不是 http.FileSystem，所以要手動 Open 轉成 http.File。
	mux.HandleFunc("/static/", RequireLogin(func(w http.ResponseWriter, r *http.Request) {
		serveSub(w, r, sub)
	}))
}

// serveSub 用 fs.FS 提供檔案，只接受 /static/ 開頭的路徑。
func serveSub(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	// 防路徑外溢：只允許前綴為 /static/ 的路徑。
	pathPrefix := "/static/"
	if !strings.HasPrefix(r.URL.Path, pathPrefix) {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, pathPrefix)
	if rel == "" || rel == "." || rel == ".." {
		http.NotFound(w, r)
		return
	}
	f, err := sub.Open(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	// 必須 return，否則 http.NotFound 寫出的 404 狀態碼會被後續的
	// w.WriteHeader(http.StatusOK) 與內容寫入覆蓋，變成「狀態碼 404 但 body 有檔案」。
	switch stat.Mode().Type() {
	case fs.ModeSymlink, fs.ModeDir, fs.ModeNamedPipe, fs.ModeSocket, fs.ModeDevice:
		http.NotFound(w, r)
		return
	}
	// 依擴充檔設定 Content-Type
	switch path.Ext(rel) {
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

func serveStatic(w http.ResponseWriter, r *http.Request, sub fs.FS, name string) {
	body, err := fs.ReadFile(sub, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch path.Ext(name) {
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// EnsureStaticNonEmpty 讓編譯器保留 embed 指示；若目錄為空則明確報錯。
var _ = strings.TrimSpace
var _ model.User
