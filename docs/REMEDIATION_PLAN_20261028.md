# AutoRepairOS 修復計畫與 UI 測試指南（2026-10-28）

> 對應報告：`docs/REVIEW_20261028.md`
> 本文件只提出**修正方向、具體作法、測試方式**，**尚未修改任何程式碼**。
> 文中程式碼片段都是草案（pseudo-code），不是已套用的變更。

---

## 0. 先講結論

### 0-1 最小可上線集（建議）

如果目標是「可以開始跟客人收錢」，我建議這個組合，其餘排後面：

| 類別 | 項目 | 為什麼非做不可 |
|---|---|---|
| 金流 | P0-1、P0-2、P0-3、P0-5 | 會直接造成營收/庫存/帳務對不上 |
| 金流 | P0-6、P0-7 | 單號撞號、客戶欠款失真 |
| 帳號 | P0-4 | 客戶 LINE 帳號可被陌生人接管 |
| 權限 | P1-1、P1-2、P1-3、P1-4 | 技師可看金額、他人個資、他人工單 |
| 工程 | CI 跑 test/vet + 本文件第 6 節的測試骨架 | 沒有安全網，後面的修復會互相踩 |

粗估：**5～7 個工作天**（不含 P2 與完整 E2E）。全部做完（含 P2、E2E、文件）約 **2～3 週**。

### 0-2 三個原則

1. **先寫失敗測試，再修。** 這幾個 bug 都是「沒有任何測試在守」才會活到現在；修完沒有測試，下一次重構會再回來。
2. **金額與狀態的真相只有一個地方。** 目前 `unpaid_amount`、`total_spent`、`status` 都有人在寫，但沒有单一口徑；長期要靠「衍生計算」而不是「到處加減」。
3. **權限用「預設拒絕 + 集中授權函式」。** 現在每個 handler 自己記得檢查，於是列印與 worklog 就漏了。與其補 3 個 `if`，不如補 1 個所有工單相關 handler 都會呼叫的 `authorizeWorkOrder`。

---

## 1. 動工前的安全網（Phase 0，0.5～1 天）

### 1.1 CI 目前只 build，沒有跑測試

`.github/workflows/ci.yml` 現在只有：

- `go build ./...`
- `docker build`

也就是說 **`go test` 從來沒在 CI 跑過**，這正是 `TestManualRoleVisibilityMatchesNav` 可以紅著存在的原因。

建議改成：

```yaml
- name: Vet
  run: go vet ./...
- name: Test
  run: go test ./internal/... ./cmd/... ./web/... -count=1 -coverprofile=coverage.out
- name: Templ drift
  run: |
    go run github.com/a-h/templ/cmd/templ@v0.3.1100 generate ./...
    git diff --exit-code
```

補充：

- 不要用 `go test ./...` 當 CI 指令。`release/` 與 `unpublished/tests/go` 都在 module 內，會被一起編譯；本機 `go test ./...` 已經因此把 release 的測試拉進來。要嘛把這些資料夾移出 module，要嘛 CI 明確列 package。
- `-race` 建議只在 Linux CI 跑。Windows 的 `-race` 需要 cgo/GCC，本機不一定有。
- CI 也應該跑 `docker build` 之外的「容器內啟動 + `/login` 200」煙霧測試（見 6.7）。

### 1.2 把路由從 `main.go` 抽出來（這對 UI 測試是關鍵）

現在 `main.go` 從第 87 行開始直接 `mux.HandleFunc(...)`，測試只能「直接呼叫 handler」，所以測不到：

- 路由本身有沒有掛對（`GET /work-orders/{id}/receipt` 是 `AllStaff` 還是 `CounterUp`）
- CSRF middleware 與 auth wrapper 的組合
- 404/405、方法錯誤、redirect 行為

建議新增 `internal/routes/routes.go`：

```go
// New 建立完整路由（含 middleware），main 只負責啟動與關閉。
func New(deps Deps) http.Handler {
    mux := http.NewServeMux()
    registerPublic(mux, deps)
    registerWorkOrders(mux, deps)
    // ...
    return handler.CSRFProtection(mux)
}
```

`main.go` 只留：

```go
srv := &http.Server{Addr: ":" + port, Handler: routes.New(...)}
```

測試就能 `httptest.NewServer(routes.New(testDeps))`，用真的 cookie jar 跑完整流程。這件事本身沒有使用者可見變化，但**後面每個 UI 測試都會用到**。

### 1.3 測試 DB 改用真的 migration

現在 `internal/handler/*_test.go` 各自手寫 `CREATE TABLE`（例如 `csrf_test.go:21`、`photos_upload_test.go:32`）。這有兩個問題：

- 測試 schema 與正式 schema 會漂移（漏欄位、型別不同）。
- 新 migration 加欄位時，測試不會自動跟上。

建議加一個 helper：

```go
func newMigratedTestDB(t *testing.T) *sql.DB {
    t.Helper()
    path := filepath.Join(t.TempDir(), "test.db")
    d, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
    // ...
    orig := db.DB
    db.DB = d
    t.Cleanup(func() { d.Close(); db.DB = orig })
    if err := db.Migrate(); err != nil { t.Fatal(err) }
    return d
}
```

好處：每個測試都是**真 schema + 真 migration**，而且 `t.TempDir()` 自動清理，不會碰到 `data/autorepair.db`。

> 注意：`db.Migrate()` 吃全域 `db.DB`，所以要在測試內替換再還原（現有 `testhelper_test.go:15` 已經是這個模式）。

### 1.4 共用測試輔助（建議新檔 `internal/handler/testsupport_test.go`）

```go
type testClient struct {
    t      *testing.T
    srv    *httptest.Server
    jar    http.CookieJar
    csrf   string
    userID int64
}

func loginAs(t *testing.T, srv *httptest.Server, username, password string) *testClient
func (c *testClient) get(path string) *http.Response
func (c *testClient) postForm(path string, form url.Values) *http.Response
func (c *testClient) csrfFrom(html string) string // regexp: name="_csrf" value="..."
```

- `http.Client` 用 `cookiejar.New(nil)`，瀏覽器怎麼帶 cookie，測試就怎麼帶。
- CSRF token 從登入後的 HTML 抽，或直接用 `session.CSRFToken(token)`（`csrf_test.go:34` 已有範例）。
- 登入後回傳的 client 要記住角色，方便做「角色 × 路由」矩陣。

### 1.5 先把基準線弄綠

1. 在 `docs/USER_GUIDE_zh-TW.md:191` 的技師清單補「SOP 模板」，讓 `TestManualRoleVisibilityMatchesNav` 變綠。
2. 跑一次 `go test ./internal/... -cover`，把各 package 覆蓋率記在 CI artifact。
3. 訂目標：`service` 80%（金流核心）、`handler` 50%、`session` 80%、`db` 60%。不用一次到位，但每次修 P0 都要往上加。

### 1.6 每次 migration 的固定流程

