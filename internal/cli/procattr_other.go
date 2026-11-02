//go:build !linux

package cli

import (
	"os/exec"
	"syscall"
)

// childAttr は Linux 以外(開発端末)ではプロセスグループだけ分ける。
func childAttr(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
