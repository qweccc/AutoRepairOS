package handler

import "testing"

func TestSplitAnalyticsTags(t *testing.T) {
	got := splitAnalyticsTags("")
	if len(got) != 1 || got[0] != "(未標記)" {
		t.Errorf("empty: %v", got)
	}
	got = splitAnalyticsTags("a, b, a")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("dedup: %v", got)
	}
	got = splitAnalyticsTags("  x  ,  y  ")
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("trim: %v", got)
	}
}

func TestSelectedAttr(t *testing.T) {
	if selectedAttr(true) != " selected" {
		t.Error("true should be selected")
	}
	if selectedAttr(false) != "" {
		t.Error("false should be empty")
	}
}
