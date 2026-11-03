package approval

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 連鎖の切れの回復: pending_discarded の形、捨てた変更の delays[] の消去、通知は state.lock を放してから送る。
func TestRecoveryShapeAndLocking(t *testing.T) {
	e := newEnv(t, true)
	p1 := e.propose("restore-test-increase", incr)
	e.approveOK(p1)
	if plan, _ := e.apply(nil); len(plan.Changes) != 1 { // p1 は applying
		t.Fatal("開始できない")
	}
	p2 := e.propose("delay-change", map[string][]string{"hours": {"30"}})
	e.approveOK(p2)
	b, _, _ := e.a.store().b.read(logName)
	forged := strings.Replace(string(b), `"prev_hash":null`, `"prev_hash":"00"`, 1)
	_ = e.a.store().b.write(logName, []byte(forged))
	_, err := e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}}, "i-2")
	want(t, "apply", err, contract.CodePendingChainBroken, "")
	var ce *contract.Error
	var d struct {
		Kind      string
		Seq       *int
		Discarded []string
	}
	asContractErr(err, &ce)
	_ = json.Unmarshal(ce.Detail, &d)
	if len(d.Discarded) != 2 || d.Kind != "prev_mismatch" {
		t.Errorf("detail: %s", ce.Detail)
	}
	if len(e.rec.locked) != 0 {
		t.Errorf("state.lock を持ったまま通知した: %v", e.rec.locked)
	}
	var entry struct {
		Kind string
		Body struct {
			Lines     []string
			Discarded []string
			Failed    []map[string]string
		}
	}
	last := e.rec.Entries[len(e.rec.Entries)-1]
	_ = json.Unmarshal(last, &entry)
	if entry.Kind != "pending_discarded" || len(entry.Body.Lines) < 3 || len(entry.Body.Failed) != 1 || entry.Body.Failed[0]["code"] != "CHAIN_BROKEN" || len(entry.Body.Discarded) != 2 {
		t.Errorf("pending_discarded: %s", last)
	}
	var types []string
	for _, ev := range e.rec.Events {
		types = append(types, ev.Type)
	}
	if !strings.Contains(strings.Join(types, ","), "journal.chain_broken") || !strings.Contains(strings.Join(types, ","), "change.failed") {
		t.Errorf("通知: %v", types)
	}
	if raw, _ := e.st.Get(ctx, "delays"); strings.Contains(string(raw), p2.ChangeID) {
		t.Errorf("捨てた変更の delays[] が残る: %s", raw)
	}
	malformed := newEnv(t, true)
	malformed.propose("key-rotate-waive", nil)
	_ = malformed.a.store().b.write(logName, []byte("not json\n"))
	_, err = malformed.a.Pending(ctx, call("pending", nil, nil))
	asContractErr(err, &ce)
	if !strings.Contains(string(ce.Detail), `"seq":null`) {
		t.Errorf("読めない行の seq: %s", ce.Detail)
	}
}

func TestApplyIDStates(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	_, err := e.apply(map[string][]string{"id": {p.ChangeID}})
	want(t, "未承認", err, contract.CodeChangeStateInvalid, "") // spec 3.1.3(D-0107): 未承認への apply --id
	if lastState(t, e, p.ChangeID) != "proposed" {
		t.Error("未承認の変更が failed になった")
	}
	_, err = e.apply(map[string][]string{"id": {"c-20990101-0001"}})
	want(t, "未知", err, contract.CodeChangeStateInvalid, "")
	_, _ = e.a.Cancel(ctx, call("cancel", nil, map[string][]string{"id": {p.ChangeID}}))
	_, err = e.apply(map[string][]string{"id": {p.ChangeID}})
	want(t, "取り消し済み", err, contract.CodeChangeStateInvalid, "")
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	n := len(e.rec.Entries)
	plan, err := e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}, DryRun: true}, "i-1")
	if err != nil || len(plan.Changes) != 1 || lastState(t, e, p.ChangeID) != "approved" || len(e.rec.Entries) != n {
		t.Fatalf("dry-run が書いた: %+v %v", plan, err)
	}
	e.clock.Advance(contract.ApprovedExpiry + hours(1))
	if _, err := e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}, DryRun: true}, "i-1"); err != nil || lastState(t, e, p.ChangeID) != "approved" {
		t.Errorf("dry-run が失効させた: %v", err)
	}
}

// 再開でも記録を照合する(書き換えた記録は再開せず failed)。
func TestResumeVerifiesRecord(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	_, _ = e.apply(nil)
	raw, _, _ := e.a.store().b.read(changeName(p.ChangeID))
	_ = e.a.store().b.write(changeName(p.ChangeID), []byte(strings.Replace(string(raw), "weekly", "monthly", 1)))
	plan, _ := e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}}, "i-2")
	if len(plan.Changes) != 0 || len(plan.Failed) != 1 || !strings.Contains(string(plan.Failed[0]), "SIGNATURE_INVALID") {
		t.Fatalf("%+v", plan)
	}
}

