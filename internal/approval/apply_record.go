package approval

// 適用の操作の列の要素の実行(登録だけ)と、各要素の結果の記録(spec 3.1.2.11、7.5 の approval の apply の入口)。

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// applyingChange は applying の変更の記録と、その操作の列。
func (a *Approval) applyingChange(changeID string) (changeRec, []contract.OpStep, *pstore, error) {
	s := a.store()
	evs, _, ce, err := s.events()
	if err != nil || ce != nil {
		return changeRec{}, nil, s, chainOrErr(ce, err)
	}
	_, states := replay(evs)
	st := states[changeID]
	if st == nil {
		return changeRec{}, nil, s, stateErr(changeID, nil)
	}
	if st.Last != evApplying {
		return changeRec{}, nil, s, stateErr(changeID, &st.Last)
	}
	rec, _, err := s.readChange(changeID)
	if err != nil {
		return rec, nil, s, err
	}
	a.mu.Lock()
	steps, ok := a.steps[changeID]
	a.mu.Unlock()
	if !ok {
		steps = fullSteps(rec)
	}
	return rec, steps, s, nil
}

// ApplyEnroll は contract.Approval を満たす。kind: enroll の要素を Enroller で実行する。
// 合格しなければ変更は failed として記録し(定義は変えない)、失敗は StepResult.Err に入れて返す。
func (a *Approval) ApplyEnroll(ctx context.Context, changeID string, pos int) (contract.StepResult, error) {
	if err := a.ready(); err != nil {
		return contract.StepResult{}, err
	}
	if a.Enroller == nil {
		return contract.StepResult{}, internalErr()
	}
	rec, steps, s, err := a.applyingChange(changeID)
	if err != nil {
		return contract.StepResult{}, err
	}
	if pos < 0 || pos >= len(steps) || steps[pos].Kind != stepEnroll {
		return contract.StepResult{}, argErr("--pos", "format")
	}
	in, err := a.enrollInput(rec, steps[pos])
	a.mu.Lock()
	a.cur = contract.Call{} // 管理用資格情報は登録の入力に写した後、保持しない
	a.mu.Unlock()
	if err != nil {
		return contract.StepResult{}, err
	}
	res, err := a.Enroller.Enroll(ctx, in)
	if err != nil {
		var ce *contract.Error
		if !errors.As(err, &ce) {
			return contract.StepResult{}, err
		}
		if ferr := a.failOrKeep(ctx, s, rec, steps[pos], ce.Code); ferr != nil {
			return contract.StepResult{}, ferr
		}
		return contract.StepResult{Err: ce}, nil
	}
	b, err := json.Marshal(res)
	return contract.StepResult{Result: b}, err
}

func (a *Approval) enrollInput(rec changeRec, st contract.OpStep) (contract.EnrollInput, error) {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(rec.Args, &args); err != nil {
		return contract.EnrollInput{}, internalErr()
	}
	in := contract.EnrollInput{Kind: "restore-dest", WriteState: false}
	key := "restore"
	if st.Entry == "source enroll" {
		in.Kind, key = "source", "source"
	}
	in.Section = args[key]
	var sec struct {
		HostKeyFP string `json:"host_key_fp"`
		Host      string `json:"host"`
		Dest      struct {
			KeyRef string `json:"key_ref"`
		} `json:"dest"`
	}
	_ = json.Unmarshal(in.Section, &sec)
	in.HostKeyFP, in.Host = sec.HostKeyFP, sec.Host
	if in.Kind == "source" && in.HostKeyFP == "" { // カードで照らした指紋が無いまま登録しない
		return in, hostkeyErr("", "SHA256:unset")
	}
	a.mu.Lock()
	c := a.cur
	a.mu.Unlock()
	ref := sec.Dest.KeyRef
	if in.Kind == "source" {
		ref = one(c, "source-admin-ref")
	}
	in.AdminRef = ref
	if v, ok := c.Secrets[ref]; ok && ref != "" {
		in.Admin = contract.NewSecret(v)
	}
	return in, nil
}

// failOrKeep は失敗の扱い: RUN_ABORTED は常に applying のまま、DEF_FILE_WRITE_FAILED は define の失敗なら applying のまま(change.failed を warn で送る)、
// ほかは failed(そのときの原因コード)にする。
func (a *Approval) failOrKeep(ctx context.Context, s *pstore, rec changeRec, st contract.OpStep, code contract.Code) error {
	keep := code == contract.CodeRunAborted || code == contract.CodeDefFileWriteFailed && st.Kind == stepDefine && isDefineOnly(rec)
	if !keep {
		return a.appendFailed(ctx, s, rec, string(code))
	}
	if code == contract.CodeDefFileWriteFailed {
		a.emit(ctx, "change.failed", map[string]any{"change_id": rec.ChangeID, "op": rec.Op, "error_code": string(code), "state": evApplying})
	}
	return nil
}

// ApplyRecord は contract.Approval を満たす。各要素の後に cli が呼び、最後の要素の後に applied を、失敗で failed を追記する。
func (a *Approval) ApplyRecord(ctx context.Context, changeID string, pos int, r contract.StepResult) error {
	if err := a.ready(); err != nil {
		return err
	}
	rec, steps, s, err := a.applyingChange(changeID)
	if err != nil {
		var ce *contract.Error
		if errors.As(err, &ce) && ce.Code == contract.CodeChangeStateInvalid && r.Err != nil && a.alreadyFailed(changeID) {
			return nil // ApplyEnroll が failed を記録済み
		}
		return err
	}
	if pos < 0 || pos >= len(steps) {
		return argErr("--pos", "format")
	}
	if r.Err != nil {
		return a.failOrKeep(ctx, s, rec, steps[pos], r.Err.Code)
	}
	if pos != len(steps)-1 {
		return nil
	}
	return a.appendApplied(ctx, s, rec, r.Result)
}

func (a *Approval) alreadyFailed(changeID string) bool {
	evs, _, ce, err := a.store().events()
	if err != nil || ce != nil {
		return false
	}
	_, states := replay(evs)
	return states[changeID] != nil && states[changeID].Last == evFailed
}

func (a *Approval) appendApplied(ctx context.Context, s *pstore, rec changeRec, result json.RawMessage) error {
	if len(result) == 0 || !json.Valid(result) {
		result = json.RawMessage(`null`)
	}
	h, err := a.State.Lock(ctx)
	if err != nil {
		return err
	}
	ev, err := s.appendEvent(logEvent{Event: evApplied, ChangeID: rec.ChangeID, Result: result})
	if err == nil {
		err = a.dropDelay(ctx, h, rec.ChangeID)
	}
	if err == nil {
		err = a.record(ctx, h, ev, nil)
	}
	h.Release()
	if err == nil {
		a.emit(ctx, "change.applied", map[string]any{"change_id": rec.ChangeID, "op": rec.Op, "not_before": nil, "expired_reason": nil})
	}
	return err
}
