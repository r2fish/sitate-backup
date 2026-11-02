package store

// レビューの差し戻し(H1〜H3、中2〜中5、中7)に対する試験。

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// H1: 鍵表が行の途中で 2 チャンクに切れた世代でも、マニフェストとデータのチャンクのすべてを延ばし、prune が通る。
func TestKeyTableSplitMidLine(t *testing.T) {
	f := newFixture(t)
	f.ktParts = 3
	t0 := f.clk.T
	a := f.addSnapshot(8, t0.Add(150*day), false)
	f.clk.Advance(100 * day)
	res, err := f.r.Extend(ctx, "A")
	harness.Implemented(t, err)
	if err != nil || len(res.Failed) != 0 || res.Extended != len(a.Refs)+1 {
		t.Fatalf("Extend: %+v %v(期待 %d)", res, err, len(a.Refs)+1)
	}
	want := t0.Add(157 * day)
	for _, id := range a.Refs {
		if got := f.retainOf(id); !got.Equal(want) {
			t.Errorf("%s が延びていない: %v", id.Key, got)
		}
	}
	if _, err := f.r.Prune(ctx); err != nil {
		t.Errorf("Prune: %v", err)
	}
}

// H1: 読めない鍵表のチャンクがあっても、マニフェストと読めた範囲の参照は延ばしてから failed[] に載せる。prune は誤りで止まって何も消さない。
func TestKeyTablePartlyUnreadable(t *testing.T) {
	f := newFixture(t)
	f.ktParts, f.badLastKT = 2, true
	t0 := f.clk.T
	a := f.addSnapshot(8, t0.Add(150*day), false)
	old := f.put("chunks/zz/old", "o")
	f.clk.Advance(100 * day)
	res, err := f.r.Extend(ctx, "A")
	if err != nil || len(res.Failed) != 1 || res.Failed[0].Code != contract.CodeDecryptFailed || res.Extended == 0 {
		t.Fatalf("Extend: %+v %v", res, err)
	}
	if got := f.retainOf(a.Manifest); !got.Equal(t0.Add(157 * day)) {
		t.Errorf("マニフェスト自身が延びていない: %v", got)
	}
	_, err = f.r.Prune(ctx)
	harness.ExpectCode(t, err, contract.CodeDecryptFailed)
	if !f.exists(objID{"chunks/zz/old", old.VersionID}) {
		t.Error("参照を引けないのに消した")
	}
}

// H2: prune が消した chunks/ の版は Index から外す。
func TestPruneRemovesFromIndex(t *testing.T) {
	f := newFixture(t)
	u := f.put("chunks/zz/unref", "u")
	keep := f.addSnapshot(1, f.clk.T.Add(500*day), false)
	f.clk.Advance(100 * day)
	res, err := f.r.Prune(ctx)
	if err != nil || res.Deleted != 1 || len(f.idx.removed) != 1 || f.idx.removed[0] != u.VersionID {
		t.Errorf("Index.Remove: %+v %+v %v", res, f.idx.removed, err)
	}
	for _, id := range keep.Refs {
		if _, ok := f.idx.recs[id.VersionID]; !ok {
			t.Errorf("保持中の世代の記録を外した: %s", id.Key)
		}
	}
}

// H3: チャンクの記録は Index から読み、延ばした後は SetRetain で更新する。保管先を読み直さない(記録が目標以上なら、保管先の値が短くても要求を送らない)。
func TestExtendUsesIndexRecords(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	a := f.addSnapshot(3, t0.Add(150*day), false)
	f.clk.Advance(100 * day)
	skip := a.Refs[0] // 記録は目標以上(保管先は短いまま)
	x := f.idx.recs[skip.VersionID]
	x.RetainUntil = t0.Add(400 * day)
	f.idx.add(x)
	res, err := f.r.Extend(ctx, "A")
	if err != nil || res.Extended != len(a.Refs) { // 記録が目標以上の 1 件を除き、マニフェストを足した数
		t.Fatalf("Extend: %+v %v", res, err)
	}
	if got := f.retainOf(skip); !got.Equal(t0.Add(90 * day)) {
		t.Errorf("記録が目標以上の版に要求を送った: %v", got)
	}
	for _, id := range a.Refs[1:] {
		if got := f.idx.recs[id.VersionID].RetainUntil; !got.Equal(t0.Add(157 * day)) {
			t.Errorf("SetRetain で記録を更新する: %s %v", id.Key, got)
		}
	}
	if res, err := f.r.Extend(ctx, "A"); err != nil || res.Extended != 0 {
		t.Errorf("2 回目は記録が目標に届いている: %+v %v", res, err)
	}
}

