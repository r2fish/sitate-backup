package adapter

// 子のプロセスグループの後始末、時限での返り、標準エラーの取りこぼし、出力の違反での停止(spec 3.1.1 の規則 13、3.2.1)。

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// alive は pid のプロセスが生きているか(ゾンビは死んだものとして数える)。/proc が無い環境では試験を飛ばす。
func alive(t *testing.T, pid int) bool {
	t.Helper()
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if _, serr := os.Stat("/proc/self/stat"); serr != nil {
			t.Skip("/proc が無い")
		}
		return false
	}
	i := strings.LastIndexByte(string(b), ')')
	return i < 0 || i+2 >= len(b) || b[i+2] != 'Z'
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	for i := 0; i < 100; i++ {
		if b, err := os.ReadFile(path); err == nil {
			if n, cerr := strconv.Atoi(strings.TrimSpace(string(b))); cerr == nil {
				return n
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid が書かれない: %s", path)
	return 0
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if !alive(t, pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("プロセス %d が残った", pid)
}

// 親が正常に終わった後、同じグループに残った孫を止める。
func TestGroupKilledAfterNormalExit(t *testing.T) {
	b := build(t, notifySpec(`cat >/dev/null; sleep 30 </dev/null >/dev/null 2>&1 & echo $! > "$0.pid"; echo '`+okResult+`'`))
	if _, err := runNotify(b); err != nil {
		t.Fatal(err)
	}
	waitDead(t, readPID(t, filepath.Join(b.dir, "exe.pid")))
}

// 時限で親が SIGTERM で死んだ後、SIGTERM を無視する孫を止める。
func TestGroupKilledAfterTimeout(t *testing.T) {
	b := build(t, notifySpec(`trap '' TERM; sleep 30 </dev/null >/dev/null 2>&1 & echo $! > "$0.pid"; trap - TERM; cat >/dev/null; sleep 30`))
	b.r.timeouts = map[string]time.Duration{"notify": 200 * time.Millisecond}
	b.r.grace = 10 * time.Second
	_, err := runNotify(b)
	errDetail(t, err, contract.CodeAdapterTimeout)
	waitDead(t, readPID(t, filepath.Join(b.dir, "exe.pid")))
}

func needSetsid(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid が無い")
	}
}

// setsid で抜けた孫が標準出力・標準エラーを握っていても、時限と猶予の直後に ADAPTER_TIMEOUT で返る。
func TestTimeoutReturnsDespiteDetachedGrandchild(t *testing.T) {
	needSetsid(t)
	b := build(t, notifySpec(`trap '' TERM; setsid sleep 30 & while :; do sleep 1; done`))
	b.r.timeouts = map[string]time.Duration{"notify": 200 * time.Millisecond}
	b.r.grace = 200 * time.Millisecond
	start := time.Now()
	_, err := runNotify(b)
	errDetail(t, err, contract.CodeAdapterTimeout)
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("孫の寿命まで待った: %v", el)
	}
}

// 親が終わった後に孫が読み口を握っていても、結果は返り、待ちは短い。
func TestReturnsAfterExitDespiteDetachedGrandchild(t *testing.T) {
	needSetsid(t)
	for name, redir := range map[string]string{"標準出力と標準エラー": "", "標準エラーだけ": ">/dev/null"} {
		t.Run(name, func(t *testing.T) {
			b := build(t, notifySpec(`cat >/dev/null; setsid sleep 30 </dev/null `+redir+` & echo '`+okResult+`'`))
			b.r.drain = 200 * time.Millisecond
			start := time.Now()
			res, err := runNotify(b)
			if err != nil || len(res) == 0 {
				t.Fatalf("結果: %s %v", res, err)
			}
			if el := time.Since(start); el > 5*time.Second {
				t.Errorf("孫の寿命まで待った: %v", el)
			}
		})
	}
}

// 異常終了の直前に書いた標準エラー(読み口が閉じる前に読み終える)を、毎回最後の行まで記録する。
func TestStderrTailNeverLost(t *testing.T) {
	b := build(t, notifySpec(`cat >/dev/null; head -c 60000 /dev/zero | tr '\0' x >&2; echo >&2; echo LASTLINE >&2; exit 3`))
	var rec StderrRecord
	b.r.Stderr = func(r StderrRecord) { rec = r }
	for i := 0; i < 60; i++ {
		rec = StderrRecord{}
		if _, err := runNotify(b); err == nil {
			t.Fatal("終了 3 が成功に見える")
		}
		if !strings.HasSuffix(rec.Text, "LASTLINE\n") || rec.Dropped != 0 || len(rec.Text) != 60000+1+9 {
			t.Fatalf("%d 回目: 標準エラーの末尾を失った: len=%d dropped=%d", i, len(rec.Text), rec.Dropped)
		}
	}
}

// 出力の違反を見つけたら子を止め、終了コードや時限より先に INTERNAL(output)を返す。
func TestOutputViolationStopsChild(t *testing.T) {
	b := build(t, notifySpec(`cat >/dev/null; echo $$ > "$0.pid"; echo junk; sleep 30; echo '`+okResult+`'`))
	start := time.Now()
	_, err := runNotify(b)
	internalWith(t, err, "output")
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("子の終わりまで待った: %v", el)
	}
	waitDead(t, readPID(t, filepath.Join(b.dir, "exe.pid")))
}

