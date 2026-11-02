package store

// Retention の試験(偽の保管先。作業指示書 5.3 の完了条件 2・6): extend が記録した版だけを延ばし短縮しない、prune が期限切れ・未参照・ロック期限切れの版だけを消し
// 保護中の世代を残す、削除停止、取り消し。

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const day = 24 * time.Hour

// 目標 A: 保持中の世代が参照する版(マニフェスト・エントリ・鍵表・データのチャンク)だけを、保持期限 + 7 日へ延ばす。期限切れの世代の参照・未参照の版・
// 目標より長い版には触れず(短縮しない)、数えない。
func TestExtendAOnlyRetainedReferences(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	a := f.addSnapshot(3, t0.Add(120*day), false)
	b := f.addSnapshot(2, t0.Add(95*day), false) // 今(+100 日)には保持期限が切れている。保持期限 + 7 日は記録の retain-until より遅い
	u := objID{"chunks/zz/unref", f.put("chunks/zz/unref", "u").VersionID}
	f.clk.Advance(100 * day)
	long := a.Refs[0]
	if err := f.core.PutRetention("b", long.Key, long.VersionID, t0.Add(300*day), fake.RoleWrite); err != nil {
		t.Fatal(err)
	}
	res, err := f.r.Extend(ctx, "A")
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	want := t0.Add(127 * day)
	for _, id := range append([]objID{a.Manifest}, a.Refs[1:]...) {
		if got := f.retainOf(id); !got.Equal(want) {
			t.Errorf("%s の retain-until = %v, 期待 %v(保持期限 + 7 日)", id.Key, got, want)
		}
	}
	if got := f.retainOf(long); !got.Equal(t0.Add(300 * day)) {
		t.Errorf("より長い retain-until を短くした: %v", got)
	}
	for _, id := range append([]objID{b.Manifest, u}, b.Refs...) {
		if got := f.retainOf(id); !got.Equal(t0.Add(90 * day)) {
			t.Errorf("保持期限の切れた世代・未参照の版に触れた: %s %v", id.Key, got)
		}
	}
	if res.Extended != len(a.Refs) || len(res.Failed) != 0 || !res.MinRetainUntil.Equal(want) {
		t.Errorf("Extended=%d(期待 %d) failed=%v min=%v(期待 %v)", res.Extended, len(a.Refs), res.Failed, res.MinRetainUntil, want)
	}
	var recs []struct {
		Ref         string    `json:"ref"`
		RetainUntil time.Time `json:"retain_until"`
	}
	raw, _ := f.sf.Get(ctx, "snapshots")
	_ = json.Unmarshal(raw, &recs)
	if !recs[0].RetainUntil.Equal(want) || !recs[1].RetainUntil.Equal(t0.Add(90*day)) {
		t.Errorf("世代の記録の retain-until: %+v", recs)
	}
	if again, err := f.r.Extend(ctx, "A"); err != nil || again.Extended != 0 {
		t.Errorf("2 回目は延ばす版が無い(現在の retain-until が目標以上): %+v %v", again, err)
	}
}

