//go:build linux

package adapter

import "syscall"

// setPdeath は核の終了で子を止める(PR_SET_PDEATHSIG。spec 3.1.1 の規則 13)。
func setPdeath(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGKILL }
