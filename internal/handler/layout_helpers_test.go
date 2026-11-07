package handler

import "testing"

func TestAtoiDefault(t *testing.T) {
	if got := atoiDefault("42", 0); got != 42 {
		t.Errorf("got %d", got)
	}
	if got := atoiDefault("abc", 5); got != 5 {
		t.Errorf("got %d", got)
	}
	if got := atoiDefault("  10  ", 0); got != 10 {
		t.Errorf("got %d", got)
	}
}

func TestAtofDefault(t *testing.T) {
	if got := atofDefault("3.14", 0); got != 3.14 {
		t.Errorf("got %v", got)
	}
	if got := atofDefault("abc", 1.5); got != 1.5 {
		t.Errorf("got %v", got)
	}
}

func TestAtoiOrNil(t *testing.T) {
	if got := atoiOrNil(""); got != nil {
		t.Errorf("empty should be nil, got %v", got)
	}
	if got := atoiOrNil("abc"); got != nil {
		t.Errorf("invalid should be nil, got %v", got)
	}
	if got := atoiOrNil("0"); got != nil {
		t.Errorf("zero should be nil, got %v", got)
	}
	if got := atoiOrNil("-5"); got != nil {
		t.Errorf("negative should be nil, got %v", got)
	}
	if got := atoiOrNil("42"); got != 42 {
		t.Errorf("valid should be 42, got %v", got)
	}
}
