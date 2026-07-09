//go:build unit && unix

package pluginhost

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/stretchr/testify/require"
)

// TestTerminateKillsGrandchildren 断言 disable 的进程组终止覆盖插件 fork 的
// 孙进程：不设 Setpgid 时孙进程只会被 reparent 到 init 继续运行。
func TestTerminateKillsGrandchildren(t *testing.T) {
	f := newSupervisorFixture(t)
	const id = pluginkit.ID("ext.tree")
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	f.installFakePlugin(t, id, fakePluginConfig{Mode: "ok", GrandchildPIDFile: pidFile})
	f.setEnabled(t, id, true)

	gcPid := waitGrandchildPid(t, pidFile)
	require.NoError(t, syscall.Kill(gcPid, 0), "孙进程应在插件运行期间存活")
	// 兜底：断言失败时不把 sleep 留在宿主机上。
	t.Cleanup(func() { _ = syscall.Kill(gcPid, syscall.SIGKILL) })

	f.setEnabled(t, id, false)

	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(gcPid, 0), syscall.ESRCH)
	}, 5*time.Second, 20*time.Millisecond, "进程组 SIGTERM 应一并终止孙进程")
}

// waitGrandchildPid 轮询 PID 文件直到假插件把孙进程 PID 落盘。
func waitGrandchildPid(t *testing.T, pidFile string) int {
	t.Helper()
	var pid int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || n <= 0 {
			return false
		}
		pid = n
		return true
	}, 5*time.Second, 20*time.Millisecond, "等待孙进程 PID 落盘")
	return pid
}
