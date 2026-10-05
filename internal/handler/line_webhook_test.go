package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
	"autorepairos/internal/seed"
)

// setupLineWebhook 準備一個帶真實 channel secret 的測試環境。
func setupLineWebhook(t *testing.T, secret string) *httptest.Server {
	t.Helper()
	os.Setenv("LINE_CHANNEL_ID", "1234567890")
	os.Setenv("LINE_CHANNEL_SECRET", secret)
	os.Setenv("LINE_CHANNEL_TOKEN", "dummy-token-for-test")
	// Shared() 會快取設定，測試需重載才能套用新的環境變數。
	line.ResetSharedForTest()
	t.Cleanup(line.ResetSharedForTest)

	dir := t.TempDir()
	cfg := &config.Config{
		DBPath:            "file:" + dir + "/hook.db?_busy_timeout=5000",
		RunSeed:           true,
		DefaultOwner:      "owner",
		DefaultOwnerPwd:   "Owner001!",
		DefaultCounter:    "counter",
		DefaultCounterPwd: "Counter001!",
		DefaultTech:       "tech",
		DefaultTechPwd:    "Tech001!",
	}
	if err := db.Init(cfg); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	seed.Run(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook/line", LineWebhook)
	return httptest.NewServer(mux)
}

// sign 依 LINE 的方式產生簽名：HMAC-SHA256(channelSecret, body) 前綴 "signature="。
func sign(secret string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return "signature=" + hex.EncodeToString(h.Sum(nil))
}

func postEvent(t *testing.T, srv *httptest.Server, body string, signature string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", srv.URL+"/webhook/line", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if signature != "" {
		req.Header.Set("X-Line-Signature", signature)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	return resp
}

const testSecret = "test-channel-secret-abc123"

// 簽名錯誤必須被拒絕——這是 webhook 的唯一防線，驗不過就等於門戶大開。
func TestWebhookRejectsBadSignature(t *testing.T) {
	srv := setupLineWebhook(t, testSecret)
	defer srv.Close()

	body := `{"destination":"1234567890","events":[{"type":"follow","source":{"type":"user","userId":"U_injected"},"replyToken":"rt"}]}`

	for _, sig := range []string{
		"",                                 // 完全沒有簽名
		"signature=deadbeef",               // 亂簽
		sign("wrong-secret", []byte(body)), // 用錯的 secret
	} {
		resp := postEvent(t, srv, body, sig)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("sig=%.20q got %d, want 401", sig, resp.StatusCode)
		}
	}

	// 確認攻擊者真的沒能把 ID 寫進資料庫
	var n int
	db.DB.QueryRow("SELECT COUNT(1) FROM customers WHERE line_user_id = 'U_injected'").Scan(&n)
	if n != 0 {
		t.Error("偽造請求竟成功寫入 line_user_id")
	}
}

// 簽名正確時應接受，且 follow 事件不該自動綁錯客戶。
func TestWebhookAcceptsValidSignature(t *testing.T) {
	srv := setupLineWebhook(t, testSecret)
	defer srv.Close()

	body := `{"destination":"1234567890","events":[{"type":"follow","source":{"type":"user","userId":"U_newuser"},"replyToken":"rt123"}]}`
	resp := postEvent(t, srv, body, sign(testSecret, []byte(body)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid signature got %d, want 200", resp.StatusCode)
	}
	t.Log("follow 事件已接受")

	// 應寫入一則 line_follow 通知提醒櫃檯手動綁定，而不是猜測客戶
	var kind string
	err := db.DB.QueryRow("SELECT kind FROM notifications WHERE kind='line_follow' LIMIT 1").Scan(&kind)
	if err != nil {
		t.Errorf("未寫入 line_follow 提醒: %v", err)
	}
}

// 空 events（LINE 後台的 Verify 按鈕）應回 200。
func TestWebhookEmptyEventsAccepted(t *testing.T) {
	srv := setupLineWebhook(t, testSecret)
	defer srv.Close()

	body := `{"destination":"1234567890","events":[]}`
	resp := postEvent(t, srv, body, sign(testSecret, []byte(body)))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("empty events got %d, want 200", resp.StatusCode)
	}
}

