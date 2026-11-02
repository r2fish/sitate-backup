package approval

import (
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func gate(t *testing.T, line string) contract.GateDecision {
	t.Helper()
	d, err := (&Approval{}).Gate(ctx, line)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// 引数の分割・空白・繰り返し・値に隠した引数で拒否を抜けられない。
var evasionCases = map[string]string{
	"version\t--dry-run":                   "lexical",
	"version\n--dry-run":                   "lexical",
	" version":                             "lexical",
	"version ":                             "lexical",
	"version\x00":                          "lexical",
	"version \xff":                         "lexical",
	"version  --dry-run":                   "lexical",
	"VERSION":                              "not_allowed",
	"version extra":                        "not_allowed",
	"gate version":                         "not_allowed",
	"bootstrap":                            "not_allowed",
	"version --dry-run --invoked-by":       "invoked_by",
	"def validate --file - --invoked-by x": "invoked_by",
	"init-storage --endpoint --invoked-by": "invoked_by",
	"restore --to restore-dest:/d --relay": "relay",
	"restore --to restore-dest:/d --endpoint --relay": "relay",
	"restore --to /var/x":                             "local_restore",
	"restore --to ../x":                               "local_restore",
	"restore --to  restore-dest:/d":                   "lexical",
	"restore --to restore-dest:/d --to /var/z":        "duplicate",
	"restore --to /var/z --to restore-dest:/d":        "local_restore",
	"restore --to file:///var/z":                      "local_restore",
	"restore --to Restore-dest:/d":                    "local_restore",
	"def validate --file x":                           "file_path",
	"def validate --file ./x":                         "file_path",
	"propose seal --file /etc/passwd":                 "file_path",
	"propose def-change --file x":                     "file_path",
	"propose def-change --file - --file -":            "duplicate",
	"propose --file -":                                "not_allowed",
	"propose nosuch":                                  "not_allowed",
	"propose seal extra --file -":                     "not_allowed",
	"version --dry-run=1":                             "flag_form",
	"version --dry":                                   "flag_form",
	"version -d":                                      "flag_form",
	"version --":                                      "not_allowed",
	"version --nosuch":                                "not_allowed",
	"version --secret-fd 3":                           "secret_fd",
	"backup --secret-fd 3":                            "secret_fd",
	"init --secret-fd":                                "secret_fd",
	"init --secret-fd x":                              "secret_fd",
	"init --secret-fd -1":                             "secret_fd",
	"init --secret-fd 3 --secret-fd 4":                "duplicate",
	"init --secret-fd=3":                              "flag_form",
	"init-storage --admin-ref A":                      "not_allowed",
	"init-storage --admin-ref a;rm":                   "not_allowed",
	"init-storage --admin-ref $(x)":                   "not_allowed",
	"init --adopt --adopt":                            "duplicate",
	"init --approval-pubkey a$b":                      "not_allowed",
	"init-storage --bucket -x":                        "not_allowed",
	"init-storage --bucket --reissue":                 "not_allowed",
	"secrets set":                                     "not_allowed",
	"secrets set A!":                                  "not_allowed",
	"secrets set a b":                                 "not_allowed",
	"kit confirm --copies 3":                          "not_allowed",
	"version --run-id":                                "not_allowed",
}

func TestGateDeniesEvasions(t *testing.T) {
	for line, reason := range evasionCases {
		d := gate(t, line)
		if d.Allowed || d.Denied == nil {
			t.Errorf("通った: %q", line)
			continue
		}
		var det struct{ Reason string }
		_ = jsonUnmarshalDetail(d.Denied.Detail, &det)
		if det.Reason != reason {
			t.Errorf("%q: reason %q want %q", line, det.Reason, reason)
		}
		if strings.Contains(string(d.Denied.Detail), ";rm") {
			t.Errorf("detail に語が入った: %s", d.Denied.Detail)
		}
	}
}

func TestGateAllowsAndRewrites(t *testing.T) {
	for line, argv := range map[string]string{
		`restore --to restore-dest:/d --paths ["a"]`: `restore --to restore-dest:/d --paths ["a"]`,
		"restore --to ssh://u@h:22/d":                "restore --to ssh://u@h:22/d",
		"restore --to source:/d":                     "restore --to source:/d",
		"def validate --file -":                      "def validate --file -",
		"propose seal --file -":                      "propose seal --file -",
		"secrets set my-ref --stdin":                 "secrets set my-ref --stdin",
		"apply --secret-fd 5 --id c-1":               "apply --id c-1",
		"init --secret-fd 12":                        "init",
	} {
		d := gate(t, line)
		if !d.Allowed || strings.Join(d.Argv, " ") != argv {
			t.Errorf("%q: %+v", line, d)
		}
		if strings.Contains(line, "secret-fd") != (d.SecretOut == "stdout-first-record") {
			t.Errorf("%q: 秘密の出力先 %q", line, d.SecretOut)
		}
	}
	if w := GateCommandWords("restore fetch --session x --bad"); strings.Join(w, " ") != "restore fetch" {
		t.Errorf("command: %v", w)
	}
	if w := GateCommandWords("nosuch"); strings.Join(w, " ") != "gate" {
		t.Errorf("command: %v", w)
	}
}

func TestChecksWeaken(t *testing.T) {
	for name, c := range map[string]struct{ before, after string }{
		"無効化":                    {`{}`, `{"checks":{"entropy":{"enabled":false}}}`},
		"サンプルを増やす(反例 a)":         {`{"checks":{"e":{"params":{"entropy_sample_bytes":1024}}}}`, `{"checks":{"e":{"params":{"entropy_sample_bytes":10240}}}}`},
		"最小サイズを下げる(反例 b)":        {`{"checks":{"e":{"params":{"entropy_min_file_bytes":2000}}}}`, `{"checks":{"e":{"params":{"entropy_min_file_bytes":1000}}}}`},
		"厳しい値を書いたキーを消す":          {`{"checks":{"e":{"params":{"entropy_ratio_fail":0.4}}}}`, `{"checks":{"e":{}}}`},
		"閾値を書かない定義へ定数より緩い値":      {`{}`, `{"checks":{"e":{"params":{"entropy_high_bits":8}}}}`},
		"auto_hold_days の延長":     {`{"auto_hold_days":14}`, `{"auto_hold_days":30}`},
		"サンプルを定数から変える(どちらの向きでも)": {`{}`, `{"checks":{"e":{"params":{"entropy_sample_bytes":1}}}}`},
	} {
		if w, err := ChecksWeaken([]byte(c.before), []byte(c.after)); err != nil || !w {
			t.Errorf("%s: 緩和と判定されない", name)
		}
	}
	for name, c := range map[string]struct{ before, after string }{
		"厳しくする":      {`{}`, `{"checks":{"e":{"params":{"entropy_ratio_fail":0.4}}}}`},
		"短縮":         {`{"auto_hold_days":14}`, `{"auto_hold_days":7}`},
		"変更なし":       {`{}`, `{}`},
		"緩い値を消して定数へ": {`{"checks":{"e":{"params":{"entropy_ratio_fail":0.9}}}}`, `{}`},
	} {
		if w, err := ChecksWeaken([]byte(c.before), []byte(c.after)); err != nil || w {
			t.Errorf("%s: 緩和でないのに緩和", name)
		}
	}
}
