package cli

// apply の入口(spec 3.1.2.11)。approval が返す操作の列を順に実行し、各要素の後に結果を approval に記録させる。
// approval は登録(enroll)以外の操作を実行しない。define(承認済みの定義の切り替え)と状態だけを書く run は cli が StateFiles を呼んで行う
// (applydefine.go、applyrun.go)。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() { routeOwn["apply"] = applyEntry }

// 操作の列の要素の種類(approval/steps.go と同じ。契約の OpStep.Kind)。
const (
	stepKindEnroll = "enroll"
	stepKindDefine = "define"
	stepKindRun    = "run"
)

// storeClock は apply が判定に使う時計(spec 3.6 の「時計」): 保管先の応答の Date に、試験ビルドの模擬時間のオフセットを足した値。
// 時計のずれ(TIME_SKEW)は ObjectStore.CheckSkew が見る。結線は approval の Clock にこれを渡す。
type storeClock struct {
	d *Deps
}

// NewApplyClock は apply が使う時計を返す(Deps.Object が保管先)。Object が無ければ NOT_IMPLEMENTED。
func NewApplyClock(d *Deps) contract.Clock { return storeClock{d: d} }

func (c storeClock) Now(ctx context.Context) (time.Time, error) {
	if c.d.Object == nil {
		return time.Time{}, contract.ErrNotImplemented("cli.apply")
	}
	if err := c.d.Object.CheckSkew(ctx); err != nil {
		return time.Time{}, err
	}
	t, err := c.d.Object.StoreTime(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if o, ok := c.d.Clock.(contract.Offsetter); ok {
		off, oerr := o.Offset()
		if oerr != nil {
			return time.Time{}, oerr
		}
		t = t.Add(off)
	}
	return t.UTC().Truncate(time.Second), nil
}

type applyOut struct {
	Applied []map[string]any  `json:"applied"`
	Failed  []json.RawMessage `json:"failed"`
	Waiting []json.RawMessage `json:"waiting"`
}

func applyEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if d.Approval == nil {
		return notImpl("apply")
	}
	plan, err := d.Approval.ApplyPlan(ctx, c, processInstance())
	if err != nil {
		return contract.Reply{}, err
	}
	out := applyOut{Applied: []map[string]any{}, Failed: nonNil(plan.Failed), Waiting: nonNil(plan.Waiting)}
	if !c.DryRun {
		for _, ch := range plan.Changes {
			if err := d.applyChange(ctx, c, ch, &out); err != nil {
				return contract.Reply{}, err
			}
		}
	}
	b, _ := json.Marshal(out)
	return contract.Reply{Result: b}, nil
}

func nonNil(l []json.RawMessage) []json.RawMessage {
	if l == nil {
		return []json.RawMessage{}
	}
	return l
}

// applyChange は変更 1 件の操作の列を順に実行する。返す誤りは apply 全体を終える誤り(RUN_ABORTED、approval の記録の失敗)で、
// 変更の失敗(failed への記録)は out.Failed に足して nil を返す。
func (d *Deps) applyChange(ctx context.Context, c contract.Call, ch contract.ChangeOps, out *applyOut) error {
	var enroll *contract.EnrollResult
	var last json.RawMessage
	for pos, st := range ch.Steps {
		sr, err := d.execStep(ctx, c, ch, pos, st, enroll)
		if err != nil {
			return err
		}
		if st.Kind == stepKindEnroll && sr.Err == nil {
			var er contract.EnrollResult
			if json.Unmarshal(sr.Result, &er) == nil {
				enroll = &er
			}
		}
		if err := d.Approval.ApplyRecord(ctx, ch.ChangeID, pos, sr); err != nil {
			return err
		}
		if sr.Err != nil {
			if sr.Err.Code == contract.CodeRunAborted {
				return sr.Err
			}
			if !keptApplying(ch, st, sr.Err.Code) {
				out.Failed = append(out.Failed, mustMarshal(map[string]string{"change_id": ch.ChangeID, "op": ch.Op, "code": string(sr.Err.Code)}))
			}
			return nil
		}
		last = sr.Result
	}
	if len(last) == 0 {
		last = json.RawMessage(`{}`)
	}
	out.Applied = append(out.Applied, map[string]any{"change_id": ch.ChangeID, "op": ch.Op, "state": "applied", "result": last})
	return nil
}

// keptApplying は失敗しても変更が applying のまま残る場合(approval の failOrKeep と同じ規則。spec 3.1.2.11 の流れの 6):
// define だけの列で定義ファイルの書き戻しに失敗したとき。
func keptApplying(ch contract.ChangeOps, st contract.OpStep, code contract.Code) bool {
	return code == contract.CodeDefFileWriteFailed && st.Kind == stepKindDefine && len(ch.Steps) == 1
}

// execStep は要素 1 つを実行する。取り消された文脈は RUN_ABORTED の StepResult にする。実行できなかった理由は StepResult.Err に入れ、
// 第 2 の戻り値は approval の呼び出しの失敗(記録できなかった)だけ。
func (d *Deps) execStep(ctx context.Context, c contract.Call, ch contract.ChangeOps, pos int, st contract.OpStep, enroll *contract.EnrollResult) (contract.StepResult, error) {
	if ctx.Err() != nil {
		return contract.StepResult{Err: newErr(contract.CodeRunAborted, map[string]any{"reason": abortReason(ctx)})}, nil
	}
	switch st.Kind {
	case stepKindEnroll:
		return d.Approval.ApplyEnroll(ctx, ch.ChangeID, pos)
	case stepKindDefine:
		return stepOf(ctx)(d.defineStep(ctx, ch, st, enroll)), nil
	case stepKindRun:
		return stepOf(ctx)(d.runStep(ctx, c, ch, st)), nil
	}
	return contract.StepResult{Err: internalErr()}, nil
}

// stepOf は結果か誤りを StepResult にする。取り消しによる失敗は RUN_ABORTED にそろえる。
func stepOf(ctx context.Context) func(json.RawMessage, error) contract.StepResult {
	return func(res json.RawMessage, err error) contract.StepResult {
		if err != nil && ctx.Err() != nil {
			return contract.StepResult{Err: newErr(contract.CodeRunAborted, map[string]any{"reason": abortReason(ctx)})}
		}
		if err != nil {
			return contract.StepResult{Err: toError(err)}
		}
		return contract.StepResult{Result: res}
	}
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// processInstance は実行の個体の識別子 <boot_id>:<pid>:<pid_start_ticks>(spec 4.5「実行中の印と instance」)。--run-id ではない:
// run_id が同じ再実行が、前の個体の applying を自分のものと見なして再開しないのを防ぐ。
func processInstance() string {
	pid, ticks, boot := selfProc()
	return fmt.Sprintf("%s:%d:%d", boot, pid, ticks)
}
