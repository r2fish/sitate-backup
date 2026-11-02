package restore

// レビューの差し戻しの試験(restore-test): 作りかけの資源、破棄の確認、掃引、復元先の報告の検証、取り消し、通知。

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

var (
	absent  = json.RawMessage(`{"resources":[],"state":"absent"}`)
	stopped = json.RawMessage(`{"done":true,"verified_by":"ssh","remaining":[]}`)
)

func okStep(r json.RawMessage) fake.Step { return fake.Step{Outcome: fake.OutcomeSuccess, Result: r} }

func startedStep() fake.Step {
	return okStep(json.RawMessage(`{"session_id":"` + rtRun + `","ssh":{"host":"dest.example.invalid","port":22,"user":"sitate-restore"},"host_key_fp":"` + fpDest + `","scratch":{"path":"/scratch/x","method":"gocryptfs"},"resources":[{"type":"workdir","id":"` + rtRun + `"}]}`))
}

// pinnedWorld は host_keys を持つ rtWorld。台本は呼び出し側が置く。
func pinnedWorld(t *testing.T) *env {
	e := rtWorld(t)
	e.St.Items["host_keys"] = json.RawMessage(`[{"role":"restore_dest","host":"dest.example.invalid","host_key_fp":"` + fpDest + `","session_id":null}]`)
	return e
}

// script は復元先アダプタの台本を差し替える。
func (w *passWorld) script(steps ...fake.Step) {
	ar := fake.NewAdapterRunner()
	ar.Script("restore-dest", steps...)
	w.AR, w.Restore.Adapters = ar, ar
}

// onlyState は台帳(同じ record_id は後のものを採る)が 1 件で、その state が want のとき真。
func onlyState(st *fake.StateFiles, want string) bool {
	recs, _ := readLedger(ctx, st)
	return len(recs) == 1 && recs[0].State == want
}

func ledgerHas(st *fake.StateFiles, id string) bool {
	recs, _ := readLedger(ctx, st)
	for _, r := range recs {
		for _, x := range r.Resources {
			if x.ID == id {
				return true
			}
		}
	}
	return false
}

func clearVerified(st *fake.StateFiles) {
	var arr []map[string]any
	_ = json.Unmarshal(st.Items["snapshots"], &arr)
	for _, a := range arr {
		a["restore_verified"] = nil
	}
	st.Items["snapshots"], _ = json.Marshal(arr)
}

func opsOf(e *env) []string { return (&passWorld{env: e}).adapterOps() }

// F3: start の打ち切り(ADAPTER_TIMEOUT)と応答の喪失では、資源の有無が分からないので台帳を消さず、stop と inspect で確かめる。
func TestRestoreTestStartTimeoutOrLostResponseTearsDown(t *testing.T) {
	for name, oc := range map[string]fake.Outcome{"打ち切り": fake.OutcomeTimeout, "応答の喪失": fake.OutcomeLostResponse} {
		e := pinnedWorld(t)
		e.AR.Script("restore-dest", okStep(absent), fake.Step{Outcome: oc}, okStep(stopped), okStep(absent))
		_, err := e.RestoreTest(ctx, rtCall(false))
		if err == nil {
			t.Fatalf("%s: 成功した", name)
		}
		if ops := opsOf(e); !slices.Equal(ops, []string{"inspect", "start", "stop", "inspect"}) {
			t.Errorf("%s: ops = %v", name, ops)
		}
		if len(e.St.Ledger) != 0 {
			t.Errorf("%s: 破棄を確かめたら台帳を消す: %d", name, len(e.St.Ledger))
		}
	}
	// 確かめられなければ台帳に残す。
	e := pinnedWorld(t)
	e.AR.Script("restore-dest", okStep(absent), fake.Step{Outcome: fake.OutcomeTimeout}, okStep(stopped), okStep(json.RawMessage(`{"resources":[{"type":"workdir","id":"x","session_id":"`+rtRun+`"}],"state":"present"}`)))
	_, _ = e.RestoreTest(ctx, rtCall(false))
	if !onlyState(e.St, "destroy_failed") {
		t.Errorf("残存があるのに destroy_failed で残っていない: %s", e.St.Ledger)
	}
}