// H3: Index が無いまま chunks/ を延ばさない。
func TestExtendWithoutIndexStops(t *testing.T) {
	f := newFixture(t)
	a := f.addSnapshot(1, f.clk.T.Add(150*day), false)
	f.clk.Advance(100 * day)
	f.r.Index = nil
	_, err := f.r.Extend(ctx, "A")
	harness.ExpectCode(t, err, contract.CodeInternal)
	if got := f.retainOf(a.Manifest); got.After(f.clk.T.Add(-9 * day)) {
		t.Errorf("止まるはずが延びた: %v", got)
	}
}

// H3: 記録の retain-until がゼロの版を verify は照合なしにしない(retention_short に載せる)。
func TestVerifyZeroRecordIsNotSkipped(t *testing.T) {
	f := newFixture(t)
	x := f.putRef("chunks/a")
	x.RetainUntil = time.Time{}
	res, err := f.r.Verify(ctx, []contract.VersionRef{x})
	if err != nil || len(res.RetentionShort) != 1 {
		t.Errorf("ゼロの記録: %+v %v", res, err)
	}
}

// 中2: 延長の間に足された記録を、書き戻しで消さない(state.lock の中で読み直して当てる)。
func TestExtendKeepsRecordsAddedMeanwhile(t *testing.T) {
	f := newFixture(t)
	a := f.addSnapshot(2, f.clk.T.Add(150*day), false)
	f.clk.Advance(100 * day)
	f.r.Store = &onExtend{ObjectStore: f.st, hook: func() {
		f.objs = append(f.objs, map[string]any{"key": "journal/000000000009-zz", "version_id": "v9", "size": 1, "etag": "e", "retain_until": f.clk.T.Format(time.RFC3339), "written_at": f.clk.T.Format(time.RFC3339)})
		f.flush()
	}}
	if _, err := f.r.Extend(ctx, "A"); err != nil {
		t.Fatal(err)
	}
	raw, _ := f.sf.Get(ctx, "objects")
	if !strings.Contains(string(raw), "journal/000000000009-zz") {
		t.Errorf("途中で足された記録が消えた: %s", raw)
	}
	_ = a
}

type onExtend struct {
	*fake.ObjectStore
	hook func()
	once sync.Once
}

func (o *onExtend) Extend(ctx context.Context, key, vid string, u time.Time) error {
	o.once.Do(o.hook)
	return o.ObjectStore.Extend(ctx, key, vid, u)
}

// 中3: 読めない世代・記録があるとき、prune は誤りで止まり何も消さない。extend は failed[] に載せて残りを続ける。
func TestUnreadableRecordsFailSafe(t *testing.T) {
	f := newFixture(t)
	a := f.addSnapshot(2, f.clk.T.Add(150*day), false)
	old := f.put("chunks/zz/old", "o")
	f.snaps = append(f.snaps, map[string]any{"ref": "m-broken", "version_id": "v1"}) // retention_until が無い
	f.flush()
	f.clk.Advance(100 * day)
	_, err := f.r.Prune(ctx)
	harness.ExpectCode(t, err, contract.CodeInternal)
	if !f.exists(objID{"chunks/zz/old", old.VersionID}) {
		t.Error("読めない世代があるのに消した")
	}
	res, err := f.r.Extend(ctx, "A")
	if err != nil || len(res.Failed) != 1 || res.Failed[0].Key != "manifests/m-broken" || res.Extended != len(a.Refs)+1 {
		t.Errorf("Extend: %+v %v", res, err)
	}
	f.objs = append(f.objs, map[string]any{"key": "repo.json", "version_id": "v2"}) // written_at・retain_until が無い
	f.flush()
	_, err = f.r.Extend(ctx, "B")
	harness.ExpectCode(t, err, contract.CodeInternal)
}

