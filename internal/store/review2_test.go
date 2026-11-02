package store

// レビューの差し戻し(H4、H5、中1、軽微 L2、中7 の変異)に対する試験。

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// H4: 時計が合ったまま TIME_SKEW_MAX の半分より長く要求が無かった後でも、書き込みは止まらない。Date を読み直してから計算し、retain-until は新しい Date 基準。
func TestPutAfterIdleRefreshesDate(t *testing.T) {
	px := newProxied(t)
	wall := time.Now()
	px.S.wall = func() time.Time { return wall }
	if _, err := px.S.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionDaily); err != nil {
		t.Fatal(err)
	}
	px.Clock.T = px.Clock.T.Add(6 * time.Minute) // 保管先も手元の時計も 6 分進んだ(その間は要求なし)
	wall = wall.Add(6 * time.Minute)
	n := px.P.count()
	r, err := px.S.Put(ctx, "chunks/b", strings.NewReader("x"), contract.RetentionDaily)
	harness.Implemented(t, err)
	if err != nil {
		t.Fatalf("古い Date で止まった: %v", err)
	}
	px.P.mu.Lock()
	first := px.P.reqs[n]
	px.P.mu.Unlock()
	if first.Method != "GET" || !strings.Contains(first.Path, "versioning") {
		t.Errorf("要求の前に Date を読み直す: %s %s", first.Method, first.Path)
	}
	if want := px.Clock.T.AddDate(0, 0, contract.RetentionDailyDays); !r.RetainUntil.Equal(want) {
		t.Errorf("retain-until は新しい Date 基準: %v 期待 %v", r.RetainUntil, want)
	}
}

// 中1: CheckSkew は古い Date を読み直してから判定し、ずれていれば TIME_SKEW。Retention の Extend・Prune・Verify は保管先へ書かず・読まずに TIME_SKEW で止まる。
func TestRetentionOpsStopOnSkew(t *testing.T) {
	px := newProxied(t)
	rt := &Retention{Store: px.S, State: &fake.StateFiles{Items: map[string]json.RawMessage{"sealed": json.RawMessage("true")}}, Codec: &harness.RefCodec{}, KeyS: fake.KeyService{}, Clock: &fake.Clock{T: px.Clock.T}}
	if err := px.S.CheckSkew(ctx); err != nil {
		t.Fatalf("合っている時計: %v", err)
	}
	px.S.Offset = offs(contract.TimeSkewMax + time.Minute)
	harness.ExpectCode(t, px.S.CheckSkew(ctx), contract.CodeTimeSkew)
	n := px.P.count()
	_, err := rt.Extend(ctx, "B")
	harness.ExpectCode(t, err, contract.CodeTimeSkew)
	_, err = rt.Prune(ctx)
	harness.ExpectCode(t, err, contract.CodeTimeSkew)
	_, err = rt.Verify(ctx, nil)
	harness.ExpectCode(t, err, contract.CodeTimeSkew)
	if got := px.P.count(); got != n {
		t.Errorf("TIME_SKEW の後に要求を送った: %d 件", got-n)
	}
}

// 中1: 延長の途中で TIME_SKEW になったら残りを止めて誤りで返す(failed[] にだけ載せて終了 0 にしない)。
func TestExtendStopsOnSkewMidway(t *testing.T) {
	f := newFixture(t)
	f.addSnapshot(6, f.clk.T.Add(150*day), false)
	f.clk.Advance(100 * day)
	f.r.Store = &skewOnExtend{ObjectStore: f.st}
	_, err := f.r.Extend(ctx, "A")
	harness.ExpectCode(t, err, contract.CodeTimeSkew)
}

type skewOnExtend struct{ *fake.ObjectStore }

func (s *skewOnExtend) Extend(context.Context, string, string, time.Time) error {
	return newErr(contract.CodeTimeSkew, map[string]any{"skew_s": 400, "max_s": 300})
}

// L2: 未知の保持区分は書かない(INTERNAL。要求を送らない)。版 ID の無い応答は成功にしない。
func TestPutRejectsUnknownClassAndEmptyVersion(t *testing.T) {
	px := newProxied(t)
	if _, err := px.S.BucketConfig(ctx); err != nil {
		t.Fatal(err)
	}
	n := px.P.count()
	_, err := px.S.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionClass("weekly"))
	harness.ExpectCode(t, err, contract.CodeInternal)
	if px.P.count() != n {
		t.Error("未知の保持区分で要求を送った")
	}
	px.P.hook = func(_ int, r *http.Request, _ http.ResponseWriter) int {
		if r.Method == "PUT" {
			return http.StatusOK // 版 ID の無い成功
		}
		return 0
	}
	_, err = px.S.Put(ctx, "chunks/b", strings.NewReader("x"), contract.RetentionDaily)
	harness.ExpectCode(t, err, contract.CodeInternal)
}

