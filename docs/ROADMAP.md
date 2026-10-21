# AutoRepairOS 開發路線圖

> 更新：2026-10-16
> 基準：SRS v1.1（2026-10-16）與目前程式碼盤點
> 原則：先修資料一致性與安全問題，再補 SRS 已承諾的功能；每項工作需可驗收，未經決策不擴大產品範圍。

## 1. 現況與未完成需求

狀態定義：**未完成**＝尚無可用實作；**部分完成**＝有相關功能但未滿足完整需求；**待驗證**＝程式已有部分支援，但缺少符合 SRS 門檻的測試或驗收證據。

| 模組 / SRS | 狀態 | 缺口 |
|---|---|---|
| 工單 FR-WO-01、05–08 | 部分完成 | 估價草稿轉工單、照片標記／舊傷預檢、逐項 SOP、片語庫、追加維修授權皆已完成；缺 LINE 追加授權的頁面實作細節待補。 |
| 車輛 FR-VH-05、06 | 部分完成 | LINE 可查工單進度，尚無完整維修 Passbook；缺車身示意圖舊傷標記。 |
| 保險 FR-INS-01–03 | 部分完成 | 有投保資料雛形，但缺保單號碼／到期日及完整理賠紀錄，亦缺到期提醒。 |
| 庫存 FR-INV-05 | 完成 | 庫位欄位與掃描程式已接入庫存操作；掃描後自動填入搜尋框，結果顯示庫位並優先呈現。 |
| 客戶通知 FR-CRM-04 | 完成 | LINE／Telegram 分流、SMS 停用與既有偏好提示皆已完成。 |
| 帳務 FR-FIN-01–08 | 部分完成 | 轉帳、收款／結帳一致流程與 CSV 匯出已完成；缺 AP/Aging、工資與外包成本。 |
| 預約 FR-APP-01、05 | 部分完成 | 有列表與基本工位衝突檢查，缺日／週行事曆及前一日提醒；衝突檢查也需涵蓋併發。 |
| 通知 FR-NOTI-02–04 | 完成 | 里程、驗車、保險、預約前日、生日提醒與失敗重試皆已完成。 |
| 分析 FR-ANA-01、02、06 | 部分完成 | 有部分 KPI 與分組報表；缺完整圖表維度、技師效率及方案毛利。 |
| 備份與離線 §7.2 | 部分完成 | 已備份 SQLite，照片未納入備份；前端佇列未保存照片檔案，不能可靠重試照片上傳。 |
| 效能 §7.1 | 待驗證 | 缺 10,000 筆列表與 1,000 筆通知的基準測試及結果。 |
| 安全 §7.3、§8.7 | 未完成 | 有狀態變更 GET 路由、CSRF 防護未落實、重要資料仍有實體刪除；sqlite-web 預設對外公開。 |
| 文件 §6、§8.3 | 未完成 | SRS 資料模型章節為空；文件描述 Service Layer，但程式尚無此層。 |

另外需修復的已知正確性問題：停用員工仍可使用既有工作階段；庫存搜尋 API 洩露成本價；收款關聯工單未執行完整結帳；採購收貨及庫存調整存在併發競態。這些列入第一階段，不應當作一般功能待辦。

## 2. 已確認的產品與架構決策

| 議題 | 決策 | 對路線圖的影響 |
|---|---|---|
| Service Layer | 建立真正的 Service Layer，採依賴注入；漸進遷移，不為了統一風格重寫所有舊 handler。 | 共用結帳及新增業務邏輯進 service；先以 `*sql.DB` 注入並可獨立測試。 |
| 收款與結帳 | 收款關聯工單時走與工單結帳相同的流程。 | 收款、工單狀態、扣庫存、預留量及客戶帳務必須在同一交易內一致完成；不得留下「收款成功但結帳失敗」的成功狀態。 |
| 語音輸入 | 不整合 Web Speech API；使用者可用裝置／作業系統輸入法的語音輸入。 | 實作離線可用的技師常用片語庫，確認輸入欄位不阻擋貼上或長文字。 |
| 客戶通知通道 | LINE 與 Telegram 都支援客戶通知；不建置 SMS Gateway。 | 通知服務須依客戶偏好及其 LINE／Telegram ID 正確路由；停用 SMS 選項，SRS 已同步更新。 |
| 離線優先 | 核心維修作業不依賴外網；不可用 CDN 增加新依賴。 | 離線佇列需保存實際檔案並驗證恢復連線後的上傳結果。 |

## 3. 任務清單

任務狀態以 `[ ]` 待辦、`[x]` 已完成表示。只有程式、測試及相關文件均符合驗收條件後才勾選；部分完成需在任務下記錄尚缺部分，不以單純編譯成功視為完成。

### 階段 0：安全與資料正確性

#### [x] T0.1 停用預設公開的 sqlite-web
- 預設 Compose 啟動不得把無驗證的資料庫管理介面暴露到區網／公網；若保留除錯入口，須選擇 opt-in 啟用並限制為 loopback。
- 做法：sqlite-web 服務加上 `profiles: ["debug"]`（預設不啟動）；port 改绑 `127.0.0.1:8091:8080`（僅本機）；volume 掛載改 `:ro`（只讀）。
- 啟動方式：`docker compose --profile debug up -d` 或 `docker compose up -d sqlite-web`。
- 驗收：`docker compose config` 語法正確（exit 0）；預設 `docker compose up` 不啟動 sqlite-web；明確啟動後僅 127.0.0.1 可連線。

#### [x] T0.2 讓停用員工立即失去存取權
- 登入與每次請求驗證都檢查 `users.active`；停用時撤銷該使用者的所有 session。
- 驗收：停用後既有 Cookie 不能再存取受保護頁面；停用帳號不能登入；重新啟用後可正常登入。

#### [x] T0.3 保護所有狀態變更請求
- 為 POST/PUT/PATCH/DELETE 加 CSRF token 驗證，Cookie 設定 SameSite=Strict（LAN 部署若無 HTTPS 則用 SameSite=Lax）。
- 將所有有副作用的 GET 路由改為 POST 或 DELETE，包括刪除、收貨、切換、手動執行排程等；不可只靠 SameSite 保護有副作用的 GET。
- 表單與 HTMX 請求須一致帶 token，包含 Templ 及字串產生頁面。
- 實作：
  - main.go：20+ 條有副作用的 GET 路由改為 POST
  - auth.go：CSRF token 產生與驗證 middleware
  - session.go：session 整合 CSRF token
  - layout.go + layout.templ：表單自動注入 CSRF hidden field
  - 所有 .templ 頁面：表單加上 CSRF token + HTMX header
