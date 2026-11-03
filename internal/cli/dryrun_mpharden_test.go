package cli

// まとめレビュー H3: --dry-run は実行しない(spec 3.1.1)。hold set・backup・notify set は、状態も保管先も秘密ファイルも通知も動かさない。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

func wantNotImplemented(t *testing.T, err error) {
	t.Helper()
	var ni *contract.NotImplemented
	if !errors.As(err, &ni) {
		t.Fatalf("NOT_IMPLEMENTED を期待したが %v", err)
	}
}

func TestHoldSetDryRunDoesNotHold(t *testing.T) {
	f, h := holdEnv(t)
	_, err := holdSetEntry(f.d, bg, contract.Call{Words: []string{"hold", "set"}, DryRun: true})
	wantNotImplemented(t, err)
	if len(h.got) != 0 {
		t.Errorf("削除停止を掛けた: %+v", h.got)
	}
	if n := len(f.rec.Notifications()); n != 0 {
		t.Errorf("通知 %d 件", n)
	}
	if it := f.ends(t, "hold"); len(it) != 0 {
		t.Errorf("journal の hold: %d", len(it))
	}
}

func TestBackupDryRunRunsNothing(t *testing.T) {
	f := newFlow(t)
	called := 0
	f.d.Acquire = &fake.Acquire{Fn: map[string]func() (contract.Reply, error){
		"Selftest": func() (contract.Reply, error) { called++; return contract.Reply{}, nil },
		"Run":      func() (contract.Reply, error) { called++; return contract.Reply{}, nil },
	}}
	_, err := backupEntry(f.d, bg, contract.Call{Words: []string{"backup"}, RunID: runA, DryRun: true})
	wantNotImplemented(t, err)
	if called != 0 {
		t.Errorf("取得を %d 回呼んだ", called)
	}
	if n := len(f.rec.Notifications()); n != 0 {
		t.Errorf("通知 %d 件", n)
	}
	if it := f.ends(t, "run_end"); len(it) != 0 {
		t.Errorf("run_end: %d", len(it))
	}
}

func TestNotifySetDryRunWritesNoSecret(t *testing.T) {
	d, _ := notifyDeps(t, `{"hook-a":"https://hooks.example.test/x"}`+"\n")
	o, code := runCLI(d, newBuf(), "notify", "set", "--to", "hook-a", "--secret-ref", "hook-a", "--secrets-stdin", "--dry-run")
	e, _ := o["error"].(map[string]any)
	if code == 0 || e["code"] != "NOT_IMPLEMENTED" {
		t.Errorf("%v (code %d)", o, code)
	}
	if _, err := os.Stat(filepath.Join(d.StateDir, "secrets", "hook-a.json")); err == nil {
		t.Error("dry-run が秘密ファイルを置いた")
	}
}

type proposeStub struct {
	applyStub
	err error
}

func (p *proposeStub) Propose(context.Context, contract.Call) (contract.Reply, error) {
	return contract.Reply{Result: []byte(`{}`)}, p.err
}

// propose notify-change の資格情報(--secrets-stdin)は、作成時に秘密ファイルへ置く。提案を作れなければ消す。使用中の参照名は上書きしない(ARG_INVALID)。
func TestProposeNotifyChangePlacesAndDropsSecret(t *testing.T) {
	call := func(secrets string) contract.Call {
		return contract.Call{Words: []string{"propose"}, Positional: []string{"notify-change"}, Secrets: contract.SecretInput{"hook-2": []byte(secrets)}}
	}
	file := func(d *Deps) string { return filepath.Join(d.StateDir, "secrets", "hook-2.json") }
	d, _ := notifyDeps(t, "")
	d.Approval = &proposeStub{}
	if _, err := proposeEntry(d, bg, call(`"https://hooks.example.test/2"`)); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(file(d)); err != nil || !strings.Contains(string(b), `"kind":"token"`) {
		t.Errorf("資格情報を置いていない: %v %s", err, b)
	}
	if _, err := proposeEntry(d, bg, call(`"https://hooks.example.test/other"`)); err == nil {
		t.Error("使用中の参照名を上書きした")
	}
	d2, _ := notifyDeps(t, "")
	d2.Approval = &proposeStub{err: errors.New("boom")}
	if _, err := proposeEntry(d2, bg, call(`"https://hooks.example.test/2"`)); err == nil {
		t.Fatal("提案の失敗が返らない")
	}
	if _, err := os.Stat(file(d2)); err == nil {
		t.Error("提案を作れなかったのに置いた値が残っている")
	}
}
