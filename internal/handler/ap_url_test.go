package handler

import "testing"

func TestApStatementCSVURL(t *testing.T) {
	got := apStatementCSVURL(1, "2026-01-01", "2026-01-31")
	if got == "" {
		t.Error("should not be empty")
	}
	if got != "/ap/statement.csv?from=2026-01-01&supplier_id=1&to=2026-01-31" {
		t.Errorf("got %s", got)
	}
}
