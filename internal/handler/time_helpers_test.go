package handler

import "testing"

func TestToday(t *testing.T) {
	got := today()
	if len(got) != 10 {
		t.Errorf("got %s", got)
	}
}

func TestNowStamp(t *testing.T) {
	got := nowStamp()
	if len(got) != 19 {
		t.Errorf("got %s", got)
	}
}