1. 先備份：`data/autorepair.db` 複製一份（正式機用 `VACUUM INTO` 或停機複製）。
2. 在**備份副本**上跑新 migration，確認時間與結果。
3. migration 只往前加，不改舊檔；編號接 `047` 之後。
4. 上線前後各跑一次對帳查詢（見第 5 節），數值要對得上。
5. `templ` 有改 `.templ` 就要重生成，並把 `*_templ.go` 一起進版控：

```powershell
go run github.com/a-h/templ/cmd/templ@v0.3.1100 generate ./...
```

> 目前環境沒有安裝 `templ` CLI，但 `web/pages/*_templ.go` 已進版控（42 個檔）。用 `go run ...@版本` 可以避免每台機器版本不同。

---

## 2. P0 修復（逐項）

### P0-1／P0-2 收款不應提前把工單結帳

**根因**

`internal/service/checkout.go:322-335` 的 `receiptCompletesWorkOrder` 只看「收款總額 >= 工單總額」，沒有看工單狀態，也沒有看總額是否為 0。於是：

- `pending` / `in_progress` 工單只要收到足額，就 `checkoutTx`（`:138-149`、`:210-221`）。
- 總額 0 的工單，`received(0) >= total(0)` 永遠成立，任何一筆 0 元收款都會觸發結帳。

**修正方向**

把「可結帳」定義成三個條件同時成立：

1. 工單狀態是 `completed`（本系統的「待結帳」）
2. `total_amount > 0`
3. 有效收款總額 >= `total_amount`

另外兩個入口要一起改：

- `UpdateReceipt`（`:210-221`）也會觸發結帳；改收款金額不該讓舊工單突然結帳。
- `Service.Checkout`（`:56-60`）目前可被直接呼叫；若沒有 UI 入口，建議加「僅 owner/counter + 明確確認」或直接移除。

**作法（建議 Option A：允許預收，但不提前結帳）**

```go
func receiptCompletesWorkOrder(ctx context.Context, tx sqlExecutor, workOrderID int64) (bool, error) {
    var status string
    var total, received float64
    err := tx.QueryRowContext(ctx, `
        SELECT wo.status, wo.total_amount,
               COALESCE((SELECT SUM(r.pay_total) FROM receivables r
                         WHERE r.work_order_id = wo.id AND r.voided = 0), 0)
        FROM active_work_orders wo
        WHERE wo.id = ?`, workOrderID).Scan(&status, &total, &received)
    if errors.Is(err, sql.ErrNoRows) { return false, ErrNotFound }
    if err != nil { return false, fmt.Errorf("calculate work order receipts: %w", err) }
    if status != "completed" || total <= 0 { return false, nil }
    return received+0.005 >= total, nil   // 先容忍 0.005，長期改整數分
}
```

Option A 的行為：

- 工單還在 `pending` / `in_progress` / `waiting_parts`：收款照記，但不結帳、不扣庫存。UI 訊息改成「已記錄預收（工單尚未完工）」。
- 工單轉 `completed` 時，若已預收足額，才在那一刻結帳扣料。這符合「完工才扣料」的現實。
- 若要更保守，可採 **Option B：收款頁只允許關聯 `completed` 工單**，預收一律不關聯工單、備註寫明。Option B 改動小，但櫃檯要多一步「把預收款分配到工單」，比較容易漏。

我建議 **Option A**，因為它同時解決「先收訂金」與「事後補收」兩種情境，且不需要新資料表。

**UI 調整**

- `web/pages/stage7_forms.templ:349`：工單欄位改成「可搜尋的已完成工單 + 顯示狀態 badge」；非 `completed` 的工單在選單中標示「預收（完工後才結帳）」。
- 收款列表與工單頁顯示「已收 / 應收 / 尚欠」，不要只顯示總額。
- 工單狀態改成 `completed` 後，如果有足額預收，顯示「將自動結帳」提示與明確按鈕（避免「存檔即扣料」的驚嚇）。

**測試（先寫，會紅）**

`internal/service/checkout_test.go` 擴充成表驅動：

| 工單狀態 | 總額 | 收款 | 預期 status | 預期庫存 | 預期 total_spent |
|---|---|---|---|---|---|
| pending | 1000 | 1000 | pending | 不變 | 不變 |
| in_progress | 1000 | 1000 | in_progress | 不變 | 不變 |
| waiting_parts | 1000 | 500 | waiting_parts | 不變 | 不變 |
| completed | 1000 | 500 | completed | 不變 | 不變 |
| completed | 1000 | 1000 | paid | -qty | +1000 |
| completed | 0 | 0 | completed | 不變 | 不變 |
| completed | 1000 | 1200 | paid | -qty | +1000（超收另計） |

再加：

- `TestReceiptDoesNotCheckoutPendingWorkOrder`
- `TestZeroTotalWorkOrderNeverAutoChecksOut`
- `TestUpdateReceiptDoesNotCheckoutBeforeCompletion`
- `TestDepositThenCompleteTriggersCheckoutOnce`（Option A）

**驗收**

- 上述測試全綠，且 `checkout_test.go:99-100` 舊的「unpaid == 0」斷言要依新語意改掉。
- 手動：建 `pending` 工單 → 收全額 → 狀態仍是待處理、庫存沒動 → 按完工 → 變成已結帳、庫存才扣。

**風險**

- 若現場已經習慣「先收款就等於結帳」，行為會改變；上線前要公告，並在收款成功訊息明確寫出「尚未結帳」。
- 舊資料中已經被提前結帳的工單不會自動回復；需要一次性報表列出「status=paid 但 completed_at 為空」的單子，人工確認。

---

### P0-3 作廢收款要能回沖

**根因**

`VoidReceipt`（`checkout.go:228-258`）只把 `voided=1`，註解也明說「不回沖」。結果：

- 工單仍是 `paid`，但實際收款不足。
- 庫存已扣、`total_spent` 已加，全部留著。
- 收款頁「含作廢」看得到紀錄，但帳對不上。

**修正方向**

作廢必須是**補償交易（compensating transaction）**，與原本結帳在同一交易內完成。分兩步：

1. 先把客戶聚合數字改成「衍生計算」（見 P0-7），作廢後自動正確。
2. 再處理工單狀態與庫存回沖。

**作法**

```go
func (s *Service) VoidReceipt(ctx context.Context, id int64) error {
    return s.withTx(ctx, "void receipt", func(tx sqlExecutor) error {
        // 1. 鎖定並讀取收款 + 關聯工單
        // 2. UPDATE receivables SET voided=1, void_reason=?, voided_by=?, voided_at=?
        //    WHERE id=? AND voided=0
        // 3. 重算該工單的有效收款總額
        // 4. 若工單 status='paid' 且 received < total：
        //      a. 逐項回沖庫存（stock_quantity += qty, reserved_quantity += qty）
        //      b. 寫 inventory_movements（reason='作廢回沖', source='void_receipt'）
        //      c. UPDATE work_orders SET status='completed'
        // 5. 不需手動改 customers：聚合改成查詢時計算
        return nil
    })
}
```

重點決策：

