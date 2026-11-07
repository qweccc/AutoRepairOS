package handler

import "testing"

func TestRandomDigits(t *testing.T) {
	got, err := randomDigits(6)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Errorf("len = %d, want 6", len(got))
	}
}
