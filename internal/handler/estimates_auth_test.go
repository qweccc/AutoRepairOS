package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/session"
)

func TestEstimateWriteRoutesRequireCounterUpRoleAndLogin(t *testing.T) {
	setupStaffActiveTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active)
		VALUES (1, 'tech', 'x', 'technician', '技師', 1)`); err != nil {
		t.Fatal(err)
	}
	token, err := session.Start(1)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	restricted := CounterUp(func(http.ResponseWriter, *http.Request) { called = true })

	unauthenticated := httptest.NewRecorder()
	restricted.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/estimates/1/convert", nil))
	if unauthenticated.Code != http.StatusFound || unauthenticated.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated status/location = %d/%q; want redirect to login",
			unauthenticated.Code, unauthenticated.Header().Get("Location"))
	}

	request := httptest.NewRequest(http.MethodPost, "/estimates/1/convert", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	forbidden := httptest.NewRecorder()
	restricted.ServeHTTP(forbidden, request)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("technician status = %d; want %d", forbidden.Code, http.StatusForbidden)
	}
	if called {
		t.Fatal("restricted estimate operation was invoked for a technician")
	}
}
