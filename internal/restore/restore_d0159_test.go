package restore

// D-0159(仕様書 1.2.8)の試験: 復元先の登録の host_key_fp・allow_ssh_from の省略・observed_from、台帳の消去と破棄の呼び直し。
// 偽の復元先アダプタが受けた要求を見る(spec 3.2.4 の「ホスト鍵」「値の出所」、3.5 の「破棄の失敗」、3.1.2.6)。

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const defNoFrom = `{"version":1,"storage":[{"key_ref":"store-key"}],"restore":{"adapter":"ssh-host","dest":{"at":"ssh://restore@dest.example.invalid:22","key_ref":"dest-key"}}}`

func reqOf(t *testing.T, e *env, op string) map[string]json.RawMessage {
	t.Helper()
	for _, c := range e.AR.Calls {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(c.Req, &m)
		if string(m["op"]) == `"`+op+`"` {
			return m
		}
	}
	t.Fatalf("op %s の呼び出しが無い: %v", op, opsOf(e))
	return nil
}

func enrollResult(fp string, observed any) fake.Step {
	b, _ := json.Marshal(map[string]any{"contract": "sitate.restore-dest/v1", "host_key_fp": fp, "user": "sitate-restore", "observed_from": observed, "prereq": []any{}})
	return okStep(b)
}

func enrollCall(args map[string][]string) contract.Call {
	return contract.Call{Words: []string{"restore-dest", "enroll"}, RunID: rtRun, Args: args}
}

func enrollmentOf(t *testing.T, e *env) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.St.Items["restore_dest_enrollment"], &m); err != nil {
		t.Fatalf("restore_dest_enrollment が読めない: %s", e.St.Items["restore_dest_enrollment"])
	}
	if err := contract.Validate("data/state.json#/$defs/state/properties/restore_dest_enrollment", e.St.Items["restore_dest_enrollment"]); err != nil {
		t.Errorf("restore_dest_enrollment が状態のスキーマに合わない: %v", err)
	}
	return m
}

// 初回の登録(固定値が無い)は要求の host_key_fp を省く。restore.ssh_from があれば allow_ssh_from に入れ、結果の observed_from を記録する。
func TestEnrollFirstOmitsHostKeyFPAndRecordsObserved(t *testing.T) {
	e, _ := isoDest(t, "", false)
	e.St.Items = nil
	e.AR.Script("restore-dest", enrollResult(fpDest, "192.0.2.1"))
	if _, err := e.RestoreDestEnroll(ctx, enrollCall(nil)); err != nil {
		t.Fatal(err)
	}
	req := reqOf(t, e, "enroll")
	if _, has := req["host_key_fp"]; has {
		t.Errorf("初回の登録は host_key_fp を省く: %s", req["host_key_fp"])
	}
	if string(req["allow_ssh_from"]) != `["192.0.2.10"]` {
		t.Errorf("restore.ssh_from があれば allow_ssh_from に入れる: %s", req["allow_ssh_from"])
	}
	if m := enrollmentOf(t, e); m["observed_from"] != "192.0.2.1" {
		t.Errorf("observed_from を記録していない: %v", m)
	}
}

// restore.ssh_from が無い定義: 要求の allow_ssh_from を省き(空の配列にもしない)、結果の observed_from を記録する。
func TestEnrollWithoutSSHFromOmitsAllowAndRecords(t *testing.T) {
	e, _ := isoDest(t, "", true)
	e.St.Items = nil
	e.St.Def = []byte(defNoFrom)
	e.AR = fake.NewAdapterRunner()
	e.Restore.Adapters = e.AR
	e.AR.Script("restore-dest", enrollResult(fpDest, "192.0.2.1"))
	if _, err := e.RestoreDestEnroll(ctx, enrollCall(nil)); err != nil {
		t.Fatal(err)
	}
	if v, has := reqOf(t, e, "enroll")["allow_ssh_from"]; has {
		t.Errorf("restore.ssh_from が無いとき allow_ssh_from は省く: %s", v)
	}
	if m := enrollmentOf(t, e); m["observed_from"] != "192.0.2.1" {
		t.Errorf("observed_from を記録していない: %v", m)
	}
}