- **作廢要不要退庫存？** 要。否則帳說沒收錢、料卻已扣，庫存永久少一筆。
- **如果料已經被下一個工單用掉？** 回沖後可能變成負庫存。這是事實，不要偷偷 clamp 成 0；讓 `inventory/negative` 頁面看得到，並在作廢時警告「此料目前庫存不足，回沖後將為負」。
- **誰能作廢？** 建議 owner，或 counter + 二次確認 + 原因。目前 `VoidReceivable` 是 `CounterUp`，任何櫃檯都能按，且沒有原因欄位。
- **已開發票/已請款？** 目前沒有發票模組，先不處理；未來接電子發票時，作廢要連動折讓。

**Migration**

`receivables` 加欄位（新的 `048_*.up.sql`）：

```sql
ALTER TABLE receivables ADD COLUMN void_reason TEXT;
ALTER TABLE receivables ADD COLUMN voided_by INTEGER;
ALTER TABLE receivables ADD COLUMN voided_at TEXT;
```

**測試**

- `TestVoidFullReceiptReversesCheckout`：completed 1000 → 收 1000 → paid、庫存 -2 → 作廢 → completed、庫存 +2、`inventory_movements` 多一筆負負得正的回沖、客戶應收回到 1000。
- `TestVoidPartialKeepsPaidIfStillCovered`：兩筆各 500，作廢一筆 → 仍 paid、庫存不動。
- `TestVoidRejectsAlreadyVoided`、`TestVoidRequiresReason`、`TestVoidByTechForbidden`。
- 手動：作廢後到工單頁、庫存異動頁、客戶頁三處對數字。

**風險**

- 回沖會改變歷史庫存；若現場已盤點過，會出現差異。建議上線前先跑一次「已 paid 但收款不足」的清單，人工決定哪些要補收、哪些要作廢。
- 若未來接會計系統，作廢應產生反向傳票，而不是刪除。

---

### P0-4 LINE 手機綁定改成「一次性綁定碼」

**根因**

`internal/handler/line_webhook.go:271-310`：客戶傳任何符合電話格式的訊息，只要該電話在 `customers` 唯一，就直接 `bindLineUser(ev.UserID, customerID)`。等於：

- 知道（或猜到）客戶手機的人，用自己的 LINE 傳一次號碼，就接管該客戶的 passbook。
- 回覆訊息會說「找到 N 位」與比對結果，可用來枚舉客戶是否存在。
- 原本已綁定的客戶會被靜默改綁（取決於 `bindLineUser` 的 UPSERT 行為），舊帳號不會收到通知。

**修正方向**

改成 **櫃檯產生一次性碼 → 客戶在 LINE 輸入碼**，並且：

- 不因電話自動綁定。
- 一碼一用、短 TTL、綁定時檢查雙方是否已被佔用。
- 失敗回應一律相同，不洩漏「這支電話存不存在」。
- 重綁需要櫃檯/owner 確認，並通知舊 LINE 帳號。

**作法**

新增 migration：

```sql
CREATE TABLE IF NOT EXISTS line_binding_codes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id  INTEGER NOT NULL,
    code_hash    TEXT NOT NULL,          -- SHA-256(code)，不存明碼
    expires_at   TEXT NOT NULL,
    used_at      TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0,
    created_by   INTEGER,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_line_binding_codes_customer
    ON line_binding_codes(customer_id, used_at);
```

流程：

1. 客戶頁新增「產生 LINE 綁定碼」按鈕（owner/counter），POST 產生 6 位數，顯示一次，10 分鐘有效。
2. Webhook 只接受 `綁定 123456` 這種格式；一般訊息不再觸發綁定。
3. 驗證：碼存在、未過期、未使用、`attempts < 5`；通過後檢查：
   - 此 `line_user_id` 是否已綁其他客戶 → 拒絕並提示「請先解除舊綁定」。
   - 此客戶是否已綁其他 `line_user_id` → 需 owner 確認才覆蓋，並發訊息給舊帳號。
4. 成功後 `used_at`、寫 audit log。
5. 速率限制：同一 LINE user 10 分鐘 5 次；同一 IP 每分鐘 20 次（可先用記憶體計數器，重啟歸零可接受）。

**測試**

- `unpublished/tests/go/internal/handler/line_binding_test.go` 已有 LINE 測試雛形，可直接擴充。
- 用 `LINE_CHANNEL_SECRET=test-secret` 起測試 server，對 body 做 HMAC-SHA256 + base64 放進 `X-Line-Signature`（`line/client.go:68` 就是這樣算）。
- 案例：正確碼、過期碼、重複使用、錯 5 次鎖定、已綁其他客戶、已綁其他 LINE、一般訊息不綁定、回應不洩漏客戶存在。
- 手動：用兩個 LINE 帳號測「A 先綁 → B 輸入同一電話 → 不應成功」。

**風險**

- 多一道櫃檯步驟，現場會抱怨。可搭配「列印在工單上的 QR code／短網址」降低摩擦，但 QR 內容只能是綁定碼頁面，不能含客戶 ID 明碼。
- 既有已綁定的客戶不受影響；提供「重新綁定」按鈕即可。

---

### P0-5 收款金額驗證

**根因**

`receivables.go:399-411` 只擋「全部為 0」，`service.CreateReceipt` 只擋負數。缺：

- `pay_total` 與各付款方式加總不一致（可超收、可少收）。
- `pay_total` 超過該工單尚欠金額。
- `NaN` / `Inf`（`strconv.ParseFloat` 會接受 `NaN`、`Inf`）。
- 稅額/折扣為負、折扣大於總額。
- 日期格式錯誤或未來日期。

**修正方向**

在 handler 與 service 各做一層，service 是最後防線：

1. 新增 `parseMoney(string) (cents int64, err error)`，用整數分運算，拒絕 `NaN/Inf`、超過 2 位小數（或自動四捨五入並提示）。
2. 驗證規則：
   - 每種付款方式 >= 0
   - `|pay_total - (cash+card+check+transfer)| <= 0.01`，除非 `pay_total` 留空（由系統加總）
   - `pay_total > 0`（0 元收款沒有意義；若真的要沖帳，走作廢）
   - `pay_total <= 尚欠 + 0.01`；超收需 owner + 明確「溢收」勾選與原因
   - `discount >= 0`、`tax >= 0`、`discount <= detail_total`
   - `pay_date` 是合法 `YYYY-MM-DD`，不晚於今天（或允許 +1 天，依現場）
3. 驗證失敗回傳**欄位級錯誤**，保留使用者輸入，不要只 redirect 一句話。

**測試**

- 表驅動：負數、NaN、Inf、加總不符、超收、0 元、折扣大於總額、未來日期、空白日期。
- Handler 測試要檢查「錯誤訊息出現在表單上」且「DB 沒有新增」。
- UI：在收款表單填 500 + 500 + 合計 1200 → 應顯示「合計與付款方式不符」，不要把 1200 存進去。

**風險**

- 舊資料可能有「合計與方式不符」的收款；驗證只套用在新增/編輯，不影響歷史。
- 若現場允許「事後補差額」導致超收，需保留 owner override，但一定要留原因。

