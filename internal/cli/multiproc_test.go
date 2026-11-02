package cli

// 複数プロセスの試験(作業指示書 5.17 の完了条件 5)。実プロセスの flock は同じプロセスの中では確かめられないので、子プロセス(この試験の実行ファイル自身)を使う。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const helperEnv = "SITATE_CLI_TEST_HOLDER_DIR"

// 子プロセス: ロックを取って印を書き、本体で止まる(started を置く)。SIGKILL されるまで終わらない。
func TestMain(m *testing.M) {
	if dir := os.Getenv(helperEnv); dir != "" {
		switch os.Getenv(helperModeEnv) {
		case "pdeathsig":
			holdWithChild(dir)
		case "stuck":
			holdShortLimits(dir)
		default:
			holdAndBlock(dir)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type noopT struct{}

func (noopT) Helper() {}

func holdAndBlock(dir string) {
	f := newFlowAt(noopT{}, dir)
	p, _, _ := parseArgs([]string{"backup"})
	id := runA
	o := outcome{command: p.sub.Words, runID: &id}
	f.d.lockedRun(bg, o, p, contract.Call{Words: p.sub.Words, RunID: id}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		_ = os.WriteFile(filepath.Join(dir, "started"), nil, 0o600)
		select {}
	})
}

func TestMultiProcLockedThenKilled(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	cmd.Env = append(os.Environ(), helperEnv+"="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	started := filepath.Join(dir, "started")
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(started); err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatal("子プロセスが本体に入らない")
	}
	f := newFlowAt(t, dir)
	o := f.run(t, []string{"backup"}, okEntry)
	if o.err == nil || o.err.Code != contract.CodeLocked || exitOf(o.err) != 7 || !strings.Contains(string(o.err.Detail), `"holder_command":["backup"]`) {
		t.Fatalf("別プロセスが持つロック: %+v", o.err)
	}
	if !markerExists(f.d) {
		t.Error("LOCKED の実行が持ち主の印を消した")
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()
	o = f.run(t, []string{"backup"}, okEntry)
	if o.err != nil {
		t.Fatalf("持ち主の SIGKILL の後: %+v", o.err)
	}
	if ints := f.ends(t, "run_interrupted"); len(ints) != 1 {
		t.Fatalf("run_interrupted: %+v", ints)
	}
	if markerExists(f.d) {
		t.Error("成功の実行の後に印が残る")
	}
}
