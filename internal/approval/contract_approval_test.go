package approval

// 契約試験(作業指示書 3.2): 試験ベクトルを骨格に通す。骨格は NOT_IMPLEMENTED で落ち、nil を返すだけの実装は値の比較で落ち、
// 仕様どおりの実装は panic しない(偽物を差し込む)。WP は骨格を実装し、この試験は変えない。
// gate の判定(許可リスト・字句規則・--secret-fd の読み替え・拒否の detail)は U1d が持つ。ベクトルは internal/contract/testdata/approval/gate.json。
// 正規化とハッシュ(JCS・def_hash)は contract.CanonicalJSON / CanonicalHash が持ち、contract の試験が RFC 8785 のベクトルで照らす。

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var ctx = context.Background()

func newApproval() *Approval {
	return &Approval{State: &fake.StateFiles{}, Enroller: &fake.Enroller{}, Recorder: &fake.Recorder{}, Clock: &fake.Clock{T: fake.StartTime}}
}

type gateVectors struct {
	Denied  []struct{ Name, Line, Reason string } `json:"denied"`
	Allowed []struct {
		Name, Line string
		Argv       []string
		SecretOut  string `json:"secret_out"`
	} `json:"allowed"`
}

func TestContractApprovalGateDenied(t *testing.T) {
	var v gateVectors
	harness.LoadVectors(t, "approval/gate.json", &v)
	for _, c := range v.Denied {
		d, err := newApproval().Gate(ctx, c.Line)
		harness.Implemented(t, err)
		if err != nil || d.Allowed || d.Denied == nil {
			t.Errorf("%s: 拒否されない: %+v %v", c.Name, d, err)
			continue
		}
		if d.Denied.Code != contract.CodeGateDenied || d.Denied.Class != "input" {
			t.Errorf("%s: %s/%s", c.Name, d.Denied.Code, d.Denied.Class)
		}
		var det struct {
			Reason string
			Index  *int
		}
		_ = json.Unmarshal(d.Denied.Detail, &det)
		if det.Reason != c.Reason {
			t.Errorf("%s: reason %q want %q", c.Name, det.Reason, c.Reason)
		}
		// detail は GATE_DENIED のスキーマ(reason と index の両方が必須)に合う。
		env := map[string]any{"schema": "sitate.out/v1", "command": []string{"gate"}, "ok": false, "dry_run": false, "run_id": "r-20261018T173000Z-3f9a",
			"result": nil, "warnings": []any{}, "error": map[string]any{"code": d.Denied.Code, "class": d.Denied.Class, "retryable": false, "detail": json.RawMessage(d.Denied.Detail), "next": []any{}}}
		b, _ := json.Marshal(env)
		if err := contract.Validate("cli/envelope.json", b); err != nil {
			t.Errorf("%s: 封筒のスキーマに合わない: %v", c.Name, err)
		}
	}
}

func TestContractApprovalGateAllowed(t *testing.T) {
	var v gateVectors
	harness.LoadVectors(t, "approval/gate.json", &v)
	for _, c := range v.Allowed {
		d, err := newApproval().Gate(ctx, c.Line)
		harness.Implemented(t, err)
		if err != nil || !d.Allowed || d.Denied != nil || !reflect.DeepEqual(d.Argv, c.Argv) || d.SecretOut != c.SecretOut {
			t.Errorf("%s: %+v %v(argv %v・秘密の出力先 %q が必要)", c.Name, d, err, c.Argv, c.SecretOut)
		}
	}
}

func call(w string, pos []string, args map[string][]string) contract.Call {
	return contract.Call{Words: []string{w}, Positional: pos, Args: args}
}

type proposeRes struct {
	ChangeID string `json:"change_id"`
	Reused   bool   `json:"reused"`
	State    string `json:"state"`
	Hash     string `json:"canonical_hash"`
}

func mustPropose(t *testing.T, a *Approval, hours string) proposeRes {
	t.Helper()
	r, err := a.Propose(ctx, call("propose", []string{"delay-change"}, map[string][]string{"hours": {hours}}))
	harness.Shape(t, []string{"propose"}, r, err)
	var p proposeRes
	harness.Result(t, r, &p)
	return p
}

