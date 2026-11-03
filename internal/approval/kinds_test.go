package approval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func proposeErr(e *env, op string, args map[string][]string) error {
	_, err := e.a.Propose(ctx, contract.Call{Words: []string{"propose"}, Positional: []string{op}, Args: args})
	return err
}

// 遅延の桁あふれ: 上限を超える hours は拒み、定義の値が範囲外でも既定へ落とさず誤りにする。
func TestDelayBounds(t *testing.T) {
	e := newEnv(t, true)
	for _, h := range []string{"5124096", "8761", "6", "0"} {
		want(t, "hours="+h, proposeErr(e, "delay-change", map[string][]string{"hours": {h}}), contract.CodeDefInvalid, "")
	}
	for _, h := range []string{"7", "8760"} {
		if err := proposeErr(e, "delay-change", map[string][]string{"hours": {h}}); err != nil {
			t.Errorf("hours=%s: %v", h, err)
		}
	}
	e.setDef(`{"approval_delay_hours":5124096}`)
	want(t, "定義の値が範囲外", proposeErr(e, "hold-release", nil), contract.CodeDefInvalid, "")
}

// 種別ごとの遅延の表(3.6): 遅延 0 の 11 個とそれ以外。
func TestDelayTable(t *testing.T) {
	e := newEnv(t, true)
	e.a.TestNotifier = &fakeProofs{}
	e.setDef(`{"restore_test":"monthly","notify":{"to":["a@x"]}}`)
	zero := map[string]map[string][]string{
		"storage-init":          {"bucket": {"b"}},
		"read-key-issue":        {"bucket": {"b"}},
		"restore-elsewhere":     {"to": {"ssh://h"}},
		"notify-add":            {"notify": {`{"to":["a@x","b@x"]}`}},
		"key-rotate-waive":      nil,
		"restore-test-increase": {"to": {"weekly"}},
		"storage-read-extra":    {"to": {"source:/d"}},
	}
	for op, args := range zero {
		if h := e.delayHoursOf(e.propose(op, args).ChangeID); h != 0 {
			t.Errorf("%s の遅延 %d", op, h)
		}
	}
	e.setDef(`{"schedule":"02:30"}`)
	e.withDef(`{"schedule":"03:00"}`)
	if h := e.delayHoursOf(e.propose("def-change", map[string][]string{"file": {"-"}}).ChangeID); h != 0 {
		t.Errorf("def-change の遅延 %d", h)
	}
	e.setDef(`{}`)
	delayed := map[string]map[string][]string{
		"restore-change":      {"restore": {`{"adapter":"ssh-host"}`}},
		"source-change":       {"source": {`{"host":"h","admin_user":"deploy"}`}},
		"notify-change":       {"notify": {`{"to":["z@x"]}`}},
		"key-rotate":          nil,
		"phrase-reissue":      nil,
		"delay-change":        {"hours": {"30"}},
		"approval-key-change": {"pubkey": {"AAAAC3NzaC1lZDI1NTE5AAAAIGbxOGV5NxOb0yiCwZ3t1x1ZnQpOmhL3dw0iBZC4MoXC"}},
		"hold-release":        nil,
		"adapter-add":         {"dir": {"/x"}},
		"checks-weaken":       {"file": {"-"}},
		"def-change-unlisted": {"file": {"-"}},
	}
	for op, args := range delayed {
		if op == "checks-weaken" {
			e.withDef(`{"checks":{"e":{"enabled":false}}}`)
		}
		if op == "def-change-unlisted" {
			e.withDef(`{"x_unlisted":1}`)
		}
		if err := proposeErr(e, op, args); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if h := e.delayHoursOf(e.propose(op, args).ChangeID); h != 24 {
			t.Errorf("%s の遅延 %d", op, h)
		}
	}
	e = newEnv(t, true)
	e.setDef(`{"approval_delay_hours":48}`)
	if h := e.delayHoursOf(e.propose("hold-release", nil).ChangeID); h != 48 {
		t.Errorf("定義の遅延時間: %d", h)
	}
}

type fakeProofs struct{ out map[string]string }

func (f *fakeProofs) Test(_ ctxT, to []string) (map[string]string, error) {
	if f.out != nil {
		return f.out, nil
	}
	m := map[string]string{}
	for _, t := range to {
		m[t] = "AAAAAAAAAAAAAAAAAAAAAA==|sha256:" + strings.Repeat("0", 64)
	}
	return m, nil
}