---

### P0-6 收款單號改成序號表 + UNIQUE

**根因**

`receivables.go:569-578`：

```go
db.DB.QueryRow(`SELECT COUNT(1) FROM receivables WHERE voucher_no LIKE ?`, prefix+"%").Scan(&n)
return prefix + pad3(n+1)
```

- `COUNT` 在併發下會拿到相同數字。
- 刪除/作廢不會減少 COUNT，但如果曾經手動改號，就可能重複。
- 沒有 DB UNIQUE 約束，重複也會寫進去。

**修正方向**

1. 建 `voucher_sequences` 表，用原子 UPSERT 取號。
2. `receivables.voucher_no` 加 UNIQUE index（含作廢紀錄，單號終身唯一）。
3. 取號移到**新增收款的交易內**；表單顯示的號碼只是預覽，POST 時以交易內取得的號碼為準（或遇到衝突自動重取）。

**Migration（草案）**

```sql
-- 先處理歷史重複：保留最早一筆，其餘加 -DUP{id}
UPDATE receivables
SET voucher_no = voucher_no || '-DUP' || id
WHERE id NOT IN (SELECT MIN(id) FROM receivables GROUP BY voucher_no);

CREATE UNIQUE INDEX IF NOT EXISTS ux_receivables_voucher_no
    ON receivables(voucher_no);

CREATE TABLE IF NOT EXISTS voucher_sequences (
    day      TEXT PRIMARY KEY,   -- YYYYMMDD
    next_no  INTEGER NOT NULL
);

INSERT OR IGNORE INTO voucher_sequences(day, next_no)
SELECT substr(voucher_no, 5, 8),
       MAX(CAST(substr(voucher_no, 14) AS INTEGER)) + 1
FROM receivables
WHERE voucher_no GLOB 'RCV-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9]*'
GROUP BY substr(voucher_no, 5, 8);
```

取號：

```go
func nextVoucherNoTx(ctx context.Context, tx sqlExecutor, day string) (string, error) {
    var n int
    err := tx.QueryRowContext(ctx, `
        INSERT INTO voucher_sequences(day, next_no) VALUES(?, 1)
        ON CONFLICT(day) DO UPDATE SET next_no = next_no + 1
        RETURNING next_no`, day).Scan(&n)
    if err != nil { return "", err }
    return fmt.Sprintf("RCV-%s-%03d", day, n), nil
}
```

> 注意：`ON CONFLICT ... RETURNING` 在現代 SQLite 支援；`modernc.org/sqlite` 1.37 沒問題。若擔心，改用 `UPDATE ... RETURNING` + `INSERT OR IGNORE` 兩段式。

**測試**

- `TestNextVoucherNoConcurrent`：50 goroutines 同一天各建一筆，`SELECT COUNT(DISTINCT voucher_no)` 必須 50，且沒有 500。
- `TestVoucherUniqueIndex`：直接 INSERT 重複號碼應失敗。
- 手動：開兩個瀏覽器分頁同時送出收款，兩筆都要成功且號碼不同。

**風險**

- Migration 會改歷史單號（加 `-DUP`）。執行前先產出「重複單號清單」給使用者確認；若舊系統匯入的單號本來就重複，這是唯一能加 UNIQUE 的方法。
- 若使用者堅持保留重複舊號，改用 partial unique index（只約束 `RCV-` 新格式）也可以，但就失去全域唯一。

---

### P0-7 客戶應收/消費總額改成衍生計算

**根因**

- `checkout.go:428-432`：`unpaid_amount = 0` 直接歸零，但全專案沒有任何地方把它加回去（`grep unpaid_amount` 只有讀取與這一行寫入）。
- `total_spent` 在結帳時 `+= total_amount`，作廢不回沖。
- 結果：客戶頁的「未收金額」永遠 0，「消費總額」只增不減。

**修正方向**

**不要再用「事件時加減」維護聚合欄位**，改成查詢時計算。這是 P0-3 能簡單做的前提。

定義：

- `total_spent` = 該客戶所有**未作廢**收款的 `pay_total` 加總（真實現金流入）。
- `unpaid_amount` = 該客戶所有 `completed` 且未結清工單的「總額 - 已收」加總。

查詢範例：

```sql
-- 顧客列表
SELECT c.id, c.name,
       COALESCE((SELECT SUM(r.pay_total) FROM receivables r
                 WHERE r.customer_id = c.id AND r.voided = 0), 0) AS total_spent,
       COALESCE((SELECT SUM(MAX(wo.total_amount - COALESCE(p.paid,0), 0))
                 FROM work_orders wo
                 LEFT JOIN (SELECT work_order_id, SUM(pay_total) paid
                            FROM receivables WHERE voided=0 GROUP BY work_order_id) p
                   ON p.work_order_id = wo.id
                 WHERE wo.customer_id = c.id AND wo.deleted_at IS NULL
                   AND wo.status = 'completed'), 0) AS unpaid_amount
FROM active_customers c
```

（SQLite 的 `MAX(a,b)` 是純量函式，可用；若寫成 `MAX(...)` 聚合要注意別混淆。）

**作法**

1. 拿掉 `checkoutTx` 裡的 `unpaid_amount = 0` 與 `total_spent +=`。
2. 新增 `internal/handler/customer_aggregates.go`（或 service）集中這兩個查詢，顧客列表、詳情、報表、API 全部呼叫它。
3. `customers` 欄位先保留（避免舊報表壞），但**不再寫入**；下一個版本再 drop。
4. 若報表需要「帳面營收」（total_amount）而非「實收」，另開 `billed_total`，不要混用。

**測試**

- 兩個 completed 工單：一個全付、一個未付 → 顧客頁 unpaid = 未付那張的總額，total_spent = 已付那張。
- 作廢已付那張 → total_spent 減回、unpaid 增加。
- 結帳第二張 → unpaid = 0。
- 舊測試 `checkout_test.go:99-100` 要改成符合新定義。

**風險**

- `total_spent` 的語意改變（帳面 → 實收），報表數字會變。上線前先跑新舊算法對照表，讓使用者確認哪個才是他要的「消費總額」。
- 若有人直接用 SQL 讀 `customers.unpaid_amount`，會拿到舊值。文件要註明「此欄位已棄用」。

---

## 3. P1 修復方向

### P1-1 列印/完工單 IDOR

**作法**

- 新增集中授權：

```go
// authorizeWorkOrderView：owner/counter 全部；tech 只能看自己被指派或自己建立的工單。
func authorizeWorkOrderView(user model.User, woID int64) error
```

- 套用到：`PrintWorkOrderReceipt`（`receipt.go:17`）、`PrintWorkOrderCompletion`（`print_docs.go:168`）、`PrintWorkOrderQuotation`、`PrintWorkOrderEstimateSource`、`ShowWorkOrder` 内的照片/工時連結。
- 列印頁含金額者再加 `canSeeMoney(user)`；技師只能印「不含金額」的施工單（如果現場需要）。
- 目前 `main.go:312` 的 `receipt` 與 `:317` 的 `completion` 都是 `AllStaff`，直接印出金額與個資。