// 完了条件 5: 破棄を確かめられない(inspect が present・unknown・呼び出しの失敗)ときは、台帳に destroy_failed で残し、
// 結果は合否のまま警告 TEARDOWN_UNVERIFIED と通知 teardown.unverified を出す。
func TestRestoreTestTeardownUnverified(t *testing.T) {
	cases := map[string]fake.Step{
		"present": okStep(json.RawMessage(`{"resources":[{"type":"workdir","id":"w","session_id":"` + rtRun + `"}],"state":"present"}`)),
		"unknown": okStep(json.RawMessage(`{"resources":[],"state":"unknown"}`)),
		"失敗":      {Outcome: fake.OutcomeError},
	}
	for name, last := range cases {
		w := newPass(t)
		w.script(okStep(absent), startedStep(), okStep(stopped), last)
		rep, err := w.RestoreTest(ctx, rtCall(false))
		harness.Shape(t, []string{"restore-test"}, rep, err)
		if err != nil {
			continue
		}
		if len(rep.Warnings) != 1 || rep.Warnings[0].Code != contract.CodeTeardownUnverified {
			t.Errorf("%s: 警告 = %+v", name, rep.Warnings)
		}
		if !onlyState(w.St, "destroy_failed") {
			t.Errorf("%s: 台帳 = %s", name, w.St.Ledger)
		}
		found := false
		for _, ev := range w.Rec.Events {
			found = found || ev.Type == "teardown.unverified"
		}
		if !found {
			t.Errorf("%s: teardown.unverified を通知していない", name)
		}
	}
}

// RunCleanup も同じ: 確かめられなければ記録を残し、残存を返す。
func TestRunCleanupUnverifiedKeepsRecord(t *testing.T) {
	for name, last := range map[string]fake.Step{
		"present": okStep(json.RawMessage(`{"resources":[{"type":"workdir","id":"w","session_id":"r-20270227T000000Z-aaaa"}],"state":"present"}`)),
		"unknown": okStep(json.RawMessage(`{"resources":[],"state":"unknown"}`)),
		"失敗":      {Outcome: fake.OutcomeError},
	} {
		st := &fake.StateFiles{Ledger: []json.RawMessage{mkLedger(t, "r-20270227T000000Z-aaaa", "restore_test", "created", "0000000000000001")}}
		ar := fake.NewAdapterRunner()
		ar.Script("restore-dest", okStep(stopped), last)
		_, un, err := (&RunCleanup{State: st, Adapters: ar}).Cleanup(ctx, "r-20270301T000000Z-eeee", "r-20270227T000000Z-aaaa")
		if err != nil || len(un) == 0 || !onlyState(st, "destroy_failed") {
			t.Errorf("%s: un=%v err=%v ledger=%s", name, un, err, st.Ledger)
		}
	}
}

// F9: session_id の無い資源は破棄しない(INTERNAL)。F19: 台帳に無い session_id の資源は記録を足して破棄し、ある session_id には足すだけ。
func TestSweepBranches(t *testing.T) {
	dest := destRef{At: destURL, KeyRef: "dest-key"}
	run := func(res string, ledger ...json.RawMessage) (*fake.AdapterRunner, *fake.StateFiles, error) {
		st := &fake.StateFiles{Ledger: ledger}
		ar := fake.NewAdapterRunner()
		ar.Script("restore-dest", okStep(json.RawMessage(res)), okStep(stopped), okStep(absent))
		return ar, st, sweep(ctx, ar, st, nil, "r-20270301T000000Z-3f9a", "ssh-host", dest, fpDest, fake.StartTime)
	}
	ar, _, err := run(`{"resources":[{"type":"workdir","id":"w","session_id":""}],"state":"present"}`)
	harness.ExpectCode(t, err, contract.CodeInternal)
	if len(ar.Calls) != 1 {
		t.Errorf("session_id が空なのに破棄を呼んだ: %d 回", len(ar.Calls))
	}
	ar, st, err := run(`{"resources":[{"type":"workdir","id":"orph","session_id":"r-20270101T000000Z-0001"}],"state":"present"}`)
	if err != nil || len(ar.Calls) != 3 || len(st.Ledger) != 0 || !strings.Contains(string(ar.Calls[1].Req), `"orph"`) || !strings.Contains(string(ar.Calls[1].Req), "r-20270101T000000Z-0001") {
		t.Errorf("孤児の破棄: %v calls=%d ledger=%d", err, len(ar.Calls), len(st.Ledger))
	}
	known := mkLedger(t, "r-20270102T000000Z-0002", "restore_test", "created", "0000000000000009")
	ar, st, err = run(`{"resources":[{"type":"workdir","id":"extra","session_id":"r-20270102T000000Z-0002"}],"state":"present"}`, known)
	if err != nil || len(ar.Calls) != 1 || !ledgerHas(st, "extra") {
		t.Errorf("既知の session は資源を足すだけ: %v calls=%d ledger=%s", err, len(ar.Calls), st.Ledger)
	}
}