// 已綁定的 LINE user 再次送 follow 事件，不應覆寫其他客戶的綁定。
func TestWebhookFollowDoesNotStealBinding(t *testing.T) {
	srv := setupLineWebhook(t, testSecret)
	defer srv.Close()

	db.MustExec(`INSERT INTO customers(name, phone, line_user_id) VALUES ('王小姐','0911000000','U_owner')`)

	body := `{"destination":"1234567890","events":[{"type":"follow","source":{"type":"user","userId":"U_owner"},"replyToken":"rt"}]}`
	resp := postEvent(t, srv, body, sign(testSecret, []byte(body)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}

	var name, lineID string
	if err := db.DB.QueryRow("SELECT name, line_user_id FROM customers WHERE line_user_id='U_owner'").Scan(&name, &lineID); err != nil {
		t.Fatalf("原本的綁定不見了: %v", err)
	}
	if name != "王小姐" {
		t.Errorf("綁定被改動，name=%s", name)
	}
}

// 客戶查詢進度：只應回覆自己的工單。
func TestWebhookCustomerStatusQuery(t *testing.T) {
	srv := setupLineWebhook(t, testSecret)
	defer srv.Close()

	db.MustExec(`INSERT INTO customers(name, phone, line_user_id) VALUES ('王小姐','0911000001','U_cust1')`)
	db.MustExec(`INSERT INTO vehicles(customer_id, plate_number, brand_model) VALUES (1,'ABC-1234','Vitz')`)
	db.MustExec(`INSERT INTO work_orders(vehicle_id, customer_id, status, current_mileage) VALUES (1,1,'in_progress',1000)`)

	// 這位客戶自己查
	reply := handleCustomerMessage(nil, messageEventForTest("U_cust1", "進度"))
	if !strings.Contains(reply, "ABC-1234") || !strings.Contains(reply, "進行中") {
		t.Errorf("自己的工單查詢結果不正確: %q", reply)
	}

	// 別人查同一個「工單 1」，不該拿到資料
	reply2 := handleCustomerMessage(nil, messageEventForTest("U_stranger", "工單 1"))
	if !strings.Contains(reply2, "找不到") {
		t.Errorf("他人查詢應被拒絕，得到: %q", reply2)
	}
	t.Logf("自己的查詢: %s", reply)
	t.Logf("他人的查詢: %s", reply2)
}

// postback 核准：只允許核准自己的工單，且已結帳不可再改。
func TestWebhookPostbackApprove(t *testing.T) {
	srv := setupLineWebhook(t, testSecret)
	defer srv.Close()

	db.MustExec(`INSERT INTO customers(name, phone, line_user_id) VALUES ('李先生','0911000002','U_cust2')`)
	db.MustExec(`INSERT INTO vehicles(customer_id, plate_number, brand_model) VALUES (1,'XYZ-9999','Corolla')`)
	db.MustExec(`INSERT INTO work_orders(vehicle_id, customer_id, status, current_mileage, approval_status) VALUES (1,1,'pending',1000,'pending')`)

	handlePostback(nil, postbackEventForTest("U_cust2", "approve:1"))

	var approval string
	if err := db.DB.QueryRow("SELECT approval_status FROM work_orders WHERE id=1").Scan(&approval); err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if approval != "approved" {
		t.Errorf("核准未生效，approval_status=%s", approval)
	}

	// 已結帳的工單不應被 postback 改動
	db.MustExec(`INSERT INTO work_orders(vehicle_id, customer_id, status, current_mileage, approval_status) VALUES (1,1,'paid',1000,'pending')`)
	handlePostback(nil, postbackEventForTest("U_cust2", "approve:2"))
	var a2 string
	db.DB.QueryRow("SELECT approval_status FROM work_orders WHERE id=2").Scan(&a2)
	if a2 != "pending" {
		t.Errorf("已結帳工單不應被核准，變成 %s", a2)
	}
}

// 未設定 channel secret 時應拒絕，而不是無條件放行。
func TestWebhookWithoutSecretRefuses(t *testing.T) {
	srv := setupLineWebhook(t, "")
	defer srv.Close()

	body := `{"events":[{"type":"follow","source":{"userId":"U_x"},"replyToken":"rt"}]}`
	resp := postEvent(t, srv, body, sign("anything", []byte(body)))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("未設定 secret 時應回 503，得到 %d", resp.StatusCode)
	}
}

// 請求 body 必須是合法 JSON。
func TestWebhookMalformedJSON(t *testing.T) {
	srv := setupLineWebhook(t, testSecret)
	defer srv.Close()

	body := `{not json`
	resp := postEvent(t, srv, body, sign(testSecret, []byte(body)))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("壞 JSON 應回 400，得到 %d", resp.StatusCode)
	}
}

// event JSON 結構解析：確認欄位名稱與 LINE 實際格式一致。
func TestParseWebhookStructures(t *testing.T) {
	raw := []byte(`{"destination":"Uabc","events":[
		{"type":"follow","source":{"type":"user","userId":"Ufollow"},"replyToken":"rt1","timestamp":1},
		{"type":"message","source":{"type":"user","userId":"Umsg"},"replyToken":"rt2",
		 "timestamp":2,"message":{"id":"m1","type":"text","text":"進度"}},
		{"type":"postback","source":{"type":"user","userId":"Upb"},"replyToken":"rt3",
		 "postback":{"data":"approve:7"}},
		{"type":"message","source":{"userId":"Uimg"},"message":{"id":"m2","type":"image"}}
	]}`)
	ev, err := parseWebhookForTest(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(ev.FollowEvents()) != 1 || ev.FollowEvents()[0].UserID != "Ufollow" {
		t.Errorf("follow 解析錯誤: %+v", ev.FollowEvents())
	}
	msgs := ev.MessageEvents()
	if len(msgs) != 1 || msgs[0].Text != "進度" {
		t.Errorf("message 解析錯誤（圖片應被略過）: %+v", msgs)
	}
	pbs := ev.PostbackEvents()
	if len(pbs) != 1 || pbs[0].Data != "approve:7" {
		t.Errorf("postback 解析錯誤: %+v", pbs)
	}
}

// 未使用的匯入避免編譯警告；確保 json 套件確實被使用。
var _ = json.Marshal
var _ = io.Discard

// 以下為測試用的小便利函式，避免直接依賴 line 套件的型別細節。

func messageEventForTest(userID, text string) line.MessageEvent {
	return line.MessageEvent{UserID: userID, ReplyToken: "rt", Text: text}
}

func postbackEventForTest(userID, data string) line.PostbackEvent {
	return line.PostbackEvent{UserID: userID, ReplyToken: "rt", Data: data}
}

func parseWebhookForTest(raw []byte) (*line.WebhookEvent, error) {
	return line.ParseWebhook(raw)
}
