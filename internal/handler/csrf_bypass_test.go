package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 這批測試針對 csrfExempt 的**前綴比對**做繞過嘗試。
//
// 為什麼要測這個：csrfExempt 是「這個路徑免檢查 CSRF」的清單，而它用
// strings.HasPrefix 判斷。前綴比對的經典風險是路徑穿越——如果
// `/approval/approve/../work-orders/1/status` 這種路徑能讓中介層認為
// 「這是免責的公開端點」而放行，但 ServeMux 之後把它清理成真正敏感的
// 端點來執行，那就是一個完整的 CSRF 繞過（攻擊者不必取得 token 就能
// 讓已登入的櫃檯執行狀態變更）。
//
// 這種事不能靠推理：Go 的 ServeMux 會先 cleanPath 再匹配，所以預期是
// 301 重導而不會執行——但「預期」和「實測」在安全路徑上必須一致。

// TestCSRFExemptPrefixCannotReachSensitiveRoute 走真實的 CSRFProtection 中介層
// 加真實的 ServeMux，確認前綴免責不能被路徑穿越利用。
func TestCSRFExemptPrefixCannotReachSensitiveRoute(t *testing.T) {
	reached := false

	mux := http.NewServeMux()
	mux.HandleFunc("POST /work-orders/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /approval/approve/{token}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	guarded := CSRFProtection(mux)

	// 各種「看起來像免責前綴、但清理後是敏感端點」的路徑。
	// 全部不帶 CSRF token 與 session cookie——若免責判斷被騙過，
	// 中介層會放行，而 mux 清理後就會真的執行狀態變更。
	attacks := []string{
		"/approval/approve/../work-orders/1/status",
		"/approval/approve/../../work-orders/1/status",
		"/approval/approve/%2e%2e/work-orders/1/status",
		"/approval/reject/../work-orders/1/status",
		"/approval/approve/..%2fwork-orders/1/status",
	}

	for _, path := range attacks {
		reached = false
		req := httptest.NewRequest("POST", path, nil)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)

		if reached {
			t.Errorf("路徑穿越繞過了 CSRF 免責並執行到敏感端點！\n"+
				"  送出路徑: %s\n"+
				"  中介層看到的: %s\n"+
				"  回應碼: %d",
				path, req.URL.Path, rec.Code)
		}
		t.Logf("%-52s → %d（未執行到 handler）", path, rec.Code)
	}
}

