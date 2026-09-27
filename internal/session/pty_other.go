//go:build !linux

package session

import (
	"errors"
	"os"
	"os/exec"
)

// 非 Linux 平台只是为了让 `go build ./...` 在开发机上跑通（顺带做静态检查）。
// zterm 的运行时目标只有飞牛 NAS（Linux），Windows/macOS 上创建会话会明确报错。

var errNoPTY = errors.New("zterm: 伪终端只有 Linux 支持，本机仅用于编译检查")

func startPTY(cmd *exec.Cmd, cols, rows uint16) (*os.File, error) { return nil, errNoPTY }

func resizePTY(f *os.File, cols, rows uint16) error { return errNoPTY }

func killGroup(pid int) {}
