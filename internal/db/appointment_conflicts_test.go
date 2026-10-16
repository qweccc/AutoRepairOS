package db

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestAppointmentBayOverlapTriggersAreAtomicForInsertAndUpdate(t *testing.T) {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.SetMaxOpenConns(1)
	if _, err := d.Exec(`CREATE TABLE appointments (
		id INTEGER PRIMARY KEY, appointment_date TEXT, appointment_time TEXT,
		bay TEXT, status TEXT DEFAULT 'booked', deleted_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	script, err := migrationFS.ReadFile("migration/033_appointment_calendar_conflicts.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := execScript(tx, string(script)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	insert := func(date, timeSlot, bay, status string) error {
		_, err := d.Exec(`INSERT INTO appointments
			(appointment_date,appointment_time,bay,status) VALUES(?,?,?,?)`,
			date, timeSlot, bay, status)
		return err
	}
	if err := insert("2026-10-19", "09:00", "LIFT", "booked"); err != nil {
		t.Fatal(err)
	}
	if err := insert("2026-10-19", "09:30", "LIFT", "booked"); err == nil ||
		!strings.Contains(err.Error(), "appointment_bay_overlap") {
		t.Fatalf("overlapping insert error=%v; want appointment_bay_overlap", err)
	}
	if err := insert("2026-10-19", "10:00", "LIFT", "booked"); err != nil {
		t.Fatalf("back-to-back insert should be allowed: %v", err)
	}
	if _, err := d.Exec(`UPDATE appointments SET appointment_time='09:30' WHERE id=2`); err == nil ||
		!strings.Contains(err.Error(), "appointment_bay_overlap") {
		t.Fatalf("overlapping update error=%v; want appointment_bay_overlap", err)
	}
	if _, err := d.Exec(`UPDATE appointments SET status='cancelled' WHERE id=1`); err != nil {
		t.Fatalf("cancelling a conflicting slot must be allowed: %v", err)
	}
	if err := insert("2026-10-19", "09:00", "LIFT", "booked"); err != nil {
		t.Fatalf("cancelled appointment should release its bay: %v", err)
	}
	if err := insert("2026-10-19", "09:00", "ALIGN", "booked"); err != nil {
		t.Fatalf("different bay should be allowed: %v", err)
	}
	if err := insert("2026-10-19", "23:30", "PAINT", "booked"); err != nil {
		t.Fatal(err)
	}
	if err := insert("2026-10-20", "00:00", "PAINT", "booked"); err == nil ||
		!strings.Contains(err.Error(), "appointment_bay_overlap") {
		t.Fatalf("overlap across midnight error=%v; want appointment_bay_overlap", err)
	}
	if err := insert("2026-10-20", "00:30", "PAINT", "booked"); err != nil {
		t.Fatalf("the adjacent next-day interval should be allowed: %v", err)
	}
	if err := insert("2026-10-19", "", "LIFT", "booked"); err == nil ||
		!strings.Contains(err.Error(), "invalid appointment time") {
		t.Fatalf("invalid time error=%v; want invalid appointment time", err)
	}
}
