//go:build !linux

package adapter

import "syscall"

// setPdeath は Linux 以外では何もしない(PR_SET_PDEATHSIG は Linux だけ)。
func setPdeath(*syscall.SysProcAttr) {}
