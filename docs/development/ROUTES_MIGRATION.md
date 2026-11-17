# 路由遷移計畫（main.go → internal/routes）

## 目標
將 `main.go` 中 ~400 條 `chi` 路由逐步遷移到 `internal/routes.New()`，以便 `httptest` 直接測試路由與 middleware。

## 原則
- 每階段搬遷一小批路由，並從 `main.go` 刪除對應註冊，避免 ServeMux 重複 pattern。
- `routes.Deps` 逐步擴充所需依賴（cfg、db handle 等）。
- 每階段執行 `go build ./...`、`go test ./...`、`templ generate -check`。

## 階段
- Phase 0：`routes.New()` 回傳空 mux（已完成）。
- Phase 1：公開/唯讀 API 路由（進行中）。
- Phase 2：管理 CRUD 路由。
- Phase 3：列印/報表路由。
- Phase 4：main.go 僅保留 middleware/啟動邏輯。

## 進度
- 2026-11-04：建立計畫文件。