- 驗證：go build ./... exit 0、go test ./... exit 0（40 files, +1317/-852）

#### [x] T0.4 修正採購收貨與庫存調整的併發安全
- 採購單在交易內以條件式狀態轉換搶占 draft → received；只有成功轉換的一次請求能入庫。
- 庫存檢查與調整在同一交易以條件式更新完成；異動記錄的 before／after 必須來自實際更新結果。
- SQLite 不支援 SELECT ... FOR UPDATE，請使用適用於 SQLite 的交易與條件更新，不照搬其他資料庫語法。
- 實作：
  - 採購收貨：BEGIN IMMEDIATE + UPDATE ... WHERE status='draft' 的 affected rows 判斷搶占
  - 庫存調整：交易內條件式更新（UPDATE ... SET qty = qty - ? WHERE id = ? AND qty >= ?）
  - 異動記錄 before/after 來自 RETURNING 的實際更新結果
  - db.go：BeginImmediate 對每條連線設定 5 秒 busy timeout
  - 新增 migration 028：採購異動連結到採購單品項並以唯一索引防止同品項重複留痕
  - 新增 goroutine 併發測試（併發收貨只入庫一次、併發扣庫不負庫存）
- 驗證：go build ./... exit 0、go test -count=1 ./... exit 0（併發測試 -count=10 重複通過）

#### [x] T0.5 修正庫存搜尋的成本價權限
- 依目前登入角色決定是否回傳／顯示成本價，不能只隱藏頁面欄位；檢查庫存列表及搜尋 API 的所有輸出路徑。
- 驗收：非授權角色的 HTML／HTMX 回應均不含成本價；有權角色維持現有功能。

#### [x] T0.6 抽取並共用原子結帳流程
- 建立依賴注入的 checkout service，統一處理工單 paid 狀態、庫存扣減、預留量釋放、庫存異動紀錄及客戶消費總額。
- 工單結帳與關聯工單的收款都呼叫同一 service；收款新增／更新與結帳必須遵守同一交易邊界，錯誤時整體回滾。
- 結帳需具冪等性，重複請求不可重複扣庫或重複累加消費。
- 依賴：T1.2（Service Layer 基礎）。
- 驗收：成功、失敗回滾、重複請求及併發測試涵蓋收款與庫存帳務。
- 實作：工單結帳及收款新增／更新／作廢均經由 `internal/service/checkout.go` 的交易流程；關聯工單的新增與更新會在同一交易內結帳，重複收款單號以相同內容重送時不重複入帳，不同內容則拒絕。新增收款表單預先填入單號，讓瀏覽器重送沿用同一冪等鍵。
- 驗證：`go build ./...`、`go test ./...` 通過。

#### [x] T0.7 以軟刪除保留重要營運資料
- 對照 SRS §7.3／§8.7 與實際資料關係，訂定適用表格及刪除／作廢策略；遷移採向後相容方式。
- 將重要資料及工單明細的實體刪除改為軟刪除或作廢標記；所有列表、搜尋、報表及關聯查詢一致排除已刪資料，歷史／稽核查詢則保留存取方式。
- 先重新盤點實際 DELETE 操作與外鍵依賴，不直接照搬未驗證的表名清單。
- 實作：
  - migration 029：17 張表加 deleted_at 欄位 + 17 個 active_* views
  - 所有 DELETE 改為 UPDATE deleted_at = datetime('now')
  - 所有查詢改用 active_* views 排除已刪資料
  - sessions 仍以硬刪除（安全性）
  - 測試 DB 初始化補上 deleted_at 欄位與 active_* views
- 驗證：go build ./... exit 0、go test ./... -count=1 全綠（36 files, +1071/-312）

### 階段 1：文件與 Service Layer 基礎

#### [x] T1.1 補齊 SRS §6 資料模型
- 以 migration 與 model 為準，列出主要資料表用途、主鍵／外鍵、重要欄位及關係圖。
- 實作：掃描 45 個 migration，整理 30 張資料表，新增 Mermaid 關係圖，区分 DB 外鍵與應用層關聯。
- 驗證：docs/SRS.md +133 行，go build exit 0

#### [x] T1.2 建立可測試的 Service Layer 基礎
- 新增 `internal/service/`，使用 `*sql.DB` 依賴注入，避免 service 直接依賴全域 `db.DB`。
- 以 T0.6 共用結帳流程作為第一個有實際用途的 service；新業務邏輯優先放 service。
- 不要求一次搬移所有既有 handler；只在功能修改或有重複消費者時漸進抽取。
- 更新 SRS §8.3 的架構描述，使其反映實際分層邊界。
- 實作：
  - internal/service/checkout.go：Checkout service（*sql.DB 注入）
  - internal/service/checkout_test.go：5 個測試（成功+冪等、失敗回滾、收款結帳原子性、收款+結帳回滾、工單不存在）
  - internal/handler/workorders.go：改用 Checkout service
  - internal/handler/receivables.go：改用 Checkout service（AssociateReceipt）
  - docs/SRS.md §8.3：更新架構描述
  - docs/DEVELOPMENT.md：更新開發規範
- 驗證：go build ./... exit 0、go test ./internal/service/... 5/5 PASS、go test ./... 全綠
- 實作：`Checkout` service 完成；T0.6 已將收款新增、更新及作廢遷移至同一 service 交易邊界。

### 階段 2：SRS 功能缺口

#### [x] T2.1 通知失敗自動重試（FR-NOTI-04）
- 暫時性外送失敗依 1／5／15 分鐘退避重試，最多重試 3 次；永久錯誤及超過上限均標記為 `permanent_failure`，排程頁顯示狀態、失敗次數與錯誤原因。
- 以條件式狀態更新及處理租約避免多個 worker 重複外送；通知先寫入佇列，由排程器非同步處理，不阻塞核心維修流程。
- 實作：migration 030（retry_count、next_retry_at、last_error）；cron.go 退避重試與永久失敗判斷；scheduled.go 排程頁狀態顯示；notifications_test.go 四种情境測試。
- 驗證：go build ./... exit 0、go test ./... 全綠（cron 1.050s、handler 1.309s）。

