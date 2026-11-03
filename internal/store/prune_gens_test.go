package store

// prune が扱う世代の種類(作業指示書 5.18): 0 件の対象を含む世代(TARGET_EMPTY。health: incomplete)と一部しか取れていない世代(incomplete)は保護中ではなく、
// 保持期限を過ぎれば通常どおり消える。最新の健全な世代(保護中)は、期限を過ぎても、後ろの世代が incomplete でも消さない。

import (
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func (f *fixture) markIncomplete(ref string) {
	for _, s := range f.snaps {
		if s["ref"] == ref {
			s["health"], s["targets_ok"] = "incomplete", false
		}
	}
	f.flush()
}

func TestPruneIncompleteAndEmptyGenerationsExpireProtectedStays(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	healthy := f.addSnapshot(2, t0.Add(30*day), true) // 最新の健全な世代(保護中)。期限は過ぎる
	empty := f.addSnapshot(0, t0.Add(30*day), false)  // 0 件の対象(データのチャンクが無い)
	f.markIncomplete(empty.Ref)
	part := f.addSnapshot(2, t0.Add(30*day), false) // 一部しか取れていない
	f.markIncomplete(part.Ref)
	f.clk.Advance(100 * day)
	res, err := f.r.Prune(ctx)
	harness.Implemented(t, err)
	if err != nil || res.Held {
		t.Fatalf("Prune: %+v %v", res, err)
	}
	for _, id := range append(append([]objID{empty.Manifest, part.Manifest}, empty.Refs...), part.Refs...) {
		if f.exists(id) {
			t.Errorf("期限切れの incomplete の世代の版が残っている: %s", id.Key)
		}
	}
	for _, id := range append([]objID{healthy.Manifest}, healthy.Refs...) {
		if !f.exists(id) {
			t.Errorf("保護中の世代の版が消えた: %s", id.Key)
		}
	}
	if res.Protected != 1 {
		t.Errorf("保護中の数: %+v", res)
	}
}

// 世代の保持期限を過ぎても、実際のロックが残る版は消さない(期限の判定で残り、保管先へ削除を試みない)。
func TestPruneLiveLockVersionStays(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	old := f.addSnapshot(2, t0.Add(30*day), false)
	f.clk.Advance(30*day + time.Hour) // 世代の保持期限は過ぎたが、書き込みのロックは 90 日残る
	res, err := f.r.Prune(ctx)
	harness.Implemented(t, err)
	if err != nil || res.Deleted != 0 {
		t.Fatalf("ロック中の版を消した: %+v %v", res, err)
	}
	for _, id := range append([]objID{old.Manifest}, old.Refs...) {
		if !f.exists(id) {
			t.Errorf("ロック中の版が消えた: %s", id.Key)
		}
	}
}
