package cli

// 複数プロセスの試験の続き(作業指示書 5.17 の完了条件 5 の 3・4・5・11)。子プロセスは試験の実行ファイル自身。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const helperModeEnv = "SITATE_CLI_TEST_HOLDER_MODE"

// holdWithChild: ロックを取った実行の本体で子を起動する(子は cli の childAttr = 自分のプロセスグループ + PR_SET_PDEATHSIG)。
func holdWithChild(dir string) {
	runtime.LockOSThread() // PR_SET_PDEATHSIG は子を作ったスレッドの終了で効くので、本体のスレッドを固定する
	f := newFlowAt(noopT{}, dir)
	p, _, _ := parseArgs([]string{"backup"})
	id := runA
	o := outcome{command: p.sub.Words, runID: &id}
	f.d.lockedRun(bg, o, p, contract.Call{Words: p.sub.Words, RunID: id}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		cmd := exec.Command("sleep", "300")
		childAttr(cmd)
		if err := cmd.Start(); err != nil {
			return contract.Reply{}, err
		}
		_ = os.WriteFile(filepath.Join(dir, "child.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "started"), nil, 0o600)
		select {}
	})
}

// holdShortLimits: 上限と猶予を短くした実行。本体は取り消しを無視して止まる(監視の goroutine だけが終わらせる)。封筒は out.json へ書く。
func holdShortLimits(dir string) {
	f := newFlowAt(noopT{}, dir)
	f.d.Limits = Limits{MaxDuration: 300 * time.Millisecond, AbortGrace: 300 * time.Millisecond, RecordDeadline: 2 * time.Second, StateLockWait: 5 * time.Second}
	f.d.Lease = &Lease{Dir: dir, MaxDuration: f.d.Limits.MaxDuration}
	out, _ := os.Create(filepath.Join(dir, "out.json"))
	f.d.out = out
	p, _, _ := parseArgs([]string{"backup"})
	id := runA
	o := outcome{command: p.sub.Words, runID: &id}
	f.d.lockedRun(bg, o, p, contract.Call{Words: p.sub.Words, RunID: id}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		_ = os.WriteFile(filepath.Join(dir, "started"), nil, 0o600)
		select {}
	})
}

func startHolder(t *testing.T, dir, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	cmd.Env = append(os.Environ(), helperEnv+"="+dir, helperModeEnv+"="+mode)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	started := filepath.Join(dir, "started")
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(started); err == nil {
			return cmd
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("子プロセスが本体に入らない")
	return nil
}

func alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	return !strings.Contains(string(b), ") Z ") // ゾンビは死んでいる
}

// 完了条件 5 の 4(後半): 核が SIGKILL で終わった後に、ロックを取った実行が起動した子プロセスが残らない。
func TestMultiProcChildDiesWithOwner(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PR_SET_PDEATHSIG は Linux")
	}
	dir := t.TempDir()
	cmd := startHolder(t, dir, "pdeathsig")
	raw, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(raw))
	if !alive(pid) {
		t.Fatal("子が起動していない")
	}
	if pg, _ := syscall.Getpgid(pid); pg != pid {
		t.Errorf("子が自分のプロセスグループでない: pgid=%d pid=%d", pg, pid)
	}
	_ = cmd.Process.Signal(syscall.SIGKILL)
	_, _ = cmd.Process.Wait()
	for i := 0; i < 100 && alive(pid); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("核の終了の後に子が残った")
	}
}

// 完了条件 5 の 4(前半): ロックの記述子は close-on-exec(exec した子に渡らない)。
func TestLockFDIsCloseOnExec(t *testing.T) {
	l := &Lease{Dir: t.TempDir()}
	if ok, err := l.Acquire(bg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, l.f.Fd(), syscall.F_GETFD, 0)
	if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
		t.Fatalf("FD_CLOEXEC が無い: flags=%d errno=%v", flags, errno)
	}
}

// 完了条件 5 の 5: GC を走らせた後もロックが保たれる(取った Lease への参照を捨てても、記述子が閉じられて解けない)。
func TestLockSurvivesGC(t *testing.T) {
	dir := t.TempDir()
	func() {
		l := &Lease{Dir: dir}
		if ok, err := l.Acquire(bg); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}()
	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	other := &Lease{Dir: dir}
	if ok, err := other.Acquire(bg); ok || err != nil {
		t.Fatalf("GC の後にロックが解けた: ok=%v err=%v", ok, err)
	}
}

// 完了条件 5 の 3: 持ち主を SIGSTOP で止めている間、他の実行は LOCKED を返し、deadline_at + 猶予 + 記録の締め切りを過ぎたら run.stuck を通知する。
// 同じ持ち主について 24 時間に 1 回にまとめる。
func TestMultiProcStoppedOwnerLockedThenStuckOnce(t *testing.T) {
	dir := t.TempDir()
	cmd := startHolder(t, dir, "")
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Signal(syscall.SIGCONT) }()
	f := newFlowAt(t, dir)
	lim := f.d.Limits
	accept := func() { // 受理された配送だけがまとめの基点になる。受理の時刻は今の時計
		at := f.clk.T
		f.ntf.Result = []contract.DeliveryResult{{To: "ops@example.jp", Channel: "email", Delivered: true, Attempts: 1, AcceptedAt: &at}}
	}
	count := func() int { // まとめられた通知は配送の結果が空
		n := 0
		for _, x := range f.rec.Notifications() {
			if d, _ := x["delivered"].([]map[string]any); x["type"] == "run.stuck" && len(d) > 0 {
				n++
			}
		}
		return n
	}
	accept()
	o := f.run(t, []string{"backup"}, okEntry)
	if o.err == nil || o.err.Code != contract.CodeLocked || count() != 0 {
		t.Fatalf("締め切り前: %+v stuck=%d", o.err, count())
	}
	f.clk.T = f.clk.T.Add(lim.MaxDuration + lim.AbortGrace + lim.RecordDeadline + time.Second)
	accept()
	for i := 0; i < 2; i++ {
		if o = f.run(t, []string{"backup"}, okEntry); o.err == nil || o.err.Code != contract.CodeLocked {
			t.Fatalf("止まった持ち主の間: %+v", o.err)
		}
	}
	if count() != 1 {
		t.Errorf("run.stuck は 24 時間に 1 回にまとめる: %d", count())
	}
	f.clk.T = f.clk.T.Add(25 * time.Hour)
	accept()
	_ = f.run(t, []string{"backup"}, okEntry)
	if count() != 2 {
		t.Errorf("24 時間後は再び通知する: %d", count())
	}
}

// 完了条件 5 の 11: 上限 + 猶予を超えて SIGSTOP した実行を再開すると、監視の goroutine が直ちに超過を検知して RUN_ABORTED(max_duration)で終わる。
func TestMultiProcResumedAfterLimitAbortsImmediately(t *testing.T) {
	dir := t.TempDir()
	cmd := startHolder(t, dir, "stuck")
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(900 * time.Millisecond) // 上限 + 猶予(600ms)を超える
	resumed := time.Now()
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("再開した実行が終わらない")
	}
	if d := time.Since(resumed); d > 2500*time.Millisecond {
		t.Errorf("直ちに終わらない: %v", d)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "out.json"))
	if !strings.Contains(string(out), `"RUN_ABORTED"`) || !strings.Contains(string(out), "max_duration") {
		t.Fatalf("封筒: %s", out)
	}
	f := newFlowAt(t, dir)
	if o := f.run(t, []string{"backup"}, okEntry); o.err != nil { // 次の実行が始まる
		t.Fatalf("次の実行: %+v", o.err)
	}
}
