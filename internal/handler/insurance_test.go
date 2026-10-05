package handler



import (

	"io"

	"net/http"

	"net/http/cookiejar"

	"net/http/httptest"

	"net/url"

	"strings"

	"testing"



	"autorepairos/internal/config"

	"autorepairos/internal/db"

	"autorepairos/internal/seed"

)



// 保險公司 CRUD 測試。

//

// 參考 CAR35 的 Company 表（Carp78 廠商/公司主檔、Carp92 公司別帳務彙總）：

// 保險公司是理賠案件的歸屬對象，必須能維護簡稱、全名、統編與客服電話，

// 且「刪除」其實是停用（active=0），因為歷史工單會引用到。

func setupInsuranceServer(t *testing.T) *httptest.Server {

	t.Helper()

	dir := t.TempDir()

	cfg := &config.Config{

		DBPath:            "file:" + dir + "/test.db?_journal_mode=WAL&_busy_timeout=5000",

		RunSeed:           true,

		DefaultOwner:      "owner",

		DefaultOwnerPwd:   "Owner001!",

		DefaultCounter:    "counter",

		DefaultCounterPwd: "Counter001!",

		DefaultTech:       "tech",

		DefaultTechPwd:    "Tech001!",

	}

	if err := db.Init(cfg); err != nil {

		t.Fatalf("db.Init failed: %v", err)

	}

	t.Cleanup(db.Close)

	if err := db.Migrate(); err != nil {

		t.Fatalf("db.Migrate failed: %v", err)

	}

	seed.Run(cfg)



	mux := http.NewServeMux()

	mux.HandleFunc("POST /login", LoginHandler)

	mux.HandleFunc("GET /login", LoginHandler)

	mux.HandleFunc("GET /insurance-companies", AllStaff(ListInsuranceCompanies))

	mux.HandleFunc("POST /insurance-companies", CounterUp(CreateInsuranceCompany))

	mux.HandleFunc("GET /insurance-companies/{id}/edit", CounterUpPage(EditInsuranceCompanyForm))

	mux.HandleFunc("POST /insurance-companies/{id}", CounterUp(UpdateInsuranceCompany))

	mux.HandleFunc("GET /insurance-companies/{id}/delete", OwnerOnly(DeactivateInsuranceCompany))

	return httptest.NewServer(mux)

}



// insuranceLogin 真的走登入流程取得 session，不塞偽造的 token。

// 不自動跟隨 3xx，才能斷言 handler 實際回 200 而非最終頁面。

func insuranceLogin(t *testing.T, srv *httptest.Server, user, pwd string) *http.Client {

	t.Helper()

	jar, _ := cookiejar.New(nil)

	c := &http.Client{

		Jar: jar,

		CheckRedirect: func(req *http.Request, via []*http.Request) error {

			return http.ErrUseLastResponse

		},

	}

	resp, err := c.PostForm(srv.URL+"/login",

		url.Values{"username": {user}, "password": {pwd}})

	if err != nil {

		t.Fatalf("login %s: %v", user, err)

	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {

		t.Fatalf("login %s: got %d, want 302", user, resp.StatusCode)

	}

	return c

}



func insurancePost(c *http.Client, base, path string, form url.Values) (int, string) {

	resp, err := c.PostForm(base+path, form)

	if err != nil {

		return 0, err.Error()

	}

	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, string(b)

}



func insuranceGet(t *testing.T, c *http.Client, reqURL string) string {

	t.Helper()

	resp, err := c.Get(reqURL)

	if err != nil {

		t.Fatalf("GET %s: %v", reqURL, err)

	}

	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)

	return string(b)

}



// insuranceGetStatus 只取狀態碼（給 /delete 這種 GET 副作用的動作）。

func insuranceGetStatus(c *http.Client, reqURL string) (int, string) {

	resp, err := c.Get(reqURL)

	if err != nil {

		return 0, err.Error()

	}

	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, string(b)

}