// F10: 同じ run_id の前の試みの記録の破棄を確かめられなければ、新しい資源を作らず(start を呼ばず)次回へ送る。
func TestRestoreTestLeftoverSameSessionDefers(t *testing.T) {
	e := pinnedWorld(t)
	e.St.Ledger = []json.RawMessage{mkLedger(t, rtRun, "restore_test", "created", "0000000000000001")}
	e.AR.Script("restore-dest", okStep(absent), okStep(stopped), okStep(json.RawMessage(`{"resources":[{"type":"workdir","id":"w","session_id":"`+rtRun+`"}],"state":"present"}`)))
	rep, err := e.RestoreTest(ctx, rtCall(false))
	harness.Shape(t, []string{"restore-test"}, rep, err)
	wantRaw(t, resultOf(t, rep, "restore_test"), "deferred_reason", `"teardown_pending"`)
	if slices.Contains(opsOf(e), "start") {
		t.Error("破棄を確かめられないのに start を呼んだ")
	}
	var typ []string
	for _, ev := range e.Rec.Events {
		typ = append(typ, ev.Type)
	}
	if !slices.Contains(typ, "restore_test.deferred") {
		t.Errorf("延期を通知していない: %v", typ)
	}
}

// 復元先のアダプタが固定した指紋と違う指紋を返したら、SSH もせず鍵も渡さない。
func TestRestoreTestAdapterFingerprintMismatch(t *testing.T) {
	w := newPass(t)
	w.script(okStep(absent), okStep(json.RawMessage(strings.Replace(string(startedStep().Result), fpDest, "SHA256:other", 1))), okStep(stopped), okStep(absent))
	_, err := w.RestoreTest(ctx, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeRestoreHostkeyMismatch)
	if len(w.SSH.Recorded()) != 0 || w.keyLine != "" {
		t.Error("SSH か鍵の受け渡しに進んだ")
	}
}

func envelope(ok bool, result, errObj map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"schema": "sitate.out/v1", "command": []string{"restore"}, "ok": ok, "dry_run": false, "run_id": rtRun, "result": result, "warnings": []any{}, "error": errObj})
	return b
}

// 復元先の報告は信頼しない: hash_mismatch が 0 でない・非 0 終了・0 件の「成功」・スキーマに合わない detail・知らないコード・巨大な出力は、合格にしない。
func TestRestoreTestRemoteReportNotTrusted(t *testing.T) {
	good := map[string]any{"backup_ref": refA, "health": "pass", "files": 3, "bytes": 30, "hash_mismatch": 0, "checks": []any{}, "duration_s": 1, "read_bytes": 1}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for a, b := range good {
			m[a] = b
		}
		m[k] = v
		return m
	}
	cases := map[string]struct {
		out  []byte
		exit int
		code contract.Code
	}{
		"hash_mismatch>0": {envelope(true, with("hash_mismatch", 1), nil), 0, contract.CodeInternal},
		"非 0 終了":          {envelope(true, good, nil), 3, contract.CodeInternal},
		"0 件":             {envelope(true, with("files", 0), nil), 0, contract.CodeRestoreMismatch},
		"大きさの違い":          {envelope(true, with("bytes", 5), nil), 0, contract.CodeRestoreMismatch},
		"detail が不正":      {envelope(false, nil, map[string]any{"code": "RESTORE_MISMATCH", "detail": map[string]any{"evil": "注入"}}), 6, contract.CodeInternal},
		"知らないコード":         {envelope(false, nil, map[string]any{"code": "SECRET_MISSING", "detail": map[string]any{"ref": "x"}}), 6, contract.CodeInternal},
		"巨大な出力":           {[]byte(strings.Repeat("x", maxOutput+10)), 0, contract.CodeInternal},
	}
	for name, c := range cases {
		w := newPass(t)
		clearVerified(w.St)
		w.restoreOut, w.restoreExit = c.out, c.exit
		rep, err := w.RestoreTest(ctx, rtCall(false))
		harness.ExpectCode(t, err, c.code)
		if strings.Contains(string(rep.Result), `"proof":{`) {
			t.Errorf("%s: proof を出した", name)
		}
		var snaps []snapRec
		_ = json.Unmarshal(w.St.Items["snapshots"], &snaps)
		for _, s := range snaps {
			if s.Ref == refA && s.RestoreVerified != nil && *s.RestoreVerified {
				t.Errorf("%s: 不合格の世代に restore_verified: true を付けた", name)
			}
		}
	}

}

