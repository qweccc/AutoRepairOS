//go:build windows

package vision

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestPlatformHelpers 驗證 Windows 專屬的行程管理機制（Job Object + 隱藏視窗）。
// 非 Windows 版是 stub，請見 vision_platform_unix_test.go。
func TestPlatformHelpers(t *testing.T) {
	// assignKillOnJobClose 對無效 pid 回 0；有效 pid 建立 job
	if got := assignKillOnJobClose(-1); got != 0 {
		t.Errorf("invalid pid job = %d", got)
	}
	closeJob(0) // 不 panic
	attr := hideWindowAttr()
	if sp, ok := attr.(*syscall.SysProcAttr); !ok || sp == nil {
		t.Errorf("hideWindowAttr = %#v", attr)
	}
	var job uintptr
	// 用子程序（ping）而非測試程序本身：把自己放進 KILL_ON_JOB_CLOSE job，
	// closeJob 會連測試程序一起殺掉。
	cmd := exec.Command("ping", "-n", "3", "127.0.0.1")
	if err := cmd.Start(); err == nil {
		job = assignKillOnJobClose(cmd.Process.Pid)
		if job != 0 {
			closeJob(job)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}
