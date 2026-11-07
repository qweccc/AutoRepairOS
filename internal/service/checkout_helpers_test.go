package service

import "testing"

func TestNullableString(t *testing.T) {
	if got := nullableString(""); got != "" {
		t.Errorf("empty should return empty string, got %v", got)
	}
	if got := nullableString("hello"); got != "hello" {
		t.Errorf("got %v", got)
	}
}

func TestNullableID(t *testing.T) {
	if got := nullableID(0); got != nil {
		t.Errorf("zero should be nil, got %v", got)
	}
	if got := nullableID(-1); got != nil {
		t.Errorf("negative should be nil, got %v", got)
	}
	if got := nullableID(42); got != int64(42) {
		t.Errorf("got %v", got)
	}
}