// 目標 B: 保護中の世代(保持期限を過ぎていても)の参照と共有物(最新のキーリング・repo.json・範囲内の journal/・使われている bin/)を今 + 90 日へ。
// 古い版・範囲外・使われていない版は延ばさない。
func TestExtendBProtectedAndSharedObjects(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	prot := f.addSnapshot(2, t0.Add(30*day), true)
	other := f.addSnapshot(2, t0.Add(30*day), false)
	now := t0.Add(100 * day) // extend を呼ぶ時刻。共有物は t0 に書いた(自然な retain-until は t0 + 90 日)ことにして、記録の書き込み時刻だけを変える
	obj := func(key string, at time.Time) objID { return f.addObject(key, f.put(key, "x"), at) }
	kr1 := obj("keyring/kr-0123456789abcdef0123456789abcdef", t0)
	jOld := obj("journal/000000000001-aa", now.Add(-400*day))
	jIn := obj("journal/000000000002-bb", now.Add(-200*day))
	binUsed := obj("bin/0.1.0/linux-x86_64/sitate", t0)
	binCur := obj("bin/0.2.0/linux-x86_64/sitate", t0)
	binOld := obj("bin/0.0.1/linux-x86_64/sitate", t0)
	f.clk.Advance(50 * day)
	kr2 := obj("keyring/kr-0123456789abcdef0123456789abcdef", f.clk.T)
	repo := obj("repo.json", f.clk.T)
	f.clk.Advance(50 * day)
	f.r.BinVersion = "0.2.0"
	res, err := f.r.Extend(ctx, "B")
	harness.Implemented(t, err)
	if err != nil || len(res.Failed) != 0 {
		t.Fatalf("Extend B: %+v %v", res, err)
	}
	want := f.clk.T.Add(90 * day)
	for _, id := range append([]objID{prot.Manifest, kr2, repo, jIn, binUsed, binCur}, prot.Refs...) {
		if got := f.retainOf(id); !equalWithin(got, want, 2*time.Second) {
			t.Errorf("%s は今 + 90 日のはず: %v 期待 %v", id.Key, got, want)
		}
	}
	for _, id := range append([]objID{kr1, jOld, binOld, other.Manifest}, other.Refs...) {
		if got := f.retainOf(id); got.After(t0.Add(91 * day)) {
			t.Errorf("%s は目標 B の対象でない: %v", id.Key, got)
		}
	}
	if res.Extended != 6+len(prot.Refs) {
		t.Errorf("Extended = %d, 期待 %d(共有物 6 + 保護中の世代の参照)", res.Extended, 6+len(prot.Refs))
	}
}

// 取り消されたら延ばし終えた版の記録を残して RUN_ABORTED(次の extend が残りを含めてやり直す)。記録は保管先の実際の retain-until と食い違わない。
func TestExtendAbortKeepsRecordsOfFinished(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	a := f.addSnapshot(20, t0.Add(150*day), false)
	f.clk.Advance(80 * day)
	c, cancel := context.WithCancel(ctx)
	f.r.Store = &cancelAfter{ObjectStore: f.st, cancel: cancel, after: 3}
	f.r.State = &ctxState{StateFiles: f.sf}
	res, err := f.r.Extend(c, "A")
	harness.ExpectCode(t, err, contract.CodeRunAborted)
	if res.Extended == 0 || res.Extended >= len(a.Refs)+1 {
		t.Errorf("途中で止まる回のはず: Extended=%d / %d", res.Extended, len(a.Refs)+1)
	}
	done := 0
	for _, x := range f.idx.recs { // チャンクの記録(Index)は保管先の実際の retain-until と食い違わない
		got := f.retainOf(objID{x.Key, x.VersionID})
		if !x.RetainUntil.Equal(got) {
			t.Errorf("記録 %v と保管先 %v が食い違う: %s", x.RetainUntil, got, x.Key)
		}
		if got.After(t0.Add(91 * day)) {
			done++
		}
	}
	if done == 0 {
		t.Error("延ばし終えた版の記録が残っていない")
	}
	done += manifestDone(f, a, t0)
	f.r.Store, f.r.State = f.st, f.sf
	again, err := f.r.Extend(ctx, "A")
	if err != nil || again.Extended+done != len(a.Refs)+1 {
		t.Errorf("次の extend は残りをやり直す: 1 回目の完了 %d + 2 回目 %+v %v", done, again, err)
	}
}

type cancelAfter struct {
	*fake.ObjectStore
	cancel context.CancelFunc
	after  int32
	n      atomic.Int32
}

func (c *cancelAfter) Extend(ctx context.Context, key, vid string, until time.Time) error {
	if c.n.Add(1) == c.after {
		c.cancel()
	}
	return c.ObjectStore.Extend(ctx, key, vid, until)
}

