package approval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func lastState(t *testing.T, e *env, id string) string {
	t.Helper()
	l := pendingChanges(t, e.a, map[string][]string{"id": {id}})
	if len(l) != 1 {
		t.Fatalf("pending --id %s: %+v", id, l)
	}
	return l[0].State
}

// 遅延の前の適用: 遅延のある変更は not_before の前に applying にならない。
func TestApplyBeforeDelay(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("delay-change", map[string][]string{"hours": {"24"}})
	e.approveOK(p)
	e.clock.Advance(hours(23))
	plan, err := e.apply(nil)
	if err != nil || len(plan.Changes) != 0 || lastState(t, e, p.ChangeID) != "approved" {
		t.Fatalf("遅延の前に適用された: %+v %v", plan, err)
	}
	e.clock.Advance(hours(1))
	plan, err = e.apply(nil)
	if err != nil || len(plan.Changes) != 1 || plan.Changes[0].Steps[0].Kind != "define" || lastState(t, e, p.ChangeID) != "applying" {
		t.Fatalf("遅延の後: %+v %v", plan, err)
	}
	if err := e.a.ApplyRecord(ctx, p.ChangeID, 0, contract.StepResult{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if lastState(t, e, p.ChangeID) != "applied" {
		t.Error("applied にならない")
	}
}

// 遅延中の通知が配送されなければ (3) を満たさず適用しない。24 時間ずつ延びて 7 日で失効。
func TestApplyUndeliveredExtendsThenExpires(t *testing.T) {
	e := newEnv(t, false)
	p := e.propose("delay-change", map[string][]string{"hours": {"24"}})
	e.approveOK(p)
	for i := 0; i < 30; i++ {
		e.clock.Advance(hours(24))
		plan, err := e.apply(nil)
		if err != nil || len(plan.Changes) != 0 {
			t.Fatalf("配送されないのに適用: %+v %v", plan, err)
		}
		if lastState(t, e, p.ChangeID) == "expired" {
			return
		}
	}
	t.Error("expired にならない")
}

func TestApplyRechecksSignatureAndPrecondition(t *testing.T) {
	// 承認の記録の書き換え(別の hash)
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	raw, _, _ := e.a.store().b.read(approvalName(p.ChangeID))
	_ = e.a.store().b.write(approvalName(p.ChangeID), []byte(strings.Replace(string(raw), "sha256:", "sha256:0", 1)))
	plan, err := e.apply(nil)
	if err != nil || len(plan.Changes) != 0 || len(plan.Failed) != 1 || lastState(t, e, p.ChangeID) != "failed" {
		t.Fatalf("書き換えられた承認が適用された: %+v %v", plan, err)
	}
	if !strings.Contains(string(plan.Failed[0]), "SIGNATURE_INVALID") {
		t.Errorf("code: %s", plan.Failed[0])
	}

	// 変更の記録の書き換え
	e = newEnv(t, true)
	p = e.propose("restore-test-increase", incr)
	e.approveOK(p)
	raw, _, _ = e.a.store().b.read(changeName(p.ChangeID))
	_ = e.a.store().b.write(changeName(p.ChangeID), []byte(strings.Replace(string(raw), "weekly", "monthly", 1)))
	plan, _ = e.apply(nil)
	if len(plan.Changes) != 0 || len(plan.Failed) != 1 || !strings.Contains(string(plan.Failed[0]), "SIGNATURE_INVALID") {
		t.Fatalf("書き換えられた記録が適用された: %+v", plan)
	}

	// signed_at と received_at の差(承認の記録の received_at を後ろへずらす)
	e = newEnv(t, true)
	p = e.propose("restore-test-increase", incr)
	e.approveOK(p)
	ap, _, _ := e.a.store().readApproval(p.ChangeID)
	ap.Approvers[0].ReceivedAt = fmtTime(e.clock.T.Add(hours(1)))
	b, _ := marshalCanonical(ap)
	_ = e.a.store().b.write(approvalName(p.ChangeID), b)
	e.clock.Advance(hours(2))
	plan, _ = e.apply(nil)
	if len(plan.Changes) != 0 || len(plan.Failed) != 1 {
		t.Fatalf("時刻の照合の違反が適用された: %+v", plan)
	}

	// 前提の変更(定義が変わった)
	e = newEnv(t, true)
	p = e.propose("restore-test-increase", incr)
	e.approveOK(p)
	e.st.Items["repo_id"] = json.RawMessage(`"rp-other"`)
	plan, _ = e.apply(nil)
	if len(plan.Changes) != 0 || len(plan.Failed) != 1 || !strings.Contains(string(plan.Failed[0]), "PRECONDITION_CHANGED") {
		t.Fatalf("前提が変わったのに適用: %+v", plan)
	}
}

// 未承認・取り消し済みの変更は適用の計画に出ない。
func TestApplyOnlyApproved(t *testing.T) {
	e := newEnv(t, true)
	e.propose("restore-test-increase", incr) // 未承認
	q := e.propose("key-rotate-waive", nil)
	e.approveOK(q)
	_, _ = e.a.Cancel(ctx, call("cancel", nil, map[string][]string{"id": {q.ChangeID}}))
	plan, err := e.apply(nil)
	if err != nil || len(plan.Changes) != 0 {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestApplyCancelAfterApplyingRejected(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	if plan, _ := e.apply(nil); len(plan.Changes) != 1 {
		t.Fatalf("%+v", plan)
	}
	_, err := e.a.Cancel(ctx, call("cancel", nil, map[string][]string{"id": {p.ChangeID}}))
	want(t, "applying の後の cancel", err, contract.CodeChangeStateInvalid, "")
}

// applying は同時に 1 つ。中断した define だけの変更は define から再開し、ほかの変更は waiting に返る。
func TestApplyResumeAndSingleApplying(t *testing.T) {
	e := newEnv(t, true)
	p1 := e.propose("restore-test-increase", incr)
	e.approveOK(p1)
	if plan, _ := e.apply(nil); len(plan.Changes) != 1 {
		t.Fatal("開始できない")
	}
	p2 := e.propose("key-rotate-waive", nil)
	e.approveOK(p2)
	plan, err := e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {p2.ChangeID}}}, "i-2")
	if err != nil || len(plan.Changes) != 1 || plan.Changes[0].ChangeID != p1.ChangeID || plan.Changes[0].Steps[0].Kind != "define" {
		t.Fatalf("再開: %+v %v", plan, err)
	}
	if len(plan.Waiting) != 1 || !strings.Contains(string(plan.Waiting[0]), "applying_in_progress") || lastState(t, e, p2.ChangeID) != "approved" {
		t.Fatalf("waiting: %+v", plan.Waiting)
	}
	e2 := newEnv(t, true)
	q := e2.propose("restore-elsewhere", map[string][]string{"to": {"ssh://x"}})
	e2.approveOK(q)
	if plan, _ := e2.apply(map[string][]string{"id": {q.ChangeID}, "host-ref": {"h"}}); len(plan.Changes) != 1 {
		t.Fatalf("%+v", plan)
	}
	plan, _ = e2.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}}, "i-9")
	if len(plan.Failed) != 1 || !strings.Contains(string(plan.Failed[0]), "RUN_INTERRUPTED") {
		t.Fatalf("RUN_INTERRUPTED: %+v", plan)
	}
}

