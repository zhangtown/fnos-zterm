//go:build linux

package session

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// startPTY 在 Linux 上真正分配一个伪终端（pty）并把子进程挂上去。
// 用 pty.StartWithSize 而不是手工 openpty + Setsid/Setctty，是因为它已经处理好
// 会话首进程、控制终端与初始窗口大小这几件容易出错的事。
func startPTY(cmd *exec.Cmd, cols, rows uint16) (*os.File, error) {
	return pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
}

// resizePTY 更新内核里的终端窗口大小，让前台程序收到 SIGWINCH。
func resizePTY(f *os.File, cols, rows uint16) error {
	return pty.Setsize(f, &pty.Winsize{Cols: cols, Rows: rows})
}

// killGroup 杀整个进程组：会话子进程是 setsid 出来的组长，负 pid 才能连带
// 它在终端里拉起的子进程（比如 ssh、vim、docker exec）一起收拾干净。
func killGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
