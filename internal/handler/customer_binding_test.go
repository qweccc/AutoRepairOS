package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// 客戶 LINE / Telegram 綁定的行為測試。
//
// 這裡的重點不是「能不能綁」，而是「能不能阻止把同一個 LINE 帳號綁到兩個客戶」——
// 那會讓 A 客戶在 LINE 查到 B 客戶的維修紀錄與金額（跨客戶資料外洩）。

func setupBindingTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	statements := []string{
		`CREATE TABLE customers (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, phone TEXT UNIQUE,
			line_user_id TEXT, telegram_user_id TEXT, address TEXT,
			notification_preference TEXT, notes TEXT, tags TEXT, campaign_code TEXT,
			birthday TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		// migration 044 的兩條 partial unique index：DB 層的最後防線。
		// 測試要一起建立，否則測不到「繞過 handler 也擋得住」。
		`CREATE UNIQUE INDEX idx_customers_line_user_unique ON customers(line_user_id)
			WHERE line_user_id IS NOT NULL AND line_user_id <> ''`,
		`CREATE UNIQUE INDEX idx_customers_telegram_user_unique ON customers(telegram_user_id)
			WHERE telegram_user_id IS NOT NULL AND telegram_user_id <> ''`,
	}
	for _, s := range statements {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("setup binding db: %v", err)
		}
	}
}

func bindingFixture(t *testing.T, name, phone string) int64 {
	t.Helper()
	res, err := db.DB.Exec(`INSERT INTO customers (name, phone) VALUES (?, ?)`, name, phone)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func lineID(n int) string {
	// 產生合法的 U + 32 hex 格式
	base := "0123456789abcdef"
	s := "U"
	for i := 0; i < 32; i++ {
		s += string(base[(n+i)%16])
	}
	return s
}

// 綁定成功：狀態變成已綁定。
func TestBindCustomerLineSucceeds(t *testing.T) {
	setupBindingTestDB(t)
	id := bindingFixture(t, "王先生", "0911000001")
	line := lineID(1)

	req := httptest.NewRequest(http.MethodPost, "/customers/1/line-bind",
		strings.NewReader(url.Values{"line_user_id": {line}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	BindCustomerLine(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("應 302 導回編輯頁，得到 %d", rec.Code)
	}
	var got string
	if err := db.DB.QueryRow(`SELECT COALESCE(line_user_id,'') FROM customers WHERE id=?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != line {
		t.Errorf("line_user_id = %q，應為 %q", got, line)
	}
	// 成功訊息走 msg=（綠色）
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "msg=") {
		t.Errorf("成功應帶 msg= 參數，Location=%q", loc)
	}
}