// 秘密が要る変更は秘密が無ければ waiting のまま、applying を書かない。--id 無しで秘密を渡すと ARG_INVALID。
func TestApplySecrets(t *testing.T) {
	e := newEnv(t, true)
	q := e.propose("storage-init", map[string][]string{"bucket": {"b"}})
	e.approveOK(q)
	plan, err := e.apply(nil)
	if err != nil || len(plan.Changes) != 0 || len(plan.Waiting) != 1 || lastState(t, e, q.ChangeID) != "approved" {
		t.Fatalf("%+v %v", plan, err)
	}
	_, err = e.apply(map[string][]string{"admin-ref": {"x"}})
	want(t, "--id 無し", err, contract.CodeArgInvalid, "missing")
}

func TestApplyRecordFailureRules(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	_, _ = e.apply(nil)
	for _, code := range []contract.Code{contract.CodeRunAborted, contract.CodeDefFileWriteFailed} {
		if err := e.a.ApplyRecord(ctx, p.ChangeID, 0, contract.StepResult{Err: &contract.Error{Code: code}}); err != nil || lastState(t, e, p.ChangeID) != "applying" {
			t.Fatalf("%s は applying のまま: %v", code, err)
		}
	}
	if err := e.a.ApplyRecord(ctx, p.ChangeID, 0, contract.StepResult{Err: &contract.Error{Code: contract.CodeInternal}}); err != nil || lastState(t, e, p.ChangeID) != "failed" {
		t.Fatalf("置き換えの前の失敗は failed: %v", err)
	}
}

