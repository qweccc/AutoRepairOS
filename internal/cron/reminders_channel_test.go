package cron

import "testing"

func TestReminderChannelRouting(t *testing.T) {
	cases := []struct {
		pref, lineID, telegramID, discordID, slackID string
		wantChannel                                  string
		wantEnabled                                  bool
	}{
		{"line", "U1", "", "", "", "line", true},
		{"telegram", "", "123", "", "", "telegram", true},
		{"discord", "", "", "D1", "", "discord", true},
		{"slack", "", "", "", "S1", "slack", true},
		{"discord", "", "", "", "", "discord", false}, // 偏好 discord 但未綁定
		{"slack", "", "", "", "", "slack", false},
		{"none", "U1", "", "", "", "", false},
	}
	for _, tc := range cases {
		c := reminderCandidate{pref: tc.pref, lineID: tc.lineID, telegramID: tc.telegramID, discordID: tc.discordID, slackID: tc.slackID}
		ch, ok := c.channel()
		if ch != tc.wantChannel || ok != tc.wantEnabled {
			t.Errorf("pref=%q → (%q,%v), want (%q,%v)", tc.pref, ch, ok, tc.wantChannel, tc.wantEnabled)
		}
	}
}