package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/state"
)

func stuckSent(f *flowEnv) bool {
	for _, n := range f.rec.Notifications() {
		if n["type"] == "run.stuck" {
			return true
		}
	}
	return false
}

// 完了条件 5 の 3 (規則 15): 印の deadline_at + 猶予 + 記録の締め切りを過ぎてからだけ run.stuck。境界の前は送らない。
func TestRunStuckThresholdIsDeadlinePlusGracePlusRecord(t *testing.T) {
	const dl = "2026-10-02T00:00:00Z"
	mk := `{"run_id":"r-20261018T170000Z-aaaa","command":["verify"],"pid":1,"pid_start_ticks":1,"boot_id":"x","started_at":"2026-10-01T00:00:00Z","deadline_at":"` + dl + `"}`
	deadline, _ := time.Parse(time.RFC3339, dl)
	lim := DefaultLimits()
	edge := deadline.Add(lim.AbortGrace + lim.RecordDeadline)
	for _, tc := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"期限の直後", deadline.Add(time.Second), false},
		{"猶予の途中", deadline.Add(lim.AbortGrace), false},
		{"境界の 1 秒前", edge.Add(-time.Second), false},
		{"境界の 1 秒後", edge.Add(time.Second), true},
	} {
		f := newFlow(t)
		f.clk.T = tc.now
		holder := &Lease{Dir: f.d.StateDir}
		_, _ = holder.Acquire(bg)
		_, _ = holder.WriteMarker(bg, []byte(mk))
		f.run(t, []string{"backup"}, okEntry)
		if got := stuckSent(f); got != tc.want {
			t.Errorf("%s: run.stuck=%v want %v", tc.name, got, tc.want)
		}
	}
}

// 完了条件 5 の 6 (規則 10): 取り消しの後、猶予の前には監視の goroutine は中止しない。猶予の前に終わった本体の成功は中止に書き換えない。
func TestWatchdogWaitsForGraceAndKeepsCompletion(t *testing.T) {
	f := newFlow(t)
	f.d.Limits = Limits{MaxDuration: 40 * time.Millisecond, AbortGrace: 600 * time.Millisecond, RecordDeadline: time.Second, StateLockWait: time.Second}
	f.d.Lease = &Lease{Dir: f.d.StateDir, MaxDuration: 40 * time.Millisecond}
	var exited atomic.Bool
	f.d.exit = func(int) { exited.Store(true) }
	done := make(chan outcome, 1)
	go func() {
		done <- f.run(t, []string{"backup"}, func(_ *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
			<-c.Done()
			time.Sleep(80 * time.Millisecond) // 取り消しを受けた後、猶予より前に成功で終わる
			return contract.Reply{Result: json.RawMessage(`{"done":true}`)}, nil
		})
	}()
	var o outcome
	select {
	case o = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("本体の完了が受け付けられない(猶予の前に中止が確定した)")
	}
	time.Sleep(700 * time.Millisecond) // 猶予を過ぎても、終わった実行を監視が中止しない
	if o.err != nil || exited.Load() {
		t.Fatalf("成功を中止に書き換えた/猶予の前に終了した: err=%v exit=%v", o.err, exited.Load())
	}
	if e := f.ends(t, "run_end"); len(e) != 1 || !strings.Contains(string(e[0].Body), `"ok":true`) {
		t.Errorf("run_end: %+v", e)
	}
}

