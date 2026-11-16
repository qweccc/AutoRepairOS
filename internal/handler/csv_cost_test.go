package handler

import (
	"context"
	"testing"
)

func TestCsvField(t *testing.T) {
	if got := csvField("hello"); got != "hello" {
		t.Errorf("got %s", got)
	}
	if got := csvField("a,b"); got != `"a,b"` {
		t.Errorf("got %s", got)
	}
}

func TestCostLabel(t *testing.T) {
	if got := costLabel(context.Background(), false, 100); got != "未記錄" {
		t.Errorf("got %s", got)
	}
	if got := costLabel(context.Background(), true, 0); got == "未記錄" {
		t.Error("recorded should not be 未記錄")
	}
}
