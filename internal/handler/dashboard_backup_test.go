package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func TestDashboardBackupHealthIssue(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	tech := model.User{ID: 1, Role: "technician"}

	render := func(u model.User) string {
		rec := httptest.NewRecorder()
		IndexHandler(rec, httptest.NewRequest("GET", "/", nil), u)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	// 完全沒有備份紀錄 → owner 看得到警示、技師看不到。
	if !strings.Contains(render(owner), "備份異常") {
		t.Fatal("owner should see backup warning when no backups exist")
	}
	if strings.Contains(render(tech), "備份異常") {
		t.Fatal("technician should not see backup warning")
	}

	// 48 小時內有成功備份且最新為 success → 警示消失。
	if _, err := db.DB.Exec(`INSERT INTO backup_records (backup_date, status) VALUES ('2026-11-19', 'success')`); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(render(owner), "備份異常") {
		t.Fatal("warning should clear after a fresh successful backup")
	}

	// 最新一次 failed → 警示再現。
	if _, err := db.DB.Exec(`INSERT INTO backup_records (backup_date, status, error_message) VALUES ('2026-11-19', 'failed', 'disk full')`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(render(owner), "備份異常") {
		t.Fatal("warning should appear when latest backup failed")
	}
}