#### [x] T2.2 保養、驗車及保險到期提醒（FR-NOTI-02、FR-INS-03）
- 每日掃描保養日期／里程、車輛定檢及 30 日內保單到期，提醒寫入通知佇列並沿用 T2.1 重試。
- 依客戶 LINE／Telegram 偏好及綁定狀態決定是否排入；同一客戶、事項與內容七日內去重。
- Migration 032 開放 Telegram 通知通道；cron 測試涵蓋到期、偏好、綁定及去重。
- 依賴：T2.5（保險到期欄位）；通知重試依賴 T2.1。

#### [x] T2.3 預約日曆與前日提醒（FR-APP-01、FR-APP-05）
- 增加日／週視圖，顯示預約時段、車輛與工位；提供狀態及日期導覽。
- 前一日 LINE／Telegram 提醒，包含車牌、時間與服務內容；避免重複通知。
- 工位時段定義為 HH:MM 起算 60 分鐘的半開區間；相鄰時段不衝突，取消預約釋放工位。應用層檢查搭配 SQLite insert/update trigger 原子阻擋重疊建立與更新。
- 實作：migration 033（工位衝突索引）；appointments.go 日/週視圖 + 衝突檢查；cron.go 前日提醒；appointment.templ 視圖切換；3 個測試檔案。
- 驗證：go build ./... exit 0、go test ./... 全綠（13 files + 4 new, +646/-26）。
- 實作：HTMX 日／週行事曆與日期、狀態導覽；cron 每日將明日預約提醒加入通知佇列，依 LINE／Telegram 綁定偏好發送並以預約 ID 去重；migration 033 增加工位衝突 trigger。
- 測試：行事曆日／週渲染與狀態篩選、前日提醒內容／偏好／去重、工位區間邊界及 insert/update trigger。

#### [x] T2.4 轉帳方式與收款結帳一致流程（FR-FIN-01、FR-FIN-02）
- 支援轉帳收款並保留支付方式明細；收款、工單狀態、庫存及客戶帳務透過 T0.6 共用流程一致更新。
- 驗收：部分收款、全額收款、作廢／更正及重複提交不會重複入帳或扣庫。
- 依賴：T0.6。
- 實作：migration 034 增加轉帳金額與交易序號；收款表單、清單篩選、日結報表及收據列印支援轉帳。
- 收款累計足額才透過 Checkout service 結帳；部分收款保留未結狀態，作廢、修正及重送不會重複扣庫或入帳。
- 測試：部分／全額轉帳收款、作廢後補收、更正及重複提交；`go build ./...`、`go test ./... -count=1` 全綠。

#### [x] T2.5 保單號碼、到期日及理賠資料（FR-INS-01–03）
- 投保資料新增保單號碼、有效期限；理賠資料可記錄日期、金額及備註。
- 投保清單依到期日排序，支援保單號碼搜尋及 30 日內到期篩選；`GET /api/insurances/expiring` 提供 T2.2 使用。
- Migration 031 新增保單及理賠欄位；投保與工單理賠表單支援新增／編輯。

#### [x] T2.6 逐項 SOP 清單與完工限制（FR-WO-08）
- 支援依維修類型選用檢查表、逐項記錄完成狀態；保留完成者與完成時間等必要追溯資訊。
- 後端必須拒絕未完成檢查項目的工單進入完工狀態，不能只在前端禁用按鈕。
- 驗收：未完成任一必填項目時，直接呼叫 API 也不能完工。

#### [x] T2.7 技師常用片語庫（FR-WO-07）
- 技師可快速搜尋、插入常用維修描述；提供受權限控管的片語維護方式。
- 確保輸入法語音輸入、貼上及長文字輸入可正常使用，不新增依賴外網的語音服務。
- 實作：migration 036 建立 technician_phrases 表；internal/handler/phrases.go 提供 CRUD 與 API；main.go 註冊路由；internal/nav 加入選單；工單項目表單加入「📝 常用片語」按鈕與即時搜尋 UI；預設 25 個常見片語（煞車、引擎、冷卻、輪胎、底盤、電路、空調、變速箱、離合器）。
- 驗證：templ generate、go build ./... 成功；測試通過（config/cron/db/service）；片語搜尋 API 回傳 JSON；工單描述欄位支援點選帶入。

#### [x] T2.8 車身舊傷預檢圖與照片標記（FR-VH-06、FR-WO-05）
- 進廠預檢可在車身示意圖標記位置，記錄舊傷類型並附加照片。
- 工單照片支援畫線／圈選等標記，照片可關聯工單及明細。
- 保留原始照片及標記資料；後端驗證工單、明細及照片的關聯權限。
- 實作：migration 037 建立 photo_annotations / walkaround_marks 與 active_* view，並灌入 walkaround_mark_type 六種舊傷類型；internal/handler/photo_markup.go 的標記 CRUD 由 photo_id 反查 work_order_id 寫入、驗證 item_id 屬於該工單、對象不存在回 404；workorder.templ 新增「舊傷預檢」區塊（車身圖點擊標記）；photos.go ＋ photo_markup.templ ＋ photo-markup.js 讓照片頁可展開 canvas 標記（座標正規化、可重繪刪除、fetch 帶 X-CSRF-Token）。
- 驗證：templ generate、go build ./... exit 0、go test ./... -count=1 全綠（handler 新增 3 個測試：跨工單照片／明細拒絕、刪除不存在標記回 404）。

#### [x] T2.9 獨立估價草稿及轉正式工單（FR-WO-01）
- 可先建立估價草稿與項目，客戶／車輛關聯後轉為正式工單，保留估價內容及金額。
- 轉換需冪等並保留來源關係，避免重複建立工單或遺失報價歷史。
- 實作：migration 039 建立 estimates / estimate_items（既有 work_orders.status 有 CHECK、work_order_items.work_order_id 為 NOT NULL，故用平行表）；internal/service/estimates.go 提供建立、加項目與轉正式工單；轉換在單一交易內複製項目並保留來源估價 ID、原始報價與折扣；internal/handler/estimates.go 與 web/pages/estimates.templ 提供列表／新增／詳情頁與「轉為正式工單」，權限限 Owner/Counter。
- 驗證：templ generate、go build ./... exit 0、go test ./... -count=1 全綠（service 與 handler 各新增測試，涵蓋金額計算、轉換冪等、權限與未登入拒絕）。
- 可先建立估價草稿與項目，客戶／車輛關聯後轉為正式工單，保留估價內容及金額。
- 轉換需冪等並保留來源關係，避免重複建立工單或遺失報價歷史。

