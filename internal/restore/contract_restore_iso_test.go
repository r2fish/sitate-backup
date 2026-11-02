package restore

// restore-dest enroll / check の隔離の検査の値の契約試験(spec 3.5 の表の 5、3.1.2 `restore-dest check`、3.1.3 RESTORE_ISOLATION_FAILED)。
// 判定する内容: 復元先への SSH(固定した指紋)で検査を実行し、isolation[] の各項目の ok を復元先の報告のとおりに返す(落ちた項目だけ ok: false、code: RESTORE_ISOLATION_FAILED、
// 通った項目は ok: true、code: null)。不合格を誤り(RESTORE_ISOLATION_FAILED, detail.failed[])で返す実装も認める。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func isoDest(t *testing.T, bad string, enroll bool) (*env, *fake.SSH) {
	t.Helper()
	e := newEnv()
	e.St.Def = []byte(defPass)
	if enroll {
		e.AR.Script("restore-dest", fake.Step{Outcome: fake.OutcomeSuccess, Result: enrollVector(t)})
	} else {
		e.St.Items = map[string]json.RawMessage{"host_keys": json.RawMessage(`[{"role":"restore_dest","host":"dest.example.invalid","host_key_fp":"` + fpDest + `","session_id":null}]`)}
	}
	ssh := &fake.SSH{FP: fpDest}
	ssh.Handler = func(_ context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
		iso := []contract.IsolationProbe{{Name: "inbound", OK: true}, {Name: "outbound", OK: true}, {Name: "dns_name", OK: true}}
		for i := range iso {
			iso[i].OK = iso[i].Name != bad
		}
		b, _ := json.Marshal(contract.DestProbeOut{Isolation: iso})
		_, _ = req.Stdout.Write(b)
		return contract.SSHResult{SeenFP: fpDest}, nil
	}
	e.Restore.SSH = ssh
	e.Restore.Secrets = &fake.SecretStore{M: map[string]json.RawMessage{contract.RestoreKeyRef: sshSecret()}}
	return e, ssh
}

// isoValues は結果の isolation[] を name → (ok, code) に直す。誤りで返ったときは failed[] から同じ形を作る。
func isoValues(t *testing.T, rep contract.Reply, err error) map[string][2]any {
	t.Helper()
	out := map[string][2]any{}
	if err != nil {
		ce := harness.ExpectCode(t, err, contract.CodeRestoreIsolationFailed)
		var d struct {
			Failed []string `json:"failed"`
		}
		_ = json.Unmarshal(ce.Detail, &d)
		for _, n := range []string{"inbound", "outbound", "dns_name"} {
			out[n] = [2]any{true, nil}
		}
		for _, n := range d.Failed {
			out[n] = [2]any{false, string(contract.CodeRestoreIsolationFailed)}
		}
		return out
	}
	var res struct {
		Isolation []struct {
			Name string  `json:"name"`
			OK   bool    `json:"ok"`
			Code *string `json:"code"`
		} `json:"isolation"`
	}
	harness.Result(t, rep, &res)
	for _, i := range res.Isolation {
		var c any
		if i.Code != nil {
			c = *i.Code
		}
		out[i.Name] = [2]any{i.OK, c}
	}
	return out
}

func wantIso(t *testing.T, got map[string][2]any, bad string) {
	t.Helper()
	for _, n := range []string{"inbound", "outbound", "dns_name"} {
		want := [2]any{true, nil}
		if n == bad {
			want = [2]any{false, string(contract.CodeRestoreIsolationFailed)}
		}
		if got[n] != want {
			t.Errorf("bad=%q %s: (ok, code) = %v, want %v", bad, n, got[n], want)
		}
	}
}

func TestContractRestoreDestCheckIsolationValues(t *testing.T) {
	for _, bad := range []string{"", "inbound", "outbound", "dns_name"} {
		e, ssh := isoDest(t, bad, false)
		rep, err := e.RestoreDestCheck(ctx, contract.Call{Words: []string{"restore-dest", "check"}, RunID: rtRun})
		harness.Implemented(t, err)
		if bad == "" && err != nil {
			t.Fatalf("全項目合格なのに誤り: %v", err)
		}
		if err != nil {
			var ce *contract.Error
			if !errors.As(err, &ce) {
				t.Fatalf("型付きの誤りでない: %v", err)
			}
		}
		wantIso(t, isoValues(t, rep, err), bad)
		if len(ssh.Recorded()) == 0 {
			t.Errorf("bad=%q: 復元先へ接続して検査していない", bad)
		}
	}
}

func TestContractRestoreDestEnrollIsolationValues(t *testing.T) {
	for _, bad := range []string{"", "inbound", "outbound", "dns_name"} {
		e, ssh := isoDest(t, bad, true)
		rep, err := e.RestoreDestEnroll(ctx, contract.Call{Words: []string{"restore-dest", "enroll"}, RunID: rtRun})
		harness.Implemented(t, err)
		if bad == "" && err != nil {
			t.Fatalf("全項目合格なのに誤り: %v", err)
		}
		wantIso(t, isoValues(t, rep, err), bad)
		if len(ssh.Recorded()) == 0 {
			t.Errorf("bad=%q: 復元先へ接続して検査していない", bad)
		}
	}
}
