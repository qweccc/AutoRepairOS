package db

import "testing"

func TestPathBeforeInit(t *testing.T) {
	if Path() != "" {
		t.Fatalf("expected empty path before Init, got %q", Path())
	}
}