package cli

import (
	"os/exec"
	"syscall"
)

// childAttr は子プロセスを自分のプロセスグループで起動し、核の終了後に残さない(PR_SET_PDEATHSIG。spec 3.1.1 の規則 13)。
func childAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