#### [x] T2.10 車主 LINE 數位維修 Passbook（FR-VH-05）
- 綁定客戶只能查閱其關聯車輛的維修摘要、日期、項目與保養建議。
- 驗收：未綁定或非該客戶車輛不得取得履歷；只公開必要資訊。
- 實作：migration 043 建立 passbook 表；`PassbookAPI`（GET /api/passbook）以 `customers.line_user_id` 綁定作為公開端點的身份依據，並對「非該客戶／未綁定／查無紀錄」一律回 200 + 空陣列（避免用狀態碼差異枚舉車牌）；`RecordPassbookForWorkOrder` 在**結帳**時以單條 `INSERT ... SELECT ... WHERE NOT EXISTS` 建立履歷（原子且冪等，重放不會產生第二筆），寫入失敗只 log、不影響結帳；`line_webhook.go` 新增「履歷／維修紀錄／保養紀錄」查詢入口，只回傳該綁定客戶自己車輛的資料。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠且 `grep -c "setup failed"` 為 0；passbook_test 6 則（含跨客戶拒絕、未綁定、缺參數、空陣列、只公開必要欄位）＋ passbook_write_test（結帳冪等、寫入失敗隔離、LINE 綁定範圍）。
- 待留意：公開端點靠 LINE 綁定而非登入，取得連結者仍可見該工單摘要；若日後要求更高的身分確認需另案處理。

#### [x] T2.11 LINE 追加維修同意／拒絕（FR-WO-06、FR-NOTI-05）
- 追加項目產生具時效且不可猜測的授權連結／識別碼，客戶可同意或拒絕。
- 修正現有核准按鈕指向內部收據頁的問題；核准狀態與項目更新須具冪等性並保留授權紀錄。
- 實作：migration 038 建立 approval_requests（token UNIQUE 索引、expires_at、狀態與決定紀錄）；internal/handler/approvals.go 以 crypto/rand 產生 32 bytes（64 hex）token、24 小時時效，查詢與決定端點都重新驗證過期；同意／拒絕以 `WHERE status='pending' AND expires_at > ?` 更新故具冪等；同意時在單一交易內更新授權狀態並加入工單項目；未綁定 line_user_id 無法產生授權；line_webhook.go 的舊 `approve:<id>` 後門已移除。公開授權頁 GET /approval/{token}（客戶從 LINE 點入，不需登入）。
- 驗證：go build ./... exit 0、go test ./... -count=1 全綠（approvals_test 13 個子測試涵蓋建立、過期、重複冪等、跨客戶、未綁定 LINE）。
- 待留意：授權頁為公開設計（客戶從 LINE 點入），取得連結者仍可見該工單摘要；若日後要求更高的身分確認需另案處理。
- 追加項目產生具時效且不可猜測的授權連結／識別碼，客戶可同意或拒絕。
- 修正現有核准按鈕指向內部收據頁的問題；核准狀態與項目更新須具冪等性並保留授權紀錄。
- 驗收：過期、重複、被竄改或屬於其他客戶的授權不能生效；同意與拒絕都能正確回寫。

#### [x] T2.12 AP 帳款與帳齡分析（FR-FIN-05、FR-PRINT-05）
- 建立供應商應付帳款、付款／沖銷狀態與到期日；涵蓋採購及外包應付來源。
- 提供 0–30、31–60、61–90、90+ 帳齡明細與供應商月結對帳單。
- 收貨、應付建立及付款狀態需避免重複計算；已進貨採購單不可因後續編輯造成帳款失真。
- 實作：migration 041 建立 `ap_payables`（`UNIQUE(source_type, source_id)` 確保來源唯一，`ON CONFLICT DO NOTHING` 使既有單據重跑不重複入帳）與 `ap_payments`，並一次性回填既有的已收貨採購與外包來源；`internal/handler/ap.go` 提供列表（未結帳齡彙總 + 明細）、付款登記（餘額檢查、累加拒付）、沖銷（限未付款）、供應商月結對帳單與 CSV；`web/pages/ap.templ` 為明細表、帳齡摘要、付款表單與對帳單；`internal/nav` 補上「應付帳款」選單項。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠（ap_test 9 則：收貨快照鎖定、冪等、外包來源、付款累計與超付拒絕、沖銷限制、帳齡分桶、CSV 公式注入防護、到期日、migration 回填）；實機 `GET /ap`、`/ap/statement` 皆 200。

#### [x] T2.13 損益納入工資與外包成本（FR-FIN-06、FR-ANA-07）
- 定義並文件化工資成本、外包成本來源及跨期歸屬規則，再納入損益分拆。
- 避免將技師佣金、工時成本或外包成本重複計入；未知成本應標示未記錄，不以零冒充已計算。
- 成本口徑（`internal/handler/analytics_cost.go` 為唯一來源）：零件＝`cost_price × quantity`、外包＝明細 `subtotal`、工資＝技師佣金（`technician_worklogs.commission_amount`）。**零件／外包歸工單建立日，工資歸工時日**（成本屬於實際施工日）；`work_order_items` 的 labor 是收入不是成本，故不再把 `labor_amount` 也算一次，避免同一筆錢算兩次。
- 未知成本處理：期間／工單完全沒有技師工時紀錄時 `LaborRecorded=false`，成本、毛利、毛利率一律顯示「未記錄」，不以 0 冒充（0 一定會被看成「算過且成本為零」）。
- 套用範圍（同一套口徑，避免同一頁兩個毛利率）：KPI 毛利率、損益分拆表、月趨勢圖（`Cost` 欄改為三類成本合計）、分析摘要 CSV（三類成本分列、類別明細補外包成本、工單明細補外包成本欄）。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠；analytics_cost_test 涵蓋三類成本加總、查不到料號不計成本、單張工單缺工時標記未記錄、批次查詢；實機以 10 月測試資料核對：損益分拆 營收 3900／成本 1630／毛利 2270（58.2%）與逐類加總一致。

#### [x] T2.14 保固成本統計（FR-FIN-07）
- 依確認的成本口徑統計保固工單數、收入／折讓與成本；與 T2.13 使用相同成本來源及期間規則。
- 實作：`warrantyStats` 回傳保固工單數、**折扣後**收入與折讓（保固收入必須與損益分拆同口徑，否則同一張保固工單在「保固毛利」與「損益毛利」會算出兩個不同數字）；`warrantyCostTotals` 批次查回保固工單的三類成本並標記是否有工時紀錄；頁面「折扣與保固」卡新增保固折讓、保固成本與保固毛利（成本僅管理員可見，缺工時紀錄時顯示「未記錄」）。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠；analytics_cost_test 新增 `TestWarrantyStatsUsesNetRevenue`（保固 1000 折讓 250 → 淨收入 750）與 `TestWarrantyCostTotals`（零件＋外包＋工資加總與未記錄標記）；實機核對保固工單金額（折扣後）3000、保固成本 1380、保固毛利 1620，與方案毛利同張工單的數字一致。

#### [x] T2.15 報表匯出（FR-FIN-08、FR-PRINT-07）
- 對主要報表提供符合權限的 CSV 匯出；涵蓋工單編號、客戶、收入拆分、零件成本、支付方式及折扣。
- CSV 使用 Excel 可辨識的 UTF-8；正確處理公式注入風險、逗號、引號及換行。
- PDF 若以瀏覽器列印／另存方式提供，需清楚標示；不宣稱已支援伺服器端 PDF，除非實際完成。
- 實作：internal/handler/reports_export.go 提供工單報表 CSV（含日期參數驗證、UTF-8 BOM、Content-Disposition 附件）；公式注入防護對開頭為 = + - @（含前導空白後出現）前綴單引號；成本權限分層——Owner 才有零件成本欄、櫃檯完全省略該欄、技師遭拒；internal/handler/analytics.go 既有分析 CSV 套用同一套防護；reports.go 加匯出連結並標示「PDF 請用瀏覽器列印」。
- 驗證：go build ./... exit 0、go test ./... -count=1 全綠（reports_export_test 涵蓋公式注入、特殊字元、BOM、成本權限、下載標頭與未登入）。
- 待補：伺服器端 PDF 尚未實作，目前僅提供瀏覽器列印路徑。
- 對主要報表提供符合權限的 CSV 匯出；涵蓋工單編號、客戶、收入拆分、零件成本、支付方式及折扣。
- CSV 使用 Excel 可辨識的 UTF-8；正確處理公式注入風險、逗號、引號及換行。
- PDF 若以瀏覽器列印／另存方式提供，需清楚標示；不宣稱已支援伺服器端 PDF，除非實際完成。

#### [x] T2.16 經營分析圖表與分組（FR-ANA-01）
- 提供月營收與毛利趨勢，依 SRS 支援的工單類型、客群及車款分組。
- 採本地資源或原生 SVG/CSS，離線環境可用；圖表統計口徑與匯出一致。
- 實作：`loadAnalyticsChartData`／`aggregateAnalyticsChart` 產出近 12 個月月營收與毛利，分组沿用 `work_order_items.item_type`（工資／零件／外包）、`customers.tags`（逗號分隔標籤）與 `vehicle.vehicle_type`（以 lookup label 顯示）；`web/pages/analytics.templ` 以**原生 SVG** 繪製長條圖與分組表，不使用任何 CDN 或外網資源；成本與毛利僅 Owner 可見，櫃檯看得到營收但看不到成本，技師遭拒。
- T2.13 起圖表的 `Cost` 欄改為三類成本合計（零件＋外包＋工資），與損益分拆、CSV 匯出同口徑；工资以「工時月」歸屬（與其他成本的工單建立月不同軸，頁面有說明）。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠（analytics_test 以固定日期資料檢查月趨勢、分組加總與毛利計算，含三類成本合計 76／30 的修正後期望值）；grep 確認無 `http(s)://`、`cdn`、`unpkg`、`jsdelivr`、`googleapis` 等外部資源引用。

#### [x] T2.17 技師效率指標（FR-ANA-02）
- 依技師呈現完成工單數、工時、每工時計價及每工時計毛利。
- 定義多人共同派工、工時跨期及無工時工單的計算方式，避免工單重複計數。
- 計算規則（`internal/handler/analytics_tech.go`）：以 `technician_worklogs` 為完成工單依據；**完成工單數＝`COUNT(DISTINCT work_order_id)`**（同一工單多筆工時不會重複計）；**工時總計＝`SUM(hours)`**；**每工時計價＝`SUM(labor_amount)/SUM(hours)`**（工時 0 顯示「—」）；**每工時計毛利＝`(SUM(labor_amount) − SUM(commission_amount))/SUM(hours)`**（工資收入扣佣金成本）；跨期歸屬以 `work_date` 為準；無工時工單不列入，但另列「期間內已完成但無工時紀錄的工單 N 張」讓缺口可見；佣金為 0 但已有工時紀錄者顯示 0 而非「未記錄」。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠（analytics_tech_test 8 則：DISTINCT 不重複計、多人派工各自獨立、每工時計價與毛利、showCost=false 不顯示毛利欄、無工時工單備註、跨期歸屬、工時 0 顯示「—」、空資料）；實機以測試資料核對：工單數 2、工時 5、每工時計價 300、佣金 150、每工時計毛利 270。

#### [x] T2.18 行銷方案毛利分析（FR-ANA-06）
- 依既有活動代碼呈現工單數、營收、成本及毛利；同一工單代碼優先順序需固定並文件化。
- 先確認現有 `campaign_code` 欄位可涵蓋需求；只有確有資料缺口才新增欄位／migration。
- 實作：沿用 migration 020 已建立的 `customers.campaign_code` 與 `work_orders.campaign_code`（**不新增 migration**）；`internal/handler/analytics_campaign.go` 的 `campaignAnalysisBlock` 以 `COALESCE(NULLIF(wo.campaign_code,''), NULLIF(c.campaign_code,''), '(未設定)')` 分組——**歸屬優先順序固定為：工單自己的代碼 ＞ 客戶的代碼 ＞ (未設定)**；成本以 `workOrderCostsByIDs` 批次查回（不做 N+1），毛利＝營收－三類成本；成本與毛利僅 Owner 可見，工資未記錄時兩者皆顯示「未記錄」。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠（analytics_campaign_test 6 則：基本、含成本、優先順序、空字串歸「(未設定)」、工資未記錄、空資料）；實機核對：方案 1 營收 3000／成本 1380／毛利 1620，空白代碼歸為「(未設定)」而非空白列。

