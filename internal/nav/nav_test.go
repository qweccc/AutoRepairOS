package nav

import (
	"context"
	"os"
	"strings"
	"testing"

	"autorepairos/internal/i18n"
)

// allRoles 三個角色（與 internal/handler/authz.go 的 CHECK 約束一致）。
var allRoles = []string{RoleOwner, RoleCounter, RoleTechnician}

// visibleGroups 回傳 role 看得到的 Route 涵蓋幾個不同的 Group。
//
// 不能沿用「Routes 裡有幾個 Group」——被過濾掉的項目若剛好讓某個 Group
// 整個消失，navgrp 數量少於 Routes 的 Group 數是正確行為。
func visibleGroups(role string) map[string]bool {
	groups := map[string]bool{}
	for _, rt := range Routes {
		if rt.CanSee(role) {
			groups[rt.Group] = true
		}
	}
	return groups
}

// TestRenderGroupCount 確保每個 Group 都被包成獨立的 navgrp，
// 且 navgrp 數量等於「該角色看得到的」不同 Group 數量
// （走音時這會是最早被抓到的訊號）。
func TestRenderGroupCount(t *testing.T) {
	for _, role := range allRoles {
		want := len(visibleGroups(role))
		got := strings.Count(Render(nil, "", role), `<div class="navgrp">`)
		if got != want {
			t.Errorf("role=%s: navgrp 數量 = %d，看得到的 Group 數量 = %d（選單定義可能漏了 Group，或過濾弄壞了分組）",
				role, got, want)
		}
	}
}

// TestRenderDivBalanced navgrp 的 div 必須開閉平衡，否則整頁版面會崩。
func TestRenderDivBalanced(t *testing.T) {
	for _, role := range allRoles {
		h := Render(nil, "", role)
		if o, c := strings.Count(h, "<div"), strings.Count(h, "</div>"); o != c {
			t.Errorf("role=%s: div 開=%d 閉=%d 不平衡", role, o, c)
		}
	}
}

// TestRenderActive 只有 active 路徑拿到 class="on"，且只有一個。
func TestRenderActive(t *testing.T) {
	for _, role := range allRoles {
		for _, rt := range Routes {
			if !rt.CanSee(role) {
				continue // 看不到的項目本來就不該出現在輸出裡
			}
			h := Render(nil, rt.Path, role)
			if n := strings.Count(h, `class="on"`); n != 1 {
				t.Errorf("role=%s active=%q 應只有 1 個 on，實際 %d", role, rt.Path, n)
				continue
			}
			if !strings.Contains(h, `<a href="`+rt.Path+`" class="on">`) {
				t.Errorf("role=%s active=%q 沒有標在正確的項目上", role, rt.Path)
			}
		}
	}
}

// TestRenderNoActiveWhenUnknown 未知路徑不應亮起任何項目。
func TestRenderNoActiveWhenUnknown(t *testing.T) {
	for _, role := range allRoles {
		if h := Render(nil, "/no-such-page", role); strings.Contains(h, `class="on"`) {
			t.Errorf("role=%s: 未知路徑不應有任何 active 項目", role)
		}
	}
}

