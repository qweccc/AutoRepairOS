package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
