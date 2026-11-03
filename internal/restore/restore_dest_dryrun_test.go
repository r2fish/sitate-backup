package restore

// D-0165: restore-dest enroll --dry-run は定義の無い状態で引数(--dest・--adapter・--admin-ref)から動く(導入の手順 3)。
// アダプタの enroll の dry_run だけを呼び、鍵も固定値も登録の記録も置かない。拒否と前提の欠けは result.plan.errors[] に載る。

import (
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func dryDestCall() contract.Call {
	return contract.Call{Words: []string{"restore-dest", "enroll"}, RunID: rtRun, DryRun: true,
		Args:    map[string][]string{"dest": {"ssh://admin@dest.example.invalid:22"}, "adapter": {"ssh-host"}, "admin-ref": {"admin"}},
		Secrets: contract.SecretInput{"admin": json.RawMessage(`"PRIVATE-KEY-TEXT"`)}}
}

func dryDestEnv(step fake.Step) *env {
	e := newEnv() // 定義も候補も無い
	e.AR.Script("restore-dest", step)
	e.Restore.Secrets = &fake.SecretStore{M: map[string]json.RawMessage{}}
	return e
}

func planErrCodes(t *testing.T, rep contract.Reply) (codes []string, res map[string]json.RawMessage) {
	t.Helper()
	if err := json.Unmarshal(rep.Result, &res); err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(res["plan"], &plan); err != nil || plan.Errors == nil {
		t.Fatalf("result.plan.errors が無い: %s", rep.Result)
	}
	for _, x := range plan.Errors {
		codes = append(codes, x.Code)
	}
	return
}

func TestRestoreDestEnrollDryRunWithoutDefinition(t *testing.T) {
	good := json.RawMessage(`{"host_key_fp":"SHA256:abcDEF123","user":"sitate-restore","observed_from":"203.0.113.9","prereq":[{"name":"subuid","ok":true}]}`)
	missing := json.RawMessage(`{"host_key_fp":"SHA256:abcDEF123","user":"sitate-restore","observed_from":null,"prereq":[{"name":"subuid","ok":false}]}`)
	cases := map[string]struct {
		step fake.Step
		want []string
	}{
		"合格":      {fake.Step{Outcome: fake.OutcomeSuccess, Result: good}, nil},
		"前提の欠け":   {fake.Step{Outcome: fake.OutcomeSuccess, Result: missing}, []string{"RESTORE_ENROLL_MISMATCH"}},
		"資格情報の拒否": {fake.Step{Outcome: fake.OutcomeError, ErrCode: "auth"}, []string{"RESTORE_ENROLL_MISMATCH"}},
	}
	for name, c := range cases {
		e := dryDestEnv(c.step)
		rep, err := e.RestoreDestEnroll(ctx, dryDestCall())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		harness.Shape(t, []string{"restore-dest", "enroll"}, rep, err)
		codes, res := planErrCodes(t, rep)
		if len(codes) != len(c.want) || (len(codes) == 1 && codes[0] != c.want[0]) {
			t.Errorf("%s: plan.errors = %v", name, codes)
		}
		if name == "合格" && string(res["host_key_fp"]) != `"SHA256:abcDEF123"` {
			t.Errorf("host_key_fp = %s", res["host_key_fp"])
		}
		if len(e.AR.Calls) != 1 {
			t.Fatalf("%s: アダプタ呼び出し %d", name, len(e.AR.Calls))
		}
		var req map[string]any
		_ = json.Unmarshal(e.AR.Calls[0].Req, &req)
		if req["op"] != "enroll" || req["dry_run"] != true {
			t.Errorf("%s: 要求 %s", name, e.AR.Calls[0].Req)
		}
		if _, has := req["authorized_key"]; has {
			t.Errorf("%s: dry-run が復元先の鍵を作って渡した: %s", name, e.AR.Calls[0].Req)
		}
		if len(e.St.Items) != 0 {
			t.Errorf("%s: 状態が変わった: %v", name, e.St.Items)
		}
		if m := e.Restore.Secrets.(*fake.SecretStore).M; len(m) != 0 {
			t.Errorf("%s: 秘密ファイルに鍵を置いた", name)
		}
	}
}
