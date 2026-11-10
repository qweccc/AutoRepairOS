//go:build !windows

package vision

import "testing"

// TestPlatformHelpers 的非 Windows 版：三個 helper 都是 stub，
// 全部應該安靜地什麼都不做（Docker 部署由容器編排管理 llama-server，
// 不需要 Job Object 與隱藏視窗）。
func TestPlatformHelpers(t *testing.T) {
	if got := assignKillOnJobClose(-1); got != 0 {
		t.Errorf("assignKillOnJobClose = %d, want 0", got)
	}
	closeJob(0) // 不 panic
	if attr := hideWindowAttr(); attr != nil {
		t.Errorf("hideWindowAttr = %#v, want nil", attr)
	}
}
