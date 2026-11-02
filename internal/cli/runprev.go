package cli

// 前の実行の印の処理、完了済みの判定、LOCKED の返し方(spec 3.1.1 の規則 7・8・15)。

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"slices"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/state"
)

func (d *Deps) outWriter() io.Writer {
	if d.out != nil {
		return d.out
	}
	return os.Stdout
}

func (d *Deps) exitFn() func(int) {
	if d.exit != nil {
		return d.exit
	}
	return os.Exit
}

// emit は通知イベントを発行する(配送は Notifier。facts は JSON にする)。
func (d *Deps) emit(ctx context.Context, typ string, facts any) error {
	b, _ := json.Marshal(facts)
	_, _, err := d.Recorder.Emit(ctx, typ, b)
	return err
}

func (d *Deps) journal(ctx context.Context) ([]state.JournalItem, error) {
	if r, ok := d.Recorder.(journalReader); ok {
		return r.Entries(ctx)
	}
	return nil, nil
}

type runEndBody struct {
	Command  []string        `json:"command"`
	OK       bool            `json:"ok"`
	Code     *string         `json:"code"`
	Envelope json.RawMessage `json:"envelope"`
}

func lockedEnd(it state.JournalItem) bool {
	var b runEndBody
	_ = json.Unmarshal(it.Body, &b)
	return b.Code != nil && *b.Code == string(contract.CodeLocked)
}

// settlePrev は前の印を処理する(規則 8): 同じ run_id の終了の項目(LOCKED で終わったものを除く)があれば印を消しただけ(自分の印が既に置き換えた)で
// 何も記録せず、無ければ中断として記録(run_interrupted と run.interrupted の unfinished)する。
// 片付け(RunCleanup)は中断の記録の有無によらずロックを取った実行の始めに毎回呼ぶ(規則 13、spec 7.5)。第 3 引数は読めた印の run_id、読めなければ空。
func (b *runBody) settlePrev(ctx context.Context, prev []byte) *contract.Error {
	d := b.d
	interrupted := ""
	if len(prev) > 0 {
		// 中断の記録は実行の文脈ではなく、記録の締め切りを持つ別の文脈で行う(規則 21)。
		rc, cancel := context.WithTimeout(context.Background(), d.Limits.RecordDeadline)
		defer cancel()
		m, st := parseMarker(prev)
		completed := false
		if st == markerOK {
			// journal/ が読めないときは完了済みと決められないので、中断として記録する(前の印を失わない)。
			if items, err := d.journal(rc); err == nil {
				completed = slices.ContainsFunc(items, func(it state.JournalItem) bool {
					return it.Kind == "run_end" && it.RunID == m.RunID && !lockedEnd(it)
				})
			}
		}
		if !completed {
			body := map[string]any{"interrupted_run_id": nil, "command": nil}
			if st == markerOK {
				body["interrupted_run_id"], body["command"] = m.RunID, m.Command
				interrupted = m.RunID
			}
			_ = d.recordItem(rc, "run_interrupted", b.runID, body)
			if st == markerOK {
				_ = d.emit(rc, "run.interrupted", map[string]any{"run_id": m.RunID, "command": m.Command, "reason": "unfinished"})
			}
		}
	}
	for _, c := range d.RunCleanup {
		_, _, _ = c.Cleanup(ctx, b.runID, interrupted) // 破棄を確かめられない資源は台帳に残り、次の verify が再試行する(規則 13)
	}
	return nil
}

// completedBefore は --run-id の完了済みの判定(規則 7)。完了済みなら前回の封筒を再生した outcome を返す。
// 同じ run_id の記録の command が今回と違えば ARG_INVALID(--run-id、reused)。
func (b *runBody) completedBefore(ctx context.Context, p *parsed) (*outcome, *contract.Error) {
	if p.runID == "" {
		return nil, nil
	}
	items, err := b.d.journal(ctx)
	if err != nil {
		return nil, toError(err)
	}
	var last *runEndBody
	for _, it := range items {
		if it.Kind != "run_end" || it.RunID != b.runID {
			continue
		}
		var e runEndBody
		if json.Unmarshal(it.Body, &e) == nil {
			last = &e
		}
	}
	if last == nil {
		return nil, nil
	}
	if !slices.Equal(last.Command, b.command) {
		return nil, argErr("--run-id", "reused")
	}
	if last.Code != nil && contract.CodeTable[contract.Code(*last.Code)].Retryable {
		return nil, nil // 再試行できる終わり方は完了済みに数えず、同じ run_id で再実行する
	}
	var env wireEnvelope
	if json.Unmarshal(last.Envelope, &env) != nil {
		return nil, internalErr()
	}
	o := b.o
	o.result, o.warnings, o.err = env.Result, env.Warnings, env.Error
	return &o, nil
}

// lockedOutcome はロックを取れなかった実行の終わり方: LOCKED、印を読んで run.stuck の判定(規則 15)、終了の項目 LOCKED の記録。印は消さない。
func (d *Deps) lockedOutcome(ctx context.Context, o outcome) outcome {
	m, st := readMarker(d.StateDir)
	detail := map[string]any{"lock": "run", "holder_command": nil, "holder_started_at": nil}
	if st == markerOK {
		detail["holder_command"], detail["holder_started_at"] = m.Command, m.StartedAt
	}
	if now, err := d.Clock.Now(ctx); err == nil && st != markerAbsent {
		dl, _ := time.Parse(time.RFC3339, m.DeadlineAt)
		if st == markerUnreadable || now.After(dl.Add(d.Limits.AbortGrace+d.Limits.RecordDeadline)) {
			d.emitStuck(ctx, m, st, now)
		}
	}
	o.err = newErr(contract.CodeLocked, detail)
	rc, cancel := context.WithTimeout(ctx, d.Limits.RecordDeadline)
	defer cancel()
	b := &runBody{d: d, o: o, command: o.command}
	if o.runID != nil {
		b.runID = *o.runID
	}
	b.recordEnd(rc)
	return o
}

func (d *Deps) emitStuck(ctx context.Context, m marker, st markerState, now time.Time) {
	facts := map[string]any{"holder_run_id": nil, "holder_command": nil, "holder_pid": nil, "elapsed_s": nil}
	if st == markerOK {
		facts["holder_run_id"], facts["holder_command"], facts["holder_pid"] = m.RunID, m.Command, m.Pid
		if t, err := time.Parse(time.RFC3339, m.StartedAt); err == nil {
			facts["elapsed_s"] = int64(now.Sub(t).Seconds())
		}
	}
	_ = d.emit(ctx, "run.stuck", facts)
}