func TestSameInstanceIsNotInterrupted(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	_, _ = e.apply(nil)
	plan, _ := e.apply(nil) // 同じ instance
	if len(plan.Changes) != 0 || len(plan.Failed) != 0 || lastState(t, e, p.ChangeID) != "applying" {
		t.Fatalf("自分の applying を中断として扱った: %+v", plan)
	}
}

// 秘密は --id と一致する変更にだけ渡す。ほかの再開は秘密なしで HUMAN_SECRET_REQUIRED。
func TestSecretsOnlyForMatchingID(t *testing.T) {
	e := newEnv(t, true)
	e.a.Enroller = &recEnroller{}
	e.a.Planner = func(ctxT, string, map[string]json.RawMessage) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
		return map[string]json.RawMessage{"host_key_fp": json.RawMessage(`"SHA256:Y"`)}, nil, nil
	}
	y := e.propose("source-change", map[string][]string{"source": {`{"host":"y"}`}})
	e.approveAndWait(y, 24)
	if plan, err := e.apply(map[string][]string{"id": {y.ChangeID}, "source-admin-ref": {"ry"}}); err != nil || len(plan.Changes) != 1 {
		t.Fatalf("%+v %v", plan, err)
	}
	x := e.propose("storage-init", map[string][]string{"bucket": {"b"}})
	e.approveOK(x)
	plan, err := e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {x.ChangeID}, "admin-ref": {"rx"}, "source-admin-ref": {"rx2"}}}, "i-2")
	if err != nil || len(plan.Failed) != 1 || !strings.Contains(string(plan.Failed[0]), y.ChangeID) || !strings.Contains(string(plan.Failed[0]), "HUMAN_SECRET_REQUIRED") {
		t.Fatalf("Y に X の秘密が渡った: %+v %v", plan, err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].ChangeID != x.ChangeID {
		t.Errorf("X が始まらない: %+v", plan.Changes)
	}
}

// 登録を伴う変更の再開 (i)(ii)(iii)(3.1.2.11)。
func TestResumeSwitchCases(t *testing.T) {
	const old, nw = `{"restore":{"v":"old","dest":{"key_ref":"k"}}}`, `{"restore":{"v":"new","dest":{"key_ref":"k"}}}`
	setup := func() (*env, proposeRes) {
		e := newEnv(t, true)
		e.setDef(old)
		e.a.Planner = func(ctxT, string, map[string]json.RawMessage) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
			return map[string]json.RawMessage{"after": json.RawMessage(`{"v":"new","dest":{"key_ref":"k"}}`)}, nil, nil
		}
		p := e.propose("restore-change", map[string][]string{"restore": {`{"v":"new","dest":{"key_ref":"k"}}`}})
		e.approveAndWait(p, 24)
		call := contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {p.ChangeID}}, Secrets: contract.SecretInput{"k": json.RawMessage(`"s"`)}}
		if plan, err := e.a.ApplyPlan(ctx, call, "i-1"); err != nil || len(plan.Changes) != 1 || len(plan.Changes[0].Steps) != 2 {
			t.Fatalf("開始: %+v %v", plan, err)
		}
		return e, p
	}
	resume := func(e *env, c contract.Call) contract.ApplyPlan {
		plan, err := e.a.ApplyPlan(ctx, c, "i-2")
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	e, p := setup()
	e.setDef(nw)
	if plan := resume(e, contract.Call{Words: []string{"apply"}}); len(plan.Changes) != 1 || len(plan.Changes[0].Steps) != 1 || plan.Changes[0].Steps[0].Kind != "define" {
		t.Errorf("(ii): %+v", plan)
	}
	e, p = setup()
	withSecret := contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {p.ChangeID}}, Secrets: contract.SecretInput{"k": json.RawMessage(`"s"`)}}
	if plan := resume(e, withSecret); len(plan.Changes) != 1 || len(plan.Changes[0].Steps) != 2 {
		t.Errorf("(i): %+v", plan)
	}
	e, _ = setup()
	if plan := resume(e, contract.Call{Words: []string{"apply"}}); len(plan.Failed) != 1 || !strings.Contains(string(plan.Failed[0]), "HUMAN_SECRET_REQUIRED") {
		t.Errorf("(i) 秘密なし: %+v", plan)
	}
	e, _ = setup()
	e.setDef(`{"restore":{"v":"third"}}`)
	if plan := resume(e, contract.Call{Words: []string{"apply"}}); len(plan.Failed) != 1 || !strings.Contains(string(plan.Failed[0]), "PRECONDITION_CHANGED") {
		t.Errorf("(iii): %+v", plan)
	}
}

