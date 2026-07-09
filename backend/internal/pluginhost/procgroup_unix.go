//go:build unix

package pluginhost

import (
	"os/exec"
	"syscall"
)

// setProcGroup 让插件子进程成为新进程组组长（pgid = 子进程 pid）：
// 终止时对整组发信号，插件 fork 出的孙进程不会在 stop/kill/卸载后残留
// 并被 reparent 到 init 继续运行。
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalProcGroup 对以 pid 为组长的进程组发信号。错误原样返回（含 ESRCH），
// caller 回退到直接子进程信号：组尚未成形（fork 后 setpgid 前的窄窗口）时
// 直接信号仍可送达，组已消亡时直接信号以 ErrProcessDone 收敛。
func signalProcGroup(pid int, sig syscall.Signal) error {
	return syscall.Kill(-pid, sig)
}
