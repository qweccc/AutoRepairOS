# AutoRepairOS CSS 設計系統

全站樣式集中在 **`web/static/app.css`**（單一深色主題）。改樣式請改這裡，**勿在個別 templ 內重寫 `<style>`**（login 頁的少量額外樣式除外，僅覆寫表單版面）。

> 靜態檔實際由 `internal/handler/static/app.css` embed 進 binary（`//go:embed`）。
> 兩處內容相同，修改時**兩邊都要同步**（或改完 `cp web/static/app.css internal/handler/static/app.css`）。

---

## 色彩（深色主題）

| 用途 | 色碼 |
|---|---|
| 背景 | `#0f172a` |
| 卡片/Header 底 | `#1e293b` |
| 邊框/分隔 | `#334155` |
| 主色（連結、強調、主鈕） | `#38bdf8` |
| 文字主 | `#e2e8f0` |
| 文字次/標籤 | `#94a3b8` |
| 文字弱/佔位 | `#64748b` |
| hover 列底色 | `#263449` |

## 類別速查

### 版面
| 類別 | 用途 |
|---|---|
| `.card` | 內容卡片（每頁主要區塊） |
| `.wrap` | 登入頁置中容器 |
| `.full` | 表單內跨整行的欄位（`grid-column: 1/-1`） |

### 按鈕 `.btn` + 修飾
| 類別 | 用途 | 顏色 |
|---|---|---|
| `.btn-p` | 主要動作（新增、開工單） | 藍 |
| `.btn-s` | 次要動作（編輯、取消、搜尋） | 灰 |
| `.btn-d` | 危險（刪除、停用） | 紅 |
| `.btn-o` | 警告（調庫、停用切換） | 橘 |

### 狀態標籤 `.pill` + 色彩
| 類別 | 用途 | 顏色 |
|---|---|---|
| `.p-completed` | 完成/啟用/成功 | 綠 |
| `.p-progress` | 進行中/分類標籤 | 藍 |
| `.p-pending` | 停用/取消 | 黃 |
| `.p-waitparts` | 等待/待檢驗 | 橘 |
| `.p-apprno` | 逾期/失敗/錯誤 | 紅 |
| `.p-cancelled` | 已取消/中性 | 灰 |

### 表單 `.frm`
- `.frm` 用 CSS Grid，預設**兩欄**（手機自動單欄）
- 內部 `label` 為「上下」排版（文字在上、輸入框在下）
- 需跨整行的欄位加 `.full`
- 輸入元件：`input`/`select`/`textarea` 已統一深色樣式

### 表格
- `table`/`th`/`td` 已設深色樣式，`tr:hover` 高亮
- 數字右對齊：該 `<td>` 加 `.num`

### 其他
| 類別 | 用途 |
|---|---|
| `.stat` | 統計數字區塊（大數字 + 小標籤） |
| `.msg` + `.msg-ok`/`.msg-err` | 提示訊息（成功綠/錯誤紅） |
| `.empty` | 空狀態提示（置中灰字） |
| `.badge` | Header 角色標籤 |
| `code` | 等寬字（料號、單號） |

---

## 響應式斷點

| 斷點 | 調整 |
|---|---|
| `≤768px` | nav 字體縮小、main/card padding 縮小、表格字體 12px、stat 全寬、表單單欄 |
| `≤480px` | nav 更窄、字體 11px |

手機操作以「點選」為主，故按鈕與表格都保留足夠點擊面積。

---

## HTMX 函式庫

`web/static/htmx.min.js`（本地，非 CDN）— 離線區網部署也能用。
在 `components.Layout` 與 `login.templ` 的 `<head>` 以 `<script src="/static/htmx.min.js">` 載入。

**為什麼本地不用 CDN：** 車行是離線區網部署，CDN 斷線互動就全掛。已下載固定版本（2.0.4）內建。

---

## 新增頁面該怎麼用

1. 包在 `@components.Layout(...)` 裡 → 自動有 header/nav/app.css/htmx
2. 內容用 `.card` 區塊
3. 列表用 `<table>` + 數字欄加 `.num`
4. 操作鈕依語意選 `.btn-p`/`.btn-s`/`.btn-d`/`.btn-o`
5. 狀態用 `.pill` + 對應色彩類別
6. 表單用 `.frm`，跨整行欄位加 `.full`
7. 需要新樣式 → 加進 `app.css` 並更新本文件，勿內嵌 `<style>`
