//go:build windows

package pluginhost

import (
	"os/exec"
	"syscall"
)

// Windows 无进程组信号语义（Setpgid/负 PGID 不可用）：保持直接子进程
// 信号路径，行为与引入进程组终止之前一致。
func setProcGroup(*exec.Cmd) {}

func signalProcGroup(int, syscall.Signal) error { return syscall.EWINDOWS }
