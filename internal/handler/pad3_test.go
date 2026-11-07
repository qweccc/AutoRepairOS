package handler

import "testing"

func TestPad3(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "001"},
		{12, "012"},
		{123, "123"},
		{1234, "1234"},
	}
	for _, c := range cases {
		if got := pad3(c.n); got != c.want {
			t.Errorf("pad3(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
