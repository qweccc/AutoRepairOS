package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ---------- bay helpers ----------

func TestBayHelpers(t *testing.T) {
	newMigratedTestDB(t)
	if normBay(" lift ") != "LIFT" {
		t.Fatal("normBay")
	}
	known := []struct{ code, label string }{{"LIFT", "頂高機"}, {"PAINT", "烤漆房"}}
	if !bayKnownFrom("lift", known) || bayKnownFrom("NOPE", known) {
		t.Fatal("bayKnownFrom")
	}
	if bayLabelFrom("paint", known) != "烤漆房" || bayLabelFrom("NOPE", known) != "NOPE" {
		t.Fatal("bayLabelFrom")
	}
	if !bayKnown("LIFT") || bayKnown("NOPE") {
		t.Fatal("bayKnown default list")
	}
	if bayLabel("LIFT") != "頂高機" {
		t.Fatal("bayLabel")
	}
	// lookup_items override
	if _, err := db.DB.Exec(`INSERT INTO lookup_items (category, code, label, sort_order, active) VALUES ('bay','CUSTOM','自訂工位',1,1)`); err != nil {
		t.Fatal(err)
	}
	if bayLabel("CUSTOM") != "自訂工位" {
		t.Fatal("bayLabel from lookup")
	}
	// bayList: blank code rows skipped
	if _, err := db.DB.Exec(`INSERT INTO lookup_items (category, code, label, sort_order, active) VALUES ('bay','  ','x',2,1)`); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range bayList() {
		if b.code == "CUSTOM" {
			found = true
		}
	}
	if !found {
		t.Fatal("bayList custom")
	}
	for _, tc := range []struct{ sel, wantSub string }{
		{"LIFT", "LIFT"}, {"LEGACY-9", "LEGACY-9"}, {"", "LIFT"},
	} {
		html, err := bayOptionsHTML(tc.sel)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, tc.wantSub) && tc.sel != "" {
			t.Fatalf("bayOptionsHTML(%q) missing %q: %s", tc.sel, tc.wantSub, html)
		}
	}
}

// ---------- binding code ----------

func TestBindingHelpersAndGenerate(t *testing.T) {
	newMigratedTestDB(t)
	for _, tc := range []struct {
		p  string
		ok bool
	}{
		{"line", true}, {"telegram", true}, {"foo", false}, {"", false},
	} {
		if isValidPlatform(tc.p) != tc.ok {
			t.Fatalf("isValidPlatform(%q)", tc.p)
		}
	}
	code, err := randomDigits(6)
	if err != nil || len(code) != 6 {
		t.Fatalf("randomDigits len=%d err=%v", len(code), err)
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			t.Fatalf("non-digit %q", code)
		}
	}
	// GenerateBindingCode: invalid platform defaults to telegram
	req := httptest.NewRequest("POST", "/customers/1/generate-binding-code?platform=bogus", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	GenerateBindingCode(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "綁定碼") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var platform string
	var n int
	if err := db.DB.QueryRow(`SELECT platform, COUNT(*) FROM binding_codes`).Scan(&platform, &n); err != nil || platform != "telegram" || n != 1 {
		t.Fatalf("platform=%s n=%d err=%v", platform, n, err)
	}
	// line platform
	req2 := httptest.NewRequest("POST", "/customers/1/generate-binding-code?platform=line", nil)
	req2.SetPathValue("id", "1")
	rec2 := httptest.NewRecorder()
	GenerateBindingCode(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("code=%d", rec2.Code)
	}
}

