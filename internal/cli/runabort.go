package cli

// 実行の中止(spec 3.1.1 の規則 10・11・12・21): 上限と SIGTERM は文脈の取り消し、猶予の後も終わらなければ監視の goroutine が中止を記録して自プロセスを終了する。

import (
	"context"
	"errors"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var errSignal = errors.New("signal")

// errNolockDeadline は NOLOCK_OP_DEADLINE を持つ操作の締め切りによる取り消しの原因(RUN_ABORTED の reason: deadline)。
var errNolockDeadline = errors.New("nolock deadline")

// errStdinClosed は --snapshot-key-stdin の標準入力が終わった(SSH の切断。spec 3.5 の表の 8)ことによる取り消しの原因。
var errStdinClosed = errors.New("stdin closed")

// abortReason は RUN_ABORTED の detail.reason(max_duration / signal)。
func abortReason(ctx context.Context) string {
	if c := context.Cause(ctx); errors.Is(c, errNolockDeadline) {
		return "deadline"
	} else if errors.Is(c, errSignal) || errors.Is(c, errStdinClosed) { // 切断は SIGHUP 相当として signal に数える
		return "signal"
	}
	return "max_duration"
}

// watch は取り消しの後も本体が終わらないときに、猶予の後で中止を記録して自プロセスを終了する。
// 猶予の基準は上限なら開始 + 上限、SIGTERM なら受けた時刻。単調時計で測るので、SIGSTOP で止めた実行を再開すると直ちに超過を検知する。
func (b *runBody) watch(rctx context.Context, finished <-chan struct{}) {
	select {
	case <-finished:
		return
	case <-rctx.Done():
	}
	reason := abortReason(rctx)
	at := b.start.Add(b.d.Limits.MaxDuration + b.d.Limits.AbortGrace)
	if reason == "signal" {
		at = time.Now().Add(b.d.Limits.AbortGrace)
	}
	if wait := time.Until(at); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-finished:
			return
		case <-t.C:
		}
	}
	if !b.ended.CompareAndSwap(false, true) {
		return
	}
	o := b.recordAbort(reason)
	bts, code := o.render()
	writeLine(b.d.outWriter(), bts)
	b.d.exitFn()(code)
}

// abort は本体が自ら中止したときの終わり方。監視の goroutine が先に中止していれば、そちらがプロセスを終えるのを待つ。
func (b *runBody) abort(reason string) outcome {
	if !b.ended.CompareAndSwap(false, true) {
		select {}
	}
	return b.recordAbort(reason)
}

// recordAbort は中止の順(読み出しの自己計上 → 終了の項目 → 印の消去。規則 9)で記録する。封筒を書くのは呼び出し側。
func (b *runBody) recordAbort(reason string) outcome {
	d := b.d
	rc, cancel := context.WithTimeout(context.Background(), d.Limits.RecordDeadline)
	defer cancel()
	b.o.result = nil
	b.o.err = newErr(contract.CodeRunAborted, map[string]any{"reason": reason})
	if d.State != nil {
		_ = d.State.SettleInterrupted(rc, b.runID) // 読み出しの精算はイテレーション 3(未実装なら何もしない)
	}
	facts := map[string]any{"run_id": b.runID, "command": b.command, "reason": reason}
	// 通知の配送は締め切りの半分までにして、終了の項目の記録の時間を残す(宛先が応答しなくても run_end を書く。規則 21)。
	nc, ncancel := context.WithTimeout(rc, d.Limits.RecordDeadline/2)
	_ = d.emit(nc, "run.interrupted", facts)
	ncancel()
	if b.recordEnd(rc) {
		b.recordLastRun(rc)
		removeMarker(d.StateDir)
	}
	return b.o
}
