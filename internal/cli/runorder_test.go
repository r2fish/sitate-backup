package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/state"
)

// spyRec は記録の口を差し替える(本物の Recorder を包む)。
type spyRec struct {
	*state.Recorder
	d         *Deps
	onSync    func(ctx context.Context) error
	onRecord  func(kind string, ctx context.Context) error
	onEmit    func(ctx context.Context, typ string) error
	markerAt  map[string]bool // Record(kind) の時点で印があったか
	syncCalls int
}

func (s *spyRec) SyncRemote(ctx context.Context) error {
	s.syncCalls++
	if s.onSync != nil {
		return s.onSync(ctx)
	}
	return s.Recorder.SyncRemote(ctx)
}

func (s *spyRec) Record(ctx context.Context, entry []byte) (string, error) {
	var k struct{ Kind string }
	_ = json.Unmarshal(entry, &k)
	if s.markerAt != nil {
		s.markerAt[k.Kind] = markerExists(s.d)
	}
	if s.onRecord != nil {
		if err := s.onRecord(k.Kind, ctx); err != nil {
			return "", err
		}
	}
	return s.Recorder.Record(ctx, entry)
}

func (s *spyRec) Emit(ctx context.Context, typ string, facts []byte) (contract.Event, []contract.DeliveryResult, error) {
	if s.onEmit != nil {
		if err := s.onEmit(ctx, typ); err != nil {
			return contract.Event{}, nil, err
		}
	}
	return s.Recorder.Emit(ctx, typ, facts)
}

func newSpyFlow(t *testing.T) (*flowEnv, *spyRec) {
	t.Helper()
	f := newFlow(t)
	sp := &spyRec{Recorder: f.rec, d: f.d, markerAt: map[string]bool{}}
	f.d.Recorder = sp
	return f, sp
}

// 規則 7: 自分の印を書いた後に SyncRemote を呼ぶ(保管先への書き込みが最初の副作用で、その前に印がある)。
func TestSyncRemoteAfterMarker(t *testing.T) {
	f, sp := newSpyFlow(t)
	seen := false
	sp.onSync = func(context.Context) error { seen = markerExists(f.d); return nil }
	f.run(t, []string{"backup"}, okEntry)
	if sp.syncCalls != 1 || !seen {
		t.Fatalf("calls=%d 印の後=%v", sp.syncCalls, seen)
	}
}

// 規則 10・12: 印の後の全区間(SyncRemote)にも上限が効く。保管先が応答しなくても RUN_ABORTED で終わり、run_end を書いて印を消す。
func TestSyncRemoteHangIsAbortedByDeadline(t *testing.T) {
	f, sp := newSpyFlow(t)
	f.d.Limits.MaxDuration = 60 * time.Millisecond
	f.d.Lease = &Lease{Dir: f.d.StateDir, MaxDuration: 60 * time.Millisecond}
	sp.onSync = func(c context.Context) error { <-c.Done(); return c.Err() }
	ran := false
	o := f.run(t, []string{"backup"}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		ran = true
		return contract.Reply{}, nil
	})
	if ran || o.err == nil || o.err.Code != contract.CodeRunAborted || !strings.Contains(string(o.err.Detail), "max_duration") || markerExists(f.d) {
		t.Fatalf("ran=%v err=%v marker=%v", ran, o.err, markerExists(f.d))
	}
	if len(f.ends(t, "run_end")) != 1 {
		t.Errorf("run_end: %d", len(f.ends(t, "run_end")))
	}
}

// 前の印の中断は、前の実行の後の SyncRemote が取り消されても記録される(前の印を失わない)。
func TestPrevMarkerRecordedEvenWhenAbortedBeforeSettle(t *testing.T) {
	f, sp := newSpyFlow(t)
	f.d.Limits.MaxDuration = 60 * time.Millisecond
	f.d.Lease = &Lease{Dir: f.d.StateDir, MaxDuration: 60 * time.Millisecond}
	sp.onSync = func(c context.Context) error { <-c.Done(); return c.Err() }
	_ = os.MkdirAll(f.d.StateDir+"/run", 0o700)
	_ = os.WriteFile(markerPath(f.d.StateDir), []byte(prevMarker), 0o600)
	f.run(t, []string{"backup"}, okEntry)
	if len(f.ends(t, "run_interrupted")) != 1 {
		t.Errorf("run_interrupted: %d", len(f.ends(t, "run_interrupted")))
	}
}