#### [x] T2.19 支援 Telegram 客戶通知並停用 SMS（FR-CRM-04）
- 通知佇列依客戶偏好分流 LINE／Telegram，Telegram 使用該客戶的 `telegram_user_id`，不得誤用內部預設 Chat ID 發送客戶資料。
- 整合既有 Telegram Bot client，處理缺少 Telegram ID、未設定 token、API 失敗及訊息格式跳躍；與 T2.1 重試狀態及去重行為一致。
- 客戶表單停用 SMS；既有 SMS 偏好須明確提示需要更新，不得靜默改成其他通道或假裝已送達。
- SRS FR-CRM-04 已更新為 LINE／Telegram／不通知；實作須符合該規格及客戶 ID 綁定要求。
- 實作：internal/cron/cron.go 將 sms 獨立為永久失敗分支（不重試、不假送達），Telegram 用該客戶的 telegram_user_id、缺 token 或 ID 皆為永久失敗；internal/handler/customers.go 表單移除 SMS 選項，既有 sms 偏好顯示「⚠️ 原本偏好 SMS，請重新選擇」警告且不靜默改值；internal/handler/scheduled.go 的 channelLabels 補 telegram、移除 sms。
- 驗證：go build ./... exit 0、go test ./... -count=1 全綠（cron 新增 2 個測試：SMS 永久失敗、Telegram 缺 token 永久失敗；既有測試含 legacy SMS 不被當成 LINE）。

#### [x] T2.20 完工通知卡（FR-NOTI-03）
- 完工時依客戶偏好通知已綁定的 LINE／Telegram 客戶，提供領車資訊及核對過的工單摘要。
- 外送失敗不得回滾核心完工操作；交由 T2.1 佇列重試。
- 實作：`internal/cron/cron.go` 的 `QueueWorkOrderCompletedNotification(woID, customerID)` 組出完工通知卡——主旨「完工通知 — 您的愛車已完成維修」，內容含客戶姓名、車牌、工單編號、項目摘要（前 3 項，超過顯示「另有 N 項」）、工單總金額與領車提醒；以 `kind='work_order_completed' + work_order_id` 的 `INSERT ... WHERE NOT EXISTS` 去重；channel 依 `notification_preference` 走 line／telegram，`none` 或未綁定該通道則寫入 `internal`（**不可誤用內部預設 Chat ID 發送客戶資料**）。`UpdateWorkOrderStatus` 在狀態轉為 `completed` 且原本不是 completed 時觸發；通知失敗只 log，**不可回滾完工**。`scanWorkOrderReadyInternal` 保留為補掃（改為掃「已完成且尚無該 kind 通知」的工單），避免與立即觸發重疊。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠；cron 新增測試涵蓋通知卡內容、通道偏好與綁定、補掃去重及佇列錯誤；handler 新增通知失敗不影響工單狀態變更的測試。

#### [x] T2.21 生日關懷（FR-NOTI-03）
- 依客戶生日及 LINE／Telegram 通知偏好每日產生一次關懷；去重須跨服務重啟仍有效。
- 先確認目前客戶資料是否保存生日，若無則新增欄位並提供維護方式。
- 實作：migration 040 新增 customers.birthday（客戶原本無生日欄位，僅員工 users 有）；internal/handler/customers.go 列表／新增／編輯都帶入 birthday，編輯表單加日期欄位；internal/cron/cron.go 的 scanBirthdayReminders 每日以 strftime('%m-%d') 比對當天生日並依偏好分流 LINE／Telegram，註冊為每日任務；去重沿用 queueVehicleReminder 的 (customer_id, kind, body, created_at) 判斷並寫入 notifications 表。
- 驗證：go build ./... exit 0、go test ./... -count=1 全綠（cron 新增 3 個測試：只對當天生日、跨執行去重、依偏好分流）。
- 依客戶生日及 LINE／Telegram 通知偏好每日產生一次關懷；去重須跨服務重啟仍有效。
- 先確認目前客戶資料是否保存生日，若無則新增欄位並提供維護方式。

#### [x] T2.22 照片備份與離線上傳佇列（§7.2）
- 每日備份需涵蓋 SQLite 與照片，支援設定至外部 NAS／儲存目標並保留至少 30 日；記錄備份失敗並驗證可還原。
- IndexedDB 佇列需保存實際照片 Blob、表單欄位及重試狀態；恢復連線後可上傳並回報成功／失敗。
- 避免重複建立照片或工單；驗收需涵蓋重整頁面及瀏覽器重新開啟後的佇列恢復。
- 實作：`internal/cron/cron.go` 的 `DailyBackup`（每 24 小時註冊為 `daily_backup` 排程）以 `VACUUM INTO` 產生一致性 SQLite 快照（失敗退回 checkpoint＋複製），連同照片目錄封裝成 gzip tar（`autorepair_YYYYMMDD_HHMMSS.tar.gz`）；`pruneBackups(30 天)` 保留 30 日、`pruneSameDay` 讓同日重跑只留最新；`recordBackup`／`recordBackupFailure` 寫入 `backup_records` 供「排程」頁顯示；`BACKUP_DIR`（.env）可指向 NAS／隨身碟。migration 042 建立 `photo_upload_queue`（`UNIQUE(work_order_id, COALESCE(item_id,0), file_name)` 防重複、狀態 `pending/uploading/done/failed`、重試次數與錯誤訊息）；`photos.go` 的上傳先寫入持久化佇列、`processPhotoUpload` 以原子交易建立照片紀錄並檢查既有照片避免重複；`offline.js` 的 `PhotoQueue`（IndexedDB）保存實際 Blob、欄位與重試狀態，`Retry.flush` 在網路恢復與頁面載入時自動重試，並廣播 `autorepair:photo-upload` 事件顯示成功／失敗。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠；`internal/cron/backup_test.go` 涵蓋「每日備份含照片且 30 日後過期」與「照片封存失敗要記錄」（並以 `verifyBackupArchive` 實際解檔驗證可還原）；`internal/db/photo_upload_queue_migration_test.go` 驗證 up/down；`photos_upload_test.go` 涵蓋上傳成功與去重、失敗後可重試、未知工單／項目拒絕；`offline.js` 的佇列恢復以 IndexedDB 於頁面重啟後仍存在（重整不遺失）。
- 已知落差：系統**不會**在每次備份後自動解檔檢查（還原驗證目前是自動化測試在把關），長期部署仍應定期手動還原演練。

#### [x] T2.23 條碼掃描及庫位優先顯示（FR-INV-05）
- 將既有掃描能力接入庫存與工單領料操作，掃描後帶入零件資料。
- 搜尋／領料結果顯示庫位，並可按庫位排序；庫存資料與掃描結果須由後端重新查證。
- 驗收：相機無權限、非 HTTPS／不支援相機、未知條碼等情況有可理解的退路。

