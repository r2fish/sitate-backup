package acquire

import "testing"

// spec 3.3「接続先と資格情報」: --host は ssh://<user>@<host>:<port>。ポート省略は 22、IPv6 は [...]、<host> は source.host と同じ形(括弧を外した文字列。大小は変えない)。
func TestParseHostArgForms(t *testing.T) {
	ok := map[string]hostSpec{
		"ssh://admin@203.0.113.10:22":    {"admin", "203.0.113.10", 22},
		"ssh://admin@203.0.113.10":       {"admin", "203.0.113.10", 22},
		"ssh://admin@app01.example.test": {"admin", "app01.example.test", 22},
		"ssh://admin@[2001:db8::1]:22":   {"admin", "2001:db8::1", 22},
		"ssh://admin@[2001:db8::1]":      {"admin", "2001:db8::1", 22},
	}
	for in, want := range ok {
		got, err := parseHostArg(in)
		if err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"203.0.113.10", "ssh://203.0.113.10:22", "ssh://@203.0.113.10", "http://a@h:22", "ssh://admin@h:0", "ssh://admin@h:x", "ssh://admin@h/x"} {
		if _, err := parseHostArg(in); err == nil || err.Code != "ARG_INVALID" {
			t.Errorf("%s は ARG_INVALID: %v", in, err)
		}
	}
	if _, err := parseHostArg("ssh://admin@h:2222"); err == nil || err.Code != "ARG_INVALID" {
		t.Errorf("22 以外: %v", err)
	}
}