// 規則 9: 記録(run_end)は印の消去の前。成功でも中止でも、run_end を書く時点で印がある。
func TestRunEndRecordedBeforeMarkerRemoved(t *testing.T) {
	f, sp := newSpyFlow(t)
	f.run(t, []string{"backup"}, okEntry)
	if !sp.markerAt["run_end"] || markerExists(f.d) {
		t.Errorf("成功: run_end の時点の印=%v 終了後の印=%v", sp.markerAt["run_end"], markerExists(f.d))
	}
	f2, sp2 := newSpyFlow(t)
	f2.d.Limits.MaxDuration = 40 * time.Millisecond
	f2.d.Lease = &Lease{Dir: f2.d.StateDir, MaxDuration: 40 * time.Millisecond}
	f2.run(t, []string{"backup"}, func(_ *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
		<-c.Done()
		return contract.Reply{}, c.Err()
	})
	if !sp2.markerAt["run_end"] || markerExists(f2.d) {
		t.Errorf("中止: run_end の時点の印=%v 終了後の印=%v", sp2.markerAt["run_end"], markerExists(f2.d))
	}
}

// 規則 9・21: 終了の項目を状態ディレクトリへ書けなかったときは印を消さない(次の実行が中断として記録する)。
func TestMarkerKeptWhenRunEndNotRecorded(t *testing.T) {
	f, sp := newSpyFlow(t)
	sp.onRecord = func(kind string, _ context.Context) error {
		if kind == "run_end" {
			return errors.New("state.lock timeout")
		}
		return nil
	}
	f.run(t, []string{"backup"}, okEntry)
	if !markerExists(f.d) {
		t.Fatal("run_end を書けなかったのに印を消した")
	}
	sp.onRecord = nil
	f.run(t, []string{"backup"}, okEntry)
	if len(f.ends(t, "run_interrupted")) != 1 {
		t.Errorf("次の実行が中断を記録する: %d", len(f.ends(t, "run_interrupted")))
	}
}

// 規則 21: 中止の通知の配送が応答しなくても、終了の項目の記録の時間は残る。
func TestAbortNotificationDoesNotStarveRunEnd(t *testing.T) {
	f, sp := newSpyFlow(t)
	f.d.Limits.MaxDuration = 40 * time.Millisecond
	f.d.Limits.RecordDeadline = 400 * time.Millisecond
	f.d.Lease = &Lease{Dir: f.d.StateDir, MaxDuration: 40 * time.Millisecond}
	sp.onEmit = func(c context.Context, typ string) error {
		if typ == "run.interrupted" {
			<-c.Done()
			return c.Err()
		}
		return nil
	}
	var endCtxErr error
	sp.onRecord = func(kind string, c context.Context) error {
		if kind == "run_end" {
			endCtxErr = c.Err()
		}
		return nil
	}
	f.run(t, []string{"backup"}, func(_ *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
		<-c.Done()
		return contract.Reply{}, c.Err()
	})
	if len(f.ends(t, "run_end")) != 1 || markerExists(f.d) || endCtxErr != nil {
		t.Errorf("run_end=%d marker=%v 記録の文脈=%v", len(f.ends(t, "run_end")), markerExists(f.d), endCtxErr)
	}
}