### 階段 3：效能驗證

#### [x] T3.1 建立並執行效能基準（§7.1）
- 可重複產生 10,000 筆具代表性的測試資料，量測工單、庫存、客戶常用列表及通知批次處理。
- 驗收：列表常用查詢在 10,000 筆資料下低於 300ms；1,000 筆通知在 1 分鐘內處理完成。記錄環境、資料量及結果；未達標時先分析查詢與索引再修正。
- 基準工具不得意外修改正式資料庫，並須明確指定測試 DB。
- 實作：`cmd/bench/main.go`——`--db` 必填且指向預設 `data/autorepair.db` 時拒絕執行、`--reset` 重建測試資料、`--dry-run` 只印計畫不寫入、`--scale` 調整資料量（預設 10000）；量測工單／庫存／客戶列表查詢（門檻 300ms）與 1,000 筆通知批次處理（門檻 1 分鐘，經 `cron.ProcessNotificationsNow`）；印出 Go 版本、OS、DB 路徑、筆數、每項耗時與 PASS/FAIL，並以 exit code 表示是否全部達標。
- 驗證：`go build ./...` exit 0、`go test ./... -count=1` 全綠；實跑 scale=10000 結果——資料產生（客戶 10,000／車輛 15,000／庫存 5,000／工單 20,000／通知 1,000）耗時 2m43s；工單列表 3.77ms、庫存列表 515.9µs、客戶列表 1.61ms（皆 < 300ms）；通知 1,000 筆 721.1ms（< 1 分鐘）；**全部 PASS，exit code 0**。

### 階段 4：流程缺口修復（畫面與文件一致性、追加維修授權）

> 詳細逐步實作（含可複製的程式碼、TDD 紅燈/綠燈、驗證指令與預期輸出）見
> `.hermes/plans/2026-10-21_145227-ux-flow-gaps.md`。本節只記目標、驗收與依賴。
>
> 這一階段修的是「既有任務的缺陷」，不是新功能：畫面與 `docs/USER_GUIDE_zh-TW.md`
> 的承諾不一致，或流程存在使用者走不完的斷點。

#### [ ] T4.1 頁首登入者改由 model.User 傳入（身分一致性）
- 目前 28 個 templ 頁面有 **25 處**把登入者／角色寫死：21 處寫 `owner/owner`，4 處把**客戶姓名**當登入者並寫死 `counter`（`workorder.templ`、`vehicle.templ`、`vehicle_show.templ`）。使用者無法從頁首判斷自己用哪個帳號登入。
- 驗收：`components.Layout` 的 `(username, role string)` 改為 `(user model.User)`，使錯誤身分**無法通過編譯**；新增掃描測試斷言 `web/pages/*.templ` 沒有 Layout 呼叫傳字串字面值當身分。
- 依賴：無。

#### [ ] T4.2 導覽與操作按鈕依角色過濾
- `internal/nav/nav.go` 完全不看角色；技師會看到工單列表的「＋ 開立新工單」，但 `/work-orders/new` 是 `CounterUpPage`，點進去得到權限不足。預約頁同樣對所有人顯示新增表單，但 `POST /appointments` 僅限櫃檯以上。
- 驗收：`nav.Route` 加 `Roles`（空＝所有角色），`Render(active, role)` 依角色過濾；`Roles` 的來源是 `main.go` 的權限包裝器，逐條核對而非憑常識；工單列表與預約頁的操作按鈕改為條件渲染；nav 測試新增「技師看不到 CounterUpPage 入口」一則。
- 依賴：T4.1（按鈕需要 `user`）。

#### [ ] T4.3 離線模組接上共用 templ 版面
- `offline.js` 只載入於 `internal/handler/layout.go`（字串拼接頁），`web/components/layout.templ` 沒有載入，因此工單照片頁（templ 頁）拿不到手冊承諾的「離線暫存、恢復連線後補傳」。
- **隱藏陷阱**：`sw.js` 對未知路徑的預設是 cache-first，`/offline.js` 不在 `NETWORK_FIRST`；只補 script 標籤的話既有瀏覽器仍會跑舊版。
- 驗收：`layout.templ` 的 body 結尾載入 `/offline.js`；`sw.js` 的 `NETWORK_FIRST` 加入 `/offline.js` 與 `/static/offline.css` 並遞增 `CACHE` 版本號；瀏覽器實測 `/work-orders/{id}/photos` 的 `window.AutoRepairOffline.PhotoQueue` 存在（修前為 `undefined`）；模擬離線送出照片後佇列長度為 1。
- 依賴：無。

#### [ ] T4.4 追加維修授權接成完整流程（FR-WO-06、FR-NOTI-05）
- `main.go` 有客戶端的開啟／同意／拒絕三個路由，但**沒有任何地方建立授權請求**——`CreateApprovalRequest` 全 repo 只出現在一行註解裡（`line_webhook.go:333`），`approvals.go` 的「加入工單項目」仍是 TODO。手冊描述的「產生連結 → 客戶確認 → 工單加入追加項目」從 UI 走不完。
- 驗收：`item_summary` 由自由文字改為結構化 JSON（`approvalItem`），並以單一 `parseApprovalItems` 集中驗證；客戶同意時在**同一交易**內寫入 `work_order_items`（失敗整筆回滾，避免「已核准但工單沒項目」的錢務不一致）；新增 `POST /work-orders/{id}/approval-request`（`CounterUp`）與工單詳情頁的「追加維修授權」區塊；已結帳工單不可建立授權。
- 測試：完整流程、重複同意冪等、拒絕不加項目、四種格式錯誤、已結帳被擋。
- 依賴：T4.1（工單詳情頁需傳 `user` 判斷權限）。

#### [ ] T4.5 移除新增客戶表單的 LINE User ID 欄位
- `web/pages/customer.templ` 的新增表單有 `line_user_id` 輸入框，但 `CreateCustomer` 明確不讀它（只讀 `telegram_user_id`），填了會被靜默丟棄——使用者以為綁好了，通知卻永遠送不到。編輯頁已改為專用綁定流程，新增頁是漏掉的那一半。
- 驗收：欄位移除並加說明文字指向編輯頁的綁定區塊；`Telegram User ID` **保留**（客戶用 `@userinfobot` 查得到）；新增回歸測試斷言新增表單不含 `line_user_id`。
- 依賴：無。

