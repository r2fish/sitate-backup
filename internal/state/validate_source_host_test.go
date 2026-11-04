package state

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// sourceHostPattern は契約の host の形(common.json の $defs/host の pattern)と一致する。
func TestSourceHostPatternMatchesContract(t *testing.T) {
	b, err := os.ReadFile("../contract/schema/cli/common.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Defs map[string]struct {
			Pattern string `json:"pattern"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if got := c.Defs["host"].Pattern; got != sourceHostPattern {
		t.Errorf("契約の host の pattern = %q、state の定数 = %q", got, sourceHostPattern)
	}
}

// D-0188: source.host は裸のホスト名。URI・ユーザー・ポートを含めば /source/host の invalid_value。
// def validate(skipRefs 偽)と propose・apply が使う検査(skipRefs 真)の両方で止まる。restore.dest.at は URI のまま通る。
func TestValidateDefSourceHostForm(t *testing.T) {
	s := newFiles(t)
	cases := []struct {
		host string
		ok   bool
	}{
		{"app01.example.internal", true},
		{"203.0.113.10", true},
		{"a", true},
		{"2001:db8::1", true},
		{"ssh://u@h:22", false},
		{"ssh://u@source:22", false},
		{"source:22", false},
		{"[fe80::1]", false},
		{"fe80::1", true},
		{"ssh://h", false},
		{"u@h", false},
		{"h:22", false},
		{"h:", false},
		{"h port", false},
		{"-h", false},
		{".h", false},
		{"h/x", false},
		{strings.Repeat("a", 256), false},
	}
	for _, c := range cases {
		def := strings.Replace(baseDef, "host: app01.example.internal", `host: "`+c.host+`"`, 1)
		for _, skip := range []bool{false, true} {
			_, _, raw, err := s.ValidateDef(ctx, []byte(def), skip)
			if err != nil {
				t.Fatal(err)
			}
			var is []issue
			_ = json.Unmarshal(raw, &is)
			got := ""
			for _, i := range is {
				if i.Pointer == "/source/host" {
					got = i.Reason
				}
			}
			if c.ok && got != "" || !c.ok && got != rInvalid {
				t.Errorf("host %q skipRefs=%v: /source/host の指摘 = %q (通る=%v)", c.host, skip, got, c.ok)
			}
		}
	}
	// 空は従来どおり invalid_value。
	empty := strings.Replace(baseDef, "host: app01.example.internal", `host: ""`, 1)
	if issuesOf(t, s, empty)["/source/host"] != rInvalid {
		t.Error("空の host が通った")
	}
	// restore.dest.at は変えない(ssh:// の形のまま通る)。baseDef がそれを含む。
	if is := issuesOf(t, s, baseDef); len(is) != 0 {
		t.Errorf("baseDef: %v", is)
	}
}
