package cli

// ロックを取る実行の流れ(spec 3.1.1 の規則 1・7〜9・21):
// ロック → 前の印を読んで自分の印を書く → 前の印の処理 → 完了済みの判定 → 本体 → 記録(journal/)→ 印の消去 → 封筒。

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/state"
)

// 記録の読み書きの口。契約の Recorder に無い操作は state.Recorder の公開メソッドで受ける(結線は state.Recorder を入れる)。
type journalReader interface {
	Entries(ctx context.Context) ([]state.JournalItem, error)
}
type remoteSyncer interface {
	SyncRemote(ctx context.Context) error
}
type notificationLister interface{ Notifications() []map[string]any }

// runBody は 1 回のロックを取る実行の状態。
type runBody struct {
	d       *Deps
	o       outcome
	runID   string
	command []string
	start   time.Time // 単調時計つき(規則 22)
	ended   atomic.Bool
	extra   runExtra // 本体の入口が run_end へ渡す欄(backup の targets[])
}

// lockedRun はロックを取る実行(takesRunLock)の本体。e は部品の入口。
func (d *Deps) lockedRun(ctx context.Context, o outcome, p *parsed, call contract.Call, e entry) outcome {
	if d.Recorder == nil || d.Lease == nil || d.Clock == nil {
		o.err = toError(contract.ErrNotImplemented("cli.run"))
		return o
	}
	ok, err := d.acquireRun(ctx, p)
	if err != nil {
		o.err = toError(err)
		return o
	}
	if !ok {
		return d.lockedOutcome(ctx, o)
	}
	b := &runBody{d: d, o: o, command: p.sub.Words, start: time.Now()}
	if o.runID != nil {
		b.runID = *o.runID
		if rs, ok := d.Recorder.(interface{ SetRunID(string) }); ok {
			rs.SetRunID(b.runID) // 通知のイベントの id は発行した実行の run_id から作る(spec 4.8)
		}
	}
	return b.run(ctx, p, call, e)
}

// acquireRun はロックを非待機で取る。restore-dest teardown だけは空くまで待つ(上限は実行の上限。超えたら取れなかったとして返す。規則 4)。
func (d *Deps) acquireRun(ctx context.Context, p *parsed) (bool, error) {
	ok, err := d.Lease.Acquire(ctx)
	if err != nil || ok || len(p.sub.Words) != 2 || p.sub.Words[1] != "teardown" {
		return ok, err
	}
	wctx, cancel := context.WithTimeout(ctx, d.Limits.MaxDuration)
	defer cancel()
	for {
		select {
		case <-wctx.Done():
			return false, nil
		case <-time.After(contract.StateLockRetryInterval):
		}
		if ok, err = d.Lease.Acquire(ctx); err != nil || ok {
			return ok, err
		}
	}
}

func (b *runBody) run(ctx context.Context, p *parsed, call contract.Call, e entry) outcome {
	d := b.d
	now, err := d.Clock.Now(ctx)
	if err != nil {
		b.o.err = toError(err)
		return b.o // 印を書けていない。前の印は消さない(次の実行が中断として記録する)
	}
	pid, ticks, boot := selfProc()
	mk, _ := json.Marshal(marker{RunID: b.runID, Command: b.command, Pid: pid, PidStartTicks: ticks, BootID: boot,
		StartedAt: now.Format(time.RFC3339), DeadlineAt: now.Add(d.Limits.MaxDuration).Format(time.RFC3339)})
	prev, err := d.Lease.WriteMarker(ctx, mk) // 規則 7: ロック → 前の印を読む → 自分の印を書く → 最初の副作用
	if err != nil {
		b.o.err = toError(err)
		return b.o // 印を書けていない。消すものも記録するものも無い
	}
	g := b.guard(ctx) // 上限・SIGTERM・監視は印の後の全区間に効かせる(規則 10・12)
	defer g.stop()
	if s, ok := d.Recorder.(remoteSyncer); ok {
		_ = s.SyncRemote(g.ctx) // 状態ディレクトリにあって保管先に無い項目を写す(規則 23)。届かなければ次の実行へ送る
	}
	e1 := b.settlePrev(g.ctx, prev) // 中断の記録は取り消しの影響を受けない別の文脈で行う
	if g.ctx.Err() != nil {
		return b.abort(abortReason(g.ctx))
	}
	if e1 != nil {
		b.o.err = e1
		return b.finish()
	}
	if replay, e := b.completedBefore(g.ctx, p); e != nil || replay != nil {
		if e != nil {
			b.o.err = e
		} else {
			b.o = *replay
		}
		removeMarker(d.StateDir)
		return b.o
	}
	return b.execBody(g, call, e)
}

// guard は印の後の全区間の上限・SIGTERM・監視。
type runGuard struct {
	ctx      context.Context
	stop     func()
	finished chan struct{}
}

