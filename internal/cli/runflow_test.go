package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
	"github.com/r2fish/sitate-backup/internal/state"
)

const runA = "r-20261018T173000Z-3f9a"

type flowEnv struct {
	d   *Deps
	rec *state.Recorder
	out *bytes.Buffer
	ntf *fake.Notifier
	clk *fake.Clock
}

func newFlow(t *testing.T) *flowEnv {
	t.Helper()
	return newFlowAt(t, t.TempDir())
}

// newFlowAt は状態ディレクトリを指定して作る(複数プロセスの試験で、親と子が同じディレクトリを使う)。
func newFlowAt(t interface{ Helper() }, dir string) *flowEnv {
	t.Helper()
	clk := &fake.Clock{T: fake.StartTime}
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	codec := &harness.RefCodec{}
	sf := &state.StateFiles{Dir: dir, Codec: codec, KeyS: fake.KeyService{}, Clock: clk}
	ntf := &fake.Notifier{}
	rec := &state.Recorder{State: sf, Store: store, Codec: codec, KeyS: fake.KeyService{}, Notifier: ntf, Clock: clk,
		Recipients: func(context.Context) ([]string, *string, error) { return []string{"ops@example.jp"}, nil, nil }}
	d := &Deps{StateDir: dir, Clock: clk, Lease: &Lease{Dir: dir}, State: sf, Recorder: rec, Limits: DefaultLimits(), out: &bytes.Buffer{}}
	return &flowEnv{d: d, rec: rec, out: d.out.(*bytes.Buffer), ntf: ntf, clk: clk}
}

func (f *flowEnv) run(t *testing.T, args []string, e entry) outcome {
	t.Helper()
	p, _, perr := parseArgs(args)
	if perr != nil {
		t.Fatal(perr)
	}
	o := outcome{command: p.sub.Words}
	id := runA
	if p.runID != "" {
		id = p.runID
	}
	o.runID = &id
	return f.d.lockedRun(bg, o, p, contract.Call{Words: p.sub.Words, RunID: id}, e)
}

func okEntry(*Deps, context.Context, contract.Call) (contract.Reply, error) {
	return contract.Reply{Result: json.RawMessage(`{"done":true}`)}, nil
}

