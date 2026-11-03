package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// まとめレビュー M4: applying の instance は実行の個体(<boot_id>:<pid>:<pid_start_ticks>。spec 4.5)で、--run-id ではない。
// run_id が同じ再実行(アプリは切断の後に同じ run_id で呼び直す)でも、前の個体が残した applying を中断として再開できる。
func TestApplyInstanceIsProcessIdentityNotRunID(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	if _, err := applyEntry(f.d, bg, contract.Call{Words: []string{"apply"}, RunID: runA}); err != nil {
		t.Fatal(err)
	}
	pid, ticks, boot := selfProc()
	want := fmt.Sprintf("%s:%d:%d", boot, pid, ticks)
	if a.instance == runA || a.instance != want || !strings.Contains(a.instance, fmt.Sprint(os.Getpid())) {
		t.Errorf("instance = %q(%q のはず。run_id は %q)", a.instance, want, runA)
	}
}
