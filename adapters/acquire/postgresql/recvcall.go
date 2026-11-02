package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

// 受け口の呼び出し(補助コマンド SITATE_RECV。spec 3.2.2)。補助コマンドの終了状態は受け口の終了状態そのもので、
// 届かないときと接続が切れたときは 255。同時に呼ぶのは 1 つだけで、前の呼び出しの終了を待ってから次を呼ぶ。

const (
	exitDenied      = 126
	exitUnreachable = 255
)

type recvCall struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stdin  io.WriteCloser
}

// startRecv は `<recv> <op> <target-id>` を起動する。withStdin が偽なら標準入力は空(閉じる)。
func startRecv(recv, op, id string, withStdin bool) (*recvCall, error) {
	c := exec.Command(recv, op, id)
	c.Stderr = os.Stderr
	rc := &recvCall{cmd: c}
	var err error
	if withStdin {
		if rc.stdin, err = c.StdinPipe(); err != nil {
			return nil, err
		}
	}
	if rc.stdout, err = c.StdoutPipe(); err != nil {
		return nil, err
	}
	if err = c.Start(); err != nil {
		return nil, err
	}
	return rc, nil
}

// wait は標準出力を読み切った後に終了状態を返す。起動できない・シグナルで終わった場合は 255。
func (rc *recvCall) wait() int {
	err := rc.cmd.Wait()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() >= 0 {
		return ee.ExitCode()
	}
	return exitUnreachable
}

// kill は読むのをやめるときに子を止めて終了を待つ。
func (rc *recvCall) kill() {
	if rc.cmd.Process != nil {
		_ = rc.cmd.Process.Kill()
	}
	_ = rc.cmd.Wait()
}

// dumpClass は dump-pg・dump-pg-globals の終了状態の写像(spec 3.2.2 の「受け口の終了状態の写像」)。
type dumpClass int

const (
	dumpOK          dumpClass = iota
	dumpMismatch              // 126: RECEIVER_TARGETS_MISMATCH(終了 1)
	dumpUnreachable           // 255: STORAGE_UNREACHABLE(終了 1)
	dumpItemFail              // 1〜125: その項目のダンプの失敗(errors[] に EXIT_STATUS、item_ref はその項目)
	dumpTargetFail            // 127〜254: 受け口が無いか受け口の外で止まった(対象の failed、item_ref: null)
)

func classifyDump(st int) dumpClass {
	switch {
	case st == 0:
		return dumpOK
	case st == exitDenied:
		return dumpMismatch
	case st == exitUnreachable:
		return dumpUnreachable
	case st < exitDenied:
		return dumpItemFail
	}
	return dumpTargetFail
}