**測試**

- 角色矩陣：owner/counter/techA/techB × 4 個列印 URL；techB 對 techA 的工單應 403。
- HTML 斷言：tech 的列印頁不應出現 `NT$`、顧客電話、其他技師佣金。

### P1-2 工時/佣金 IDOR

**作法**

- `worklogs.go` 的列表與編輯：tech 只能看/改自己的（`technician_id = user.ID`），且工單需被指派給自己；owner/counter 全部。
- `ListTechnicianCommissions` 對 tech 只顯示自己的佣金，且是否顯示佣金本身應由 `canSeeMoney` 決定（見 P1-3）。
- 刪除限 owner 或原建立者 + 當日。

**測試**

- 用兩個技師帳號，A 直接 GET/POST B 的 worklog id → 403。
- 佣金頁 HTML 不含他人的名字/金額。

### P1-3 技師不應看到金額

**作法**

- `canSeeMoney` 已存在（`authz.go:125`），但顧客頁/工單頁沒用。
- 在 handler 決定 `showMoney`，傳給 templ；或在 templ 內用 `pages.CanSeeMoney(user)`。
- SQL 層能少查就少查：技師頁面不要 `SELECT total_amount`，避免出現在 HTML/JSON。
- 受影響頁面：顧客列表/詳情（`customers.go:35,99,223`）、工單詳情、收款列表 API、報表、估價。
- 注意：`/api/receivables/search` 目前是 `AllStaff`（`main.go:114`），技師可拿到金額 JSON。

**測試**

- 技師登入後抓顧客頁、工單頁、`/api/receivables/search`，斷言沒有金額字串/欄位。
- 櫃檯/owner 仍看得到（避免過度隱藏）。

### P1-4 員工個資

**作法**

- `ListStaff` / `SearchStaffAPI` 目前 `AllStaff`，且 SELECT 身分證、生日、勞健保。
- 改為：
  - 列表：只回姓名、角色、帳號、在職狀態；身分證末四碼（`****1234`）。
  - 詳情/編輯：owner 或本人；其餘 403。
  - `SearchStaffAPI` 同理，且不要用身分證搜尋（或只允許 owner）。
- 加 audit log（若做 P2 的稽核表）記錄誰看過完整個資。

**測試**

- tech 看 staff 列表：HTML 不含完整身分證/生日。
- tech 直接開 `/staff/{other}/edit` → 403。
- owner 看得到完整資料。

### P1-5 工單狀態機

**現況**：`UpdateWorkOrderStatus`（`workorders.go:735`）接受任意 target status，只檢查 SOP/approval gate；`paid` 也可以手動選。

**作法**

```go
var allowedTransitions = map[string][]string{
    "pending":       {"in_progress", "waiting_parts"},
    "in_progress":   {"waiting_parts", "completed"},
    "waiting_parts": {"in_progress", "completed"},
    "completed":     {"in_progress"},   // 重開需原因
    "paid":          {},                // 只能由結帳/作廢回沖改變
}
```

- `paid` 從 UI 下拉移除，handler 收到 `paid` 直接拒絕（除非來自 service checkout）。
- 重開 `completed → in_progress` 需 owner 或原因，並記錄。
- 前端只渲染「目前可轉的狀態」按鈕，不要讓使用者選了才被擋。

**測試**

- 每個 from × to 的矩陣，非法組合 400/403。
- `completed` 有未完成 SOP 時不能轉 `completed`（現有測試 `approval_gate_test.go` 可擴充）。
- 手動：把工單從 `pending` 直接改成 `paid` → 應被拒。

### P1-6 進貨要回寫成本

**作法**

`ReceivePurchaseOrder`（`purchase_orders.go`）在同一個交易內：

```go
// 加權平均：新成本 = (現有庫存*現有成本 + 進貨量*進貨單價) / (現有庫存+進貨量)
```

- 若現有庫存 <= 0，直接採用進貨單價。
- 寫入 `inventory_movements` 的備註要含「進貨單號、舊成本→新成本」。
- 退貨/作廢進貨要反向計算（若目前沒有退貨流程，先標記 TODO 但不留錯帳）。

**測試**

- 庫存 10 @ 100，進貨 10 @ 120 → 成本 110。
- 庫存 0，進貨 5 @ 80 → 成本 80。
- 進貨後工單用料成本/毛利報表要用新成本。

### P1-7 DB_PATH 與 SQLite 連線設定

**作法**

1. 相對路徑以**執行檔所在目錄**為基準（與 `.env` 載入邏輯一致），不要以 CWD 為準；`file:` DSN 要保留參數。
2. 確保目錄存在（`os.MkdirAll`）。
3. 把 pragma 放進 DSN，讓**每一條連線**都套用：

```
file:data/autorepair.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)
```

4. 若担心多寫入者，SQLite 用單一寫入連線 + 讀取連線池；或至少 `SetMaxOpenConns` 不要太小，讓 WAL 發揮。

**測試**

- 在 `t.TempDir()` 當 CWD、用相對 `DB_PATH=sub/test.db` 啟動，確認檔案在 exe 旁而不是 CWD。
- 併發寫入 20 筆（`-race` 在 Linux CI），不應出現 `database is locked`。
- 停電模擬太難，至少測 `PRAGMA busy_timeout` 回傳 5000。

### P1-8 登入防護

**作法**

- 失敗計數：key = `IP + username`，5 次失敗鎖 15 分鐘；成功登入清零。
- 未知帳號也跑一次 bcrypt（用固定的 dummy hash），避免時間差枚舉。
- 錯誤訊息統一「帳號或密碼錯誤」，不要說「帳號不存在」。
- Seed：
  - 三個預設帳號的密碼都必須由環境變數提供；未提供就**啟動失敗**（目前只有 owner 強制）。
  - 或首次啟動隨機產生並印在 console 一次，強制登入後改密碼。
- `test_login.py:38-39` 的 `owner/owner1234!` 改成讀 `E2E_OWNER_PASSWORD`。
- 加 audit log：登入成功/失敗、IP、時間。

**測試**

- 6 次錯誤後，正確密碼也被鎖（回 429/423 或統一訊息）。
- 重啟後鎖定狀態是否保留（若用 DB）或可接受重置（若用記憶體）——先決定語意。
- 手動：用錯密碼 5 次，換 IP/換帳號的行為要符合預期。

---

## 4. P2 分批處理

