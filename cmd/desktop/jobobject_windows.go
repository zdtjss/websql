//go:build windows

package main

import (
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobHandle 持有 Job Object 句柄，进程退出前不关闭。
// 句柄关闭（含进程退出时由 OS 自动关闭）会触发 KILL_ON_JOB_CLOSE，
// 终止所有子进程（含 msedgewebview2.exe）。
var jobHandle windows.Handle

// setupJobObject 创建一个带 KILL_ON_JOB_CLOSE 标志的 Job Object 并把
// 当前进程加入。主进程退出时（无论正常退出还是 crash），操作系统会自动
// 终止所有子进程，避免 msedgewebview2.exe 残留。
//
// 必须在 WebView2 创建子进程之前调用。Windows 8+ 支持嵌套 Job，
// 若父进程已将本进程加入其他 Job 也不会冲突。
func setupJobObject() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		log.Printf("[Desktop] 创建 Job Object 失败: %v", err)
		return
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		log.Printf("[Desktop] 设置 Job Object 限制失败: %v", err)
		return
	}

	current, err := windows.GetCurrentProcess()
	if err != nil {
		log.Printf("[Desktop] 获取当前进程句柄失败: %v", err)
		return
	}
	if err := windows.AssignProcessToJobObject(job, current); err != nil {
		log.Printf("[Desktop] 将进程加入 Job Object 失败: %v", err)
		return
	}

	jobHandle = job
	log.Printf("[Desktop] Job Object 已创建，子进程将在主进程退出时自动终止")
}
