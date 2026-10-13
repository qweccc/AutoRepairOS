//go:build !windows

package vision

// assignKillOnJobClose 的非 Windows 版本：Docker 部署不自動管理 llama-server
// （該用容器編排：ollama + OLLAMA_KEEP_ALIVE=0，或 docker exec 管理），
// 因此不需要父死子亡機制。回傳 0 表示無 job。
func assignKillOnJobClose(pid int) uintptr {
	_ = pid
	return 0
}

// closeJob 的非 Windows 版本：無 job 可關。
func closeJob(h uintptr) {
	_ = h
}

// hideWindowAttr 的非 Windows 版本：回傳 nil（無 console 視窗要隱藏）。
// 注意：回傳型別用 any，呼叫端要自行斷言（只有 Windows 版會真的用到值）。
func hideWindowAttr() any {
	return nil
}