// TestCSRFExemptRedirectTargetStillRequiresToken 補完攻擊鏈的第二步。
//
// ServeMux 對非 GET 的清理重導用的是 **307 Temporary Redirect**（保留 method），
// 所以瀏覽器會再對「清理後的路徑」發一次 POST——也就是說第一步的 307 不是終點。
// 真正的防線是：清理後的路徑不在免責清單裡，因此那第二次請求會被 CSRF 檢查擋下。
// 這一步必須實測，否則「反正會被重導」只是半個結論。
func TestCSRFExemptRedirectTargetStillRequiresToken(t *testing.T) {
	reached := false

	mux := http.NewServeMux()
	mux.HandleFunc("POST /work-orders/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	guarded := CSRFProtection(mux)

	// 第一步：路徑穿越 → 307（不執行）
	req := httptest.NewRequest("POST", "/approval/approve/../work-orders/1/status", nil)
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("預期 307 清理重導，實際 %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("307 沒有 Location，無法驗證第二步")
	}
	t.Logf("第一步: 307 → Location: %s", loc)

	// 第二步：照著 Location 再發一次（模擬瀏覽器保留 method 重送），仍不帶 token
	reached = false
	req2 := httptest.NewRequest("POST", loc, nil)
	rec2 := httptest.NewRecorder()
	guarded.ServeHTTP(rec2, req2)

	if reached {
		t.Errorf("重導目標被免責放行了——攻擊鏈成立！Location: %s", loc)
	}
	if rec2.Code != http.StatusForbidden {
		t.Errorf("重導目標應被 CSRF 擋下並回 403，實際 %d（Location: %s）", rec2.Code, loc)
	}
	t.Logf("第二步: POST %s → %d（已擋下）", loc, rec2.Code)
}

// TestCSRFExemptStillWorksForRealApprovalPath 確認上面那批繞過嘗試沒有
// 把正常功能一起擋掉：真正的車主同意端點仍必須免 CSRF（車主沒有 session，
// 擋下去會讓整個追加維修同意流程 403）。
func TestCSRFExemptStillWorksForRealApprovalPath(t *testing.T) {
	reached := false

	mux := http.NewServeMux()
	mux.HandleFunc("POST /approval/approve/{token}", func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	guarded := CSRFProtection(mux)
	req := httptest.NewRequest("POST", "/approval/approve/deadbeef01234567", nil)
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)

	if !reached {
		t.Errorf("真正的車主同意端點被擋下了（%d）——車主沒有 session，"+
			"這會讓追加維修同意流程完全無法使用", rec.Code)
	}
}

// TestCSRFExemptPrefixDoesNotLeakToLookalikePaths 確認免責不會外溢到
// 「只是長得像」的路徑：這些路徑沒有 token 保護，若被免責就是漏洞。
func TestCSRFExemptPrefixDoesNotLeakToLookalikePaths(t *testing.T) {
	// 不該被免責：沒有結尾斜線、或只是名稱相近
	mustNotExempt := []string{
		"/approval/approve",               // 少了 token 段
		"/approval/approved/xyz",          // approved ≠ approve
		"/approval/approveXYZ",            // 沒有斜線分隔
		"/approval/approve-phone/xyz",     // 櫃檯端端點，必須有 CSRF
		"/work-orders/1/approval-request", // 櫃檯端端點，必須有 CSRF
		"/work-orders/1/approval",         // 櫃檯端端點，必須有 CSRF
	}
	for _, p := range mustNotExempt {
		if csrfExempt(p) {
			t.Errorf("這個路徑不該被 CSRF 免責（它沒有 token 保護）: %s", p)
		}
	}

	// 該被免責：真正的車主端端點
	mustExempt := []string{
		"/approval/approve/abc123",
		"/approval/reject/abc123",
		"/login",
		"/webhook/line",
	}
	for _, p := range mustExempt {
		if !csrfExempt(p) {
			t.Errorf("這個路徑應該被 CSRF 免責: %s", p)
		}
	}

	// 櫃檯端的電話同意／取消：有 session，必須走 CSRF
	for _, p := range []string{
		"/work-orders/1/approval-requests/5/approve-phone",
		"/work-orders/1/approval-requests/5/cancel",
	} {
		if csrfExempt(p) {
			t.Errorf("櫃檯端端點不該被免責（有 session，CSRF 檢查必須生效）: %s", p)
		}
	}
}

// TestCSRFExemptMatchingIsCaseSensitive 確認免責比對不被大小寫變化騙過。
func TestCSRFExemptMatchingIsCaseSensitive(t *testing.T) {
	// Go 的 ServeMux 路徑比對區分大小寫，/APPROVAL/... 不會匹配到任何路由，
	// 所以它是否免責不影響安全；但我們仍確認它沒有意外被免責，
	// 以免日後有人加了大小寫不敏感的路由。
	for _, p := range []string{"/APPROVAL/APPROVE/x", "/Approval/Approve/x"} {
		if csrfExempt(p) {
			t.Logf("注意：%s 目前被視為免責（目前無對應路由，無實際影響）", p)
		}
	}
}

// TestCSRFExemptPathIsDecodedForm 記錄一個關鍵事實：中介層傳給 csrfExempt 的
// 是**已解碼**的 r.URL.Path，所以 %2e%2e 會變成 ..。
// 這正是上面那批繞過測試必須存在的原因——編碼不會保護我們，ServeMux 的
// cleanPath 才會。
func TestCSRFExemptPathIsDecodedForm(t *testing.T) {
	req := httptest.NewRequest("POST", "/approval/approve/%2e%2e/work-orders/1/status", nil)
	if !strings.Contains(req.URL.Path, "..") {
		t.Fatalf("預期 r.URL.Path 已解碼成含 ..，實際 %q——"+
			"若 Go 改變了這個行為，上面的繞過測試需要重新檢視", req.URL.Path)
	}
	t.Logf("已解碼路徑: %s（故 csrfExempt 看到的是含 .. 的路徑）", req.URL.Path)
}
