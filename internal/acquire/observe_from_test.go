package acquire

import (
	"context"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type fromSSH struct {
	out    string
	exit   int
	script string
}

func (f *fromSSH) Run(_ context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
	f.script = string(req.Script)
	_, _ = req.Stdout.Write([]byte(f.out))
	return contract.SSHResult{ExitStatus: f.exit}, nil
}

// 送信元は元サーバーの SSH_CONNECTION の先頭の語を取り、不正な値は台本を流さずに止める。
func TestObserveFrom(t *testing.T) {
	cases := []struct {
		out  string
		exit int
		want string
		ok   bool
	}{
		{"203.0.113.9\n", 0, "203.0.113.9", true},
		{"2001:db8::1\n", 0, "2001:db8::1", true},
		{"\n", 0, "", false},
		{"203.0.113.9\n", 1, "", false},
		{"\"*\"\n", 0, "", false},
		{"a,b\n", 0, "", false},
	}
	for _, c := range cases {
		f := &fromSSH{out: c.out, exit: c.exit}
		got, err := enrollDeps{SSH: f}.observeFrom(context.Background(), contract.SSHConn{})
		if c.ok != (err == nil) || got != c.want {
			t.Errorf("%q exit %d: got %q err %v", c.out, c.exit, got, err)
		}
		if !strings.Contains(f.script, "SSH_CONNECTION") {
			t.Errorf("SSH_CONNECTION を見る台本: %s", f.script)
		}
	}
}