// 中3: kit の読み取りの誤りは捨てずに返す。
func TestKitReadErrorIsReturned(t *testing.T) {
	f := newFixture(t)
	f.r.State = &failGet{StateFiles: f.sf, item: "kit"}
	_, err := f.r.Extend(ctx, "B")
	if err == nil {
		t.Error("kit の読み取りの誤りを捨てた")
	}
}

type failGet struct {
	contract.StateFiles
	item string
}

func (s *failGet) Get(ctx context.Context, item string) (json.RawMessage, error) {
	if item == s.item {
		return nil, newErr(contract.CodeInternal, nil)
	}
	return s.StateFiles.Get(ctx, item)
}

// 中4: sealed は JSON の true のときだけ進む。
func TestPruneSealedMustBeTrue(t *testing.T) {
	for _, v := range []string{`false`, `null`, `"true"`, `1`, `{}`} {
		f := newFixture(t)
		old := f.put("chunks/zz/old", "o")
		f.clk.Advance(100 * day)
		f.sf.Items["sealed"] = json.RawMessage(v)
		_, err := f.r.Prune(ctx)
		harness.ExpectCode(t, err, contract.CodeNotSealed)
		if !f.exists(objID{"chunks/zz/old", old.VersionID}) {
			t.Errorf("sealed=%s で消した", v)
		}
	}
	f := newFixture(t)
	delete(f.sf.Items, "sealed")
	_, err := f.r.Prune(ctx)
	harness.ExpectCode(t, err, contract.CodeNotSealed)
}

// 中5: manifests/ の下の記録の無いキーは UNEXPECTED_VERSION。書き込み予定のキーは除く。probe/ の記録外の版も対象。読めない形の write_intent は誤り。
func TestVerifyUnexpectedManifestsAndIntent(t *testing.T) {
	f := newFixture(t)
	f.putRef("manifests/m-evil")
	f.putRef("manifests/m-inflight")
	f.putRef("probe/zz")
	f.putRef("chunks/new-key") // 記録の無い新しい chunks/ のキーは対象外(量の比で見つける)
	f.r.Intents = func(context.Context) ([]string, error) { return []string{"manifests/m-inflight"}, nil }
	res, err := f.r.Verify(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, u := range res.UnexpectedVersions {
		got[u.Key] = true
	}
	if len(got) != 2 || !got["manifests/m-evil"] || !got["probe/zz"] {
		t.Errorf("UnexpectedVersions: %+v", res.UnexpectedVersions)
	}
	f.r.Intents = func(context.Context) ([]string, error) { return nil, newErr(contract.CodeInternal, nil) }
	_, err = f.r.Verify(ctx, nil)
	harness.ExpectCode(t, err, contract.CodeInternal) // 読めない誤りは誤りで返す
	f.r.Intents = nil
	res, err = f.r.Verify(ctx, nil) // nil は「予定なし」: 記録外の版は UNEXPECTED_VERSION のまま
	if err != nil || len(res.UnexpectedVersions) != 3 {
		t.Errorf("Intents が nil: %+v %v", res.UnexpectedVersions, err)
	}
}

// Index が nil で chunks/ を消す場面は、消す前に止まる(何も消さない)。
func TestPruneWithoutIndexStopsBeforeDeleting(t *testing.T) {
	f := newFixture(t)
	old := f.put("chunks/zz/old", "o")
	f.clk.Advance(100 * day)
	f.r.Index = nil
	_, err := f.r.Prune(ctx)
	harness.ExpectCode(t, err, contract.CodeInternal)
	if !f.exists(objID{"chunks/zz/old", old.VersionID}) {
		t.Error("Index が無いのに chunks/ を消した")
	}
}