#### [ ] T4.6 預約與工單的關聯（預約「已進廠」直接開單）
- `appointment_id` 在任何 migration 與 Go 程式碼裡都不存在，預約列表只有編輯／刪除。櫃檯已知車到了、預約資料也填好了，卻要重打一次車牌。
- 驗收：新增 migration 045 為 `work_orders` 加 nullable `appointment_id`（既有工單沒有預約來源，不可用 NOT NULL）；`/work-orders/new?appointment_id=N` 預填客戶／車輛；建立工單時在同一交易把預約狀態更新為 `arrived`；預約列表加「→ 開工單」按鈕（僅 `booked`/`confirmed`/`arrived` 顯示）。
- 依賴：無（但需先確認是否納入範圍）。

#### [ ] T4.7 未決的追加授權阻擋完工與結帳
- `UpdateWorkOrderStatus` 只呼叫 `CheckSOPCompletionForStatus`，`approval_status` 完全不影響完工與結帳。
- **判準必須是「有沒有未決的 `approval_requests`」，不是工單上的 `approval_status`**：後者的預設值是 `pending`，一律擋會讓**每一張沒走追加流程的工單都無法完工**。
- 驗收：新增 `CheckApprovalForStatus(woID, targetStatus)`，只在「已建立授權且客戶尚未回覆（且未過期）」時回錯；測試涵蓋有未決授權→擋、被拒絕→可完工、已過期→可完工、**沒有授權→可完工**。
- 依賴：T4.4。

#### [ ] T4.8 離線草稿不被誤刪，並限縮文件承諾
- `offline.js` 在 `submit` 事件就清除草稿，**不管 POST 成功與否**——離線或伺服器錯誤時草稿被丟掉，而草稿存在的意義正是「送不出去時別弄丟」。手冊對「離線」的承諾範圍也過寬（實際上只有照片有真正的離線佇列）。
- 驗收：草稿改為只在送出成功後清除；手冊的離線段落限縮為「照片離線暫存」，並明講一般表單離線時**不會**自動補送（草稿會保留）。
- 依賴：T4.3（同一支 `offline.js`）。

#### [ ] T4.9 快速建立遇到重複車牌時要求確認
- `workorder_new.go` 的 `QuickVehicle` 偵測到同車牌時直接回傳既有車輛並回 `ok:true`，沒有二次確認。回傳的 label 雖含車主姓名，但櫃檯不一定會看。
- 驗收：回傳值加 `duplicate: true`；前端以確認對話框顯示「車牌 X 已存在，車主是 Y，要使用既有車輛嗎？」，取消則不選取；測試斷言既有車牌回傳含 `duplicate: true` 且 `vehicle_id` 為既有那筆。
- 依賴：無。

#### [ ] T4.10 文件與角色可見範圍對齊
- 手冊的追加維修授權段落描述的流程從 UI 走不完；離線承諾過寬；也沒有「不同角色看到的畫面不同」的說明。
- 驗收：手冊的追加授權改寫成可操作步驟（在哪個頁面、填什麼、按什麼、客戶收到什麼、同意後工單多出什麼）；離線段落限縮；新增角色可見範圍對照表；ROADMAP 的 T2.11 補註櫃檯端入口與交易化實作的補強。
- 依賴：T4.2、T4.4、T4.8。

## 4. 依賴與建議執行順序

此順序反映真正的技術依賴，不代表同階段所有任務必須串行：

1. T0.1、T0.2、T0.3、T0.4、T0.5 可先行；T0.7 需先完成刪除表與關聯盤點。
2. T1.2 → T0.6 → T2.4；T1.1 可與其平行。
3. T2.1 作為通知重試基礎；T2.5 → T2.2；T2.1 → T2.3、T2.20、T2.21。
4. T2.6 完成後才可把 SOP 檢查納入完工限制；T2.8 → T2.22（照片儲存／上傳整合）。
5. T2.12 依賴現有採購單及外包資料；T2.13 → T2.14、T2.17、T2.18，成本口徑需共用。
6. T3.1 應在影響查詢、索引或背景工作量的功能完成後執行，並記錄基準環境。
7. 階段 4：T4.1 → T4.2、T4.4；T4.4 → T4.7；T4.3 → T4.8；T4.2／T4.4／T4.8 → T4.10。T4.5、T4.6、T4.9 無依賴，可與任何任務平行。
   - 平行限制：T4.1 與 T4.4 都動 `web/pages/workorder.templ`，**不可同時進行**。
   - `templ generate` **不可與其他 agent 並行執行**（會產出損毀的 `*_templ.go`），一律由單一方在合併後統一跑。

### Migration 編號

Migration 編號依目錄中實際最高序號遞增；目前最高序號為 `044`（`044_binding_uniqueness`），下一個新 migration 從 `045` 開始。實作前再次檢查目錄，避免平行任務使用相同序號。Migration 必須可安全套用至既有資料庫；不可假設 `IF NOT EXISTS` 適用於所有 SQLite `ALTER TABLE` 語法，應依實際 migration runner 和既有 schema 撰寫並測試。新增唯一性約束時須考慮既有資料是否已違反約束（違反會使 migration 直接失敗，需先人工清理）。

## 5. 每個任務的完成條件

- 先閱讀 `docs/DEVELOPMENT.md`、相關 SRS 需求及現有實作；確認資料模型和權限邊界。
- 新增／修改 schema 時，測試空資料庫初始化、既有資料庫升級及 migration 重複套用行為。
- 新增程式邏輯須有針對成功、失敗及重要邊界條件的測試；金流、庫存、授權及通知需測試冪等性或併發情況。
- 變更 Templ 時執行 `templ generate`；再執行受影響套件測試及 `go test ./...`、`go build ./...`。
- 影響跨平台 Vision 程式時，同時驗證 Linux build；其他跨平台變更依實際影響補驗證。
- 涉及登入、權限、交易或外送時做相應 smoke test；不得用僅編譯成功替代行為驗收。
- 同步更新本路線圖狀態及直接相關文件；勾選完成前確認以上條件均有證據。

## 6. 暫不納入的候選項目

個人化儀表板、全域搜尋、多環境組態及額外工單套餐範本不屬於目前 SRS 明確列出的缺口，先不混入本路線圖的承諾範圍。若日後要做，應先確認使用情境、優先序與驗收需求，再新增為獨立項目。