// 結果の observed_from が null なら、記録も null(ssh_from があるので止めない)。
func TestEnrollObservedNullRecordedNull(t *testing.T) {
	e, _ := isoDest(t, "", false)
	e.St.Items = nil
	e.AR.Script("restore-dest", enrollResult(fpDest, nil))
	if _, err := e.RestoreDestEnroll(ctx, enrollCall(nil)); err != nil {
		t.Fatal(err)
	}
	if m := enrollmentOf(t, e); m["observed_from"] != nil {
		t.Errorf("observed_from = %v, want null", m["observed_from"])
	}
}

// アダプタが no_observed_from を返したら DEF_INVALID(/restore/ssh_from)。固定値を書かない。
func TestEnrollNoObservedFromIsDefInvalid(t *testing.T) {
	e, _ := isoDest(t, "", false)
	e.St.Items = nil
	e.St.Def = []byte(defNoFrom)
	e.AR.Script("restore-dest", fake.Step{Outcome: fake.OutcomeError, ErrCode: "no_observed_from"})
	_, err := e.RestoreDestEnroll(ctx, enrollCall(nil))
	ce := harness.ExpectCode(t, err, contract.CodeDefInvalid)
	if !strings.Contains(string(ce.Detail), "/restore/ssh_from") {
		t.Errorf("detail の pointer が /restore/ssh_from でない: %s", ce.Detail)
	}
	for _, k := range []string{"host_keys", "restore_dest_enrollment"} {
		if v := e.St.Items[k]; len(v) != 0 && string(v) != "null" {
			t.Errorf("%s を書いた: %s", k, v)
		}
	}
}