// prune: 期限切れの世代(保護中を除く)の版と未参照で期限切れの版だけを消す。保持中の世代の参照・保護中の世代・延長の対象は(ロックが切れていても)消さない。
// ほかの世代と共有するチャンクは、その世代が保持中の間は消さない。書いたばかりの版も消さない。
func TestPruneDeletesOnlyExpiredUnreferenced(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	s2 := f.addSnapshot(2, t0.Add(30*day), false)              // 期限切れ
	s1 := f.addSnapshot(2, t0.Add(150*day), false, s2.Refs[0]) // 保持中。s2 のチャンクを共有する。ロックは切れている(延長の失敗の想定)
	s3 := f.addSnapshot(2, t0.Add(30*day), true)               // 期限切れだが保護中
	u := objID{"chunks/zz/unref", f.put("chunks/zz/unref", "u").VersionID}
	f.clk.Advance(100 * day)
	n := objID{"chunks/zz/new", f.put("chunks/zz/new", "n").VersionID}
	alive := objID{"chunks/zz/alive", f.put0("chunks/zz/alive").VersionID} // 古いがロックが生きている(期限判定で残る。skipped_locked にも数えない)
	f.recordAll(append([]objID{s2.Manifest}, s2.Refs...))
	res, err := f.r.Prune(ctx)
	harness.Implemented(t, err)
	if err != nil || res.Held {
		t.Fatalf("Prune: %+v %v", res, err)
	}
	gone := append([]objID{s2.Manifest, u}, s2.Refs[1:]...)
	for _, id := range gone {
		if f.exists(id) {
			t.Errorf("消えるはずの版が残っている: %s", id.Key)
		}
	}
	keep := append(append([]objID{s2.Refs[0], s1.Manifest, s3.Manifest, n, alive}, s1.Refs...), s3.Refs...)
	for _, id := range keep {
		if !f.exists(id) {
			t.Errorf("消してはならない版が消えた: %s", id.Key)
		}
	}
	if res.Deleted != len(gone) || res.Protected != 1 || res.SkippedLocked != 0 {
		t.Errorf("結果: %+v(消す版 %d)", res, len(gone))
	}
	raw, _ := f.sf.Get(ctx, "snapshots")
	if strings.Contains(string(raw), s2.Ref) || !strings.Contains(string(raw), s1.Ref) || !strings.Contains(string(raw), s3.Ref) {
		t.Errorf("消した世代を記録から外す: %s", raw)
	}
}

// 参照を引けない世代があるとき、prune は何も消さずに誤りを返す(消してよい版が分からない)。削除停止中と seal の前は何もしない。
func TestPruneFailsClosedHeldAndNotSealed(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	old := f.put("chunks/zz/old", "o")
	bad := f.put("manifests/m-bad", "not a manifest")
	f.snaps = append(f.snaps, map[string]any{"ref": "m-bad", "version_id": bad.VersionID, "retain_until": t0.Format(time.RFC3339), "retention_until": t0.Add(500 * day).Format(time.RFC3339), "stored_at": t0.Format(time.RFC3339)})
	f.flush()
	f.clk.Advance(100 * day)
	_, err := f.r.Prune(ctx)
	harness.ExpectCode(t, err, contract.CodeDecryptFailed)
	if !f.exists(objID{"chunks/zz/old", old.VersionID}) {
		t.Error("参照を引けないのに消した")
	}
	f.snaps = nil
	f.flush()
	setItem(f.sf, "sealed", false)
	_, err = f.r.Prune(ctx)
	harness.ExpectCode(t, err, contract.CodeNotSealed)
	setItem(f.sf, "sealed", true)
	setItem(f.sf, "hold", map[string]any{"active": true, "since": t0.Format(time.RFC3339), "auto": false, "reason": "manual", "causes": []any{}, "suppressed": []any{}})
	res, err := f.r.Prune(ctx)
	if err != nil || !res.Held || res.Deleted != 0 || !f.exists(objID{"chunks/zz/old", old.VersionID}) {
		t.Errorf("削除停止中: %+v %v", res, err)
	}
}

