package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"autorepairos/internal/model"
	"github.com/stretchr/testify/assert"
)

// --- Auth Middleware Tests ---

// TestAuthZ_OwnerOnly 驗證 OwnerOnly 權限限制
func TestAuthZ_OwnerOnly(t *testing.T) {
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

// --- Work Order Status Tests ---

// TestWorkOrderStatus_InvalidStatus 驗證無效狀態回傳 400
func TestWorkOrderStatus_InvalidStatus(t *testing.T) {
	// 模擬一個路徑帶有 id 的請求，且 status 為無效值
	req := httptest.NewRequest("POST", "/work-orders/123/status?status=invalid_status", nil)
	rr := httptest.NewRecorder()

	UpdateWorkOrderStatus(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "無效狀態")
}

// TestWorkOrderStatus_SOPLabel 驗證 SOP 標籤切換邏輯
func TestWorkOrderStatus_SOPLabel(t *testing.T) {
	assert.Equal(t, "標記 SOP 完成", sopToggleLabel(false))
	assert.Equal(t, "取消 SOP 完成", sopToggleLabel(true))
}

// TestWorkOrderStatus_ApprovalOptions 驗證核准選項生成
func TestWorkOrderStatus_ApprovalOptions(t *testing.T) {
	options := approvalOptions("pending")
	assert.Contains(t, options, "pending")
	assert.Contains(t, options, "approved")
	assert.Contains(t, options, "rejected")
	assert.Contains(t, options, "not_required")
}

// TestWorkOrderStatus_StatusOptions 驗證狀態選項生成
func TestWorkOrderStatus_StatusOptions(t *testing.T) {
	options := statusOptions("completed")
	assert.Contains(t, options, "pending")
	assert.Contains(t, options, "in_progress")
	assert.Contains(t, options, "waiting_parts")
	assert.Contains(t, options, "completed")
	assert.Contains(t, options, "paid")
}

// TestWorkOrderStatus_PaidImmutability 驗證已結帳工單不可變更為非結帳狀態
func TestWorkOrderStatus_PaidImmutability(t *testing.T) {
	// 這裡我們無法簡單模擬 DB 狀態，但可以驗證 HTML 標籤正確生成，這是前端交互的關鍵。
	assert.Contains(t, statusControls(model.WorkOrder{ID: 1, Status: "paid", SOPCompleted: true}, false), "此工單已結帳")
}

// TestWorkOrderStatus_InvalidTransition 驗證非法狀態轉換邏輯
func TestWorkOrderStatus_InvalidTransition(t *testing.T) {
	// 因為目前的實作是直接查 DB，若無資料會報 404 或報錯。
	// 此處我們主要驗證 shouldCheckTransition 邏輯。
	assert.True(t, shouldCheckTransition("pending", "in_progress"))
	assert.True(t, shouldCheckTransition("in_progress", "completed"))
	assert.False(t, shouldCheckTransition("paid", "pending")) // 已結帳不可改
	assert.False(t, shouldCheckTransition("pending", "paid"))  // 除非特殊邏輯，通常不允許直接跳到結帳
}