// 規則 10・17: ロックを要しない操作にも上限と中止がある。NOLOCK_OP_DEADLINE を持つ操作は reason: deadline、持たない操作は max_duration、SIGTERM は signal。
func TestUnlockedOpsAreAborted(t *testing.T) {
	block := func(_ *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
		<-c.Done()
		return contract.Reply{}, c.Err()
	}
	run := func(t *testing.T, key string, tune func(*Deps)) outcome {
		f, _ := newSpyFlow(t)
		tune(f.d)
		return f.d.runUnlocked(bg, outcome{command: strings.Fields(key)}, block, contract.Call{}, key)
	}
	o := run(t, "status", func(d *Deps) { d.Limits.NolockDeadline = 40 * time.Millisecond })
	if o.err == nil || o.err.Code != contract.CodeRunAborted || !strings.Contains(string(o.err.Detail), `"deadline"`) {
		t.Errorf("deadline: %v", o.err)
	}
	o = run(t, "propose", func(d *Deps) { d.Limits.MaxDuration = 40 * time.Millisecond })
	if o.err == nil || o.err.Code != contract.CodeRunAborted || !strings.Contains(string(o.err.Detail), `"max_duration"`) {
		t.Errorf("max_duration: %v", o.err)
	}
	go func() { time.Sleep(60 * time.Millisecond); _ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM) }()
	o = run(t, "propose", func(d *Deps) {})
	if o.err == nil || o.err.Code != contract.CodeRunAborted || !strings.Contains(string(o.err.Detail), `"signal"`) {
		t.Errorf("signal: %v", o.err)
	}
}

// M8: ロックを取る実行が発行したイベントの id は、その実行の run_id から作る。
func TestEventIDFollowsRunID(t *testing.T) {
	f, _ := newSpyFlow(t)
	var id string
	f.run(t, []string{"backup"}, func(d *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
		ev, _, err := d.Recorder.Emit(c, "backup.failed", []byte(`{"run_id":"`+runA+`","error_code":"X","warning_codes":[],"snapshot_ref":null,"targets_total":0,"targets_failed":0,"chunk_keys_missing":0,"reason":null,"skipped_backups_count":0}`))
		id = ev.ID
		return contract.Reply{}, err
	})
	want := "e-" + strings.TrimPrefix(runA, "r-") + "-01"
	if id != want {
		t.Errorf("id=%q want %q", id, want)
	}
}

// M9: ロックを取った実行の終わりに last_runs[] を書く(サブコマンドごとに最後の 1 件。失敗も ok: false で)。実行中の印の run_id は新しい run_id と重ねない。
func TestLastRunsWritten(t *testing.T) {
	f, _ := newSpyFlow(t)
	f.run(t, []string{"backup"}, okEntry)
	f.run(t, []string{"backup"}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		return contract.Reply{}, newErr(contract.CodeStorageUnreachable, nil)
	})
	v, err := f.d.State.Get(bg, "last_runs")
	if err != nil {
		t.Fatal(err)
	}
	var runs []lastRun
	if json.Unmarshal(v, &runs) != nil || len(runs) != 1 || runs[0].OK || runs[0].RunID != runA || runs[0].Command[0] != "backup" {
		t.Errorf("last_runs: %s", v)
	}
}

func TestRunIDInUseSeesMarker(t *testing.T) {
	f, _ := newSpyFlow(t)
	_ = os.MkdirAll(f.d.StateDir+"/run", 0o700)
	_ = os.WriteFile(markerPath(f.d.StateDir), []byte(prevMarker), 0o600)
	if !f.d.runIDInUse(bg, "r-20261018T170000Z-aaaa") || f.d.runIDInUse(bg, "r-20261018T170000Z-bbbb") {
		t.Error("実行中の印の run_id を使用中と数える")
	}
}

// 規則 23: journal/ への追記は state.lock の短い排他の下で行う。別の持ち主が state.lock を持つ間、Record は LOCKED で諦める。
func TestRecordTakesStateLock(t *testing.T) {
	f := newFlow(t)
	sf := f.d.State.(*state.StateFiles)
	sf.LockWait, sf.LockRetry = 50*time.Millisecond, 10*time.Millisecond
	other := &state.StateFiles{Dir: f.d.StateDir}
	h, err := other.Lock(bg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.rec.Record(bg, []byte(`{"at":"2026-11-03T00:00:00Z","kind":"hold","run_id":"","body":{}}`))
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeLocked {
		t.Fatalf("state.lock を持たずに追記した/別の誤り: %v", err)
	}
	if items, _ := f.rec.Entries(bg); len(items) != 0 {
		t.Errorf("state.lock を持たない間に追記された: %d 件", len(items))
	}
	h.Release()
	if _, err := f.rec.Record(bg, []byte(`{"at":"2026-11-03T00:00:00Z","kind":"hold","run_id":"","body":{}}`)); err != nil {
		t.Errorf("放した後は追記できる: %v", err)
	}
}