func (b *runBody) guard(ctx context.Context) *runGuard {
	dctx, dcancel := b.d.Lease.Deadline(ctx)
	rctx, rcancel := context.WithCancelCause(dctx)
	rctx = context.WithValue(rctx, extraKey{}, &b.extra)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	finished := make(chan struct{})
	go func() {
		select {
		case <-sig:
			rcancel(errSignal)
		case <-finished:
		}
	}()
	go b.watch(rctx, finished)
	return &runGuard{ctx: rctx, finished: finished, stop: func() {
		signal.Stop(sig)
		close(finished)
		rcancel(nil)
		dcancel()
	}}
}

// execBody は本体を実行し、終わり方を記録する。
func (b *runBody) execBody(g *runGuard, call contract.Call, e entry) outcome {
	rctx := g.ctx
	rep, err := safeEntry(e, b.d, rctx, call)
	if rctx.Err() != nil && err != nil { // 取り消しを受けた実行は自ら中止する(完了と競ったときは成功を中止に書き換えない)
		if rep.Phrase != nil {
			rep.Phrase.Wipe() // 中止の封筒に秘密は添えない
		}
		return b.abort(abortReason(rctx))
	}
	if !b.ended.CompareAndSwap(false, true) {
		select {} // 監視の goroutine が中止を記録して終了する
	}
	b.o = finish(b.o, replyOrErr{rep, err}, call.SecretOut)
	return b.finish()
}

// finish は規則 9 の順(記録 → 印の消去)を行い、封筒の元を返す。印の消去の後に呼び出し側が封筒を書く。
// 終了の項目を状態ディレクトリへ書けなかったときは印を消さない(次の実行が中断として記録する。保管先だけの失敗は規則 21 のとおり消す)。
func (b *runBody) finish() outcome {
	rc, cancel := context.WithTimeout(context.Background(), b.d.Limits.RecordDeadline)
	defer cancel()
	if b.recordEnd(rc) {
		b.recordLastRun(rc)
		removeMarker(b.d.StateDir)
	}
	return b.o
}

// lastRun は state.json の last_runs[] の要素(サブコマンドごとの最後の実行。spec 4.5)。
type lastRun struct {
	Command []string `json:"command"`
	RunID   string   `json:"run_id"`
	At      string   `json:"at"`
	OK      bool     `json:"ok"`
}

// recordLastRun は last_runs[] の同じサブコマンドの要素を今回の結果で置き換える(無ければ足す)。失敗しても実行は止めない。
func (b *runBody) recordLastRun(ctx context.Context) {
	d := b.d
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return
	}
	h, err := d.State.Lock(ctx)
	if err != nil {
		return
	}
	defer h.Release()
	var runs []lastRun
	if v, err := d.State.Get(ctx, "last_runs"); err == nil {
		_ = json.Unmarshal(v, &runs)
	}
	cur := lastRun{Command: b.command, RunID: b.runID, At: now.UTC().Format(time.RFC3339), OK: b.o.err == nil}
	replaced := false
	for i := range runs {
		if slices.Equal(runs[i].Command, b.command) {
			runs[i], replaced = cur, true
		}
	}
	if !replaced {
		runs = append(runs, cur)
	}
	nb, _ := json.Marshal(runs)
	_ = d.State.Update(ctx, h, "last_runs", nb)
}

// recordEnd は終了の項目 run_end を journal/ に記録する(保管先へ書けなくても状態ディレクトリには残る。規則 21)。
// 状態ディレクトリに残ったかを返す。
func (b *runBody) recordEnd(ctx context.Context) bool {
	env, _ := b.o.render()
	var code any
	if b.o.err != nil {
		code = string(b.o.err.Code)
	}
	body := map[string]any{"command": b.command, "ok": b.o.err == nil, "code": code, "envelope": json.RawMessage(env), "notifications": []map[string]any{}}
	if l, ok := b.d.Recorder.(notificationLister); ok {
		if n := l.Notifications(); n != nil {
			body["notifications"] = n
		}
	}
	if t := b.extra.get(); t != nil {
		body["targets"] = t
	}
	if b.d.recordItem(ctx, "run_end", b.runID, body) == nil {
		return true
	}
	return b.d.localHasEnd(b.runID) // 保管先への書き込みだけが失敗したなら状態ディレクトリには残っている
}

// localHasEnd は状態ディレクトリの journal/ に runID の終了の項目があるか。
func (d *Deps) localHasEnd(runID string) bool {
	items, err := d.journal(context.Background())
	if err != nil {
		return false
	}
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Kind == "run_end" && items[i].RunID == runID {
			return true
		}
	}
	return false
}

// recordItem は journal/ に 1 項目を追記する。at は Clock の時刻。失敗は呼び出し側へ返さない(中止の記録の途中で止めない)。
func (d *Deps) recordItem(ctx context.Context, kind, runID string, body any) error {
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return err
	}
	item, _ := json.Marshal(map[string]any{"at": now.Format(time.RFC3339), "kind": kind, "run_id": runID, "body": body})
	_, err = d.Recorder.Record(ctx, item)
	return err
}
