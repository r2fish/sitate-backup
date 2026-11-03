package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/state"
)

// doctor の timers_registered(spec 3.1.2 の表。D-0168): 表のタイマー列の単位がすべて登録され、有効になっていること。
// 単位を 1 つ外すと不合格になる(残りが有効でも合格にしない)。

func timersRegisteredProbe(t *testing.T) (*HostProbe, string) {
	t.Helper()
	dir := t.TempDir()
	tm := &state.Timers{Dir: dir, Scheduler: "systemd"} // Run が nil: 単位を書くだけ
	if err := tm.Register(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	p := &HostProbe{TimersDir: dir}
	p.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" {
			return []byte("enabled\n"), nil // 置かれた単位はすべて有効
		}
		return nil, os.ErrNotExist
	}
	return p, dir
}

func TestTimersRegisteredAllUnitsPass(t *testing.T) {
	p, dir := timersRegisteredProbe(t)
	if !p.timersRegistered(context.Background()) {
		ents, _ := os.ReadDir(dir)
		t.Fatalf("全単位が登録済みなのに不合格: %d 件", len(ents))
	}
}

func TestTimersRegisteredMissingUnitFails(t *testing.T) {
	_, dir := timersRegisteredProbe(t)
	ents, _ := os.ReadDir(dir)
	var timers []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".timer") {
			timers = append(timers, e.Name())
		}
	}
	if len(timers) < 2 {
		t.Fatalf("タイマーの単位が %d 個しか描画されない", len(timers))
	}
	for _, name := range timers {
		p2, d2 := timersRegisteredProbe(t)
		if err := os.Remove(filepath.Join(d2, name)); err != nil {
			t.Fatal(err)
		}
		if p2.timersRegistered(context.Background()) {
			t.Errorf("%s を外しても timers_registered が合格する", name)
		}
	}
}
