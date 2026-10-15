package nav

import (
	"strings"
	"testing"
)

// TestRenderGroupCount 確保每個 Group 都被包成獨立的 navgrp，
// 且 navgrp 數量等於不同 Group 的數量（走音時這會是最早被抓到的訊號）。
func TestRenderGroupCount(t *testing.T) {
	groups := map[string]bool{}
	for _, rt := range Routes {
		groups[rt.Group] = true
	}
	got := strings.Count(Render(""), `<div class="navgrp">`)
	if got != len(groups) {
		t.Fatalf("navgrp 數量 = %d，不同 Group 數量 = %d（選單定義可能漏了 Group）", got, len(groups))
	}
}

// TestRenderDivBalanced navgrp 的 div 必須開閉平衡，否則整頁版面會崩。
func TestRenderDivBalanced(t *testing.T) {
	h := Render("")
	if o, c := strings.Count(h, "<div"), strings.Count(h, "</div>"); o != c {
		t.Fatalf("div 開=%d 閉=%d 不平衡", o, c)
	}
}

// TestRenderActive 只有 active 路徑拿到 class="on"，且只有一個。
func TestRenderActive(t *testing.T) {
	for _, rt := range Routes {
		h := Render(rt.Path)
		n := strings.Count(h, `class="on"`)
		if n != 1 {
			t.Fatalf("active=%q 應只有 1 個 on，實際 %d", rt.Path, n)
		}
		if !strings.Contains(h, `<a href="`+rt.Path+`" class="on">`) {
			t.Fatalf("active=%q 沒有標在正確的項目上", rt.Path)
		}
	}
}

// TestRenderNoActiveWhenUnknown 未知路徑不應亮起任何項目。
func TestRenderNoActiveWhenUnknown(t *testing.T) {
	if h := Render("/no-such-page"); strings.Contains(h, `class="on"`) {
		t.Fatal("未知路徑不應有任何 active 項目")
	}
}

// TestRenderAllRoutesPresent 每個 Route 都要出現在輸出裡。
func TestRenderAllRoutesPresent(t *testing.T) {
	h := Render("")
	for _, rt := range Routes {
		if !strings.Contains(h, `<a href="`+rt.Path+`">`) {
			t.Fatalf("Route %q 沒有出現在選單裡", rt.Path)
		}
	}
}

// TestRoutesPathsUnique 路徑重複會讓 active 判斷與點選都出問題。
func TestRoutesPathsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range Routes {
		if seen[rt.Path] {
			t.Fatalf("路徑重複: %q", rt.Path)
		}
		seen[rt.Path] = true
	}
}

// TestRoutesNoEmptyNameOrGroup 名稱或 Group 空白會產生看不到的選單項。
func TestRoutesNoEmptyNameOrGroup(t *testing.T) {
	for _, rt := range Routes {
		if strings.TrimSpace(rt.Name) == "" {
			t.Fatalf("路徑 %q 的名稱是空的", rt.Path)
		}
		if strings.TrimSpace(rt.Group) == "" {
			t.Fatalf("路徑 %q 沒有 Group（會變成不分組的平鋪項）", rt.Path)
		}
	}
}