// 対照: 良い報告は合格し、世代に restore_verified: true が付き、egress_bytes は復元先の読み出しに核の側の読み出しを足した量。
func TestRestoreTestRemoteReportGood(t *testing.T) {
	w := newPass(t)
	clearVerified(w.St)
	rep, err := w.RestoreTest(ctx, rtCall(false))
	if err != nil {
		t.Errorf("対照: %v", err)
	}
	// egress_bytes は復元先の読み出し(封筒の read_bytes = 4096)に核の側の読み出し(キーリング・config・マニフェスト)を足した量。
	var eg struct {
		Egress int64 `json:"egress_bytes"`
	}
	_ = json.Unmarshal(rep.Result, &eg)
	if eg.Egress <= 4096 {
		t.Errorf("egress_bytes に核の側の読み出しが足されていない: %d", eg.Egress)
	}
	var snaps []snapRec
	_ = json.Unmarshal(w.St.Items["snapshots"], &snaps)
	for _, s := range snaps {
		if s.Ref == refA && (s.RestoreVerified == nil || !*s.RestoreVerified) {
			t.Error("合格した世代に restore_verified: true が付かない")
		}
	}
}

// 引数: --endpoint・--bucket・--prefix を語として渡し、prefix を書かない定義は既定の sitate/ を使う(空の語を渡さない)。
func TestRestoreTestRestoreArgv(t *testing.T) {
	for _, px := range []string{"", "my/px/"} {
		w := newPass(t)
		st := `{"endpoint":"https://s3.example.invalid","bucket":"bk","key_ref":"store-key"`
		if px != "" {
			st += `,"prefix":"` + px + `"`
		}
		w.St.Def = []byte(strings.Replace(defPass, `{"key_ref":"store-key"}`, st+`}`, 1))
		_, err := w.RestoreTest(ctx, rtCall(false))
		if err != nil {
			t.Fatal(err)
		}
		want := px
		if want == "" {
			want = contract.StoragePrefixDefault
		}
		for _, p := range [][2]string{{"--endpoint", "https://s3.example.invalid"}, {"--bucket", "bk"}, {"--prefix", want}} {
			i := slices.Index(w.restoreArgv, p[0])
			if i < 0 || i+1 >= len(w.restoreArgv) || w.restoreArgv[i+1] != p[1] {
				t.Errorf("prefix=%q: %s の値が %q でない: %q", px, p[0], p[1], w.restoreArgv)
			}
		}
		for _, a := range w.restoreArgv {
			if a == "" {
				t.Errorf("空の語を渡した: %q", w.restoreArgv)
			}
		}
	}
}

// F13: restore.ssh_from が無く、登録で観測した送信元も無い定義では、start を呼ばずに DEF_INVALID(D-0159 で enroll は呼ぶ)。
func TestSSHFromRequired(t *testing.T) {
	w := newPass(t)
	w.St.Def = []byte(strings.Replace(defPass, `"ssh_from":"192.0.2.10",`, "", 1))
	w.script()
	_, err := w.RestoreTest(ctx, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeDefInvalid)
	if len(w.AR.Calls) != 0 {
		t.Errorf("restore-test がアダプタを %d 回呼んだ(受信許可を決められないので start の前に止まる)", len(w.AR.Calls))
	}
	// 登録はアダプタに送信元を観測させる(D-0159)。読めなければ no_observed_from で DEF_INVALID。
	w.script(fake.Step{Outcome: fake.OutcomeError, ErrCode: "no_observed_from"})
	_, err = w.RestoreDestEnroll(ctx, contract.Call{Words: []string{"restore-dest", "enroll"}, RunID: rtRun})
	harness.ExpectCode(t, err, contract.CodeDefInvalid)
}

