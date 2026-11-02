package store

// Retention.Prune・Extend の契約試験(作業指示書 3.3): 期限の判定は「保管先の Date に核が注入したオフセットを足す」形で行う。
// 偽の保管先の StoreTime は実時刻で、Object Lock の期限の判定だけが模擬時間(ManualClock)で動く。核の側が共有の値のオフセット
// (Retention.Offset)を足して「今」を出す。Offset を無視する実装は、模擬時間が実時刻から離れているこの試験で落ちる。

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// simOffset は共有の値の偽物: 模擬時間(clk)と実時刻の差 + extra を返す。
type simOffset struct {
	clk   *fake.ManualClock
	extra time.Duration
}

func (o simOffset) Offset() (time.Duration, error) {
	return o.clk.T.Sub(time.Now().UTC()) + o.extra, nil
}

func newRetentionState(t *testing.T, items map[string]any) (*Retention, *fake.ObjectStore, *fake.ManualClock, *fake.StateFiles) {
	t.Helper()
	st, clk, _ := fake.NewTestStore(fake.RoleWrite)
	sf := &fake.StateFiles{Items: map[string]json.RawMessage{}}
	sf.Items["sealed"] = json.RawMessage("true") // prune は sealed が true のときだけ進む
	for k, v := range items {
		b, _ := json.Marshal(v)
		sf.Items[k] = b
	}
	r := &Retention{Store: st, State: sf, Codec: &harness.RefCodec{}, KeyS: fake.KeyService{}, Clock: &fake.Clock{T: clk.T}, Offset: simOffset{clk: clk}}
	r.Index = &fake.Index{M: map[string]contract.ChunkRef{}} // prune は消した版を索引から外す。無ければ止まる
	return r, st, clk, sf
}

func versionsByKey(t *testing.T, st *fake.ObjectStore) map[string]contract.VersionInfo {
	t.Helper()
	v, err := st.ListVersions(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]contract.VersionInfo{}
	for _, x := range v {
		if !x.DeleteMarker {
			m[x.Key] = x
		}
	}
	return m
}

func within(a, b time.Time) bool { d := a.Sub(b); return d > -time.Minute && d < time.Minute }

// 期限切れの未参照だけ消す。「今」は Date + オフセット: 模擬時間が 100 日進んだとき古い版が消え、50 日では何も消えない。
func TestContractRetentionPruneUsesDatePlusOffset(t *testing.T) {
	r, st, clk, _ := newRetentionState(t, nil)
	_ = putRef(t, st, "chunks/old")
	clk.Advance(50 * 24 * time.Hour)
	res, err := r.Prune(ctx)
	harness.Implemented(t, err)
	if err != nil || res.Deleted != 0 || len(versionsByKey(t, st)) != 1 {
		t.Fatalf("保持期限(90 日)の内は消さない: %+v %v", res, err)
	}
	clk.Advance(50 * 24 * time.Hour) // 書き込みから 100 日
	_ = putRef(t, st, "chunks/new")
	res, err = r.Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := versionsByKey(t, st)
	if _, old := got["chunks/old"]; old || len(got) != 1 || res.Deleted != 1 || res.SkippedLocked != 0 || res.Held {
		t.Errorf("期限切れの未参照だけ消す: %+v 残り=%d", res, len(got))
	}
}

// 核の「今」が保管先のロックより進んでいて削除を拒まれたら、skipped_locked に数えて続ける(誤りにしない。spec 3.1.3 の補足)。
func TestContractRetentionPruneSkippedLocked(t *testing.T) {
	r, st, _, _ := newRetentionState(t, nil)
	clk := &fake.ManualClock{T: fake.StartTime}
	r.Offset = simOffset{clk: clk, extra: 100 * 24 * time.Hour} // 核は 100 日後だと思っている。偽の保管先のロックはまだ生きている
	_ = putRef(t, st, "chunks/a")
	res, err := r.Prune(ctx)
	harness.Implemented(t, err)
	if err != nil || res.Deleted != 0 || res.SkippedLocked != 1 || len(versionsByKey(t, st)) != 1 {
		t.Errorf("ロック中の版は消えず skipped_locked: %+v %v", res, err)
	}
}