// M20: 版 ID の無い削除・延長・読み出しは要求を送らず INTERNAL。
func TestEmptyVersionIDSendsNothing(t *testing.T) {
	px := newProxied(t)
	if _, err := px.S.BucketConfig(ctx); err != nil {
		t.Fatal(err)
	}
	n := px.P.count()
	harness.ExpectCode(t, px.S.DeleteVersion(ctx, "chunks/a", ""), contract.CodeInternal)
	harness.ExpectCode(t, px.S.Extend(ctx, "chunks/a", "", time.Now()), contract.CodeInternal)
	_, err := px.S.Get(ctx, "chunks/a", "")
	harness.ExpectCode(t, err, contract.CodeInternal)
	_, err = px.S.RetainUntil(ctx, "chunks/a", "")
	harness.ExpectCode(t, err, contract.CodeInternal)
	if px.P.count() != n {
		t.Errorf("版 ID の無い操作が要求を送った: %d 件", px.P.count()-n)
	}
}

// M19: PutNoRetention・版指定なしの削除・リーガルホールドは probe/ のキーだけ。それ以外は要求を送らず INTERNAL。
func TestProbeOnlyOperations(t *testing.T) {
	px := newProxied(t)
	if _, err := px.S.BucketConfig(ctx); err != nil {
		t.Fatal(err)
	}
	n := px.P.count()
	_, err := px.S.PutNoRetention(ctx, "chunks/a", []byte("x"))
	harness.ExpectCode(t, err, contract.CodeInternal)
	harness.ExpectCode(t, px.S.UnversionedDelete(ctx, "chunks/a"), contract.CodeInternal)
	harness.ExpectCode(t, px.S.SetLegalHold(ctx, "chunks/a", "v", true), contract.CodeInternal)
	if px.P.count() != n {
		t.Errorf("probe/ 以外へ要求を送った: %d 件", px.P.count()-n)
	}
}

// H5: unversioned_delete・legal_hold は実物の *ObjectStore を要し、違う型は誤りで止まる(黙って別の操作に落ちない)。
func TestProbeTryNeedsObjectStore(t *testing.T) {
	_, _, core := fake.NewTestStore(fake.RoleAdmin)
	wr := &fake.ObjectStore{Core: core, Bucket: "b", Role: fake.RoleWrite, Params: contract.FixedRetention{}}
	p := &StoreProbe{Keys: map[string]contract.ObjectStore{"write": wr}}
	if _, err := p.PutProbe(ctx, "probe/x", []byte("p")); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"unversioned_delete", "legal_hold"} {
		_, err := p.Try(ctx, "write", op)
		harness.ExpectCode(t, err, contract.CodeInternal)
	}
	if _, err := p.Try(ctx, "write", "delete_version"); err == nil {
		t.Error("spec が試さない op(delete_version)を試せる形にしている")
	}
}

// M6: prune は probe/ の削除マーカーだけを消す(ほかの削除マーカーは DELETE_MARKER_FOUND の証拠として残す)。
func TestPruneKeepsNonProbeDeleteMarkers(t *testing.T) {
	f := newFixture(t)
	f.put("chunks/zz/a", "a")
	f.put("probe/p", "p")
	for _, k := range []string{"chunks/zz/a", "probe/p"} {
		if _, err := f.core.Delete("b", k, "", fake.RoleAdmin); err != nil {
			t.Fatal(err)
		}
	}
	f.clk.Advance(100 * day)
	if _, err := f.r.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	markers := map[string]int{}
	vs, _ := f.st.ListVersions(ctx, "")
	for _, v := range vs {
		if v.DeleteMarker {
			markers[v.Key]++
		}
	}
	if markers["chunks/zz/a"] != 1 || markers["probe/p"] != 0 {
		t.Errorf("削除マーカー: %v", markers)
	}
}

// M8: 同じチャンクを参照する保持中の世代が複数のとき、目標 A は最も遅い保持期限(世代の並びの順によらない)。
func TestExtendALatestWinsForSharedChunk(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	first := f.addSnapshot(1, t0.Add(180*day), false)
	f.addSnapshot(1, t0.Add(120*day), false, first.Refs[0]) // 後の世代の保持期限のほうが早い
	f.clk.Advance(100 * day)
	if _, err := f.r.Extend(ctx, "A"); err != nil {
		t.Fatal(err)
	}
	if got := f.retainOf(first.Refs[0]); !got.Equal(t0.Add(187 * day)) {
		t.Errorf("共有チャンクは最も遅い保持期限 + 7 日: %v", got)
	}
}