// 状態の遷移の違反: 追記は遷移の規則に反すると拒否される。
func TestTransitionRules(t *testing.T) {
	for _, c := range []struct{ from, to string }{
		{"", "applying"}, {"proposed", "applying"}, {"proposed", "applied"}, {"approved", "applied"}, {"cancelled", "applying"},
		{"cancelled", "applied"}, {"applied", "applying"}, {"failed", "applying"}, {"expired", "approved"}, {"applying", "cancelled"},
	} {
		if transitionOK(c.from, c.to) {
			t.Errorf("%s → %s が通る", c.from, c.to)
		}
	}
	for _, c := range []struct{ from, to string }{{"", "proposed"}, {"proposed", "approved"}, {"approved", "applying"}, {"applying", "applied"}, {"approved", "cancelled"}} {
		if !transitionOK(c.from, c.to) {
			t.Errorf("%s → %s が通らない", c.from, c.to)
		}
	}
}

// 連鎖の切れ: log の書き換えを検知して propose・approve を止め、apply が退避して空から始める。
func TestChainBroken(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	b, _, _ := e.a.store().b.read(logName)
	forged := strings.Replace(string(b), `"prev_hash":null`, `"prev_hash":"00"`, 1)
	_ = e.a.store().b.write(logName, []byte(forged))
	_, err := e.a.Propose(ctx, call("propose", []string{"restore-test-increase"}, incr))
	want(t, "propose", err, contract.CodePendingChainBroken, "")
	at := fmtTime(e.clock.T)
	_, err = e.approveCall(p.ChangeID, sign(e.priv, p.Hash, at), at)
	want(t, "approve", err, contract.CodePendingChainBroken, "")
	_, err = e.apply(nil)
	want(t, "apply", err, contract.CodePendingChainBroken, "")
	if _, err := e.a.Propose(ctx, call("propose", []string{"restore-test-increase"}, incr)); err != nil {
		t.Errorf("回復の後の propose: %v", err)
	}
}

// 追記そのものが遷移の規則を守る(取り消しと適用の開始の競合を、排他の中の追記が防ぐ)。
func TestAppendEventRejectsBadTransitions(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	s := e.a.store()
	if _, err := s.appendEvent(logEvent{Event: evApplying, ChangeID: p.ChangeID}); codeOf(err) != contract.CodeChangeStateInvalid {
		t.Errorf("proposed → applying: %v", err)
	}
	if _, err := e.a.Cancel(ctx, call("cancel", nil, map[string][]string{"id": {p.ChangeID}})); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{evApproved, evApplying, evApplied} {
		if _, err := s.appendEvent(logEvent{Event: ev, ChangeID: p.ChangeID}); codeOf(err) != contract.CodeChangeStateInvalid {
			t.Errorf("cancelled → %s: %v", ev, err)
		}
	}
}