// 完了条件 5 の 7 (規則 21): 保管先へ書けなくても、状態ディレクトリには終了の項目を残して印を消す。次にロックを取る実行が保管先へ写す。
func TestRemoteWriteFailureKeepsLocalRunEndAndNextRunSyncs(t *testing.T) {
	f := newFlow(t)
	rc := f.rec
	failing := true
	st, _, _ := fake.NewTestStore(fake.RoleWrite)
	rc.Store = &flakyStore{ObjectStore: st, fail: &failing}
	f.run(t, []string{"backup"}, okEntry)
	if markerExists(f.d) || len(f.ends(t, "run_end")) != 1 {
		t.Fatalf("保管先に書けなくても局所には残し、印を消す: marker=%v", markerExists(f.d))
	}
	if vs, _ := st.ListVersions(bg, "journal/"); len(vs) != 0 {
		t.Fatalf("保管先へ書けている: %v", vs)
	}
	failing = false
	f.run(t, []string{"backup"}, okEntry)
	objs, err := state.ReadObjects(bg, f.d.State)
	if err != nil {
		t.Fatal(err)
	}
	var first bool
	for _, o := range objs {
		first = first || strings.HasPrefix(o.Key, "journal/000000000001-")
	}
	if !first {
		t.Errorf("次の実行が前回の項目を保管先へ写していない(規則 23): %v", objs)
	}
}

type flakyStore struct {
	contract.ObjectStore
	fail *bool
}

func (s *flakyStore) Put(ctx context.Context, key string, r io.Reader, c contract.RetentionClass) (contract.PutResult, error) {
	if *s.fail {
		return contract.PutResult{}, errors.New("unreachable")
	}
	return s.ObjectStore.Put(ctx, key, r, c)
}

// 完了条件 5 の 9 (規則 19): ロックの基盤の失敗(EACCES・ENOLCK・対応しない FS)は LOCK_UNAVAILABLE(終了 3)。flock を差し替えて注入する。
func TestLockUnavailableOnFlockFailure(t *testing.T) {
	old := flockFn
	defer func() { flockFn = old }()
	for _, tc := range []struct {
		errno  syscall.Errno
		reason string
	}{{syscall.EACCES, "eacces"}, {syscall.ENOLCK, "enolck"}, {syscall.ENOTSUP, "unsupported_fs"}} {
		flockFn = func(int, int) error { return tc.errno }
		f := newFlow(t)
		o := f.run(t, []string{"backup"}, okEntry)
		if o.err == nil || o.err.Code != contract.CodeLockUnavailable || exitOf(o.err) != 3 || !strings.Contains(string(o.err.Detail), tc.reason) {
			t.Errorf("%v: %v", tc.errno, o.err)
		}
	}
}

// 完了条件 5 の 10 (規則 17): ロックを持つ実行の間も、ロックを要しない操作は LOCKED を返さずに完了する(cancel・hold set・propose・status)。
func TestLocklessOpsCompleteWhileRunLockHeld(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	f.d.Store = &holdStub{since: time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)}
	holder := &Lease{Dir: f.d.StateDir}
	if ok, err := holder.Acquire(bg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	_, _ = holder.WriteMarker(bg, []byte(prevMarker))
	for _, args := range [][]string{{"hold", "set"}, {"status"}, {"cancel", "--id", "c-1"}, {"propose", "delay-change", "--hours", "24"}} {
		o := execute(bg, f.d, args, secretNone)
		if o.err != nil && o.err.Code == contract.CodeLocked {
			t.Errorf("%v が LOCKED になった", args)
		}
	}
	if o := execute(bg, f.d, []string{"backup"}, secretNone); o.err == nil || o.err.Code != contract.CodeLocked {
		t.Errorf("対照: ロックを取る backup は LOCKED: %v", o.err)
	}
}

// 完了条件 5 の 16: state.lock を締め切りまでに取れなかったときは LOCK_UNAVAILABLE でなく LOCKED(detail.lock: state)。
func TestStateLockTimeoutIsLocked(t *testing.T) {
	dir := t.TempDir()
	holder := &state.StateFiles{Dir: dir}
	h, err := holder.Lock(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	sf := &state.StateFiles{Dir: dir, LockWait: 50 * time.Millisecond, LockRetry: 10 * time.Millisecond}
	_, err = sf.Lock(bg)
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeLocked || !strings.Contains(string(ce.Detail), `"state"`) {
		t.Fatalf("%v", err)
	}
}