func (f *flowEnv) ends(t *testing.T, kind string) []state.JournalItem {
	t.Helper()
	items, err := f.rec.Entries(bg)
	if err != nil {
		t.Fatal(err)
	}
	var out []state.JournalItem
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

func markerExists(d *Deps) bool { _, err := os.Stat(markerPath(d.StateDir)); return err == nil }

// 規則 9: 成功の実行は終了の項目を記録し、印を消す。
func TestRunFlowSuccessRecordsAndClearsMarker(t *testing.T) {
	f := newFlow(t)
	seen := false
	o := f.run(t, []string{"backup"}, func(d *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
		seen = markerExists(d) // 本体の中では印がある(最初の副作用の前に書く)
		return okEntry(d, c, contract.Call{})
	})
	if o.err != nil || !seen || markerExists(f.d) {
		t.Fatalf("err=%v 本体中の印=%v 終了後の印=%v", o.err, seen, markerExists(f.d))
	}
	if e := f.ends(t, "run_end"); len(e) != 1 || e[0].RunID != runA || !strings.Contains(string(e[0].Body), `"ok":true`) {
		t.Fatalf("run_end: %+v", e)
	}
}

// 規則 1: 別の持ち主がいれば LOCKED(終了 7)。印は消さず、終了の項目 LOCKED を記録する。
func TestRunFlowLocked(t *testing.T) {
	f := newFlow(t)
	holder := &Lease{Dir: f.d.StateDir}
	if ok, err := holder.Acquire(bg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	mk := `{"run_id":"r-20261018T170000Z-aaaa","command":["verify"],"pid":1,"pid_start_ticks":1,"boot_id":"x","started_at":"2026-10-18T17:00:00Z","deadline_at":"2099-01-01T00:00:00Z"}`
	if _, err := holder.WriteMarker(bg, []byte(mk)); err != nil {
		t.Fatal(err)
	}
	ran := false
	o := f.run(t, []string{"backup"}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		ran = true
		return contract.Reply{}, nil
	})
	if ran || o.err == nil || o.err.Code != contract.CodeLocked || exitOf(o.err) != 7 || !markerExists(f.d) {
		t.Fatalf("ran=%v err=%v", ran, o.err)
	}
	if !strings.Contains(string(o.err.Detail), `"holder_command":["verify"]`) {
		t.Errorf("detail: %s", o.err.Detail)
	}
	if e := f.ends(t, "run_end"); len(e) != 1 || !strings.Contains(string(e[0].Body), `"LOCKED"`) {
		t.Errorf("LOCKED の終了の項目: %+v", e)
	}
	for _, n := range f.rec.Notifications() {
		if n["type"] == "run.stuck" {
			t.Error("締め切り前なのに run.stuck を送った")
		}
	}
}

// 規則 15: 印の deadline_at + 猶予 + 記録の締め切りを過ぎてもロックが取られたままなら run.stuck。読めない印は直ちに。
func TestRunFlowStuck(t *testing.T) {
	for _, tc := range []struct {
		name, mk string
		want     bool
	}{
		{"過ぎた", `{"run_id":"r-20261018T170000Z-aaaa","command":["verify"],"pid":1,"pid_start_ticks":1,"boot_id":"x","started_at":"2026-10-01T00:00:00Z","deadline_at":"2026-10-02T00:00:00Z"}`, true},
		{"読めない", `{`, true},
		{"印なし", ``, false},
	} {
		f := newFlow(t)
		holder := &Lease{Dir: f.d.StateDir}
		_, _ = holder.Acquire(bg)
		if tc.mk != "" {
			_, _ = holder.WriteMarker(bg, []byte(tc.mk))
		}
		f.run(t, []string{"backup"}, okEntry)
		got := false
		for _, n := range f.rec.Notifications() {
			got = got || n["type"] == "run.stuck"
		}
		if got != tc.want {
			t.Errorf("%s: run.stuck=%v want %v", tc.name, got, tc.want)
		}
	}
}

// 規則 8: 前の印が残り終了の項目が無ければ中断として記録、あれば記録せず印だけ置き換わる。
func TestRunFlowInterruptedPrev(t *testing.T) {
	f := newFlow(t)
	mk := `{"run_id":"r-20261018T170000Z-aaaa","command":["verify"],"pid":1,"pid_start_ticks":1,"boot_id":"x","started_at":"2026-10-18T17:00:00Z","deadline_at":"2026-10-19T17:00:00Z"}`
	_ = os.MkdirAll(f.d.StateDir+"/run", 0o700)
	_ = os.WriteFile(markerPath(f.d.StateDir), []byte(mk), 0o600)
	f.run(t, []string{"backup"}, okEntry)
	ints := f.ends(t, "run_interrupted")
	if len(ints) != 1 || !strings.Contains(string(ints[0].Body), "r-20261018T170000Z-aaaa") {
		t.Fatalf("run_interrupted: %+v", ints)
	}
	got := false
	for _, n := range f.rec.Notifications() {
		got = got || n["type"] == "run.interrupted"
	}
	if !got {
		t.Error("run.interrupted の通知が無い")
	}
	// 同じ run_id の終了の項目があれば記録しない(規則 8)。
	f2 := newFlow(t)
	f2.d.recordItem(bg, "run_end", "r-20261018T170000Z-aaaa", map[string]any{"command": []string{"verify"}, "ok": false, "code": "STORAGE_UNREACHABLE", "envelope": map[string]any{}})
	_ = os.MkdirAll(f2.d.StateDir+"/run", 0o700)
	_ = os.WriteFile(markerPath(f2.d.StateDir), []byte(mk), 0o600)
	f2.run(t, []string{"backup"}, okEntry)
	if len(f2.ends(t, "run_interrupted")) != 0 {
		t.Error("終了の項目があるのに中断と記録した")
	}
	// LOCKED で終わった終了の項目は数えない。
	f3 := newFlow(t)
	f3.d.recordItem(bg, "run_end", "r-20261018T170000Z-aaaa", map[string]any{"command": []string{"verify"}, "ok": false, "code": "LOCKED", "envelope": map[string]any{}})
	_ = os.MkdirAll(f3.d.StateDir+"/run", 0o700)
	_ = os.WriteFile(markerPath(f3.d.StateDir), []byte(mk), 0o600)
	f3.run(t, []string{"backup"}, okEntry)
	if len(f3.ends(t, "run_interrupted")) != 1 {
		t.Error("LOCKED の終了の項目を数えた")
	}
}

// 規則 7: --run-id の完了済みは前回の封筒を返して本体を走らせない。別の command は reused。再試行できる終わり方は再実行する。
func TestRunFlowRunIDCompleted(t *testing.T) {
	f := newFlow(t)
	id := "r-20261018T173000Z-bbbb"
	args := []string{"backup", "--run-id", id}
	f.run(t, args, okEntry)
	ran := false
	o := f.run(t, args, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		ran = true
		return contract.Reply{}, nil
	})
	if ran || o.err != nil || !strings.Contains(string(o.result), `"done":true`) {
		t.Fatalf("再生: ran=%v err=%v result=%s", ran, o.err, o.result)
	}
	if o := f.run(t, []string{"extend", "--run-id", id}, okEntry); o.err == nil || !strings.Contains(string(o.err.Detail), "reused") {
		t.Errorf("別の command: %v", o.err)
	}
	id2 := "r-20261018T173000Z-cccc"
	f.run(t, []string{"backup", "--run-id", id2}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		return contract.Reply{}, newErr(contract.CodeStorageUnreachable, map[string]any{"target": "storage"})
	})
	ran = false
	f.run(t, []string{"backup", "--run-id", id2}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		ran = true
		return contract.Reply{}, nil
	})
	if !ran {
		t.Error("retryable で終わった実行を完了済みに数えた")
	}
}