// 削除停止中は何も消さず、held で示す(hold set は削除だけに効く。spec 3.1.1 の規則 18)。
func TestContractRetentionPruneHeld(t *testing.T) {
	hold := map[string]any{"active": true, "since": "2027-03-01T00:00:00Z", "auto": false, "reason": "manual", "causes": []any{}, "suppressed": []any{}}
	r, st, clk, _ := newRetentionState(t, map[string]any{"hold": hold})
	_ = putRef(t, st, "chunks/old")
	clk.Advance(200 * 24 * time.Hour)
	res, err := r.Prune(ctx)
	harness.Implemented(t, err)
	if err != nil || res.Deleted != 0 || !res.Held || len(versionsByKey(t, st)) != 1 {
		t.Errorf("削除停止中: %+v %v", res, err)
	}
}

func objRec(r contract.VersionRef, at time.Time) map[string]any {
	return map[string]any{"key": r.Key, "version_id": r.VersionID, "size": r.Size, "etag": r.ETag, "retain_until": r.RetainUntil.UTC().Format(time.RFC3339), "written_at": at.UTC().Format(time.RFC3339)}
}

// 目標 B(共有物): 今(Date + オフセット)+ 90 日へ延ばす。共有物でないチャンクには触れず、既により遅い版は短くせず数えない。
func TestContractRetentionExtendTargetB(t *testing.T) {
	r, st, clk, sf := newRetentionState(t, nil)
	t0 := clk.T
	keyring := putRef(t, st, "keyring/kr-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	chunk := putRef(t, st, "chunks/c1")
	long := putRef(t, st, "repo.json")
	if err := st.Extend(ctx, long.Key, long.VersionID, t0.AddDate(0, 0, 400)); err != nil {
		t.Fatal(err)
	}
	long.RetainUntil = t0.AddDate(0, 0, 400)
	gone := contract.VersionRef{Key: "bin/gone", VersionID: "v-none", RetainUntil: t0.AddDate(0, 0, 90)}
	b, _ := json.Marshal([]any{objRec(keyring, t0), objRec(chunk, t0), objRec(long, t0), objRec(gone, t0)})
	sf.Items["objects"] = b
	clk.Advance(30 * 24 * time.Hour)
	want := clk.T.AddDate(0, 0, 90)
	res, err := r.Extend(ctx, "B")
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	got := versionsByKey(t, st)
	if !within(got["keyring/kr-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"].RetainUntil, want) {
		t.Errorf("共有物は今 + 90 日: %v want %v", got["keyring/kr-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"].RetainUntil, want)
	}
	if !got["chunks/c1"].RetainUntil.Equal(chunk.RetainUntil) {
		t.Errorf("目標 B はチャンクに触れない: %v", got["chunks/c1"].RetainUntil)
	}
	if !got["repo.json"].RetainUntil.Equal(long.RetainUntil) {
		t.Errorf("より遅い retain-until を短くした: %v", got["repo.json"].RetainUntil)
	}
	if res.Extended != 1 || !within(res.MinRetainUntil, want) {
		t.Errorf("Extended=%d(延ばしたのは keyring だけ) min=%v want %v", res.Extended, res.MinRetainUntil, want)
	}
	if len(res.Failed) != 1 || res.Failed[0].Key != "bin/gone" || res.Failed[0].VersionID != "v-none" {
		t.Fatalf("存在しない版は failed[] に載せて残りを続ける: %+v", res.Failed)
	}
	if _, ok := contract.CodeTable[res.Failed[0].Code]; !ok {
		t.Errorf("failed[].code が原因コードの表に無い: %q", res.Failed[0].Code)
	}
	// 記録(state.objects)の retain-until を延ばした値にする。
	var recs []struct {
		Key         string    `json:"key"`
		RetainUntil time.Time `json:"retain_until"`
	}
	raw, _ := sf.Get(ctx, "objects")
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatal(err)
	}
	for _, x := range recs {
		if x.Key == "keyring/kr-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" && !within(x.RetainUntil, want) {
			t.Errorf("記録の retain-until が延びていない: %v", x.RetainUntil)
		}
	}
}

// 目標 A は保持中の世代の参照だけ: 世代の無い記録では何も延ばさず(共有物にも触れない)、誤りにもしない。
func TestContractRetentionExtendTargetANoSnapshots(t *testing.T) {
	r, st, clk, sf := newRetentionState(t, nil)
	keyring := putRef(t, st, "keyring/kr-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	b, _ := json.Marshal([]any{objRec(keyring, clk.T)})
	sf.Items["objects"] = b
	clk.Advance(30 * 24 * time.Hour)
	res, err := r.Extend(ctx, "A")
	harness.Implemented(t, err)
	if err != nil || res.Extended != 0 || len(res.Failed) != 0 || !versionsByKey(t, st)["keyring/kr-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"].RetainUntil.Equal(keyring.RetainUntil) {
		t.Errorf("目標 A: %+v %v", res, err)
	}
}
