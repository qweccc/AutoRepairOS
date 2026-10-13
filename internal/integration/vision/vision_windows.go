//go:build windows

package vision

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// assignKillOnJobClose 把子程序（llama-server）放進帶 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// 的 Job Object：持有 job handle 的父程序（AutoRepairOS）連同 job 一起消失時，
// Windows 自動終止 job 裡的所有程序。
//
// 為什麼需要：main 的 defer StopVisionClient 只在「正常結束/Ctrl+C」時跑；
// 車行人員用工作管理員強制結束 app 時（SIGKILL 等價）defer 不會執行，
// llama-server 會變孤兒程序永遠佔著記憶體（閒置看守也在 app 裡，一起死）。
// Job Object 由作業系統保證「父死子亡」，不依賴 app 有機會跑清理碼。
//
// 回傳 job handle（0 = 失敗；失敗時退回原本行為：靠 idle watcher 與 Stop 清理）。
func assignKillOnJobClose(pid int) uintptr {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0
	}
	// OpenProcess 拿到的 handle 用完就關（job 只記錄程序，不依賴這個 handle）。
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return 0
	}
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(proc)
		windows.CloseHandle(job)
		return 0
	}
	windows.CloseHandle(proc)
	return uintptr(job)
}

// closeJob 關閉 job handle。因 KILL_ON_JOB_CLOSE，這會終止 job 裡所有子程序
// （正常關閉路徑：Stop() → closeJob；強制結束路徑：app 死亡時系統自動關 handle）。
func closeJob(h uintptr) {
	if h != 0 {
		windows.CloseHandle(windows.Handle(h))
	}
}

// hideWindowAttr 回傳 Windows 專用的 SysProcAttr：子程序不彈出黑色 console 視窗。
// 回傳 any（nil 表示不使用）；呼叫端斷言 *syscall.SysProcAttr。
func hideWindowAttr() any {
	return &syscall.SysProcAttr{HideWindow: true}
}
