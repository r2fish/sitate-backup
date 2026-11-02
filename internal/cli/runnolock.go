package cli

// ロックを要しない操作の上限と中止(spec 3.1.1 の規則 10・12・17): 締め切り NOLOCK_OP_DEADLINE を持つ操作はそれ(RUN_ABORTED の reason: deadline)、
// それ以外は実行の上限(max_duration)と SIGTERM(signal)で文脈を取り消し、取り消しを受けた操作は自ら RUN_ABORTED で中止する。
// 猶予の後も終わらなければ中止を書いて自プロセスを終了する。印も journal/ の項目も書かない(ロックを持たない)。

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var errMaxDuration = errors.New("max duration")

func (d *Deps) nolockLimit(key string) (time.Duration, error) {
	if noLockDeadline[key] {
		if d.Limits.NolockDeadline > 0 {
			return d.Limits.NolockDeadline, errNolockDeadline
		}
		return contract.NolockOpDeadline, errNolockDeadline
	}
	if d.Limits.MaxDuration > 0 {
		return d.Limits.MaxDuration, errMaxDuration
	}
	return contract.RunMaxDuration, errMaxDuration
}

func (d *Deps) runUnlocked(ctx context.Context, o outcome, e entry, call contract.Call, key string) outcome {
	limit, cause := d.nolockLimit(key)
	rctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	timer := time.AfterFunc(limit, func() { cancel(cause) })
	defer timer.Stop()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	defer signal.Stop(sig)
	finished := make(chan struct{})
	defer close(finished)
	var ended atomic.Bool
	go func() {
		select {
		case <-sig:
			cancel(errSignal)
		case <-finished:
			return
		case <-rctx.Done():
		}
		select {
		case <-finished:
			return
		case <-time.After(d.Limits.AbortGrace):
		}
		if !ended.CompareAndSwap(false, true) { // 本体が先に終わり方を決めた
			return
		}
		ao := d.abortUnlocked(o, abortReason(rctx))
		b, code := ao.render()
		writeLine(d.outWriter(), b)
		d.exitFn()(code)
	}()
	rep, err := safeEntry(e, d, withCall(rctx, call), call)
	if rctx.Err() != nil && err != nil {
		if !ended.CompareAndSwap(false, true) {
			select {} // 監視の goroutine が終了させる
		}
		return d.abortUnlocked(o, abortReason(rctx))
	}
	ended.Store(true)
	return finish(o, replyOrErr{rep, err}, call.SecretOut)
}

// abortUnlocked はロックを要しない操作の中止: RUN_ABORTED と run.interrupted(記録の締め切りの文脈。通知は締め切りの半分まで)。
func (d *Deps) abortUnlocked(o outcome, reason string) outcome {
	o.result = nil
	o.err = newErr(contract.CodeRunAborted, map[string]any{"reason": reason})
	if d.Recorder != nil && d.Clock != nil {
		rc, cancel := context.WithTimeout(context.Background(), d.Limits.RecordDeadline)
		defer cancel()
		var rid any
		if o.runID != nil {
			rid = *o.runID
		}
		_ = d.emit(rc, "run.interrupted", map[string]any{"run_id": rid, "command": o.command, "reason": reason})
	}
	return o
}
