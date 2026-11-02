package approval

// apply の適用の計画(spec 3.1.2.11、3.6 の apply の行)。approval は登録以外の操作を実行せず、実行すべき操作の列を返す(実行は cli)。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type applyRun struct {
	a        *Approval
	ctx      context.Context
	s        *pstore
	c        contract.Call
	instance string
	now      time.Time
	plan     contract.ApplyPlan
	order    []string
	states   map[string]*chgState
	ds       []delayRec
	onlyID   string
	unsealed bool // seal の前の危険な変更を見送った(NOT_SEALED)
}

func (r *applyRun) reload() error {
	evs, _, ce, err := r.s.events()
	if err != nil || ce != nil {
		return chainOrErr(ce, err)
	}
	r.order, r.states = replay(evs)
	r.ds, err = r.a.readDelays(r.ctx)
	return err
}

// ApplyPlan は contract.Approval を満たす。実行の始めの順(3.1.2.11): 連鎖の確認 → 中断した変更の再開 → 失効と遅延中の通知 → 期限の来た変更の開始前の照合と applying の追記。
func (a *Approval) ApplyPlan(ctx context.Context, c contract.Call, instance string) (contract.ApplyPlan, error) {
	empty := contract.ApplyPlan{Instance: instance, Changes: []contract.ChangeOps{}, Waiting: []json.RawMessage{}, Failed: []json.RawMessage{}}
	if err := a.ready(); err != nil {
		return empty, err
	}
	if instance == "" {
		return empty, argErr("--run-id", "missing")
	}
	if requiresSecretArgs(c) && one(c, "id") == "" {
		return empty, argErr("--id", "missing")
	}
	s := a.store()
	if _, _, ce, err := s.events(); err != nil || ce != nil {
		if ce != nil && c.DryRun {
			return empty, ce.toError(nil)
		}
		if ce != nil {
			return empty, a.recoverChain(ctx, s, ce)
		}
		return empty, err
	}
	now, err := a.now(ctx)
	if err != nil {
		return empty, err
	}
	a.mu.Lock()
	a.cur, a.steps = c, map[string][]contract.OpStep{}
	a.mu.Unlock()
	r := &applyRun{a: a, ctx: ctx, s: s, c: c, instance: instance, now: now, plan: empty, onlyID: one(c, "id")}
	phases := []func() error{r.reload, r.resumeInterrupted, r.expireAndRemind, r.checkOnlyID, r.beginDue}
	if c.DryRun { // 追記も通知もしない
		phases = []func() error{r.reload, r.checkOnlyID, r.beginDue}
	}
	for _, phase := range phases {
		if err := phase(); err != nil {
			return r.plan, err
		}
	}
	if r.unsealed && len(r.plan.Changes) == 0 {
		return r.plan, apiErr(contract.CodeNotSealed, nil)
	}
	return r.plan, nil
}

func (r *applyRun) setSteps(id string, steps []contract.OpStep) {
	r.a.mu.Lock()
	r.a.steps[id] = steps
	r.a.mu.Unlock()
}

func (r *applyRun) wait(id, reason string) {
	r.plan.Waiting = append(r.plan.Waiting, mustJSON(map[string]string{"change_id": id, "reason": reason}))
}

// fail は変更を failed にして change.failed を送り、failed[] に返す(4.6 の failed の事象)。
func (r *applyRun) fail(rec changeRec, code contract.Code) error {
	if err := r.a.appendFailed(r.ctx, r.s, rec, string(code)); err != nil {
		return err
	}
	r.plan.Failed = append(r.plan.Failed, mustJSON(map[string]string{"change_id": rec.ChangeID, "op": rec.Op, "code": string(code)}))
	return r.reload()
}

// appendFailed は state.lock の下で failed を追記し、delays[] の要素を消して change.failed を送る。
func (a *Approval) appendFailed(ctx context.Context, s *pstore, rec changeRec, code string) error {
	h, err := a.State.Lock(ctx)
	if err != nil {
		return err
	}
	ev, err := s.appendEvent(logEvent{Event: evFailed, ChangeID: rec.ChangeID, Code: code})
	if err == nil {
		err = a.dropDelay(ctx, h, rec.ChangeID)
	}
	if err == nil {
		err = a.record(ctx, h, ev, nil)
	}
	h.Release()
	if err == nil {
		a.emit(ctx, "change.failed", map[string]any{"change_id": rec.ChangeID, "op": rec.Op, "error_code": code, "state": evFailed})
	}
	return err
}