// TestRenderAllRoutesPresent 每個 Route 都要出現在輸出裡——但只對看得到它的角色。
// 看不到的角色則必須完全沒有那個連結（連 href 都不該出現）。
func TestRenderAllRoutesPresent(t *testing.T) {
	for _, role := range allRoles {
		h := Render(nil, "", role)
		for _, rt := range Routes {
			link := `href="` + rt.Path + `"`
			if rt.CanSee(role) {
				if !strings.Contains(h, `<a `+link+`>`) {
					t.Errorf("role=%s: Route %q 沒有出現在選單裡", role, rt.Path)
				}
			} else if strings.Contains(h, link) {
				t.Errorf("role=%s: Route %q 不該出現在選單裡（該頁為 CounterUpPage，技師點進去只會被擋）", role, rt.Path)
			}
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

// TestRenderFiltersRoutesByRole 技師看不到櫃檯以上的頁面。
//
// 為什麼重要：看得到卻點不進去比看不到更糟——使用者會以為系統壞了，
// 而不是「我沒有權限」。
func TestRenderFiltersRoutesByRole(t *testing.T) {
	tech := Render(nil, "", RoleTechnician)
	for _, p := range []string{"/analytics", "/ap", "/receivables", "/expenses", "/lookup"} {
		if strings.Contains(tech, `href="`+p+`"`) {
			t.Errorf("技師不該看到 %s（該頁為 CounterUpPage）", p)
		}
	}
	// 技師該看得到的（AllStaff）——注意 /staff 是 AllStaff，不是 OwnerOnly；
	// 這是實際查 main.go 的結果，不是常識推測。
	// /reports 不在這裡：包裝器雖是 AllStaff，但 ReportsHome 內部 canSeeMoney
	// 會讓技師 403，所以導覽必須隱藏它。
	for _, p := range []string{"/work-orders", "/customers", "/vehicles", "/phrases", "/staff"} {
		if !strings.Contains(tech, `href="`+p+`"`) {
			t.Errorf("技師應該看得到 %s（該頁為 AllStaff）", p)
		}
	}
	// 內部再檢查而技師進不去的頁面，導覽也必須隱藏
	for _, p := range []string{"/reports"} {
		if strings.Contains(tech, `href="`+p+`"`) {
			t.Errorf("技師不該看到 %s（handler 內部 canSeeMoney 會回 403）", p)
		}
	}

	// 櫃檯以上要看得到全部
	owner := Render(nil, "", RoleOwner)
	for _, rt := range Routes {
		if !strings.Contains(owner, `href="`+rt.Path+`"`) {
			t.Errorf("owner 應該看得到全部選單項目，缺了 %q", rt.Path)
		}
	}
	if got, want := strings.Count(owner, "<a href="), len(Routes); got != want {
		t.Errorf("owner 的選單項目數 = %d，Routes 數 = %d", got, want)
	}
}

// TestCanSeeFailClosed 未知或空字串的角色看不到受限項目。
//
// 寧可少顯示一個項目，也不要顯示一個點進去被擋的入口。
func TestRenderI18n(t *testing.T) {
	// Without dictionaries loaded, Render falls back to Chinese Names.
	zh := Render(nil, "/", RoleOwner)
	if !strings.Contains(zh, "總覽") {
		t.Fatal("fallback should show Chinese nav")
	}
	// With en dictionaries loaded, translated names/groups appear.
	if err := i18n.Load(testLocales(t)); err != nil {
		t.Fatal(err)
	}
	en := Render(i18n.WithLang(context.Background(), "en"), "/", RoleOwner)
	if !strings.Contains(en, "Home") || strings.Contains(en, "總覽") {
		t.Fatalf("en nav not translated: %.200s", en)
	}
	if !strings.Contains(en, "Daily Ops") {
		t.Fatal("group not translated")
	}
}

func testLocales(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	zh := `{"nav.home":"總覽","group.daily":"日常作業"}`
	en := `{"nav.home":"Home","group.daily":"Daily Ops"}`
	if err := os.WriteFile(dir+"/zh-TW.json", []byte(zh), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/en.json", []byte(en), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCanSeeFailClosed(t *testing.T) {
	restricted := Route{Path: "/x", Name: "x", Group: "g", Roles: []string{RoleOwner, RoleCounter}}
	for _, role := range []string{"", "unknown", "admin", "OWNER"} {
		if restricted.CanSee(role) {
			t.Errorf("角色 %q 不該看得到受限項目", role)
		}
	}
	if !restricted.CanSee(RoleOwner) || !restricted.CanSee(RoleCounter) {
		t.Error("owner/counter 應該看得到受限項目")
	}
	if restricted.CanSee(RoleTechnician) {
		t.Error("技師不該看得到受限項目")
	}

	open := Route{Path: "/y", Name: "y", Group: "g"}
	for _, role := range append(allRoles, "") {
		if !open.CanSee(role) {
			t.Errorf("無限制的項目應該對角色 %q 可見", role)
		}
	}
}
