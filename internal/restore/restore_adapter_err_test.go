package restore

// アダプタの失敗(Error.Adapter)の扱い: 作りかけの資源の片付けと、error.code の写像(spec 3.2.4、3.1.3)。

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func startFails(t *testing.T, ea *contract.AdapterError) (*env, error) {
	t.Helper()
	e := rtWorld(t)
	e.St.Items["host_keys"] = json.RawMessage(`[{"role":"restore_dest","host":"dest.example.invalid","host_key_fp":"` + fpDest + `","session_id":null}]`)
	e.AR.Script("restore-dest",
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"resources":[],"state":"absent"}`)},
		fake.Step{Outcome: fake.OutcomeError, ErrCode: ea.Code, ErrAdapter: ea},
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"done":true,"verified_by":"ssh","remaining":[]}`)},
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"resources":[],"state":"absent"}`)})
	_, err := e.RestoreTest(ctx, rtCall(false))
	return e, err
}

// start が作りかけの資源を error.resources[] に載せて失敗したら、その資源で stop と inspect を呼び、確かめられるまで台帳に残す。
func TestRestoreTestStartFailsWithResourcesTearsDown(t *testing.T) {
	ea := &contract.AdapterError{Code: "enroll_failed", Detail: json.RawMessage(`{}`), Resources: []contract.AdapterResource{{Type: "workdir", ID: rtRun}}}
	e, err := startFails(t, ea)
	harness.ExpectCode(t, err, contract.CodeRestoreEnrollMismatch)
	ops := (&passWorld{env: e}).adapterOps()
	if !slices.Equal(ops, []string{"inspect", "start", "stop", "inspect"}) {
		t.Fatalf("作りかけの資源があるのに片付けていない: %v", ops)
	}
	var q struct {
		Resources []resRef `json:"resources"`
	}
	_ = json.Unmarshal(e.AR.Calls[2].Req, &q)
	if len(q.Resources) != 1 || q.Resources[0].ID != rtRun {
		t.Errorf("stop の要求に作りかけの資源が無い: %s", e.AR.Calls[2].Req)
	}
	if len(e.St.Ledger) != 0 {
		t.Errorf("破棄を確かめたら台帳を消す: %d", len(e.St.Ledger))
	}
}

// 何も作っていない失敗(resources が空)は stop を呼ばず、台帳の記録を消して teardown_pending にしない。
func TestRestoreTestStartFailsWithoutResourcesNoStop(t *testing.T) {
	e, err := startFails(t, &contract.AdapterError{Code: "unreachable", Detail: json.RawMessage(`{"reason":"timeout"}`)})
	ce := harness.ExpectCode(t, err, contract.CodeStorageUnreachable)
	if string(ce.Detail) != `{"http_status":null,"reason":"timeout","target":"restore_host"}` {
		t.Errorf("detail: %s", ce.Detail)
	}
	if n := len(e.AR.Calls); n != 2 || len(e.St.Ledger) != 0 {
		t.Errorf("calls=%d ledger=%d", n, len(e.St.Ledger))
	}
}

// error.code の写像: hostkey_mismatch は seen_fp を error.seen_fp から、quota は RESTORE_HOST_DENIED、disk_short は needed_gb・free_gb を detail から。
func TestRestoreTestStartErrorMapping(t *testing.T) {
	cases := []struct {
		ea   *contract.AdapterError
		code contract.Code
	}{
		{&contract.AdapterError{Code: "hostkey_mismatch", Detail: json.RawMessage(`{}`), SeenFP: "SHA256:zzz"}, contract.CodeRestoreHostkeyMismatch},
		{&contract.AdapterError{Code: "quota", Detail: json.RawMessage(`{}`)}, contract.CodeRestoreHostDenied},
		{&contract.AdapterError{Code: "disk_short", Detail: json.RawMessage(`{"needed_gb":5,"free_gb":2}`)}, contract.CodeRestoreEnrollMismatch},
		{&contract.AdapterError{Code: "capacity", Detail: json.RawMessage(`{}`)}, contract.CodeStorageUnreachable},
	}
	for _, c := range cases {
		_, err := startFails(t, c.ea)
		ce := harness.ExpectCode(t, err, c.code)
		sch := "cli/detail.json#/$defs/" + string(c.code)
		if verr := contract.Validate(sch, ce.Detail); verr != nil {
			t.Errorf("%s: detail がスキーマに合わない: %v %s", c.ea.Code, verr, ce.Detail)
		}
		if c.ea.Code == "hostkey_mismatch" && !contains(string(ce.Detail), "SHA256:zzz") {
			t.Errorf("seen_fp を写していない: %s", ce.Detail)
		}
		if c.ea.Code == "disk_short" && !contains(string(ce.Detail), `"needed_gb":5`) {
			t.Errorf("needed_gb を写していない: %s", ce.Detail)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool { return indexOf(s, sub) >= 0 })()
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
