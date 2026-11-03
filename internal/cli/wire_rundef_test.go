package cli

// 主経路の第 2 段で見つけた誤り E4 の結線の試験: seal の前も、取得(backup)の後に cli が読む定義と、復元テストの部品が読む定義は候補になる。
// 定義を扱う他のサブコマンド(def・notify・status など)は、従来どおり NOT_SEALED を見て分岐する。

import (
	"context"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/restore"
)

func notSealed(t *testing.T, err error) {
	t.Helper()
	if e, ok := err.(*contract.Error); !ok || e.Code != contract.CodeNotSealed {
		t.Fatalf("NOT_SEALED のはず: %v", err)
	}
}

func TestWireRunDefBeforeSealReadsCandidateForBackupAndRestoreTest(t *testing.T) {
	ctx := context.Background()
	d := DefaultDeps(t.TempDir())
	h, err := d.State.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.State.Update(ctx, h, "initialized", []byte("true")); err != nil {
		t.Fatal(err)
	}
	h.Release()
	if err := d.State.StageDef(ctx, []byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	_, _, err = d.State.ReadDef(ctx)
	notSealed(t, err) // 取得の前: 他のサブコマンドが見る定義は従来どおり
	rt, ok := d.Restore.(*restoreTestOnCandidate)
	if !ok {
		t.Fatalf("restore-test の部品が候補を読む形で結ばれていない: %T", d.Restore)
	}
	norm, _, err := rt.test.(*restore.Restore).State.ReadDef(ctx)
	if err != nil || string(norm) != `{"version":1}` {
		t.Fatalf("restore-test が候補の定義を読めない: %s %v", norm, err)
	}
	_, _, err = rt.restoreIface.(*restore.Restore).State.ReadDef(ctx)
	if err == nil {
		t.Fatal("restore-test 以外の復元の部品まで候補を読む(restore の seal の前の拒否が変わる)")
	}
	_, _ = d.Ingest(ctx, contract.Call{Words: []string{"backup"}}) // 取得の流れに入る(失敗してよい)
	norm, _, err = d.State.ReadDef(ctx)
	if err != nil || string(norm) != `{"version":1}` {
		t.Fatalf("backup の流れの中で cli が候補の定義を読めない: %s %v", norm, err)
	}
}

// seal の後(sealed: true)に approved.json だけが失われた状態では、候補へ開かない(NOT_SEALED のまま)。
func TestWireRunDefDoesNotOpenToCandidateAfterSeal(t *testing.T) {
	ctx := context.Background()
	d := DefaultDeps(t.TempDir())
	h, err := d.State.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"initialized", "true"}, {"sealed", "true"}} {
		if err := d.State.Update(ctx, h, kv[0], []byte(kv[1])); err != nil {
			t.Fatal(err)
		}
	}
	h.Release()
	if err := d.State.(candidateFallbackState).StateFiles.StageDef(ctx, []byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	_, _ = d.Ingest(ctx, contract.Call{Words: []string{"backup"}})
	_, _, err = d.State.ReadDef(ctx)
	notSealed(t, err)
	rt := d.Restore.(*restoreTestOnCandidate)
	_, _, err = rt.test.(*restore.Restore).State.ReadDef(ctx)
	notSealed(t, err)
}
