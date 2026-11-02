package contract

import (
	"regexp"
	"strings"
	"testing"
)

func TestGateAllowlistMatchesSubcommandTable(t *testing.T) {
	a, err := LoadGateAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, c := range a.Subcommands {
		k := strings.Join(c.Command, " ")
		if have[k] {
			t.Errorf("重複: %s", k)
		}
		have[k] = true
		names := map[string]bool{}
		for _, f := range append(append([]GateFlag{}, c.Flags...), a.CommonFlags...) {
			if names[f.Name] {
				t.Errorf("%s: 引数名の重複 %s", k, f.Name)
			}
			names[f.Name] = true
			if f.Value == "enum" != (len(f.Enum) > 0) {
				t.Errorf("%s --%s: enum の有無が値の形と合わない", k, f.Name)
			}
		}
		if c.SecretFD != (k == "init" || k == "apply") {
			t.Errorf("%s: secret_fd は init と apply だけ", k)
		}
	}
	for _, e := range a.Excluded {
		have[strings.Join(e.Command, " ")] = true
	}
	for _, s := range SubcommandTable {
		k := strings.Join(s.Words, " ")
		if s.Public == PublicInternal || s.Public == PublicTestOnly {
			if have[k] {
				t.Errorf("%s は許可リストに載せない", k)
			}
			continue
		}
		if !have[k] {
			t.Errorf("%s が許可リストに無い", k)
		}
		delete(have, k)
	}
	if len(have) != 0 {
		t.Errorf("表に無い項目: %v", have)
	}
}

func TestGateAllowlistPositionals(t *testing.T) {
	a, _ := LoadGateAllowlist()
	var propose, secrets *GatePositional
	for _, c := range a.Subcommands {
		if c.Positional != nil && c.Positional.Count != 1 {
			t.Errorf("%v: 位置引数は 1 語", c.Command)
		}
		switch strings.Join(c.Command, " ") {
		case "propose":
			propose = c.Positional
		case "secrets set":
			secrets = c.Positional
		default:
			if c.Positional != nil {
				t.Errorf("%v: 位置引数を持つのは propose と secrets set だけ", c.Command)
			}
		}
	}
	if propose == nil || secrets == nil || !secrets.Ref {
		t.Fatal("propose / secrets set の位置引数が無い")
	}
	// propose の種別は変更の記録の種別の列挙(schema/data/change.json)と同じ
	var kinds []string
	b, _ := schemaFS.ReadFile("schema/data/change.json")
	re := regexp.MustCompile(`"restore-session-extend"`)
	if !re.Match(b) || len(propose.Values) < 20 {
		t.Fatalf("propose の種別: %v", propose.Values)
	}
	for _, k := range propose.Values {
		if !strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("種別 %s が change.json に無い", k)
		}
		kinds = append(kinds, k)
	}
	if !regexp.MustCompile(a.RefPattern).MatchString("admin") || regexp.MustCompile(a.RefPattern).MatchString("Admin") {
		t.Errorf("参照名の正規表現: %s", a.RefPattern)
	}
	_ = kinds
}
