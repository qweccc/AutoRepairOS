package handler

import "testing"

func TestCsvField(t *testing.T) {
	if got := csvField("hello"); got != "hello" {
		t.Errorf("got %s", got)
	}
	if got := csvField("a,b"); got != `"a,b"` {
		t.Errorf("got %s", got)
	}
}

func TestCostLabel(t *testing.T) {
	if got := costLabel(false, 100); got != "未記錄" {
		t.Errorf("got %s", got)
	}
	if got := costLabel(true, 0); got == "未記錄" {
		t.Error("recorded should not be 未記錄")
	}
}