| 群組 | 項目 | 建議作法 | 測試 |
|---|---|---|---|
| 金額正確性 | 浮點誤差 | 邊界改用整數分（`int64` cents）；顯示才轉字串。至少把加總/比較改成 `round2` | 0.1+0.2、1000.005、折扣 33.333% |
| 金額正確性 | 收據 Flex ApproveURL 錯 | `receipt.go` 的 approve 連結改指向 `/approval/{token}`，不要用錯的 host/path | 列印 HTML 斷言 token URL |
| 資料完整性 | 工單明細硬刪 | 加 `deleted_at`，查詢改 `active_work_order_items` view；總額重算要排除 | 刪除後總額、庫存、列印都正确 |
| 資料完整性 | `MAX(id)` 取 lastInsertID | 改用 `Result.LastInsertId()`（`workorders.go:608` 已是，其他處檢查） | 併發新增兩筆，ID 不互蓋 |
| Session | 過期 fail-open | `session.Get` 過期要回錯誤並清 cookie，不可當成有效 | 過期 token 請求 → 302 /login |
| Session | token 明文存 DB | 存 SHA-256(token)，比對用 hash；舊 token 遷移或全數失效 | DB 看不到原始 token |
| Session | 沒有清理 | cron 每日刪 `expires_at < now` | 單元測試時間旅行 |
| Session | `return_to` 未驗證 | 只接受以 `/` 開頭、非 `//` 的相對路徑 | `//evil.com`、`https://evil.com` 被拒 |
| HTTP | 沒有 timeout | 改用 `http.Server{ReadHeaderTimeout:5s, ReadTimeout:30s, WriteTimeout:60s, IdleTimeout:120s}` | 慢速 body 測試（可選） |
| HTTP | 缺安全標頭 | `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`, CSP（先 report-only） | header 斷言 |
| HTTP | LAN 明文 | 文件註明只在信任網段；若要外網，前置 Caddy/nginx + TLS | 部署文件 |
| 隱私 | log 含 PII | LINE webhook 只記長度/雜湊；錯誤訊息不帶完整電話/身分證 | grep log fixture |
| 部署 | Docker 以 root | 建 non-root user、`HEALTHCHECK`、pin digest | `docker run` 後 `id` |
| 部署 | 密鑰 | `.env` 不進 image；compose 用 secrets；README 寫清楚 | `docker history` 無密鑰 |
| 專案衛生 | `release/` 進 `./...` | 移到 module 外，或 CI 明確 package 清單；`release/` 不進版控 | CI 時間/清單 |
| UX | 工單選擇器 | 可搜尋、顯示車牌/客戶/狀態、只列可收款狀態、記住最近使用 | E2E 見 6.4 |
| 文件 | 手冊/README 漂移 | 補「SOP 模板」、版本號、`.env` 說明、收款流程（預收 vs 結帳） | 文件測試（現有 nav test） |

---

## 5. Migration 與上線順序

### 5-1 建議編號

| 編號 | 內容 | 對應 |
|---|---|---|
| 048 | `voucher_sequences` + 單號 dedupe + UNIQUE index | P0-6 |
| 049 | `receivables` 作廢欄位（reason/by/at） | P0-3 |
| 050 | `line_binding_codes` | P0-4 |
| 051 | 工單明細 `deleted_at` + `active_work_order_items` view | P2 |
| 052 | session token hash 欄位（若要做） | P2 |

> 順序建議：048 → 049 → 050。每個 migration 都先用正式 DB 副本跑一次，記下秒數。

### 5-2 上線前後對帳查詢

```sql
-- 1) 收款總額 vs 工單總額（已 paid）
SELECT SUM(pay_total) FROM receivables WHERE voided=0;

-- 2) 已 paid 但有效收款不足的工單（預期 0 筆；若 >0 是舊 bug 留下的）
SELECT wo.id, wo.total_amount, COALESCE(SUM(r.pay_total),0) AS paid
FROM work_orders wo
LEFT JOIN receivables r ON r.work_order_id=wo.id AND r.voided=0
WHERE wo.status='paid' AND wo.deleted_at IS NULL
GROUP BY wo.id
HAVING paid + 0.01 < wo.total_amount;

-- 3) 重複單號
SELECT voucher_no, COUNT(*) FROM receivables GROUP BY voucher_no HAVING COUNT(*)>1;

-- 4) 負庫存
SELECT id, name, stock_quantity FROM inventory_items
WHERE COALESCE(stock_quantity,0) < 0 AND deleted_at IS NULL;
```

把这四個查詢做成 `scripts/reconcile.sql`，上線前後各跑一次，結果貼給使用者確認。

### 5-3 回滾策略

- Migration 全部是**新增表/欄位/索引**，沒有 drop；回滾程式碼即可，DB 保留新欄位不影響舊版。
- P0-4 的綁定流程用環境變數 `LINE_BIND_MODE=code|auto` 過渡一週；預設 `code`，出問題可切回 `auto`（但不建議長期）。
- P0-1 的行為改變用 `CHECKOUT_REQUIRES_COMPLETED=true` 開關，先在一間店試跑。

---

## 6. UI 測試完整指南（重點）

### 6-1 四層策略

| 層級 | 工具 | 測什麼 | 速度 | 建議比例 |
|---|---|---|---|---|
| L1 元件/模板 | `go test` + templ render | 金額/個資是否出現、按鈕是否顯示 | 毫秒 | 30% |
| L2 Handler + 真路由 | `httptest` + cookie jar + CSRF | 權限矩陣、redirect、表單驗證、DB 結果 | 秒 | 50% |
| L3 真瀏覽器 E2E | Playwright（Python，已有 `test_login.py`） | 登入→開單→完工→收款→列印的完整流程 | 十秒 | 15% |
| L4 手動驗收 | 人 + 清單 | 列印版面、手機、鍵盤、現場語感 | 分鐘 | 5% |

**不要**把所有東西都丟給 Playwright。E2E 很慢又容易因字串/版面變動而紅；能用 L2 測的，就不要用瀏覽器。

### 6-2 L1：模板測試

Templ 元件可以直接 render 到 buffer：

```go
var buf bytes.Buffer
if err := pages.CustomerRow(customer, false).Render(context.Background(), &buf); err != nil {
    t.Fatal(err)
}
html := buf.String()
if strings.Contains(html, "12,000") {
    t.Fatal("技師不應看到金額")
}
```

建議測：

- `showMoney=false` 時，金額欄位完全不存在（不是 `display:none`，因為 HTML 仍可被讀）。
- `showSensitive=false` 時，身分證只出現 `****1234`。
- 狀態按鈕只包含允許的轉換。
- 空資料/長字串/特殊字元（`<script>`）不會破版（現有 `xss_regression_test.go` 可延伸）。

### 6-3 L2：Handler + 真路由（最高 ROI）

有了 1.2 的 `routes.New`，可以這樣寫：

```go
func TestTechCannotSeeMoney(t *testing.T) {
    srv := newTestServer(t)          // routes.New + migrated temp DB
    tech := loginAs(t, srv, "tech", testTechPassword)

    for _, path := range []string{"/customers", "/work-orders", "/api/receivables/search"} {
        body := tech.getBody(t, path)
        if strings.Contains(body, "NT$") || strings.Contains(body, `"pay_total"`) {
            t.Errorf("%s 洩漏金額給技師", path)
        }
    }
}
```

**角色 × 路由矩陣**（建議自動化，避免下一個 route 又漏）：

