package handler

import "testing"

func TestNotifPref(t *testing.T) {
	for _, v := range []string{"line", "telegram", "none", "sms"} {
		if notifPref(v) != v {
			t.Errorf("notifPref(%q) = %q", v, notifPref(v))
		}
	}
	if notifPref("other") != "none" {
		t.Errorf("notifPref(other) = %q", notifPref("other"))
	}
}
