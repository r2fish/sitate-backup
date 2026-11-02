package contract_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func TestCanonicalJSONVectors(t *testing.T) {
	var d struct {
		Cases []struct{ Name, Input, Canonical, Sha256 string }
		Equal []struct{ Name, A, B string } `json:"def_hash_equal"`
		Diff  []struct{ Name, A, B string } `json:"def_hash_differ"`
	}
	harness.LoadVectors(t, "approval/canonical.json", &d)
	for _, c := range d.Cases {
		got, err := contract.CanonicalJSON([]byte(c.Input))
		if err != nil || string(got) != c.Canonical {
			t.Errorf("%s: %q %v want %q", c.Name, got, err, c.Canonical)
		}
		if h, _ := contract.CanonicalHash([]byte(c.Input)); h != contract.HashPrefix+c.Sha256 {
			t.Errorf("%s: hash %s", c.Name, h)
		}
	}
	for _, c := range d.Equal {
		a, _ := contract.CanonicalHash([]byte(c.A))
		b, _ := contract.CanonicalHash([]byte(c.B))
		if a != b {
			t.Errorf("%s: 同じ定義で hash が違う", c.Name)
		}
	}
	for _, c := range d.Diff {
		a, _ := contract.CanonicalHash([]byte(c.A))
		b, _ := contract.CanonicalHash([]byte(c.B))
		if a == b {
			t.Errorf("%s: 違う定義で hash が同じ", c.Name)
		}
	}
	if _, err := contract.CanonicalJSON([]byte(`{"a":`)); err == nil {
		t.Error("壊れた JSON を受けた")
	}
}

// 標準出力の 2 レコード(spec 3.1.4)の試験ベクトル。
func TestStdoutRecordVectors(t *testing.T) {
	var d struct {
		Cases []struct {
			Name      string
			Stdout    []json.RawMessage
			Raw       []string // 途中で切れた行を含む出力(stdout の代わりに 1 行ずつ書く)
			End       string   // complete(既定)か ssh_disconnected
			Want      string
			HasSecret bool `json:"has_secret"`
			Error     string
		}
		Invalid []struct {
			Name   string
			Record json.RawMessage
			Error  string
		} `json:"secret_record_invalid"`
	}
	harness.LoadVectors(t, "cli/stdout-records.json", &d)
	for _, c := range d.Cases {
		code, has, err := harness.ClassifyStdout(c.Stdout)
		if c.Raw != nil {
			code, has, err = harness.ClassifyStream(c.Raw, c.End)
		}
		if err != nil || (c.Want == "ok") != (code == "") || code != c.Error || (c.Want == "ok" && has != c.HasSecret) {
			t.Errorf("%s: code=%q has=%v err=%v", c.Name, code, has, err)
		}
		for i, r := range c.Stdout {
			var h struct{ Schema string }
			_ = json.Unmarshal(r, &h)
			schema := ""
			switch h.Schema {
			case harness.SecretRecordSchema:
				schema = "cli/secret-record.json"
			case "sitate.out/v1":
				schema = "cli/envelope.json"
			}
			if schema == "" {
				continue // 封筒でも秘密のレコードでもないレコード(null・数・別の schema)は、破棄の試験のための入力で、形の検査の対象外
			}
			if err := contract.Validate(schema, r); err != nil {
				t.Errorf("%s[%d]: %v", c.Name, i, err)
			}
		}
	}
	for _, c := range d.Invalid {
		if contract.Validate("cli/secret-record.json", c.Record) == nil {
			t.Errorf("%s: 拒否すべき秘密のレコードを通した", c.Name)
		}
		// 秘密のレコードの形が不正なら、封筒が正しくても全体を破棄する(spec 3.1.4 の判定の順)。
		var env json.RawMessage
		for _, k := range d.Cases {
			if k.Name == "secret-then-envelope" {
				env = k.Stdout[len(k.Stdout)-1]
			}
		}
		if code, _, _ := harness.ClassifyStdout([]json.RawMessage{c.Record, env}); code != c.Error || c.Error == "" {
			t.Errorf("%s: ClassifyStdout = %q want %q", c.Name, code, c.Error)
		}
	}
}

func TestAddMonthsUTC(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"2028-02-29T03:00:00Z", 12, "2029-02-28T03:00:00Z"},
		{"2027-01-31T00:00:00Z", 1, "2027-02-28T00:00:00Z"},
		{"2027-03-01T10:20:30Z", 12, "2028-03-01T10:20:30Z"},
		{"2027-11-30T00:00:00Z", 3, "2028-02-29T00:00:00Z"},
	} {
		in, _ := time.Parse(time.RFC3339, c.in)
		if got := contract.AddMonthsUTC(in, c.n).Format(time.RFC3339); got != c.want {
			t.Errorf("%s + %d か月 = %s want %s", c.in, c.n, got, c.want)
		}
	}
}