// 提案: 新しい提案は proposed・reused=false、同じ内容は同じ id を reused=true で返す、内容が違えば別の id。種別の列挙に無い値と引数の誤りは ARG_INVALID。
func TestContractApprovalPropose(t *testing.T) {
	a := newApproval()
	p1 := mustPropose(t, a, "24")
	if p1.ChangeID == "" || p1.Reused || p1.State != "proposed" || p1.Hash == "" {
		t.Fatalf("新しい提案: %+v", p1)
	}
	if p2 := mustPropose(t, a, "24"); p2.ChangeID != p1.ChangeID || !p2.Reused || p2.Hash != p1.Hash {
		t.Errorf("同じ内容の保留中の提案は同じ id を reused=true で返す: %+v", p2)
	}
	if p3 := mustPropose(t, a, "48"); p3.ChangeID == p1.ChangeID || p3.Reused || p3.Hash == p1.Hash {
		t.Errorf("内容が違えば別の提案: %+v", p3)
	}
	_, err := a.Propose(ctx, call("propose", []string{"no-such-kind"}, nil))
	harness.ExpectCode(t, err, contract.CodeArgInvalid)
	_, err = a.Propose(ctx, call("propose", []string{"delay-change"}, map[string][]string{"hours": {"x"}}))
	harness.ExpectCode(t, err, contract.CodeArgInvalid)
}

func pendingChanges(t *testing.T, a *Approval, args map[string][]string) []struct{ ChangeID, Op, State string } {
	t.Helper()
	r, err := a.Pending(ctx, call("pending", nil, args))
	harness.Shape(t, []string{"pending"}, r, err)
	var v struct {
		Changes []struct {
			ChangeID string `json:"change_id"`
			Op       string `json:"op"`
			State    string `json:"state"`
		} `json:"changes"`
	}
	harness.Result(t, r, &v)
	out := make([]struct{ ChangeID, Op, State string }, len(v.Changes))
	for i, c := range v.Changes {
		out[i] = struct{ ChangeID, Op, State string }{c.ChangeID, c.Op, c.State}
	}
	return out
}

// pending: 保留中の提案が state つきで並び、--id はその変更を返し、未知の id は CHANGE_STATE_INVALID。
func TestContractApprovalPending(t *testing.T) {
	a := newApproval()
	if l := pendingChanges(t, a, nil); len(l) != 0 {
		t.Fatalf("提案の無い一覧: %+v", l)
	}
	p := mustPropose(t, a, "24")
	l := pendingChanges(t, a, nil)
	if len(l) != 1 || l[0].ChangeID != p.ChangeID || l[0].Op != "delay-change" || l[0].State != "proposed" {
		t.Errorf("保留中の一覧: %+v", l)
	}
	if one := pendingChanges(t, a, map[string][]string{"id": {p.ChangeID}}); len(one) != 1 || one[0].ChangeID != p.ChangeID {
		t.Errorf("--id: %+v", one)
	}
	_, err := a.Pending(ctx, call("pending", nil, map[string][]string{"id": {"c-20990101-0001"}}))
	harness.ExpectCode(t, err, contract.CodeChangeStateInvalid)
}

// cancel: 保留中の変更を取り消し(cancelled_at)、取り消し済みは変化なし、取り消した変更は保留中の一覧から外れ --id では state=cancelled で返る。未知の id は CHANGE_STATE_INVALID。
func TestContractApprovalCancel(t *testing.T) {
	a := newApproval()
	p := mustPropose(t, a, "24")
	id := map[string][]string{"id": {p.ChangeID}}
	for i := 0; i < 2; i++ {
		r, err := a.Cancel(ctx, call("cancel", nil, id))
		harness.Shape(t, []string{"cancel"}, r, err)
		var v struct {
			At string `json:"cancelled_at"`
		}
		harness.Result(t, r, &v)
		if v.At == "" {
			t.Errorf("cancelled_at が無い(%d 回目): %s", i+1, r.Result)
		}
	}
	if l := pendingChanges(t, a, nil); len(l) != 0 {
		t.Errorf("取り消した変更が保留中に残る: %+v", l)
	}
	if one := pendingChanges(t, a, id); len(one) != 1 || one[0].State != "cancelled" {
		t.Errorf("取り消した変更を --id で引く: %+v", one)
	}
	_, err := a.Cancel(ctx, call("cancel", nil, map[string][]string{"id": {"c-20990101-0001"}}))
	harness.ExpectCode(t, err, contract.CodeChangeStateInvalid)
}

