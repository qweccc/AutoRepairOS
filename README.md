# AutoRepair OS - 快速開始指南

## 環境需求
- Go 1.22+

> 純 Go 單檔服務，不需要 Node.js 或 Docker 也能在本機直接跑。Node.js / Docker 僅在容器化部署時選用。

## 初始設置

### 1. 克隆專案
```bash
cd AutoRepairOS
```

### 2. 下載依賴
```bash
go mod download
```

### 3. 執行種子數據（初始化預設帳號）
```bash
go run ./seed
```

### 4. 啟動服務器
```bash
go run .
```

服務器將在 `http://localhost:8090` 啟動（主機埠；容器內為 8080）。本機 8080 保留給 llama-server。

> 若本机沒有 Docker，可直接用已編譯的 `autos.exe`（`go build -o autos.exe .`）或 `./autos.exe` 執行。

## 預設帳號

| 角色 | 帳號 | 密碼 |
|------|------|------|
| Owner | owner | Owner001! |
| Counter | counter | Counter001! |
| Technician | tech | Tech001! |

## Docker 部署

### 構建和啟動
```bash
docker compose up -d
```

### 查看日誌
```bash
docker compose logs -f
```

### 停止服務
```bash
docker compose down
```

## HTTP 端點

> 服務為單頁式（無 RESTful `/api/` 前綴）。下表為實際路由。

### 身份驗證
- `GET /login` - 登入頁面
- `POST /login` - 登入（帳號 / 密碼）
- `GET /logout` - 登出
- `GET /api/me` - 獲取當前用戶資訊
- `GET /{$}` 與 `GET /index.html` - 首頁

### 客戶
- `GET /customers` - 客戶列表（帶搜尋 `?q=`）
- `POST /customers` - 新增客戶
- `GET /customers/{id}/edit` - 編輯頁
- `POST /customers/{id}` - 更新客戶
- `GET /customers/{id}/delete` - 刪除

### 車輛
- `GET /vehicles` - 車輛列表
- `POST /vehicles` - 新增車輛
- `GET /vehicles/{id}/edit` - 編輯頁
- `POST /vehicles/{id}` - 更新車輛
- `GET /vehicles/{id}/delete` - 刪除

### 工單
- `GET /work-orders` - 工單列表（狀態分頁 / 搜尋）
- `GET /work-orders/new` - 開立新工單
- `POST /work-orders` - 建立工單
- `GET /work-orders/{id}` - 工單詳情
- `POST /work-orders/{id}/status` - 變更狀態
- `POST /work-orders/{id}/approval` - 核准
- `POST /work-orders/{id}/sop` - 切換 SOP
- `POST /work-orders/{id}/items` - 加入項目
- `GET /work-orders/{id}/items/{item_id}/delete` - 刪除項目

### 保險公司
- `GET /insurance-companies` - 保險公司列表
- `POST /insurance-companies` - 新增保險公司
- `GET /insurance-companies/{id}/edit` - 編輯頁
- `POST /insurance-companies/{id}` - 更新保險公司
- `GET /insurance-companies/{id}/delete` - 停用（軟刪除）

### LINE Webhook
- `POST /webhook/line` - LINE 接收端點

## 資料庫

SQLite 資料庫文件位於 `data/autorepair.db`。支援 WAL 模式以提高併發性能。

## 開發

### 執行測試
```bash
go test ./...
```

### 程式碼格式化
```bash
go fmt ./...
```

### 依賴管理
```bash
go mod tidy
```

## 配置

可通過環境變數配置：

| 變數 | 預設值 | 說明 |
|------|--------|------|
| PORT | 8080 | 服務器端口（容器內）；Docker 主機埠為 8090 |
| DB_PATH | file:data/autorepair.db?_journal_mode=WAL&_busy_timeout=5000 | 資料庫路徑 |
| SESSION_SECRET | autorepair-dev-secret-change-in-production | session 加密密鑰 |
| RUN_SEED | false | 是否執行種子數據 |

## 注意事項

1. **安全性**：生產環境中請更改預設密碼和 SESSION_SECRET
2. **備份**：定期備份 data/autorepair.db 文件
3. **日誌**：查看日誌以診斷問題