// 核心防線：同一個 LINE 帳號不能綁到第二個客戶。
func TestBindCustomerLineRejectsIDOwnedByOtherCustomer(t *testing.T) {
	setupBindingTestDB(t)
	first := bindingFixture(t, "甲客戶", "0911000001")
	second := bindingFixture(t, "乙客戶", "0911000002")
	line := lineID(2)

	if _, err := db.DB.Exec(`UPDATE customers SET line_user_id=? WHERE id=?`, line, first); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/customers/2/line-bind",
		strings.NewReader(url.Values{"line_user_id": {line}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "2")
	rec := httptest.NewRecorder()

	BindCustomerLine(rec, req)

	var got string
	db.DB.QueryRow(`SELECT COALESCE(line_user_id,'') FROM customers WHERE id=?`, second).Scan(&got)
	if got != "" {
		t.Errorf("乙客戶不該被綁定同一個 LINE 帳號，卻得到 %q", got)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Errorf("應以錯誤訊息擋下（err=），Location=%q", loc)
	}
	if !strings.Contains(loc, "%231") { // #1 url-encoded = %231
		t.Errorf("錯誤訊息應指出佔用者的客戶編號，Location=%q", loc)
	}
}

// 格式不對要擋（打錯字比不填更危險：會靜默送不到通知）。
func TestBindCustomerLineRejectsBadFormat(t *testing.T) {
	setupBindingTestDB(t)
	bindingFixture(t, "王先生", "0911000001")

	for _, bad := range []string{"", "abc", "12345", "U123", "U12 34", "U@1234567890", "A1234567890abcdef"} {
		req := httptest.NewRequest(http.MethodPost, "/customers/1/line-bind",
			strings.NewReader(url.Values{"line_user_id": {bad}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("id", "1")
		rec := httptest.NewRecorder()
		BindCustomerLine(rec, req)

		var got string
		db.DB.QueryRow(`SELECT COALESCE(line_user_id,'') FROM customers WHERE id=1`).Scan(&got)
		if got != "" {
			t.Errorf("格式 %q 不該被寫入，卻得到 %q", bad, got)
		}
		if !strings.Contains(rec.Header().Get("Location"), "err=") {
			t.Errorf("格式 %q 應被擋下並帶 err=", bad)
		}
	}
}

// 重新綁定：同一客戶可以換成新的 LINE 帳號（覆蓋自己的舊值）。
func TestBindCustomerLineAllowsRebindingSelf(t *testing.T) {
	setupBindingTestDB(t)
	id := bindingFixture(t, "王先生", "0911000001")
	if _, err := db.DB.Exec(`UPDATE customers SET line_user_id=? WHERE id=?`, lineID(3), id); err != nil {
		t.Fatal(err)
	}
	next := lineID(4)

	req := httptest.NewRequest(http.MethodPost, "/customers/1/line-bind",
		strings.NewReader(url.Values{"line_user_id": {next}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	BindCustomerLine(rec, req)

	var got string
	db.DB.QueryRow(`SELECT COALESCE(line_user_id,'') FROM customers WHERE id=?`, id).Scan(&got)
	if got != next {
		t.Errorf("重新綁定後 line_user_id = %q，應為 %q", got, next)
	}
}

// 取消綁定：清空欄位，之後客戶可以重新走自動綁定。
func TestUnbindCustomerLineClearsBinding(t *testing.T) {
	setupBindingTestDB(t)
	id := bindingFixture(t, "王先生", "0911000001")
	if _, err := db.DB.Exec(`UPDATE customers SET line_user_id=? WHERE id=?`, lineID(5), id); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/customers/1/line-unbind", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	UnbindCustomerLine(rec, req)

	var got string
	db.DB.QueryRow(`SELECT COALESCE(line_user_id,'') FROM customers WHERE id=?`, id).Scan(&got)
	if got != "" {
		t.Errorf("取消綁定後應為空，得到 %q", got)
	}
}

// 回歸測試：客戶編輯表單不得再寫入 line_user_id。
//
// 這是本次修掉的外洩路徑——舊版的 UPDATE 直接吃表單的 line_user_id，
// 繞過唯一性檢查。這裡用「帶著 line_user_id 的表單去打 UpdateCustomer」
// 驗證它會被忽略。
func TestUpdateCustomerIgnoresLineUserID(t *testing.T) {
	setupBindingTestDB(t)
	id := bindingFixture(t, "王先生", "0911000001")
	original := lineID(6)
	if _, err := db.DB.Exec(`UPDATE customers SET line_user_id=? WHERE id=?`, original, id); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"name":                    {"王先生"},
		"phone":                   {"0911000001"},
		"line_user_id":            {lineID(7)}, // 舊版會被寫進去
		"notification_preference": {"line"},
	}
	req := httptest.NewRequest(http.MethodPost, "/customers/1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	UpdateCustomer(rec, req)

	var got string
	db.DB.QueryRow(`SELECT COALESCE(line_user_id,'') FROM customers WHERE id=?`, id).Scan(&got)
	if got != original {
		t.Errorf("UpdateCustomer 不該改動 line_user_id：原本 %q，變成 %q", original, got)
	}
}

// Telegram：格式不對要擋。
func TestUpdateCustomerRejectsBadTelegramID(t *testing.T) {
	setupBindingTestDB(t)
	bindingFixture(t, "王先生", "0911000001")

	for _, bad := range []string{"abc", "12", "123456789012345678901", "12a45"} {
		form := url.Values{
			"name":                    {"王先生"},
			"phone":                   {"0911000001"},
			"telegram_user_id":        {bad},
			"notification_preference": {"telegram"},
		}

		req := httptest.NewRequest(http.MethodPost, "/customers/1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("id", "1")
		rec := httptest.NewRecorder()
		UpdateCustomer(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Telegram ID %q 應回 400，得到 %d", bad, rec.Code)
		}
		var got string
		db.DB.QueryRow(`SELECT COALESCE(telegram_user_id,'') FROM customers WHERE id=1`).Scan(&got)
		if got != "" {
			t.Errorf("Telegram ID %q 不該被寫入，卻得到 %q", bad, got)
		}
	}
}

// Telegram：同一個 chat id 不能綁兩個客戶（否則通知送錯人）。
func TestUpdateCustomerRejectsDuplicateTelegramID(t *testing.T) {
	setupBindingTestDB(t)
	first := bindingFixture(t, "甲客戶", "0911000001")
	bindingFixture(t, "乙客戶", "0911000002")
	if _, err := db.DB.Exec(`UPDATE customers SET telegram_user_id='123456789' WHERE id=?`, first); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"name":                    {"乙客戶"},
		"phone":                   {"0911000002"},
		"telegram_user_id":        {"123456789"},
		"notification_preference": {"telegram"},
	}
	req := httptest.NewRequest(http.MethodPost, "/customers/2", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "2")
	rec := httptest.NewRecorder()
	UpdateCustomer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("重複的 Telegram ID 應回 400，得到 %d", rec.Code)
	}
	var got string
	db.DB.QueryRow(`SELECT COALESCE(telegram_user_id,'') FROM customers WHERE id=2`).Scan(&got)
	if got != "" {
		t.Errorf("乙客戶不該被綁定，卻得到 %q", got)
	}
}

// DB 層最後防線：即使繞過 handler 直接寫，唯一索引也要擋住重複綁定。
func TestBindingUniqueIndexBlocksDirectDuplicate(t *testing.T) {
	setupBindingTestDB(t)
	first := bindingFixture(t, "甲客戶", "0911000001")
	bindingFixture(t, "乙客戶", "0911000002")
	line := lineID(8)
	if _, err := db.DB.Exec(`UPDATE customers SET line_user_id=? WHERE id=?`, line, first); err != nil {
		t.Fatal(err)
	}

	_, err := db.DB.Exec(`UPDATE customers SET line_user_id=? WHERE id=2`, line)
	if err == nil {
		t.Fatal("DB 應以 UNIQUE 約束擋下重複的 line_user_id")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "UNIQUE") {
		t.Errorf("錯誤應為 UNIQUE 違反，得到 %v", err)
	}
}

// 未綁定的客戶（NULL 或空字串）不該互相衝突，否則整間車行只能有一個未綁定客戶。
func TestBindingUniqueIndexAllowsManyUnbound(t *testing.T) {
	setupBindingTestDB(t)
	for i := 0; i < 5; i++ {
		id := bindingFixture(t, "客戶", "09110000"+string(rune('0'+i)))
		if _, err := db.DB.Exec(`UPDATE customers SET line_user_id='' WHERE id=?`, id); err != nil {
			t.Errorf("空字串 line_user_id 不該衝突: %v", err)
		}
	}
}

func TestBindCustomerTelegramSucceeds(t *testing.T) {
	setupBindingTestDB(t)
	id := bindingFixture(t, "王先生", "0911000001")
	req := httptest.NewRequest(http.MethodPost, "/customers/1/telegram-bind",
		strings.NewReader(url.Values{"telegram_user_id": {"123456789"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	BindCustomerTelegram(rec, req)

	var got string
	if err := db.DB.QueryRow(`SELECT COALESCE(telegram_user_id,'') FROM customers WHERE id=?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "123456789" {
		t.Fatalf("telegram_user_id = %q, want 123456789", got)
	}
}

func TestBindCustomerTelegramRejectsDuplicateAndBadFormat(t *testing.T) {
	setupBindingTestDB(t)
	first := bindingFixture(t, "甲客戶", "0911000001")
	bindingFixture(t, "乙客戶", "0911000002")
	if _, err := db.DB.Exec(`UPDATE customers SET telegram_user_id='123456789' WHERE id=?`, first); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		id, value string
	}{
		{"2", "123456789"},
		{"2", "abc"},
		{"2", "1234"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/customers/"+tc.id+"/telegram-bind",
			strings.NewReader(url.Values{"telegram_user_id": {tc.value}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("id", tc.id)
		rec := httptest.NewRecorder()
		BindCustomerTelegram(rec, req)
		if !strings.Contains(rec.Header().Get("Location"), "err=") {
			t.Errorf("Telegram ID %q should be rejected, Location=%q", tc.value, rec.Header().Get("Location"))
		}
	}
}

func TestBindTelegramUserMatchesNormalizedPhone(t *testing.T) {
	setupBindingTestDB(t)
	id := bindingFixture(t, "王先生", "0912-345-678")
	if got := customerCountByPhone(normalizePhone("+886 912 345 678")); got != 1 {
		t.Fatalf("normalized phone count = %d, want 1", got)
	}
	if err := bindTelegramUser("987654321", id); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.DB.QueryRow(`SELECT telegram_user_id FROM customers WHERE id=?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "987654321" {
		t.Fatalf("telegram_user_id = %q, want 987654321", got)
	}
}
