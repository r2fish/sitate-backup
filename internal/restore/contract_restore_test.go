package restore

// 契約試験(作業指示書 3.2): 偽物(U0 の fake)を差し込んだ入口の外形と、restore-dest の登録の状態の書き込み(5.6 の完了条件 11)。
// 骨格は NOT_IMPLEMENTED で落ち、nil を返すだけの実装は値の比較で落ち、仕様どおりの実装は panic しない。WP は骨格を実装し、この試験は変えない。
// 合格記録 sitate.restore-proof/v1 の署名(Crypto)の試験ベクトルは internal/format の契約試験(Ed25519)と testdata/data/valid の記録の例が持つ。

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var ctx = context.Background()

// env は偽物を差し込んだ Restore と、試験が操作する偽物への手がかり。
type env struct {
	*Restore
	St   *fake.StateFiles
	Clk  *fake.ManualClock
	Core *fake.Core
	AR   *fake.AdapterRunner
	Rec  *fake.Recorder
}

func newEnv() *env {
	store, clk, core := fake.NewTestStore(fake.RoleRead)
	st := &fake.StateFiles{}
	ar := fake.NewAdapterRunner()
	rec := &fake.Recorder{}
	r := &Restore{Store: &strictStore{ObjectStore: store}, State: st, Adapters: ar, Codec: fake.Codec{Size: 4}, KeyS: fake.KeyService{},
		Crypto: fake.Crypto{}, Recorder: rec, Clock: &fake.Clock{T: fake.StartTime}}
	return &env{Restore: r, St: st, Clk: clk, Core: core, AR: ar, Rec: rec}
}

func newRestore() (*Restore, *fake.StateFiles) {
	e := newEnv()
	return e.Restore, e.St
}

func rvArgs(t *testing.T, exp, act contract.RestoreVerifyInput) contract.Call {
	t.Helper()
	e, _ := json.Marshal(exp)
	a, _ := json.Marshal(act)
	return contract.Call{Words: []string{"restore-verify"}, Args: map[string][]string{contract.ArgBackup: {refA}, contract.ArgRestoreVerifyExpected: {string(e)}, contract.ArgRestoreVerifyActual: {string(a)}}}
}

func rvFile(ref string, size int64, h string) contract.RestoreVerifyFile {
	return contract.RestoreVerifyFile{EntryRef: ref, Size: size, Hash: "blake3:" + strings.Repeat(h, 64)}
}

// restore-verify: 件数・大きさ・内容ハッシュを期待(マニフェスト)と復元先の報告で照合する。
func TestContractRestoreVerify(t *testing.T) {
	f1, f2, f3 := "f:"+strings.Repeat("1", 32), "f:"+strings.Repeat("2", 32), "d:t1:custom"
	exp := contract.RestoreVerifyInput{Files: []contract.RestoreVerifyFile{rvFile(f1, 10, "a"), rvFile(f2, 20, "b"), rvFile(f3, 5, "c")}}
	type mm struct{ EntryRef, Reason string }
	cases := []struct {
		name       string
		act        contract.RestoreVerifyInput
		ok         bool
		okN, failN int
		want       []mm
	}{
		{"全部一致(順序は問わない)", contract.RestoreVerifyInput{Files: []contract.RestoreVerifyFile{rvFile(f3, 5, "c"), rvFile(f1, 10, "a"), rvFile(f2, 20, "b")}}, true, 3, 0, nil},
		{"欠け", contract.RestoreVerifyInput{Files: []contract.RestoreVerifyFile{rvFile(f1, 10, "a"), rvFile(f3, 5, "c")}}, false, 2, 1, []mm{{f2, contract.RestoreVerifyMissing}}},
		{"余分", contract.RestoreVerifyInput{Files: []contract.RestoreVerifyFile{rvFile(f1, 10, "a"), rvFile(f2, 20, "b"), rvFile(f3, 5, "c"), rvFile("f:"+strings.Repeat("9", 32), 1, "d")}}, false, 3, 1, []mm{{"f:" + strings.Repeat("9", 32), contract.RestoreVerifyUnexpected}}},
		{"大きさ違い", contract.RestoreVerifyInput{Files: []contract.RestoreVerifyFile{rvFile(f1, 11, "a"), rvFile(f2, 20, "b"), rvFile(f3, 5, "c")}}, false, 2, 1, []mm{{f1, contract.RestoreVerifySize}}},
		{"内容ハッシュ違い", contract.RestoreVerifyInput{Files: []contract.RestoreVerifyFile{rvFile(f1, 10, "a"), rvFile(f2, 20, "e"), rvFile(f3, 5, "c")}}, false, 2, 1, []mm{{f2, contract.RestoreVerifyHash}}},
	}
	for _, c := range cases {
		r, _ := newRestore()
		rep, err := r.RestoreVerify(ctx, rvArgs(t, exp, c.act))
		harness.Implemented(t, err)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		m := resultOf(t, rep, "restore_verify")
		var res struct {
			OK                   bool
			Files, Bytes         int64
			FilesOK, FilesFailed int `json:"-"`
			Mismatches           []mm
		}
		_ = json.Unmarshal(rep.Result, &res)
		wantRaw(t, m, "ok", map[bool]string{true: "true", false: "false"}[c.ok])
		wantRaw(t, m, "files", "3")
		wantRaw(t, m, "bytes", "35")
		wantRaw(t, m, "files_ok", strconv.Itoa(c.okN))
		wantRaw(t, m, "files_failed", strconv.Itoa(c.failN))
		var got []mm
		var raw []struct {
			EntryRef string `json:"entry_ref"`
			Reason   string `json:"reason"`
		}
		_ = json.Unmarshal(m["mismatches"], &raw)
		for _, x := range raw {
			got = append(got, mm{x.EntryRef, x.Reason})
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: mismatches = %v (期待 %v)", c.name, got, c.want)
		}
		if c.want == nil && string(m["mismatches"]) != "[]" {
			t.Errorf("%s: 不一致が無いときは空の配列: %s", c.name, m["mismatches"])
		}
	}
	// 入力が読めなければ ARG_INVALID。
	r, _ := newRestore()
	_, err := r.RestoreVerify(ctx, contract.Call{Words: []string{"restore-verify"}, Args: map[string][]string{contract.ArgRestoreVerifyExpected: {"not json"}}})
	harness.ExpectCode(t, err, contract.CodeArgInvalid)
}

