package handler

import "testing"

func TestApStatusPresentation(t *testing.T) {
	label, pill := apStatusPresentation("paid")
	if label != "已付清" || pill != "p-completed" {
		t.Errorf("got %s %s", label, pill)
	}
	label, pill = apStatusPresentation("unknown")
	if label != "未付款" || pill != "p-pending" {
		t.Errorf("got %s %s", label, pill)
	}
}

func TestApSourceLabel(t *testing.T) {
	if got := apSourceLabel("purchase_receive", 1); got != "採購進貨 #1" {
		t.Errorf("got %s", got)
	}
	if got := apSourceLabel("subcontract", 2); got != "外包 #2" {
		t.Errorf("got %s", got)
	}
}