// 規則 10: 上限を超えて文脈の取り消しを受けた実行が自ら中止すると RUN_ABORTED(max_duration)。
func TestRunFlowSelfAbort(t *testing.T) {
	f := newFlow(t)
	f.d.Limits.MaxDuration = 50 * time.Millisecond
	f.d.Lease = &Lease{Dir: f.d.StateDir, MaxDuration: 50 * time.Millisecond}
	o := f.run(t, []string{"backup"}, func(_ *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
		<-c.Done()
		return contract.Reply{}, c.Err()
	})
	if o.err == nil || o.err.Code != contract.CodeRunAborted || !strings.Contains(string(o.err.Detail), "max_duration") || markerExists(f.d) {
		t.Fatalf("%v marker=%v", o.err, markerExists(f.d))
	}
	if e := f.ends(t, "run_end"); len(e) != 1 || !strings.Contains(string(e[0].Body), "RUN_ABORTED") {
		t.Errorf("run_end: %+v", e)
	}
}

// 規則 10・21: 取り消しの後も終わらない本体を、監視の goroutine が猶予の後に中止して終了する(記録 → 印の消去 → 封筒 → 終了)。
func TestRunFlowWatchdogAborts(t *testing.T) {
	f := newFlow(t)
	f.d.Limits = Limits{MaxDuration: 50 * time.Millisecond, AbortGrace: 100 * time.Millisecond, RecordDeadline: time.Second, StateLockWait: time.Second}
	f.d.Lease = &Lease{Dir: f.d.StateDir, MaxDuration: 50 * time.Millisecond}
	var mu sync.Mutex
	code := -1
	exited := make(chan struct{})
	f.d.exit = func(c int) { mu.Lock(); code = c; mu.Unlock(); close(exited); select {} }
	buf := &syncBuf{}
	f.d.out = buf
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	p, _, _ := parseArgs([]string{"backup"})
	id := runA
	go f.d.lockedRun(bg, outcome{command: p.sub.Words, runID: &id}, p, contract.Call{RunID: id},
		func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
			<-hang
			return contract.Reply{}, nil
		})
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("監視の goroutine が終了しなかった")
	}
	mu.Lock()
	defer mu.Unlock()
	if code != 7 || !strings.Contains(buf.String(), `"RUN_ABORTED"`) || markerExists(f.d) {
		t.Fatalf("code=%d out=%s marker=%v", code, buf.String(), markerExists(f.d))
	}
	if e := f.ends(t, "run_end"); len(e) != 1 {
		t.Errorf("run_end: %+v", e)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// 規則 12: SIGTERM は文脈の取り消しで、RUN_ABORTED(signal)と run.interrupted(signal)の記録になる。
func TestRunFlowSigterm(t *testing.T) {
	f := newFlow(t)
	started := make(chan struct{})
	go func() {
		<-started
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	o := f.run(t, []string{"backup"}, func(_ *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
		close(started)
		<-c.Done()
		return contract.Reply{}, c.Err()
	})
	if o.err == nil || o.err.Code != contract.CodeRunAborted || !strings.Contains(string(o.err.Detail), `"signal"`) || markerExists(f.d) {
		t.Fatalf("%v", o.err)
	}
	got := false
	for _, n := range f.rec.Notifications() {
		got = got || n["type"] == "run.interrupted"
	}
	if !got {
		t.Error("run.interrupted が無い")
	}
}
