package handler

import "testing"

func TestNormBay(t *testing.T) {
	if got := normBay("  a  "); got != "A" {
		t.Errorf("got %s", got)
	}
	if got := normBay(""); got != "" {
		t.Errorf("got %s", got)
	}
}