// 再登録は state.json の固定値を要求の host_key_fp に入れる。--host-key-fp があれば固定値の代わりにそれを入れ、合えば固定値を置き換える。
func TestReenrollPinsStoredFPAndHostKeyFPFlagReplaces(t *testing.T) {
	const newFP = "SHA256:newfp0123"
	e, _ := isoDest(t, "", false) // host_keys に fpDest
	e.AR.Script("restore-dest", enrollResult(fpDest, "192.0.2.1"))
	if _, err := e.RestoreDestEnroll(ctx, enrollCall(nil)); err != nil {
		t.Fatal(err)
	}
	if got := string(reqOf(t, e, "enroll")["host_key_fp"]); got != `"`+fpDest+`"` {
		t.Errorf("再登録は固定値を入れる: %s", got)
	}
	e2, ssh := isoDest(t, "", false)
	inner := ssh.Handler // 新しい固定値のホストを演じる(登録の後の隔離の検査は新しい指紋で接続する)
	ssh.FP = newFP
	ssh.Handler = func(c context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
		r, err := inner(c, req)
		r.SeenFP = newFP
		return r, err
	}
	e2.AR.Script("restore-dest", enrollResult(newFP, "192.0.2.1"))
	rep, err := e2.RestoreDestEnroll(ctx, enrollCall(map[string][]string{"host-key-fp": {newFP}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(reqOf(t, e2, "enroll")["host_key_fp"]); got != `"`+newFP+`"` {
		t.Errorf("--host-key-fp は固定値の代わりに照らす: %s", got)
	}
	if hk := string(e2.St.Items["host_keys"]); !strings.Contains(hk, newFP) || strings.Contains(hk, fpDest) {
		t.Errorf("合えば固定値を置き換える: %s", hk)
	}
	wantRaw(t, resultOf(t, rep, "restore_dest_enroll"), "replaced", "true")
}

// --host-key-fp に合わないホスト鍵は置き換えない: RESTORE_HOSTKEY_MISMATCH で、固定値は元のまま。
func TestHostKeyFPFlagMismatchDoesNotReplace(t *testing.T) {
	e, _ := isoDest(t, "", false)
	before := string(e.St.Items["host_keys"])
	e.AR.Script("restore-dest", fake.Step{Outcome: fake.OutcomeError, ErrCode: "hostkey_mismatch",
		ErrAdapter: &contract.AdapterError{Code: "hostkey_mismatch", Detail: json.RawMessage(`{}`), SeenFP: "SHA256:seen"}})
	_, err := e.RestoreDestEnroll(ctx, enrollCall(map[string][]string{"host-key-fp": {"SHA256:newfp0123"}}))
	harness.ExpectCode(t, err, contract.CodeRestoreHostkeyMismatch)
	if string(e.St.Items["host_keys"]) != before {
		t.Errorf("合わないのに固定値が変わった: %s", e.St.Items["host_keys"])
	}
	// アダプタが違う指紋を返してきても(要求を無視する実装)置き換えない。
	e2, _ := isoDest(t, "", false)
	before = string(e2.St.Items["host_keys"])
	e2.AR.Script("restore-dest", enrollResult("SHA256:other", "192.0.2.1"))
	_, err = e2.RestoreDestEnroll(ctx, enrollCall(map[string][]string{"host-key-fp": {"SHA256:newfp0123"}}))
	harness.ExpectCode(t, err, contract.CodeRestoreHostkeyMismatch)
	if string(e2.St.Items["host_keys"]) != before {
		t.Errorf("違う指紋で固定値が変わった: %s", e2.St.Items["host_keys"])
	}
}

// restore-change(apply の enroll の要素)の登録は提案の host_key_fp を要求に入れる。提案に無ければ省く。書かない。
func TestEnrollerPutsProposalFPInRequest(t *testing.T) {
	for _, fp := range []string{"SHA256:prop0123", ""} {
		r, st := newRestore()
		ar := r.Adapters.(*fake.AdapterRunner)
		res := fpDest
		if fp != "" {
			res = fp
		}
		ar.Script("restore-dest", enrollResult(res, "192.0.2.1"))
		e := &RestoreDestEnroller{Store: r.Store, State: st, Adapters: r.Adapters, Codec: r.Codec, KeyS: r.KeyS, Crypto: r.Crypto, Recorder: r.Recorder, Clock: r.Clock}
		out, err := e.Enroll(ctx, contract.EnrollInput{Kind: "restore-dest", HostKeyFP: fp, WriteState: false,
			Section: json.RawMessage(`{"adapter":"ssh-host","dest":{"at":"ssh://restore@dest.example.invalid:22","key_ref":"dest-key"}}`)})
		if err != nil {
			t.Fatalf("fp=%q: %v", fp, err)
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(ar.Calls[0].Req, &m)
		got, has := m["host_key_fp"]
		if fp == "" && has || fp != "" && string(got) != `"`+fp+`"` {
			t.Errorf("fp=%q: 要求の host_key_fp = %s", fp, got)
		}
		if _, has := m["allow_ssh_from"]; has {
			t.Errorf("ssh_from の無い節で allow_ssh_from を入れた: %s", m["allow_ssh_from"])
		}
		var rec map[string]any
		if json.Unmarshal(out.RestoreDestEnroll, &rec) != nil || rec["observed_from"] != "192.0.2.1" {
			t.Errorf("登録の結果に observed_from が無い: %s", out.RestoreDestEnroll)
		}
	}
}

// 復元テストの start の allow_ssh_from: restore.ssh_from があればそれ、無ければ登録で記録した observed_from、どちらも無ければ DEF_INVALID(start を呼ばない)。
func TestStartAllowSSHFromRule(t *testing.T) {
	cases := []struct {
		name, def, enrollment, want string
		defInvalid                  bool
	}{
		{"ssh_from を優先", defSSH, `{"egress_allow":[],"observed_from":"192.0.2.77"}`, `["192.0.2.10"]`, false},
		{"observed_from を使う", defNoFrom, `{"egress_allow":[],"observed_from":"192.0.2.77"}`, `["192.0.2.77"]`, false},
		{"observed_from が null", defNoFrom, `{"egress_allow":[],"observed_from":null}`, "", true},
		{"登録の記録が無い", defNoFrom, `null`, "", true},
	}
	for _, c := range cases {
		e := pinnedWorld(t)
		e.St.Def = []byte(c.def)
		e.St.Items["restore_dest_enrollment"] = json.RawMessage(c.enrollment)
		e.AR.Script("restore-dest", okStep(absent), fake.Step{Outcome: fake.OutcomeError, ErrCode: "ADAPTER_FAILED"})
		_, err := e.RestoreTest(ctx, rtCall(false))
		if c.defInvalid {
			ce := harness.ExpectCode(t, err, contract.CodeDefInvalid)
			if !strings.Contains(string(ce.Detail), "/restore/ssh_from") {
				t.Errorf("%s: pointer: %s", c.name, ce.Detail)
			}
			if slices.Contains(opsOf(e), "start") {
				t.Errorf("%s: DEF_INVALID なのに start を呼んだ: %v", c.name, opsOf(e))
			}
			continue
		}
		harness.ExpectCode(t, err, contract.CodeAdapterError)
		if got := string(reqOf(t, e, "start")["allow_ssh_from"]); got != c.want {
			t.Errorf("%s: start の allow_ssh_from = %s, want %s", c.name, got, c.want)
		}
	}
}

func processStop() json.RawMessage {
	return json.RawMessage(`{"done":false,"verified_by":"ssh","remaining":[{"type":"process","id":"4242"}]}`)
}

// stop の remaining に type: process があれば、inspect が absent でも消さない(平文を扱う処理が残ったまま記録を消さない)。
func TestTeardownKeepsLedgerWhenProcessRemains(t *testing.T) {
	rec := ledgerRec{RecordID: "l-0123456789abcdef", Kind: "dest_session", RunID: rtRun, Adapter: "ssh-host", Dest: destRef{At: destURL, KeyRef: "dest-key"},
		Purpose: "restore_test", SessionID: rtRun, State: "created", Resources: []resRef{{Type: "workdir", ID: rtRun}}}
	ar := fake.NewAdapterRunner()
	ar.Script("restore-dest", okStep(processStop()), okStep(absent))
	tr := teardownSession(ctx, ar, nil, rec)
	if tr.Absent {
		t.Errorf("process が残っているのに Absent: %+v", tr)
	}
	if !slices.Contains(tr.Remaining, resRef{Type: "process", ID: "4242"}) {
		t.Errorf("remaining に process が無い: %+v", tr.Remaining)
	}
	// process 以外の残りは従来どおり inspect の absent が決める(remaining が空でなくても、absent なら消す)。
	ar = fake.NewAdapterRunner()
	ar.Script("restore-dest", okStep(json.RawMessage(`{"done":false,"verified_by":"ssh","remaining":[{"type":"container","id":"abc123def456"}]}`)), okStep(absent))
	if tr := teardownSession(ctx, ar, nil, rec); !tr.Absent {
		t.Errorf("process でない残りは inspect の absent に従う: %+v", tr)
	}
}

// 復元テストの終わりの破棄: process が残れば台帳は destroy_failed で残り、警告 TEARDOWN_UNVERIFIED。
func TestRestoreTestTeardownProcessRemainsUnverified(t *testing.T) {
	w := newPass(t)
	w.script(okStep(absent), startedStep(), okStep(processStop()), okStep(absent))
	rep, err := w.RestoreTest(ctx, rtCall(false))
	if err != nil {
		t.Fatal(err)
	}
	if !onlyState(w.St, "destroy_failed") {
		t.Errorf("process が残るのに台帳が destroy_failed でない: %s", w.St.Ledger)
	}
	var codes []contract.Code
	for _, x := range rep.Warnings {
		codes = append(codes, x.Code)
	}
	if !slices.Contains(codes, contract.CodeTeardownUnverified) {
		t.Errorf("警告 TEARDOWN_UNVERIFIED が無い: %v", codes)
	}
}

// destroy_failed の記録がある次の restore-test は、始めに stop・inspect を 1 回呼び直す。消せれば続け(台帳から消す)、残れば deferred。
func TestRestoreTestRetriesDestroyFailedFirst(t *testing.T) {
	const old = "r-20270227T000000Z-aaaa"
	w := newPass(t)
	w.St.Ledger = []json.RawMessage{ledgerSession(t, old, old, true)}
	w.script(okStep(stopped), okStep(absent), okStep(absent), startedStep(), okStep(stopped), okStep(absent))
	rep, err := w.RestoreTest(ctx, rtCall(false))
	if err != nil {
		t.Fatal(err)
	}
	m := resultOf(t, rep, "restore_test")
	wantRaw(t, m, "deferred", "false")
	wantRaw(t, m, "restore_ok", "true")
	if ops := w.adapterOps(); !slices.Equal(ops, []string{"stop", "inspect", "inspect", "start", "stop", "inspect"}) {
		t.Errorf("ops = %v", ops)
	}
	var first map[string]json.RawMessage
	_ = json.Unmarshal(w.AR.Calls[0].Req, &first)
	if string(first["session_id"]) != `"`+old+`"` {
		t.Errorf("呼び直しの対象が前の session_id でない: %s", first["session_id"])
	}
	if recs, _ := readLedger(ctx, w.St); len(recs) != 0 {
		t.Errorf("消せて続けた実行の後に台帳が残っている: %s", w.St.Ledger)
	}
	for name, steps := range map[string][]fake.Step{
		"残る(workdir)": {okStep(json.RawMessage(`{"done":false,"verified_by":"ssh","remaining":[{"type":"workdir","id":"` + old + `"}]}`)), okStep(json.RawMessage(`{"resources":[{"type":"workdir","id":"` + old + `","session_id":"` + old + `"}],"state":"present"}`))},
		"残る(process)": {okStep(processStop()), okStep(absent)},
	} {
		w := newPass(t)
		w.St.Ledger = []json.RawMessage{ledgerSession(t, old, old, true)}
		w.script(steps...)
		rep, err := w.RestoreTest(ctx, rtCall(false))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		m := resultOf(t, rep, "restore_test")
		wantRaw(t, m, "deferred", "true")
		wantRaw(t, m, "deferred_reason", `"teardown_pending"`)
		if ops := w.adapterOps(); !slices.Equal(ops, []string{"stop", "inspect"}) {
			t.Errorf("%s: 呼び直しは stop・inspect の 1 回だけ: %v", name, ops)
		}
		if !onlyState(w.St, "destroy_failed") {
			t.Errorf("%s: 台帳が destroy_failed でない: %s", name, w.St.Ledger)
		}
		if !strings.Contains(string(w.St.Ledger[len(w.St.Ledger)-1]), "2027-02-27T00:00:00Z") {
			t.Errorf("%s: 最初に確かめられなかった時刻が変わった: %s", name, w.St.Ledger)
		}
	}
}

// seal の後の restore-dest enroll の直接実行は危険(restore-change)で、APPROVAL_REQUIRED。--host-key-fp の有無によらずアダプタを呼ばず、固定値も変えない。
// seal の前(未 seal)は従来どおり動く。restore-change の apply の中の登録(RestoreDestEnroller)は止めない。
func TestEnrollAfterSealNeedsRestoreChange(t *testing.T) {
	for _, args := range []map[string][]string{nil, {"host-key-fp": {"SHA256:newfp0123"}}} {
		for _, dry := range []bool{false, true} {
			e, _ := isoDest(t, "", false)
			e.St.Items["sealed"] = json.RawMessage("true")
			before := string(e.St.Items["host_keys"])
			e.AR.Script("restore-dest", enrollResult(fpDest, "192.0.2.1"))
			c := enrollCall(args)
			c.DryRun = dry
			_, err := e.RestoreDestEnroll(ctx, c)
			ce := harness.ExpectCode(t, err, contract.CodeApprovalRequired)
			if !strings.Contains(string(ce.Detail), `"restore-change"`) {
				t.Errorf("detail.kind が restore-change でない: %s", ce.Detail)
			}
			if len(e.AR.Calls) != 0 {
				t.Errorf("アダプタを %d 回呼んだ", len(e.AR.Calls))
			}
			if string(e.St.Items["host_keys"]) != before {
				t.Errorf("固定値が変わった: %s", e.St.Items["host_keys"])
			}
		}
	}
	e, _ := isoDest(t, "", true)
	e.St.Items = map[string]json.RawMessage{"sealed": json.RawMessage("false")}
	if _, err := e.RestoreDestEnroll(ctx, enrollCall(nil)); err != nil {
		t.Errorf("未 seal の登録が止まった: %v", err)
	}
	// apply の中の登録は seal の後でも動く。
	r, st := newRestore()
	st.Items = map[string]json.RawMessage{"sealed": json.RawMessage("true")}
	r.Adapters.(*fake.AdapterRunner).Script("restore-dest", enrollResult(fpDest, "192.0.2.1"))
	en := &RestoreDestEnroller{Store: r.Store, State: st, Adapters: r.Adapters, Codec: r.Codec, KeyS: r.KeyS, Crypto: r.Crypto, Recorder: r.Recorder, Clock: r.Clock}
	if _, err := en.Enroll(ctx, contract.EnrollInput{Kind: "restore-dest", HostKeyFP: fpDest,
		Section: json.RawMessage(`{"adapter":"ssh-host","dest":{"at":"ssh://restore@dest.example.invalid:22","key_ref":"dest-key"}}`)}); err != nil {
		t.Errorf("restore-change の apply の登録が止まった: %v", err)
	}
}
