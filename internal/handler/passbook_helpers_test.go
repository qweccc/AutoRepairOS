package handler

import "testing"

func TestPassbookServiceTypeLabel(t *testing.T) {
	if got := passbookServiceTypeLabel("inspection"); got != "檢查" {
		t.Errorf("got %s", got)
	}
	if got := passbookServiceTypeLabel("maintenance"); got != "保養" {
		t.Errorf("got %s", got)
	}
	if got := passbookServiceTypeLabel("unknown"); got != "維修" {
		t.Errorf("got %s", got)
	}
}

func TestPassbookStatusLabel(t *testing.T) {
	if got := passbookStatusLabel("pending"); got != "待付款" {
		t.Errorf("got %s", got)
	}
	if got := passbookStatusLabel("unknown"); got != "unknown" {
		t.Errorf("got %s", got)
	}
}