```go
type routeCase struct {
    method, path string
    owner, counter, tech int // 預期 status
}
```

- 從 `main.go`/`routes.go` 維護一份清單；新增 route 沒填預期值就讓測試失敗。
- 這比人工看 `main.go` 可靠，也能抓到「誤用 `AllStaff`」的回歸。

**CSRF 測試**：現有 `csrf_bypass_test.go` 已覆盖「無 token 被擋」；補「有 token 但角色不足仍被擋」，避免把 CSRF 當成授權。

**表單驗證測試**：對收款表單送各種壞值，斷言：

- HTTP 200（回到表單）或 302 + flash
- 錯誤訊息含關鍵字
- DB `COUNT(*)` 不變

### 6-4 L3：Playwright E2E

#### 安裝

```powershell
py -m pip install pytest pytest-playwright playwright requests
py -m playwright install chromium
```

> 這是**開發/測試依賴**，不違反「不用 npm/CDN/前端框架」的產品慣例；瀏覽器不會進 production image。

#### 目錄建議

```
scripts/e2e/
  conftest.py          # 建 DB、啟 server、登入 fixture
  helpers.py           # login / nav / 填表 / 讀 flash
  test_smoke.py
  test_money.py
  test_permissions.py
  test_print.py
  test_line_binding.py
  baselines/           # 視覺回歸截圖（可選）
```

#### `conftest.py` 草案

```python
import os, socket, subprocess, time, pathlib, pytest, requests

ROOT = pathlib.Path(__file__).resolve().parents[2]
PASSWORDS = {"owner": "e2e-owner-pass", "counter": "e2e-counter-pass", "tech": "e2e-tech-pass"}

def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]

@pytest.fixture(scope="session")
def server(tmp_path_factory):
    port = _free_port()
    db = tmp_path_factory.mktemp("db") / "e2e.db"
    env = os.environ | {
        "PORT": str(port),
        # SQLite URI 在 Windows 要用正斜線；含空白時再 urlencode
        "DB_PATH": "file:" + db.as_posix() + "?_pragma=busy_timeout(5000)",
        "RUN_SEED": "true",
        "DEFAULT_OWNER_PASSWORD": PASSWORDS["owner"],
        "DEFAULT_COUNTER_PASSWORD": PASSWORDS["counter"],
        "DEFAULT_TECH_PASSWORD": PASSWORDS["tech"],
        "APP_NAME": "AutoRepairOS E2E",
    }
    proc = subprocess.Popen([str(ROOT / ("autorepairos.exe" if os.name == "nt" else "autorepairos"))],
                            cwd=tmp_path_factory.mktemp("cwd"),
                            env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    base = f"http://127.0.0.1:{port}"
    for _ in range(60):
        try:
            if requests.get(base + "/login", timeout=1).status_code == 200:
                break
        except requests.RequestException:
            pass
        time.sleep(0.5)
    else:
        proc.kill(); raise RuntimeError("server 沒起來")
    yield base, PASSWORDS
    proc.terminate(); proc.wait(timeout=10)
```

> 建議順手加 `GET /healthz`（免登入，回 `{status,version,db:"ok"}`），等待啟動就不用依賴 `/login`，也方便 Docker `HEALTHCHECK`。

#### 登入 helper

```python
def login(page, base, username, password):
    page.goto(base + "/login")
    page.get_by_label("帳號").fill(username)
    page.get_by_label("密碼").fill(password)
    page.get_by_role("button", name="登入").click()
    page.wait_for_url(base + "/")
```

用 `get_by_label`/`get_by_role` 比 CSS selector 穩定；若表單沒有 label，順手補 `<label for>`（無障礙也受益）。

#### 建議加 `data-testid`（Templ 內）

| 位置 | testid |
|---|---|
| 工單狀態 badge | `wo-status` |
| 工單總額/已收/尚欠 | `wo-total` / `wo-paid` / `wo-due` |
| 顧客金額欄 | `customer-total-spent` / `customer-unpaid` |
| 收款表單欄位 | `receipt-pay-total` / `receipt-cash` / `receipt-work-order` |
| flash 訊息 | `flash` |
| 列印按鈕 | `print-receipt` / `print-completion` |
| 導覽項目 | `nav-work-orders` … |

不要用中文文字當唯一 selector（文案一改就紅）；`data-testid` 不影響畫面與 SEO。

#### 10 個必寫情境

| # | 情境 | 步驟 | 預期 |
|---|---|---|---|
| 1 | 預收不結帳 | 建 pending 工單（含零件）→ 收款全額關聯 | 狀態仍 pending；庫存不變；flash 含「預收」 |
| 2 | 0 元不結帳 | 建 total=0 的 in_progress 工單 → 收 0 元 | 不變 paid；不扣庫存 |
| 3 | 完工後收款 | 按完工 → 收款足額 | 變 paid；庫存 -qty；`customer-total-spent` 增加 |
| 4 | 作廢回沖 | 對 #3 作廢（填原因） | 回 completed；庫存 +qty；應收恢復 |
| 5 | 技師看不到金額 | tech 登入 → 顧客/工單/收款 API | 無 `NT$`/`pay_total`；直接開 `/receivables` 被擋 |
| 6 | 列印 IDOR | techA 開 techB 的工單列印 URL | 403（不是 200） |
| 7 | 員工個資 | tech 開 `/staff`；tech 開他人 `/staff/{id}/edit` | 列表遮罩；編輯 403 |
| 8 | 單號競態 | 兩個 context 同時送收款 | 兩筆都成功、號碼不同、無 500 |
| 9 | 登入鎖定 | 錯 5 次 → 再輸正確 | 仍被鎖；訊息一致 |
| 10 | LINE 綁定 | 送一般電話訊息；再走綁定碼 | 前者不綁定；後者成功；別人的碼無效 |

#### 列印測試

- 用 Chromium：`page.emulate_media(media="print")`，`page.pdf(path=...)`。
- A4 與熱感（`?paper=thermal`）各印一次，檢查頁數/寬度；用 `pypdf` 讀頁面尺寸。
- 視覺回歸：`expect(page.locator("[data-testid=receipt]")).to_have_screenshot("receipt-a4.png", mask=[page.locator("[data-testid=now]")])`。
- 字型在不同機器會不一樣 → baseline 只在 CI 的固定 image 產生；本機只做人工看。

#### 手機與無障礙

- `page.set_viewport_size({"width": 375, "height": 812})`，檢查 `document.documentElement.scrollWidth <= window.innerWidth`（不能整頁左右滑）。
- 鍵盤：`Tab` 走完收款表單，`Enter` 送出；focus ring 可見。
- 200% 縮放（`page.evaluate("document.body.style.zoom='2'")` 只是近似，真正要用瀏覽器設定）→ 按鈕不重疊。
- axe：把 `axe.min.js` 放 `scripts/e2e/vendor/`（不用 CDN），`page.add_script_tag(path=...)` 後跑 `axe.run()`；只擋 serious/critical。

#### 手動驗收清單（L4）

