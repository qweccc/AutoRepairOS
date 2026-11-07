package handler

import "testing"

func TestMoneyIf(t *testing.T) {
	if got := moneyIf(0); got != `<span style="color:#475569">—</span>` {
		t.Errorf("got %s", got)
	}
	if got := moneyIf(100); got != money(100) {
		t.Errorf("got %s", got)
	}
}