func TestVerifyBindingCodeBranches(t *testing.T) {
	newMigratedTestDB(t)
	seed := func(platform, code string, attempts int, used, expired bool) {
		exp := "2999-01-01 00:00:00"
		if expired {
			exp = "2000-01-01 00:00:00"
		}
		usedAt := interface{}(nil)
		if used {
			usedAt = "2026-01-01 00:00:00"
		}
		h := sha256hex([]byte(code))
		if _, err := db.DB.Exec(`INSERT INTO binding_codes (platform, customer_id, code_hash, expires_at, used_at, attempts) VALUES (?,?,?,?,?,?)`,
			platform, 1, h, exp, usedAt, attempts); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name     string
		platform string
		code     string
		setup    func()
	}{
		{"unknown code", "telegram", "000000", func() {}},
		{"used", "telegram", "111111", func() { seed("telegram", "111111", 0, true, false) }},
		{"attempts exceeded", "telegram", "222222", func() { seed("telegram", "222222", 5, false, false) }},
		{"expired", "telegram", "333333", func() { seed("telegram", "333333", 0, false, true) }},
		{"unsupported platform", "sms", "444444", func() { seed("sms", "444444", 0, false, false) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newMigratedTestDB(t)
			c.setup()
			if _, err := VerifyBindingCode(c.platform, c.code, "chat123"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCSRFHelpersDirect(t *testing.T) {
	if csrfTokenForResponse(httptest.NewRecorder()) != "" {
		t.Fatal("plain writer should give empty token")
	}
	rec := httptest.NewRecorder()
	wrapped := &csrfResponseWriter{ResponseWriter: rec, token: "tok123"}
	if csrfTokenForResponse(wrapped) != "tok123" || wrapped.csrfToken() != "tok123" {
		t.Fatal("wrapped token")
	}
	if withCSRFFormFields("<form></form>", "") != "<form></form>" {
		t.Fatal("empty token passthrough")
	}
	out := withCSRFFormFields(`<form method="post"></form>`, "abc")
	if !strings.Contains(out, `_csrf`) || !strings.Contains(out, "abc") {
		t.Fatalf("form fields: %s", out)
	}
}

// ---------- authz wrappers ----------

func seedUser(t *testing.T, id int64, role, hash string) {
	t.Helper()
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active) VALUES (?,?,?,?,?,1)`,
		id, role+"user", hash, role, "測試"); err != nil {
		t.Fatal(err)
	}
}

func reqWithSession(t *testing.T, userID int64) *http.Request {
	t.Helper()
	tok, _ := startTestSession(t, userID)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "session_token", Value: tok})
	return r
}

func TestAuthzWrappers(t *testing.T) {
	newMigratedTestDB(t)
	h, _ := bcrypt.GenerateFromPassword([]byte("pw12345"), bcrypt.MinCost)
	seedUser(t, 1, "owner", string(h))
	seedUser(t, 2, "counter", string(h))
	seedUser(t, 3, "technician", string(h))

	called := false
	plain := func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) }
	page := func(w http.ResponseWriter, r *http.Request, u model.User) { called = true; w.WriteHeader(200) }
	actorH := func(w http.ResponseWriter, r *http.Request, a Actor) { called = true; w.WriteHeader(200) }

	cases := []struct {
		name     string
		handler  http.HandlerFunc
		userID   int64 // 0 = unauthenticated
		wantCode int
		wantCall bool
	}{
		{"OwnerOnly unauth", OwnerOnly(plain), 0, 302, false},
		{"OwnerOnly owner", OwnerOnly(plain), 1, 200, true},
		{"OwnerOnly counter", OwnerOnly(plain), 2, 403, false},
		{"CounterUp counter", CounterUp(plain), 2, 200, true},
		{"CounterUp technician", CounterUp(plain), 3, 403, false},
		{"AllStaff technician", AllStaff(page), 3, 200, true},
		{"AllStaff unauth", AllStaff(page), 0, 302, false},
		{"CounterUpPage counter", CounterUpPage(page), 2, 200, true},
		{"CounterUpPage technician", CounterUpPage(page), 3, 403, false},
		{"CounterUpPage unauth", CounterUpPage(page), 0, 302, false},
		{"WithActor owner", WithActor(actorH), 1, 200, true},
		{"WithActor unauth", WithActor(actorH), 0, 302, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// note: shared DB + sessions persist across subtests within this test
			called = false
			var req *http.Request
			if c.userID == 0 {
				req = httptest.NewRequest("GET", "/", nil)
			} else {
				req = reqWithSession(t, c.userID)
			}
			rec := httptest.NewRecorder()
			c.handler.ServeHTTP(rec, req)
			if rec.Code != c.wantCode {
				t.Fatalf("code=%d want %d body=%s", rec.Code, c.wantCode, rec.Body.String())
			}
			if called != c.wantCall {
				t.Fatalf("called=%v want %v", called, c.wantCall)
			}
		})
	}
	// Actor helpers
	if !(Actor{ID: 1, Role: "owner"}).CanPickTechnician() {
		t.Fatal("owner can pick technician")
	}
	if (Actor{ID: 3, Role: "technician"}).CanPickTechnician() {
		t.Fatal("technician cannot pick")
	}
	if u := (Actor{ID: 7, Role: "counter"}).asUser(); u.ID != 7 || u.Role != "counter" {
		t.Fatalf("asUser %+v", u)
	}
	if !isOwnerOrCounter("owner") || !isOwnerOrCounter("counter") || isOwnerOrCounter("technician") {
		t.Fatal("isOwnerOrCounter")
	}
}

// ---------- auth handlers ----------

func TestAuthHandlers(t *testing.T) {
	newMigratedTestDB(t)
	h, _ := bcrypt.GenerateFromPassword([]byte("secret1"), bcrypt.MinCost)
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active) VALUES (1,'alice',?,'owner','Alice',1),(2,'bob',?,'counter','Bob',0)`, string(h), string(h)); err != nil {
		t.Fatal(err)
	}

	t.Run("MeHandler unauth", func(t *testing.T) {
		rec := httptest.NewRecorder()
		MeHandler(rec, httptest.NewRequest("GET", "/me", nil))
		if rec.Code != 401 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("MeHandler authed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		MeHandler(rec, reqWithSession(t, 1))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "alice") {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("LogoutHandler default redirect", func(t *testing.T) {
		rec := httptest.NewRecorder()
		LogoutHandler(rec, httptest.NewRequest("GET", "/logout", nil))
		if rec.Code != 302 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("LogoutHandler return_to login goes home", func(t *testing.T) {
		rec := httptest.NewRecorder()
		LogoutHandler(rec, httptest.NewRequest("GET", "/logout?return_to=/login", nil))
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Fatalf("loc=%q", loc)
		}
	})
	t.Run("LoginHandler GET", func(t *testing.T) {
		rec := httptest.NewRecorder()
		LoginHandler(rec, httptest.NewRequest("GET", "/login?return_to=/x", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("LoginHandler wrong method", func(t *testing.T) {
		rec := httptest.NewRecorder()
		LoginHandler(rec, httptest.NewRequest("PUT", "/login", nil))
		if rec.Code != 405 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	post := func(t *testing.T, form url.Values, ip string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		LoginHandler(rec, req)
		return rec
	}
	t.Run("LoginHandler empty credentials", func(t *testing.T) {
		if rec := post(t, url.Values{}, "10.1.0.1"); rec.Code != 401 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("LoginHandler unknown user", func(t *testing.T) {
		if rec := post(t, url.Values{"username": {"nobody"}, "password": {"x"}}, "10.1.0.2"); rec.Code != 401 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("LoginHandler wrong password", func(t *testing.T) {
		if rec := post(t, url.Values{"username": {"alice"}, "password": {"wrong"}}, "10.1.0.3"); rec.Code != 401 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("LoginHandler inactive", func(t *testing.T) {
		if rec := post(t, url.Values{"username": {"bob"}, "password": {"secret1"}}, "10.1.0.4"); rec.Code != 401 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("LoginHandler success", func(t *testing.T) {
		rec := post(t, url.Values{"username": {"alice"}, "password": {"secret1"}, "return_to": {"/dash"}}, "10.1.0.5")
		if rec.Code != 302 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Fatalf("loc=%q", loc)
		}
	})
	t.Run("IndexHandler all roles", func(t *testing.T) {
		for _, u := range []model.User{
			{ID: 1, Role: "owner"}, {ID: 1, Role: "counter"}, {ID: 1, Role: "technician"},
		} {
			rec := httptest.NewRecorder()
			IndexHandler(rec, httptest.NewRequest("GET", "/", nil), u)
			if rec.Code != 200 {
				t.Fatalf("role %s code=%d", u.Role, rec.Code)
			}
		}
	})
}

// ---------- customers / equipment / appointments read handlers ----------

func TestListAndSearchHandlers(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912'),(2,'李大華','0923')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO equipment (id, name, status) VALUES (1,'頂高機一號','active')`); err != nil {
		t.Logf("equipment seed: %v", err)
	}
	owner := model.User{ID: 1, Role: "owner"}

	t.Run("ListCustomers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ListCustomers(rec, httptest.NewRequest("GET", "/customers?q=王", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("SearchCustomersAPI", func(t *testing.T) {
		rec := httptest.NewRecorder()
		SearchCustomersAPI(rec, reqWithSession(t, 1))
		// seedUser not called; session user 1 may not exist -> still should respond (401 or 200)
		if rec.Code != 200 && rec.Code != 401 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("CustomerPickerAPI", func(t *testing.T) {
		rec := httptest.NewRecorder()
		CustomerPickerAPI(rec, httptest.NewRequest("GET", "/customers/picker?q=王", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("ListEquipment", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ListEquipment(rec, httptest.NewRequest("GET", "/equipment", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("SearchEquipmentAPI", func(t *testing.T) {
		rec := httptest.NewRecorder()
		SearchEquipmentAPI(rec, httptest.NewRequest("GET", "/equipment/search?q=頂", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("ListAppointments", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ListAppointments(rec, httptest.NewRequest("GET", "/appointments", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("SearchAppointmentsAPI", func(t *testing.T) {
		rec := httptest.NewRecorder()
		SearchAppointmentsAPI(rec, httptest.NewRequest("GET", "/appointments/search", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("AppointmentCalendarAPI", func(t *testing.T) {
		rec := httptest.NewRecorder()
		AppointmentCalendarAPI(rec, httptest.NewRequest("GET", "/appointments/calendar?date=2026-11-09&view=week", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})
	t.Run("appointment helpers", func(t *testing.T) {
		if taipeiToday() == "" {
			t.Fatal("taipeiToday")
		}
		if normalizeCalendarDate("bad") != "" || normalizeCalendarView("bad") != "day" || normalizeCalendarView("week") != "week" {
			t.Fatal("normalize")
		}
		if !validAppointmentDate("2026-11-09") || validAppointmentDate("2026-13-40") {
			t.Fatal("validAppointmentDate")
		}
		if !validAppointmentTime("09:30") || validAppointmentTime("25:99") {
			t.Fatal("validAppointmentTime")
		}
		_ = bayConflictMessage("LIFT", "2026-11-09", "09:00")
		_ = appointmentLabel("confirmed")
		_ = appointmentPill("confirmed")
		if !appointmentVehicleBelongsToCustomer(0, 0) {
			// zero ids: implementation-defined; just exercise it
		}
	})
}

func TestCreateCustomerValidation(t *testing.T) {
	newMigratedTestDB(t)
	// missing name -> 400
	form := url.Values{"phone": {"0912"}}
	req := httptest.NewRequest("POST", "/customers", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	CreateCustomer(rec, req)
	if rec.Code != 400 && rec.Code != 302 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	// valid create
	form2 := url.Values{"name": {"新客戶"}, "phone": {"0933"}}
	req2 := httptest.NewRequest("POST", "/customers", strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	CreateCustomer(rec2, req2)
	if rec2.Code != 302 && rec2.Code != 200 {
		t.Fatalf("code=%d body=%s", rec2.Code, rec2.Body.String())
	}
}