// prune は各削除の直前に削除停止を確かめ直す(規則 18): 途中で停止が掛かったら、その後は消さない。
func TestPruneRechecksHoldBeforeEachDelete(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 4; i++ {
		f.put("chunks/zz/o"+string(rune('a'+i)), "o")
	}
	f.clk.Advance(100 * day)
	f.r.State = &flipHold{StateFiles: f.sf, flipAt: 3}
	res, err := f.r.Prune(ctx)
	if err != nil || !res.Held || res.Deleted != 1 {
		t.Errorf("1 件消した後で停止が掛かる: %+v %v", res, err)
	}
	if v := versionsByKey(t, f.st); len(v) != 3 {
		t.Errorf("残り: %d", len(v))
	}
}

// flipHold は hold の n 回目の読み取りから停止中を返す状態ファイルの偽物。
type flipHold struct {
	contract.StateFiles
	flipAt, gets int
}

func (s *flipHold) Get(ctx context.Context, item string) (json.RawMessage, error) {
	if item == "hold" {
		s.gets++
		if s.gets >= s.flipAt {
			return json.RawMessage(`{"active":true,"since":"2027-03-01T00:00:00Z","auto":false,"reason":"manual","causes":[],"suppressed":[]}`), nil
		}
	}
	return s.StateFiles.Get(ctx, item)
}

// hold set の繰り返しで状態が変わらない(同じ原因は足さない、since・reason・auto は変えない)。復元できなかった世代は refs[] に足す。
func TestHoldSetRepeatedIsIdempotent(t *testing.T) {
	f := newFixture(t)
	code := contract.CodeRestoreMismatch
	in := contract.HoldInput{Reason: "restore_failed", Code: &code, Refs: []string{"m-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	t1, err := f.r.HoldSet(ctx, in)
	harness.Implemented(t, err)
	first, _ := f.sf.Get(ctx, "hold")
	f.r.Clock = &fake.Clock{T: f.clk.T.Add(day)}
	t2, err := f.r.HoldSet(ctx, in)
	second, _ := f.sf.Get(ctx, "hold")
	if err != nil || !t1.Equal(t2) || string(first) != string(second) {
		t.Errorf("同じ入力の繰り返しで状態が変わった: %s → %s", first, second)
	}
	in.Refs = []string{"m-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	if _, err := f.r.HoldSet(ctx, in); err != nil {
		t.Fatal(err)
	}
	third, _ := f.sf.Get(ctx, "hold")
	var h struct {
		Causes []struct {
			Refs []string `json:"refs"`
		} `json:"causes"`
	}
	_ = json.Unmarshal(third, &h)
	if len(h.Causes) != 1 || len(h.Causes[0].Refs) != 2 {
		t.Errorf("restore_failed は同じ要素の refs[] に世代を足す: %s", third)
	}
	if verr := contract.Validate("data/state.json#/$defs/hold", third); verr != nil {
		t.Errorf("state.json の hold に合わない: %v", verr)
	}
}

// ctxState は取り消された文脈で書かれたら失敗する状態ファイルの偽物(取り消された実行の記録は、記録用の別の文脈で書く)。
type ctxState struct{ contract.StateFiles }

func (s *ctxState) Update(ctx context.Context, h contract.StateLockHandle, item string, v json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.StateFiles.Update(ctx, h, item, v)
}

// manifestDone はマニフェストが延びていれば 1(記録は snapshots[] で、保管先と食い違わない)。
func manifestDone(f *fixture, a snapshot, t0 time.Time) int {
	if f.retainOf(a.Manifest).After(t0.Add(91 * day)) {
		return 1
	}
	return 0
}
