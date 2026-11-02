package cli

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// D-0159: `restore-dest enroll --host-key-fp`(seal の前に人が確かめた新しい指紋。spec 3.1.2、3.2.4 の「ホスト鍵」)を gate の許可リストと構文解析が通す。
func TestRestoreDestEnrollAcceptsHostKeyFP(t *testing.T) {
	const fp = "SHA256:abc+DEF/123="
	p, _, perr := parseArgs([]string{"restore-dest", "enroll", "--dest", "ssh://u@h:22", "--adapter", "ssh-host", "--host-key-fp", fp})
	if perr != nil {
		t.Fatalf("--host-key-fp を受けない: %v", perr)
	}
	if got := p.args["host-key-fp"]; len(got) != 1 || got[0] != fp {
		t.Errorf("args[host-key-fp] = %v", got)
	}
	a, err := contract.LoadGateAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range a.Subcommands {
		if len(c.Command) == 2 && c.Command[0] == "restore-dest" && c.Command[1] == "enroll" {
			for _, f := range c.Flags {
				found = found || f.Name == "host-key-fp" && f.Value == "word"
			}
		}
	}
	if !found {
		t.Error("gate の許可リストに restore-dest enroll の --host-key-fp(word)が無い")
	}
	// ほかのサブコマンドは受けない。
	if _, _, perr := parseArgs([]string{"restore-dest", "check", "--host-key-fp", fp}); perr == nil {
		t.Error("restore-dest check が --host-key-fp を受けた")
	}
}