// M9・M12: 確認済みキットの核の版と config/ の最新版は目標 B(共有物)。使われていない bin/ の版・古い config/ は延ばさない。
func TestExtendBKitBinAndLatestConfig(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	obj := func(key string, at time.Time) objID { return f.addObject(key, f.put(key, "x"), at) }
	kit := obj("bin/0.0.9/linux-x86_64/sitate", t0)
	unused := obj("bin/0.0.1/linux-x86_64/sitate", t0)
	cfgOld := obj("config/aa", t0)
	f.clk.Advance(30 * day)
	cfgNew := obj("config/bb", f.clk.T)
	f.clk.Advance(70 * day)
	setItem(f.sf, "kit", map[string]any{"confirmed_core_version": "0.0.9"})
	f.r.BinVersion = "0.2.0"
	if _, err := f.r.Extend(ctx, "B"); err != nil {
		t.Fatal(err)
	}
	want := f.clk.T.Add(90 * day)
	for _, id := range []objID{kit, cfgNew} {
		if got := f.retainOf(id); !equalWithin(got, want, 2*time.Second) {
			t.Errorf("%s は共有物: %v", id.Key, got)
		}
	}
	for _, id := range []objID{unused, cfgOld} {
		if got := f.retainOf(id); got.After(t0.Add(91 * day)) {
			t.Errorf("%s は対象でない: %v", id.Key, got)
		}
	}
}

// M15: 前回の verify の境目と同じ時刻に書かれた版は「前回以後」として全件読む。
func TestVerifyBoundaryVersionIsFresh(t *testing.T) {
	f := newFixture(t)
	c := newCounting(f)
	var refs []contract.VersionRef
	n := contract.VerifySamplePerMonth/30 + 20 // 抜き取りの上限より多い件数を、境目と同じ時刻に書く
	for i := 0; i < n; i++ {
		refs = append(refs, f.putRef("chunks/b"+string(rune('a'+i/26))+string(rune('a'+i%26))))
	}
	setItem(f.sf, "last_verify", map[string]any{"listed_at": f.clk.T.Format(time.RFC3339), "sample_cursor": nil})
	if _, err := f.r.Verify(ctx, refs); err != nil {
		t.Fatal(err)
	}
	if got := len(c.take()); got != n {
		t.Errorf("境目の版も全件読む: %d 件(期待 %d)", got, n)
	}
}

// M16: 記録より長く延ばされていた版を保管先が短縮として拒否したら、成功とみなして記録を目標の値にする(failed[] に載せない)。
func TestExtendShorteningRejectionIsSuccess(t *testing.T) {
	f := newFixture(t)
	t0 := f.clk.T
	id := f.addObject("repo.json", f.put("repo.json", "{}"), t0)
	if err := f.core.PutRetention("b", id.Key, id.VersionID, t0.Add(500*day), fake.RoleWrite); err != nil { // 記録は 90 日、保管先は 500 日
		t.Fatal(err)
	}
	f.clk.Advance(100 * day)
	res, err := f.r.Extend(ctx, "B")
	if err != nil || len(res.Failed) != 0 {
		t.Fatalf("Extend: %+v %v", res, err)
	}
	raw, _ := f.sf.Get(ctx, "objects")
	var recs []struct {
		RetainUntil time.Time `json:"retain_until"`
	}
	_ = json.Unmarshal(raw, &recs)
	if len(recs) != 1 || !equalWithin(recs[0].RetainUntil, f.clk.T.Add(90*day), 2*time.Second) {
		t.Errorf("記録を目標の値にする: %s", raw)
	}
}

// M22: 各削除の直前の hold の読み取りが失敗したら、消さずに誤りで止まる(fail closed)。
func TestPruneStopsWhenHoldUnreadable(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 3; i++ {
		f.put("chunks/zz/o"+string(rune('a'+i)), "o")
	}
	f.clk.Advance(100 * day)
	f.r.State = &failNth{StateFiles: f.sf, item: "hold", from: 2}
	res, err := f.r.Prune(ctx)
	if err == nil || res.Deleted != 0 {
		t.Errorf("hold が読めないのに消した: %+v %v", res, err)
	}
}

type failNth struct {
	contract.StateFiles
	item    string
	from, n int
}

func (s *failNth) Get(ctx context.Context, item string) (json.RawMessage, error) {
	if item == s.item {
		s.n++
		if s.n >= s.from {
			return nil, newErr(contract.CodeInternal, nil)
		}
	}
	return s.StateFiles.Get(ctx, item)
}