| # | 畫面 | 操作 | 預期 |
|---|---|---|---|
| M1 | 收款 | 合計與付款方式不符 | 留在表單、欄位旁紅字、DB 不新增 |
| M2 | 收款 | 連點「送出」兩次 | 只產生一筆（按鈕禁用或 idempotent） |
| M3 | 工單 | 完成後按上一頁再重整 | 不重複扣庫存（PRG + 結帳 idempotent） |
| M4 | 工單 | pending 直接選 paid | 選項不存在或被拒 |
| M5 | 列印 | A4/熱感各印一張 | 頁尾不被切、金額對、條碼清楚 |
| M6 | 顧客 | tech 登入看列表 | 沒有金額欄、沒有完整電話 |
| M7 | 顧客 | 產生 LINE 綁定碼 → 錯誤碼 5 次 | 鎖定、訊息不洩漏客戶存在 |
| M8 | 員工 | tech 開他人編輯 | 403 頁（不是空白或 500） |
| M9 | 手機 | 375px 開工單/收款 | 可操作、不左右滑、按鈕不重疊 |
| M10 | Session | 表單開著 30 分鐘後送出 | 導回登入並保留 `return_to`，登入後回原頁 |
| M11 | 權限 | 未登入直接開 `/work-orders/1/receipt` | 302 /login，不是 200 |
| M12 | 資料 | 作廢後看庫存異動 | 有一筆「作廢回沖」，數量正確 |

### 6-5 競態與效能

- `go test ./internal/service ./internal/handler -run 'Receipt|Checkout' -count=20`：抓時序問題。
- `-race` 在 Linux CI 跑；本機 Windows 若沒 GCC 可略過。
- 單號：50 goroutines；庫存：兩個工單同時扣同一料，只能一個成功（現有 `inventory_concurrency_test.go` 可延伸）。
- 效能：用 `cmd/bench`（已有）或 `go test -bench`；UI 端用 Playwright 量「工單列表 500 筆」的 `performance.now()`，目標本機 < 500ms。
- 慢查詢：開 `DB_DEBUG` 或暫時加 `EXPLAIN QUERY PLAN`，確認顧客聚合查詢有走 index（`receivables(customer_id)`、`work_orders(customer_id,status)`）。

### 6-6 測試資料

- 目前 `seed.Run` 只建三個使用者。建議加 `SEED_DEMO=true`（僅非 production）建立：
  - 3 個客戶、3 台車、5 張不同狀態工單、2 張估價、庫存 10 料、1 張進貨單。
- E2E 若不想靠 UI 建資料，可在 server 啟動後用 **API** 建（不要直接開 sqlite 連線，避免鎖）。
- 每個測試用獨立 DB（`tmp_path`），不要共用，否則順序會影響結果。

### 6-7 CI 的 E2E Job

```yaml
e2e:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with: { go-version-file: go.mod }
    - run: go build -o autorepairos .
    - uses: actions/setup-python@v5
      with: { python-version: "3.12" }
    - run: pip install pytest pytest-playwright playwright requests
    - run: playwright install --with-deps chromium
    - run: pytest scripts/e2e -q --tracing=retain-on-failure
    - uses: actions/upload-artifact@v4
      if: failure()
      with: { name: playwright-traces, path: test-results/ }
```

單元測試每個 PR 跑；E2E 可 nightly 或 release 前跑，避免拖慢日常。

---

## 7. 分階段時程與完成定義

| 階段 | 內容 | 估時 | 完成定義（DoD） |
|---|---|---|---|
| Phase 0 | CI、routes 抽取、測試 DB/helper、修文件測試 | 0.5–1 天 | `go vet` + `go test` 在 CI 綠；新增 route 有矩陣測試 |
| Phase 1 | P0-7 → P0-3 → P0-1/2 → P0-5 → P0-6 | 2–3 天 | 金流表驅動測試全綠；手動 M1–M4、M12 通過 |
| Phase 2 | P0-4 LINE 綁定碼 | 1–1.5 天 | 綁定碼生命週期測試；兩個 LINE 帳號手動驗證 |
| Phase 3 | P1-1～P1-8 | 3–4 天 | 角色矩陣測試；文件更新（手冊流程） |
| Phase 4 | P2 分批 + E2E 10 情境 + 視覺/手機 | 3–5 天 | E2E 在 CI 綠；M1–M12 全過 |
| 上線前 | 對帳、備份演練、回滾演練 | 1 天 | 四個 reconcile 查詢數字合理；備份可還原 |

**建議 commit 切分**（每個都應可獨立回復）：

1. `test: add migrated test DB helper + route matrix`
2. `fix(checkout): require completed status and positive total`
3. `feat(receivables): compensating void + audit fields`
4. `fix(customers): derive aggregates from receipts/work orders`
5. `fix(receivables): validate amounts and allocate voucher numbers atomically`
6. `feat(line): one-time binding codes`
7. `fix(authz): central work-order authorization + money/PII gating`
8. `fix(workorders): status state machine + receiving cost`
9. `chore(ci): run vet/tests + e2e job`

> 每個 commit 都要附對應測試；不要「先修再補測試」，這種 bug 就是這樣來的。

---

## 8. 風險與回滾

| 變更 | 風險 | 緩解 |
|---|---|---|
| 不提前結帳 | 現場以為已收款=已完工 | 收款成功訊息寫明狀態；上線前教育；開關可切回 |
| 作廢回沖庫存 | 與既有盤點差異 | 先跑「paid 但收款不足」清單；作廢需原因；負庫存頁面可見 |
| 聚合改成衍生 | 報表數字變動 | 新舊算法對照表给使用者確認；欄位暫留 |
| 單號 UNIQUE | 舊重複號被改名 | 先產出清單；必要時用 partial index |
| LINE 綁定碼 | 現場多一步 | QR/短網址；保留 `LINE_BIND_MODE=auto` 一週 |
| 技師隱藏金額 | 現場抱怨看不到 | 用角色設定（未來可加 `tech_can_see_money`），但預設關 |
| 權限加嚴 | 既有書籤/流程被擋 | 先記錄 403 一週（report-only），再全面阻擋 |

---

## 9. 我建議的實際下一步

1. **先做 Phase 0**（CI + 路由抽取 + 測試 DB）。這一步不改行為，風險最低，但會讓後面所有修復可驗證。
2. **Phase 1 從 P0-7 開始**，因為它會讓 P0-3 變簡單；接著 P0-1/2（同一個函式）、P0-3、P0-5、P0-6。
3. **P0-4 單獨一個 PR**，因為它牽涉 migration、webhook、UI 與現場流程，最好獨立驗收。
4. **P1 權限四項一起做**（集中授權 + 角色矩陣測試），避免補了列印又漏 API。
5. P2 與 E2E 可以並行，但不要擋上線；上線只需要「P0 + P1 權限 + 測試骨架」。

如果你要我開始動手，我建議第一個 commit 就是 `test: add migrated test DB helper + route matrix`，先把安全網鋪好，再依序修。這樣每個修復都有失敗→通過的測試證據，也方便你逐個 PR review。
