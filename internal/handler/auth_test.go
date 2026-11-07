package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"autorepairos/internal/model"
	"github.com/stretchr/testify/assert"
)

// --- Session & User Helpers ---

// TestCurrentUser_NoToken 驗證無 Token 時應回傳錯誤
func TestCurrentUser_NoToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	_, err := currentUser(req)
	assert.Error(t, err)
	assert.True(t, err == http.ErrNoCookie)
}

// TestCurrentUser_InvalidToken 驗證無效 Token 時應回傳錯誤
func TestCurrentUser_InvalidToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Cookie", "session_token=invalid_token")
	_, err := currentUser(req)
	assert.Error(t, err)
}

// --- Auth Middleware Tests ---

// MockHandler 是用於測試中間件的簡單 Handler
func MockHandler(w http.ResponseWriter, r *http.Request, user model.User) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("success"))
}

// TestAuthZ_OwnerOnly 驗證 OwnerOnly 權限限制
func TestAuthZ_OwnerOnly(t *testing.T) {
	// 注意：由於目前的 currentUser 依賴 session，
	// 為了單元測試這些中間件，我們需要在測試中模擬 session 環境或直接測試 logic。
	// 由於目前架構是全域依賴，我們先驗證基本的角色檢查邏輯。
	
	assert.True(t, isOwnerOrCounter("owner"))
	assert.False(t, isOwnerOrCounter("technician"))
	assert.False(t, isOwnerOrCounter("guest"))
}

// TestAuthZ_CounterUp 驗證 CounterUp 權限限制
func TestAuthZ_CounterUp(t *testing.T) {
	assert.True(t, isOwnerOrCounter("owner"))
	assert.True(t, isOwnerOrCounter("counter"))
	assert.False(t, isOwnerOrCounter("technician"))
}

// TestFinancialMasking 驗證財務數據可見性邏輯
func TestFinancialMasking(t *testing.T) {
	assert.True(t, canSeeMoney(model.User{Role: "owner"}))
	assert.True(t, canSeeMoney(model.User{Role: "counter"}))
	assert.False(t, canSeeMoney(model.User{Role: "technician"}))
}

// --- Handler Tests ---

// TestLogoutHandler 驗證登出流程
func TestLogoutHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/logout?return_to=/dashboard", nil)
	rr := httptest.NewRecorder()
	
	LogoutHandler(rr, req)

	assert.Equal(t, http.StatusFound, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "/dashboard")
}

// TestMeHandler_Unauthorized 驗證未登入時 MeHandler 回傳 401
func TestMeHandler_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("GET", "/me", nil)
	rr := httptest.NewRecorder()

	MeHandler(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "未登入")
}

// TestLoginFail_MissingCredentials 驗證缺少帳號密碼時回傳錯誤
func TestLoginFail_MissingCredentials(t *testing.T) {
	req := httptest.NewRequest("POST", "/login", nil)
	rr := httptest.NewRecorder()
	
	LoginHandler(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "請輸入帳號與密碼")
}