// F21: 隔離の検査の段で取り消されたら RUN_ABORTED。破棄は行い、proof は無い。
func TestRestoreTestCancelledMidway(t *testing.T) {
	w := newPass(t)
	c, cancel := context.WithCancel(ctx)
	inner := w.SSH.Handler
	w.SSH.Handler = func(cx context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
		cancel()
		return inner(cx, req)
	}
	_, err := w.RestoreTest(c, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeRunAborted)
	if ops := opsOf(w.env); len(ops) < 3 || ops[len(ops)-2] != "stop" {
		t.Errorf("取り消しの後に破棄していない: %v", ops)
	}
}

// F23: 登録は固定値を書いてから検査する。検査が落ちても固定値は残る(restore-dest check が同じ復元先を検査し直せる)。
func TestEnrollWritesFixedValuesBeforeProbe(t *testing.T) {
	e, _ := isoDest(t, "outbound", true)
	e.St.Def = []byte(defPass)
	_, err := e.RestoreDestEnroll(ctx, contract.Call{Words: []string{"restore-dest", "enroll"}, RunID: rtRun})
	harness.ExpectCode(t, err, contract.CodeRestoreIsolationFailed)
	if hk := string(e.St.Items["host_keys"]); !strings.Contains(hk, fpDest) && !strings.Contains(hk, "SHA256:abcDEF123") {
		t.Errorf("固定値が書かれていない: %s", hk)
	}
}

// F16: 台帳の更新は LedgerAppend の置き換えだけで行い、LedgerDelete を挟まない。
type delSpy struct {
	*fake.StateFiles
	deletes int
}

func (d *delSpy) LedgerDelete(c context.Context, h contract.StateLockHandle, id string) error {
	d.deletes++
	return d.StateFiles.LedgerDelete(c, h, id)
}

func TestLedgerPutDoesNotDelete(t *testing.T) {
	s := &delSpy{StateFiles: &fake.StateFiles{}}
	rec := ledgerRec{RecordID: "l-0123456789abcdef", Kind: "dest_session", RunID: rtRun, SessionID: rtRun, State: "creating"}
	if err := ledgerPut(ctx, s, rec); err != nil {
		t.Fatal(err)
	}
	rec.State = "created"
	if err := ledgerPut(ctx, s, rec); err != nil || s.deletes != 0 {
		t.Errorf("deletes=%d err=%v", s.deletes, err)
	}
}

// inspect が absent でも資源を返したら確かめられたことにしない。
func TestInspectAbsentWithResourcesIsUnverified(t *testing.T) {
	st := &fake.StateFiles{Ledger: []json.RawMessage{mkLedger(t, "r-20270227T000000Z-aaaa", "restore_test", "created", "0000000000000001")}}
	ar := fake.NewAdapterRunner()
	ar.Script("restore-dest", okStep(stopped), okStep(json.RawMessage(`{"resources":[{"type":"workdir","id":"w","session_id":"r-20270227T000000Z-aaaa"}],"state":"absent"}`)))
	_, un, err := (&RunCleanup{State: st, Adapters: ar}).Cleanup(ctx, "r-20270301T000000Z-eeee", "r-20270227T000000Z-aaaa")
	if err != nil || len(un) == 0 || !onlyState(st, "destroy_failed") {
		t.Errorf("un=%v err=%v", un, err)
	}
}

// error.code の写像: enroll の auth は RESTORE_ENROLL_MISMATCH(reason: auth)、ほかの操作の auth は RESTORE_HOST_DENIED。
func TestMapDestErrAuth(t *testing.T) {
	mk := func() error {
		return &contract.Error{Code: contract.CodeAdapterError, Class: "dependency", Detail: json.RawMessage(`{}`), Next: []contract.Next{},
			Adapter: &contract.AdapterError{Code: "auth", Detail: json.RawMessage(`{}`)}}
	}
	harness.ExpectCode(t, mapDestErr("enroll", mk(), ""), contract.CodeRestoreEnrollMismatch)
	harness.ExpectCode(t, mapDestErr("start", mk(), ""), contract.CodeRestoreHostDenied)
}
