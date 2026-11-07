package handler

import "testing"

func TestPayMethod(t *testing.T) {
	r := receivable{Cash: 100}
	m, _ := payMethod(r)
	if m != "現金" {
		t.Errorf("got %q", m)
	}
	r2 := receivable{}
	m2, _ := payMethod(r2)
	if m2 != "未收款" {
		t.Errorf("got %q", m2)
	}
}
