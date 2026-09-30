# AutoRepair OS - 快速開始指南

## 環境需求
- Go 1.22+
- Node.js（用於 Tailwind CSS 建構）
- Docker + docker-compose（用於容器部署）

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
go run ./cmd/seed
```

### 4. 啟動服務器
```bash
go run ./cmd/server
```

服務器將在 `http://localhost:8090` 啟動（主機埠；容器內為 8080）。本機 8080 保留給 llama-server。

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

## API 端點

### 身份驗證
- `GET /login` - 登入頁面
- `POST /login` - 登入
- `GET /logout` - 登出
- `GET /api/me` - 獲取當前用戶資訊

### 客戶
- `GET /api/customers` - 獲取客戶列表

### 工單
- `GET /api/workorders` - 獲取工單列表
- `POST /api/workorders` - 建立新工單

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