// approve: 未知の変更への承認は CHANGE_STATE_INVALID(署名を付けた承認の正常系は署名の形式を要するため U1d の追加の試験が持つ)。
func TestContractApprovalApprove(t *testing.T) {
	_, err := newApproval().Approve(ctx, call("approve", nil, map[string][]string{"id": {"c-20990101-0001"}, "signature": {"x"}, "signed-at": {"2026-11-02T00:00:00Z"}}))
	harness.ExpectCode(t, err, contract.CodeChangeStateInvalid)
}

// attest: 段の値域外は ARG_INVALID、署名を付ける段は記録を作り(recorded、signed=false)、繰り返しても同じ change_id を返す。
func TestContractApprovalAttest(t *testing.T) {
	a := newApproval()
	_, err := a.Attest(ctx, call("attest", nil, map[string][]string{"step": {"no_such_step"}}))
	harness.ExpectCode(t, err, contract.CodeArgInvalid)
	var ids [2]string
	for i := range ids {
		r, err := a.Attest(ctx, call("attest", nil, map[string][]string{"step": {"R3"}, "note": {`"done"`}}))
		harness.Shape(t, []string{"attest"}, r, err)
		var v struct {
			ChangeID *string `json:"change_id"`
			Step     string  `json:"step"`
			Recorded bool    `json:"recorded"`
			Signed   bool    `json:"signed"`
		}
		harness.Result(t, r, &v)
		if v.ChangeID == nil || v.Step != "R3" || !v.Recorded || v.Signed {
			t.Fatalf("attest R3: %s", r.Result)
		}
		ids[i] = *v.ChangeID
	}
	if ids[0] != ids[1] {
		t.Errorf("署名前の記録があれば作らずに返す: %v", ids)
	}
}

func TestContractApprovalApplyPlan(t *testing.T) {
	p, err := newApproval().ApplyPlan(ctx, contract.Call{Words: []string{"apply"}}, "i-1")
	harness.Implemented(t, err)
	if err != nil || p.Instance != "i-1" || p.Waiting == nil || p.Failed == nil {
		// 空でも waiting[]・failed[] は配列(null でない)。
		t.Errorf("waiting[]・failed[] が配列でない: %+v", p)
	}
}

func TestContractApprovalApplyEnroll(t *testing.T) {
	_, err := newApproval().ApplyEnroll(ctx, "c-none", 0)
	harness.Implemented(t, err)
	if err == nil {
		t.Error("存在しない変更の登録の要素が成功した")
	}
}

func TestContractApprovalApplyRecord(t *testing.T) {
	err := newApproval().ApplyRecord(ctx, "c-none", 0, contract.StepResult{})
	harness.Implemented(t, err)
	if err == nil {
		t.Error("存在しない変更への記録が成功した")
	}
}

func TestContractApprovalProposer(t *testing.T) {
	_, err := (&Proposer{}).Propose(ctx, "", nil)
	harness.Implemented(t, err)
	if err == nil {
		t.Error("種別の無い提案が成功した")
	}
}

func TestContractApprovalPendingList(t *testing.T) {
	l, err := (&PendingList{State: &fake.StateFiles{}}).List(ctx)
	harness.Implemented(t, err)
	if err != nil || l == nil {
		t.Errorf("保留中の変更の一覧は空でも配列: %v %v", l, err)
	}
}
