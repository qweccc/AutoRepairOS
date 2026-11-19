package cron

import (
	"testing"
	"time"
)

// FR-APP-05 回歸：同一天應排入「前一日提醒」與「當日進場前導」兩種，
// 且重跑不會因去重條件而互相干擾（kind 不同）。
func TestAppointmentRemindersQueueBothKinds(t *testing.T) {
	d := reminderTestDB(t)
	tpe := time.FixedZone("Asia/Taipei", 8*60*60)
	today := time.Now().In(tpe).Format("2006-01-02")
	tomorrow := time.Now().In(tpe).AddDate(0, 0, 1).Format("2006-01-02")

	if _, err := d.Exec(`INSERT INTO customers(id,name,notification_preference,line_user_id)
		VALUES(1,'甲','line','u-1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO vehicles(id,customer_id,plate_number) VALUES(1,1,'P1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO appointments(id,customer_id,vehicle_id,appointment_date,appointment_time,service_type,status)
		VALUES(1,1,1,?,'09:30','保養','booked'),(2,1,1,?,'14:00','維修','confirmed')`,
		today, tomorrow); err != nil {
		t.Fatal(err)
	}

	n, err := ScanAppointmentRemindersNow()
	if err != nil || n != 2 {
		t.Fatalf("queued=%d err=%v want 2", n, err)
	}
	var guide, reminder int
	if err := d.QueryRow(`SELECT COUNT(*) FROM notifications WHERE kind='appointment_guide'`).Scan(&guide); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM notifications WHERE kind='appointment_reminder'`).Scan(&reminder); err != nil {
		t.Fatal(err)
	}
	if guide != 1 || reminder != 1 {
		t.Fatalf("kinds guide=%d reminder=%d want 1/1", guide, reminder)
	}
	// 重跑：同一客戶同一天兩則都已在 7 天窗內 → 不可重複排入。
	n, err = ScanAppointmentRemindersNow()
	if err != nil || n != 0 {
		t.Fatalf("rerun queued=%d err=%v want 0", n, err)
	}
}
