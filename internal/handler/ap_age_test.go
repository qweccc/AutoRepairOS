package handler

import "testing"

func TestApAgeBucket(t *testing.T) {
	bucket, days, err := apAgeBucket("2026-01-01", "2026-01-01", "2026-01-15")
	if err != nil {
		t.Errorf("err: %v", err)
	}
	if bucket == "" {
		t.Error("bucket should not be empty")
	}
	_ = days
	// invalid date
	_, _, err = apAgeBucket("invalid", "", "2026-01-15")
	if err == nil {
		t.Error("should error")
	}
	// empty dueDate falls back to createdAt
	bucket, _, err = apAgeBucket("", "2026-01-01", "2026-01-15")
	if err != nil {
		t.Errorf("err: %v", err)
	}
	if bucket == "" {
		t.Error("bucket should not be empty")
	}
}