// 承認を人向けの宛先が受理した時刻 first_accepted_at から遅延を数え直す(3.6)。
func TestFirstAcceptedExtendsNotBefore(t *testing.T) {
	e := newEnv(t, true)
	e.rec.lag = hours(1)
	p := e.propose("delay-change", map[string][]string{"hours": {"24"}})
	e.approveOK(p)
	e.clock.Advance(hours(24) + time.Minute)
	e.rec.lag = 0
	if plan, _ := e.apply(nil); len(plan.Changes) != 0 {
		t.Fatal("first_accepted_at の延長が効かない")
	}
	e.clock.Advance(hours(1))
	if plan, _ := e.apply(nil); len(plan.Changes) != 1 {
		t.Fatalf("延長の後に適用されない: %+v", plan)
	}
}

func TestApprovedExpiryAndAttestStays(t *testing.T) {
	e := newEnv(t, true)
	q := e.propose("storage-init", map[string][]string{"bucket": {"b"}}) // 秘密が無く待つ
	e.approveOK(q)
	a := e.a
	r, err := a.Attest(ctx, call("attest", nil, map[string][]string{"step": {"R3"}, "note": {`"n"`}}))
	if err != nil {
		t.Fatal(err)
	}
	var at attestOut
	_ = json.Unmarshal(r.Result, &at)
	pa := proposeRes{ChangeID: *at.ChangeID, Hash: ""}
	rec, _, _ := a.store().readChange(pa.ChangeID)
	pa.Hash = rec.CanonicalHash
	e.approveOK(pa)
	e.clock.Advance(contract.ApprovedExpiry + hours(1))
	_, _ = e.apply(nil)
	if lastState(t, e, q.ChangeID) != "expired" {
		t.Error("承認済みの期限で expired にならない")
	}
	if lastState(t, e, pa.ChangeID) != "approved" {
		t.Error("署名済みの attest が失効した")
	}
}

// 再起動をまたぐ: 同じ Dir から読み直しても提案・承認が残り、ファイルの連鎖が通る。
func TestPersistsAcrossRestart(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	b := &Approval{State: e.st, Recorder: e.rec, Clock: e.clock, Dir: e.a.Dir}
	l := pendingChanges(t, b, nil)
	if len(l) != 1 || l[0].State != "approved" {
		t.Fatalf("%+v", l)
	}
	pl := &PendingList{State: e.st, Dir: e.a.Dir}
	if got, err := pl.List(ctx); err != nil || len(got) != 1 {
		t.Errorf("PendingList: %v %v", got, err)
	}
	if _, err := (&Approval{State: e.st, Recorder: e.rec, Clock: e.clock}).Pending(ctx, call("pending", nil, nil)); codeOf(err) != contract.CodeInternal {
		t.Errorf("Dir 無しは動かさない: %v", err)
	}
}

// cancel が前提の再計算と applying の追記の間に入っても、取り消した変更は applying にならない(排他の中の遷移の検査)。
func TestCancelRacesApplyingAppend(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	r := &applyRun{a: e.a, ctx: ctx, s: e.a.store(), c: contract.Call{}, instance: "i-1", plan: contract.ApplyPlan{Changes: []contract.ChangeOps{}}}
	_ = r.reload()
	rec, _, _ := r.s.readChange(p.ChangeID)
	_, _ = e.a.Cancel(ctx, call("cancel", nil, map[string][]string{"id": {p.ChangeID}})) // 照合の後・追記の前に入った cancel
	if err := r.startApplying(rec); err != nil || len(r.plan.Changes) != 0 || lastState(t, e, p.ChangeID) != "cancelled" {
		t.Fatalf("取り消した変更が適用に入った: %v %+v", err, r.plan)
	}
}

// 排他の中の追記だけを使う(通常の Record を呼ばない)。state.lock は操作の終わりに放している。
func TestOnlyLockedAppends(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	_, _ = e.apply(nil)
	_ = e.a.ApplyRecord(ctx, p.ChangeID, 0, contract.StepResult{Result: json.RawMessage(`{}`)})
	for _, c := range e.rec.Recorder.Calls() {
		if c == "Record" {
			t.Fatal("通常の Record を呼んだ")
		}
	}
	if e.st.Locked || len(e.rec.locked) != 0 {
		t.Errorf("state.lock: locked=%v emits=%v", e.st.Locked, e.rec.locked)
	}
}

// 承認の記録の key_fp を書き換えた(署名自体は有効)変更は適用しない。
func TestApplyChecksApprovalKeyFingerprint(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	ap, _, _ := e.a.store().readApproval(p.ChangeID)
	ap.Approvers[0].KeyFP = "SHA256:forged"
	b, _ := marshalCanonical(ap)
	_ = e.a.store().b.write(approvalName(p.ChangeID), b)
	plan, _ := e.apply(nil)
	if len(plan.Changes) != 0 || len(plan.Failed) != 1 {
		t.Fatalf("%+v", plan)
	}
}