// 後のイテレーションの機能(イテレーション 2)。
func TestContractRestoreJournalVerify(t *testing.T) {
	harness.Later(t, 2, "Restore.JournalVerify")
}

// 取り消された文脈を受けた復元テストは復元先へ何も流さずに RUN_ABORTED。
func TestContractRestoreTestAborted(t *testing.T) {
	e := rtWorld(t)
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, err := e.RestoreTest(c, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeRunAborted)
	if n := len(e.AR.Calls); n != 0 {
		t.Errorf("取り消された実行が復元先アダプタを %d 回呼んだ", n)
	}
}

func TestContractRunCleanup(t *testing.T) {
	c, u, err := (&RunCleanup{State: &fake.StateFiles{}, Adapters: fake.NewAdapterRunner()}).Cleanup(ctx, "r-1", "r-0")
	harness.Implemented(t, err)
	if err != nil || c == nil || u == nil {
		t.Errorf("片付ける資源が無くても一覧は配列: %v %v %v", c, u, err)
	}
}

// 台帳の記録の片付けは台帳の所在(dest)で行う(spec 3.2.4 の「後片付けの要求の所在」)。RunCleanup は定義ファイルを持たないので、
// restore-change で復元先が替わった後でも、旧復元先の at・key_ref・session_id・host_key_fp・resources[] を台帳の値のまま破棄の要求に入れる。
func TestContractRunCleanupUsesLedgerDest(t *testing.T) {
	const oldAt = "ssh://restore@old-dest.example.invalid:22"
	rec := map[string]any{}
	if err := json.Unmarshal(ledgerSession(t, "r-20270227T000000Z-aaaa", "r-20270227T000000Z-aaaa", true), &rec); err != nil {
		t.Fatal(err)
	}
	rec["state"], rec["teardown"] = "created", nil
	rec["dest"] = map[string]any{"at": oldAt, "key_ref": "old-dest-key"}
	b, _ := json.Marshal(rec)
	if err := contract.Validate("data/state.json#/$defs/ledger", b); err != nil {
		t.Fatalf("準備の台帳の記録が状態のスキーマに合わない: %v", err)
	}
	st := &fake.StateFiles{Ledger: []json.RawMessage{b}}
	ar := fake.NewAdapterRunner()
	for range 4 {
		ar.Script("restore-dest", fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{}`)})
	}
	_, _, err := (&RunCleanup{State: st, Adapters: ar}).Cleanup(ctx, "r-20270228T000000Z-bbbb", "r-20270227T000000Z-aaaa")
	harness.Implemented(t, err)
	n := 0
	for _, c := range ar.Calls {
		var q struct {
			Op        string `json:"op"`
			SessionID string `json:"session_id"`
			Dest      *struct {
				At     string `json:"at"`
				KeyRef string `json:"key_ref"`
			} `json:"dest"`
			HostKeyFP string                     `json:"host_key_fp"`
			Resources []contract.AdapterResource `json:"resources"`
		}
		_ = json.Unmarshal(c.Req, &q)
		if q.Op != "destroy" && q.Op != "stop" {
			continue
		}
		n++
		if q.Dest == nil || q.Dest.At != oldAt || q.Dest.KeyRef != "old-dest-key" || q.SessionID != "r-20270227T000000Z-aaaa" || q.HostKeyFP != "SHA256:abcDEF123" ||
			len(q.Resources) != 1 || q.Resources[0].ID != "r-20270227T000000Z-aaaa" {
			t.Errorf("破棄の要求が台帳の値でない: %s", c.Req)
		}
	}
	if n == 0 {
		t.Errorf("台帳に残る created の資源に destroy / stop を呼んでいない: %d 回の呼び出し", len(ar.Calls))
	}
}

func enrollVector(t *testing.T) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(harness.VectorPath("adapter/valid/restore-dest-result-enroll.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func enrollWith(t *testing.T, write bool) (contract.EnrollResult, *fake.StateFiles, error) {
	t.Helper()
	r, st := newRestore()
	r.Adapters.(*fake.AdapterRunner).Script("restore-dest", fake.Step{Outcome: fake.OutcomeSuccess, Result: enrollVector(t)})
	e := &RestoreDestEnroller{Store: r.Store, State: st, Adapters: r.Adapters, Codec: r.Codec, KeyS: r.KeyS, Crypto: r.Crypto, Recorder: r.Recorder, Clock: r.Clock}
	res, err := e.Enroll(ctx, contract.EnrollInput{Kind: "restore-dest", Section: json.RawMessage(`{"adapter":"ssh-host","dest":{"at":"ssh://restore@dest.example.invalid:22","key_ref":"dest-key"}}`), HostKeyFP: "SHA256:abcDEF123", WriteState: write})
	return res, st, err
}

// WriteState が真(導入時)なら host_keys[] の role: restore_dest と復元先の登録の記録を書き、
// 偽(apply の enroll の要素)なら書かずに、define が書くための登録の結果(restore_dest_enrollment)を返す。
func TestContractRestoreDestEnrollerWriteState(t *testing.T) {
	res, st, err := enrollWith(t, true)
	harness.Implemented(t, err)
	if err != nil || len(res.RestoreDestEnroll) == 0 {
		t.Fatalf("登録の結果: %+v %v", res, err)
	}
	var hks []map[string]any
	if e := json.Unmarshal(st.Items["host_keys"], &hks); e != nil || len(hks) != 1 {
		t.Fatalf("WriteState=真: host_keys[] に 1 件が書かれていない: %s", st.Items["host_keys"])
	}
	hkb, _ := json.Marshal(hks[0])
	if e := contract.Validate("data/state.json#/$defs/host_key", hkb); e != nil || hks[0]["role"] != "restore_dest" || hks[0]["host_key_fp"] != "SHA256:abcDEF123" {
		t.Errorf("host_keys[] の要素が role: restore_dest・登録した指紋でない: %s (%v)", hkb, e)
	}
	if got := st.Items["restore_dest_enrollment"]; len(got) == 0 || string(got) == "null" || string(got) != string(res.RestoreDestEnroll) {
		t.Errorf("WriteState=真: 書かれた登録の記録 %s が返した登録の結果 %s と違う", got, res.RestoreDestEnroll)
	}
	res, st, err = enrollWith(t, false)
	if err != nil || len(res.RestoreDestEnroll) == 0 || string(res.RestoreDestEnroll) == "null" {
		t.Fatalf("登録の結果(書かない): %+v %v", res, err)
	}
	for _, item := range []string{"host_keys", "restore_dest_enrollment"} {
		if v, ok := st.Items[item]; ok && len(v) != 0 && string(v) != "null" {
			t.Errorf("WriteState=偽: %s を書いてはいけない(define が書く): %s", item, v)
		}
	}
}

func TestContractRestoreDestEnrollerKind(t *testing.T) {
	r, _ := newRestore()
	e := &RestoreDestEnroller{State: r.State, Adapters: r.Adapters}
	_, err := e.Enroll(ctx, contract.EnrollInput{Kind: "source"})
	harness.Implemented(t, err)
	if err == nil {
		t.Error("kind が restore-dest でない登録が成功した")
	}
}