// 定義を受ける 3 種別は、他の種別の項目・必須項目・表に無い項目が混じる差分を DEF_CHANGE_MIXED にする(A6 (12))。
func TestDefDiffMixed(t *testing.T) {
	base := `{"schedule":"02:30","approval_delay_hours":24,"restore_test":"monthly","notify":{"to":["a@x"]},"storage":[{"bucket":"b","region":"r"}],"restore":{"dest":{"at":"ssh://u@h:22"}},"source":{"host":"h"},"auto_hold_days":14}`
	cases := map[string]string{
		"通知先":     `"notify":{"to":["evil@x"]}`,
		"遅延":      `"approval_delay_hours":7`,
		"保管先":     `"storage":[{"bucket":"other","region":"r"}]`,
		"復元先":     `"restore":{"dest":{"at":"ssh://u@evil:22"}}`,
		"復元テスト":   `"restore_test":"weekly"`,
		"元サーバー":   `"source":{"host":"evil"}`,
		"表に無い項目":  `"x_new":1`,
		"チェックの緩和": `"checks":{"e":{"enabled":false}}`,
		"自動停止の延長": `"auto_hold_days":30`,
	}
	for name, change := range cases {
		e := newEnv(t, true)
		e.setDef(base)
		doc := strings.Replace(base, "{", "{"+change+",", 1)
		if strings.HasPrefix(change, `"approval_delay_hours"`) || strings.HasPrefix(change, `"restore_test"`) || strings.HasPrefix(change, `"auto_hold_days"`) ||
			strings.HasPrefix(change, `"notify"`) || strings.HasPrefix(change, `"storage"`) || strings.HasPrefix(change, `"restore"`) || strings.HasPrefix(change, `"source"`) {
			doc = strings.Replace(base, change[:strings.Index(change, ":")+1], change[:strings.Index(change, ":")+1]+"", 1)
			doc = overlay(base, change)
		}
		e.withDef(doc)
		want(t, "def-change "+name, proposeErr(e, "def-change", map[string][]string{"file": {"-"}}), contract.CodeDefChangeMixed, "")
	}
	e := newEnv(t, true)
	e.setDef(base)
	e.withDef(overlay(base, `"notify":{"to":["evil@x"]}`))
	want(t, "checks-weaken に通知先", proposeErr(e, "checks-weaken", map[string][]string{"file": {"-"}}), contract.CodeDefChangeMixed, "")
	want(t, "unlisted に通知先", proposeErr(e, "def-change-unlisted", map[string][]string{"file": {"-"}}), contract.CodeDefChangeMixed, "")
	e.withDef(overlay(base, `"schedule":"04:00"`))
	if err := proposeErr(e, "def-change", map[string][]string{"file": {"-"}}); err != nil {
		t.Errorf("schedule は def-change: %v", err)
	}
	e.withDef(overlay(base, `"checks":{"e":{"params":{"entropy_ratio_fail":0.4}}}`))
	if err := proposeErr(e, "def-change", map[string][]string{"file": {"-"}}); err != nil {
		t.Errorf("閾値を厳しくするのは def-change: %v", err)
	}
	e.withDef(overlay(base, `"checks":{"e":{"enabled":false}}`))
	if err := proposeErr(e, "checks-weaken", map[string][]string{"file": {"-"}}); err != nil {
		t.Errorf("緩和は checks-weaken: %v", err)
	}
}

// overlay は base の最上位のキーを change の 1 キーで置き換える(無ければ足す)。
func overlay(base, change string) string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal([]byte(base), &m)
	var c map[string]json.RawMessage
	_ = json.Unmarshal([]byte("{"+change+"}"), &c)
	for k, v := range c {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestKindContentMismatch(t *testing.T) {
	e := newEnv(t, true)
	e.a.TestNotifier = &fakeProofs{}
	e.setDef(`{"restore_test":"weekly","notify":{"to":["a@x","b@x"]}}`)
	want(t, "notify-add で宛先を消す", proposeErr(e, "notify-add", map[string][]string{"notify": {`{"to":["a@x"]}`}}), contract.CodeArgInvalid, "mismatch")
	for _, to := range []string{"off", "never;rm", "weekly"} {
		want(t, "increase "+to, proposeErr(e, "restore-test-increase", map[string][]string{"to": {to}}), contract.CodeArgInvalid, "")
	}
	want(t, "reduce で増やす", proposeErr(e, "restore-test-reduce", map[string][]string{"to": {"weekly"}}), contract.CodeArgInvalid, "mismatch")
	if err := proposeErr(e, "restore-test-reduce", map[string][]string{"to": {"monthly"}}); err != nil {
		t.Errorf("reduce: %v", err)
	}
	// 復元の宛先: --to 必須、バックアップサーバー自身は拒む。
	for _, op := range []string{"restore-elsewhere", "storage-read-extra"} {
		want(t, op+" --to なし", proposeErr(e, op, nil), contract.CodeArgInvalid, "missing")
		want(t, op+" --to ローカル", proposeErr(e, op, map[string][]string{"to": {"/var/lib/x"}}), contract.CodeGateDenied, "local_restore")
	}
	d, _ := (&Approval{}).Gate(ctx, "restore --paths [\"a\"]")
	if d.Allowed {
		t.Error("--to の無い restore が gate を通る")
	}
}
