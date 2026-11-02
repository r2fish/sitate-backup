package restore

// restore-dest enroll / check の契約試験(spec 3.1.2、3.5「復元先の登録」、3.2.4)。
// 隔離の検査の ok の値の判定は contract_restore_iso_test.go(復元先の報告を SSH の偽物で返す)。ここは形と副作用。

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func destEnv(t *testing.T) *env {
	t.Helper()
	e, _ := isoDest(t, "", true)
	return e
}

// isolationOK は isolation[] が 3 つの検査(inbound・outbound・dns_name)を 1 回ずつ持ち、ok と code が整合することを確かめる。
func isolationOK(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var iso []struct {
		Name string  `json:"name"`
		OK   bool    `json:"ok"`
		Code *string `json:"code"`
	}
	if err := json.Unmarshal(raw, &iso); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, i := range iso {
		names = append(names, i.Name)
		if i.OK != (i.Code == nil) {
			t.Errorf("%s: ok=%v と code=%v が整合しない(通れば code は null、落ちれば原因コード)", i.Name, i.OK, i.Code)
		}
	}
	sort.Strings(names)
	if len(names) != 3 || names[0] != "dns_name" || names[1] != "inbound" || names[2] != "outbound" {
		t.Errorf("isolation[] は inbound・outbound・dns_name を 1 回ずつ: %v", names)
	}
}

// 導入時の restore-dest enroll: 復元先アダプタの enroll に定義の所在を渡し、固定した指紋を結果に返し、host_keys[] に書く。
func TestContractRestoreDestEnroll(t *testing.T) {
	e := destEnv(t)
	rep, err := e.RestoreDestEnroll(ctx, contract.Call{Words: []string{"restore-dest", "enroll"}, RunID: rtRun})
	harness.Shape(t, []string{"restore-dest", "enroll"}, rep, err)
	m := resultOf(t, rep, "restore_dest_enroll")
	wantRaw(t, m, "host_key_fp", `"SHA256:abcDEF123"`)
	wantRaw(t, m, "replaced", "false")
	isolationOK(t, m["isolation"])
	var req adReq
	if len(e.AR.Calls) == 0 {
		t.Fatal("復元先アダプタを呼んでいない")
	}
	_ = json.Unmarshal(e.AR.Calls[0].Req, &req)
	if e.AR.Calls[0].Kind != "restore-dest" || req.Op != "enroll" || req.Dest.At != destURL || req.Dest.KeyRef != "dest-key" {
		t.Errorf("最初の呼び出しが定義の復元先への enroll でない: %s %s", e.AR.Calls[0].Kind, e.AR.Calls[0].Req)
	}
	if hk := string(e.St.Items["host_keys"]); !strings.Contains(hk, "restore_dest") || !strings.Contains(hk, "SHA256:abcDEF123") {
		t.Errorf("導入時の登録は host_keys[] に role: restore_dest の指紋を書く: %s", hk)
	}
}

// --dry-run は何も書かない。
func TestContractRestoreDestEnrollDryRun(t *testing.T) {
	e := destEnv(t)
	rep, err := e.RestoreDestEnroll(ctx, contract.Call{Words: []string{"restore-dest", "enroll"}, RunID: rtRun, DryRun: true})
	harness.Shape(t, []string{"restore-dest", "enroll"}, rep, err)
	resultOf(t, rep, "restore_dest_enroll")
	for _, item := range []string{"host_keys", "restore_dest_enrollment"} {
		if v := e.St.Items[item]; len(v) != 0 && string(v) != "null" {
			t.Errorf("dry-run が %s を書いた: %s", item, v)
		}
	}
}

// 復元先アダプタの失敗は登録を失敗させ、host_keys[] に何も書かない。
func TestContractRestoreDestEnrollAdapterFails(t *testing.T) {
	e := newEnv()
	e.St.Def = []byte(defSSH)
	e.AR.Script("restore-dest", fake.Step{Outcome: fake.OutcomeError, ErrCode: "ADAPTER_FAILED"})
	_, err := e.RestoreDestEnroll(ctx, contract.Call{Words: []string{"restore-dest", "enroll"}, RunID: rtRun})
	harness.ExpectCode(t, err, contract.CodeAdapterError)
	if v := e.St.Items["host_keys"]; len(v) != 0 && string(v) != "null" {
		t.Errorf("失敗した登録が host_keys[] を書いた: %s", v)
	}
}

// restore-dest check は読み取りだけ: 登録の固定値を変えず、3 つの隔離の検査を返す(--session なしでは session_id・remaining・cleared を持たない)。
func TestContractRestoreDestCheck(t *testing.T) {
	e := destEnv(t)
	e.St.Items = map[string]json.RawMessage{"host_keys": json.RawMessage(`[{"role":"restore_dest","host":"dest.example.invalid","host_key_fp":"SHA256:abcDEF123","session_id":null}]`)}
	before := string(e.St.Items["host_keys"])
	rep, err := e.RestoreDestCheck(ctx, contract.Call{Words: []string{"restore-dest", "check"}, RunID: rtRun})
	harness.Shape(t, []string{"restore-dest", "check"}, rep, err)
	m := resultOf(t, rep, "restore_dest_check")
	isolationOK(t, m["isolation"])
	for _, k := range []string{"session_id", "remaining", "cleared"} {
		if _, ok := m[k]; ok {
			t.Errorf("--session が無いのに %s がある", k)
		}
	}
	if string(e.St.Items["host_keys"]) != before {
		t.Errorf("check が host_keys[] を変えた: %s", e.St.Items["host_keys"])
	}
}
