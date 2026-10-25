package pages

import (
	"strings"
	"testing"
)

func TestTagPillsHTMLEscapesTagText(t *testing.T) {
	got := tagPillsHTML(`<script>alert(1)</script>,"quoted" & 'single'`)

	if strings.Contains(got, "<script>") || strings.Contains(got, "</script>") {
		t.Fatalf("tag output contains executable script markup: %q", got)
	}
	for _, escaped := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "&#34;quoted&#34;", "&amp;", "&#39;single&#39;"} {
		if !strings.Contains(got, escaped) {
			t.Errorf("tag output %q does not contain escaped text %q", got, escaped)
		}
	}
}
