package db

import "testing"

func TestPath(t *testing.T) {
	// Path returns dbFilePath; before Init it's empty
	got := Path()
	// Just ensure no panic
	_ = got
}