// onFrame の誤りで子を止める(眠る子が実際に止まる)。
func TestOnFrameErrorKillsChild(t *testing.T) {
	b := build(t, spec{kind: "acquire", name: "files", script: `cat >/dev/null; echo $$ > "$0.pid"; cat "$0.out"; sleep 30`})
	if err := os.WriteFile(filepath.Join(b.dir, "exe.out"), frameBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	start := time.Now()
	_, err := b.r.Run(ctx, contract.RunContext{}, "acquire", "files", acquireReq(), func(contract.Frame) error { return boom })
	if !errors.Is(err, boom) || time.Since(start) > 5*time.Second {
		t.Errorf("onFrame の誤りで止まらない: %v %v", err, time.Since(start))
	}
	waitDead(t, readPID(t, filepath.Join(b.dir, "exe.pid")))
}

// PdeathSig: 核(試験バイナリ自身を子にした別プロセス)が SIGKILL で死ぬと、アダプタも死ぬ。Linux だけ。
func TestPdeathSigKillsAdapterWhenCoreDies(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PR_SET_PDEATHSIG は Linux だけ")
	}
	b := build(t, notifySpec(`cat >/dev/null; echo $$ > "$0.pid"; sleep 30`))
	core := exec.Command(os.Args[0], "-test.run=^TestHelperCore$")
	core.Env = []string{"SITATE_ADAPTER_HELPER_ROOT=" + b.root, "PATH=" + os.Getenv("PATH")}
	if err := core.Start(); err != nil {
		t.Fatal(err)
	}
	pid := readPID(t, filepath.Join(b.dir, "exe.pid"))
	if !alive(t, pid) {
		t.Fatal("アダプタが起動していない")
	}
	_ = core.Process.Kill()
	_ = core.Wait()
	waitDead(t, pid)
}

// TestHelperCore は上の試験が別プロセスで動かす「核」。環境変数が無ければ何もしない。
func TestHelperCore(t *testing.T) {
	root := os.Getenv("SITATE_ADAPTER_HELPER_ROOT")
	if root == "" {
		t.Skip("別プロセス用")
	}
	r := &Runner{allowBareAcquire: true, Root: root, Version: "1.0.0", Arch: "linux-x86_64", Verifier: okVerifier{}}
	_, _ = r.Run(ctx, contract.RunContext{PdeathSig: true}, "notify", "webhook", request("sitate.notify/v1"), nil)
}
