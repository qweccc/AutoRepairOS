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