func TestInsuranceCompanyCRUD(t *testing.T) {

	srv := setupInsuranceServer(t)

	defer srv.Close()



	c := insuranceLogin(t, srv, "owner", "Owner001!")



	// --- 建立 ---

	st, _ := insurancePost(c, srv.URL, "/insurance-companies", url.Values{

		"code":           {"國泰"},

		"name":           {"國泰世紀產物保險股份有限公司"},

		"tax_id":         {"28817468"},

		"claim_phone":    {"0800-024-888"},

		"contact_person": {"王經理"},

	})

	if st != http.StatusFound {

		t.Fatalf("建立保險公司 st=%d，期望 302（成功會 redirect）", st)

	}



	var id int64

	var name, taxID, claimPhone string

	if err := db.DB.QueryRow(`SELECT id, name, tax_id, claim_phone

		FROM insurance_companies WHERE code = ?`, "國泰").

		Scan(&id, &name, &taxID, &claimPhone); err != nil {

		t.Fatalf("查詢保險公司失敗: %v", err)

	}

	if name != "國泰世紀產物保險股份有限公司" {

		t.Errorf("name = %q，期望完整公司名", name)

	}

	if taxID != "28817468" {

		t.Errorf("tax_id = %q", taxID)

	}

	if claimPhone != "0800-024-888" {

		t.Errorf("claim_phone = %q", claimPhone)

	}



	// --- 簡稱唯一（CAR35 的 Company.Short 是唯一鍵）---

	if st, _ = insurancePost(c, srv.URL, "/insurance-companies", url.Values{

		"code": {"國泰"}, "name": {"重複的簡稱"},

	}); st == http.StatusFound {

		t.Errorf("重複簡稱 st=%d，期望被拒絕", st)

	}



	// --- 必填驗證 ---

	if st, _ = insurancePost(c, srv.URL, "/insurance-companies", url.Values{

		"code": {""}, "name": {"沒簡稱"},

	}); st == http.StatusFound {

		t.Errorf("空簡稱 st=%d，期望被拒絕", st)

	}



	// --- 列表頁 ---

	body := insuranceGet(t, c, srv.URL+"/insurance-companies")

	if !strings.Contains(body, "國泰") {

		t.Errorf("列表頁未顯示保險公司簡稱")

	}

	if !strings.Contains(body, "新增保險公司") {

		t.Errorf("列表頁缺少新增表單")

	}



	// --- 搜尋 ---

	if body = insuranceGet(t, c, srv.URL+"/insurance-companies?q=國泰"); !strings.Contains(body, "國泰") {

		t.Errorf("搜尋未找到結果")

	}

	if body = insuranceGet(t, c, srv.URL+"/insurance-companies?q=NOSUCHCO"); strings.Contains(body, ">國泰<") {

		t.Errorf("不該匹配的關鍵字卻找到結果")

	}



	// --- 編輯頁預填 ---

	editURL := srv.URL + "/insurance-companies/" + itoa64(id) + "/edit"

	if body = insuranceGet(t, c, editURL); !strings.Contains(body, "28817468") {

		t.Errorf("編輯頁未預填統編")

	} else if !strings.Contains(body, "0800-024-888") {

		t.Errorf("編輯頁未預填客服電話")

	}



	// --- 更新 ---

	if st, _ = insurancePost(c, srv.URL, "/insurance-companies/"+itoa64(id), url.Values{

		"code":           {"國泰"},

		"name":           {"國泰產險（已更新）"},

		"tax_id":         {"28817468"},

		"claim_phone":    {"0800-024-999"},

		"contact_person": {"李經理"},

	}); st != http.StatusFound {

		t.Fatalf("更新 st=%d，期望 302", st)

	}

	var updated string

	db.DB.QueryRow("SELECT name FROM insurance_companies WHERE id = ?", id).Scan(&updated)

	if updated != "國泰產險（已更新）" {

		t.Errorf("更新後 name = %q", updated)

	}



	// --- 停用（軟刪除，不可真刪）---

	st, _ = insuranceGetStatus(c, srv.URL+"/insurance-companies/"+itoa64(id)+"/delete")

	if st != http.StatusFound {

		t.Fatalf("停用 st=%d，期望 302", st)

	}

	var active int

	db.DB.QueryRow("SELECT active FROM insurance_companies WHERE id = ?", id).Scan(&active)

	if active != 0 {

		t.Errorf("停用後 active = %d，期望 0", active)

	}



	// --- 權限：技師不能維護 ---

	tech := insuranceLogin(t, srv, "tech", "Tech001!")

	if st, _ = insurancePost(tech, srv.URL, "/insurance-companies", url.Values{

		"code": {"TechTest"}, "name": {"技師不該能建立"},

	}); st == http.StatusFound {

		t.Errorf("技師建立保險公司 st=%d，期望 403", st)

	}



	// 技師可讀（設定工單時要能看到保險公司清單）

	_ = insuranceGet(t, tech, srv.URL+"/insurance-companies")

}



// 保險公司下拉選單（工單表單用）只列啟用的。

func TestInsuranceOptionsOnlyActive(t *testing.T) {

	srv := setupInsuranceServer(t)

	defer srv.Close()

	c := insuranceLogin(t, srv, "owner", "Owner001!")



	for _, code := range []string{"AAA", "BBB"} {

		insurancePost(c, srv.URL, "/insurance-companies", url.Values{

			"code": {code}, "name": {"公司" + code},

		})

	}



	var bbbID int64

	db.DB.QueryRow("SELECT id FROM insurance_companies WHERE code = ?", "BBB").Scan(&bbbID)

	insuranceGetStatus(c, srv.URL+"/insurance-companies/"+itoa64(bbbID)+"/delete")



	opts := insuranceOptionsSel(0)

	if !strings.Contains(opts, "AAA") {

		t.Errorf("選單應包含啟用的 AAA，實際: %s", opts)

	}

	if strings.Contains(opts, "BBB") {

		t.Errorf("選單不應包含已停用的 BBB，實際: %s", opts)

	}

	// 預選值為 0 時不應有 selected

	if strings.Contains(opts, "selected") {

		t.Errorf("selected=0 時不應預選任何項目")

	}

}



// 保險公司下拉可預選指定公司（工單帶入保險時用）。

func TestInsuranceOptionsSelPreselect(t *testing.T) {

	srv := setupInsuranceServer(t)

	defer srv.Close()

	c := insuranceLogin(t, srv, "owner", "Owner001!")



	insurancePost(c, srv.URL, "/insurance-companies", url.Values{

		"code": {"FUBON"}, "name": {"富邦產險"},

	})

	var fid int64

	db.DB.QueryRow("SELECT id FROM insurance_companies WHERE code = ?", "FUBON").Scan(&fid)



	opts := insuranceOptionsSel(fid)

	if !strings.Contains(opts, "FUBON") {

		t.Errorf("選單應包含 FUBON")

	}

	if !strings.Contains(opts, "selected") {

		t.Errorf("選單應預選指定的保險公司")

	}

}



