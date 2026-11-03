package store

// verify の retain-until の照合の範囲(作業指示書 5.3 の完了条件 6、spec 4.2): 前回の verify 以後に書いた版は全件、それより前は抜き取り(続きから、末尾で先頭へ)。
// 版指定の読み取りの回数を数える保管先の覆いで確かめる。

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// counting は ListVersions が retain-until を運ばない保管先(実物と同じ)で、版指定の読み取りを数える。
type counting struct {
	*fake.ObjectStore
	mu    sync.Mutex
	reads map[objID]int
}

func (c *counting) ListVersions(ctx context.Context, p string) ([]contract.VersionInfo, error) {
	vs, err := c.ObjectStore.ListVersions(ctx, p)
	for i := range vs {
		vs[i].RetainUntil = time.Time{}
	}
	return vs, err
}

func (c *counting) RetainUntil(ctx context.Context, key, vid string) (time.Time, error) {
	c.mu.Lock()
	c.reads[objID{key, vid}]++
	c.mu.Unlock()
	vs, err := c.ObjectStore.ListVersions(ctx, key)
	if err != nil {
		return time.Time{}, err
	}
	for _, v := range vs {
		if v.Key == key && v.VersionID == vid {
			return v.RetainUntil, nil
		}
	}
	return time.Time{}, contract.ErrMissing
}

func (c *counting) take() map[objID]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.reads
	c.reads = map[objID]int{}
	return r
}

func newCounting(f *fixture) *counting {
	c := &counting{ObjectStore: f.st, reads: map[objID]int{}}
	f.r.Store = c
	return c
}

func (f *fixture) putRef(key string) contract.VersionRef {
	r := f.put(key, "d-"+key)
	return contract.VersionRef{Key: key, VersionID: r.VersionID, Size: r.Size, ETag: r.ETag, RetainUntil: r.RetainUntil}
}

func (f *fixture) lastVerify() lastVerify {
	var lv lastVerify
	raw, _ := f.sf.Get(ctx, "last_verify")
	_ = json.Unmarshal(raw, &lv)
	return lv
}

// 前回の verify 以後に書いた版は全件、それより前は 1 回あたり VERIFY_SAMPLE_PER_MONTH / 30 件の抜き取り。続きから読み、末尾まで読んだら先頭に戻る。
func TestVerifyReadsNewVersionsAllAndOldBySample(t *testing.T) {
	f := newFixture(t)
	c := newCounting(f)
	per := contract.VerifySamplePerMonth / 30
	var old []contract.VersionRef
	for i := 0; i < per+50; i++ { // 古い版 150 件(書き込み時刻は 1 秒ずつ違う)
		f.clk.Advance(time.Second)
		old = append(old, f.putRef("chunks/o/"+string(rune('a'+i/26))+string(rune('a'+i%26))))
	}
	// 1 回目: 前回の verify が無いので全件。
	f.clk.Advance(day)
	res, err := f.r.Verify(ctx, old)
	harness.Implemented(t, err)
	if err != nil || len(res.RetentionShort) != 0 || len(c.take()) != len(old) {
		t.Fatalf("1 回目は全件を読む: %+v %v", res, err)
	}
	lv := f.lastVerify()
	if lv.ListedAt.IsZero() || lv.SampleCursor != nil {
		t.Fatalf("last_verify: %+v", lv)
	}
	// 2 回目の前に新しい版を 7 件書く。新しい 7 件は全件、古い 150 件からは per 件。
	f.clk.Advance(day)
	var fresh []contract.VersionRef
	for i := 0; i < 7; i++ {
		f.clk.Advance(time.Second)
		fresh = append(fresh, f.putRef("chunks/n/"+string(rune('a'+i))))
	}
	f.clk.Advance(day)
	all := append(append([]contract.VersionRef{}, old...), fresh...)
	res2, err := f.r.Verify(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Sampled != per { // 抜き取りの件数(前回以後に書いた 7 件は全件読む分で数えない)
		t.Errorf("Sampled = %d, 期待 %d", res2.Sampled, per)
	}
	r2 := c.take()
	for _, x := range fresh {
		if r2[objID{x.Key, x.VersionID}] != 1 {
			t.Errorf("前回以後に書いた版は全件読む: %s", x.Key)
		}
	}
	if len(r2) != len(fresh)+per {
		t.Fatalf("2 回目に読んだ版 = %d, 期待 %d(新しい版 + 抜き取り)", len(r2), len(fresh)+per)
	}
	for _, x := range old[:per] {
		if r2[objID{x.Key, x.VersionID}] != 1 {
			t.Errorf("抜き取りは書き込み時刻の古い順の先頭から: %s", x.Key)
		}
	}
	cur := f.lastVerify().SampleCursor
	if cur == nil || cur.Key != old[per-1].Key || cur.VersionID != old[per-1].VersionID {
		t.Fatalf("抜き取りの続きの位置: %+v", cur)
	}
	verifyThirdRun(t, f, c, all, old, fresh, per)
}

// verifyThirdRun は 3 回目: 続き(per..149)から読み、足りない分は先頭へ戻る。2 回目で新しかった版は、今回は前回の verify より前になったので抜き取りの側。
func verifyThirdRun(t *testing.T, f *fixture, c *counting, all, old, fresh []contract.VersionRef, per int) {
	t.Helper()
	f.clk.Advance(day)
	if _, err := f.r.Verify(ctx, all); err != nil {
		t.Fatal(err)
	}
	r3 := c.take()
	if len(r3) != per {
		t.Fatalf("3 回目に読んだ版 = %d, 期待 %d", len(r3), per)
	}
	for _, x := range append(append([]contract.VersionRef{}, old[per:]...), fresh...) { // 2 回目で新しかった版も、今回は書き込み時刻順の末尾の側
		if r3[objID{x.Key, x.VersionID}] != 1 {
			t.Errorf("続きから読む: %s", x.Key)
		}
	}
	wrapped := 0
	for _, x := range old[:per] {
		wrapped += r3[objID{x.Key, x.VersionID}]
	}
	if wrapped != per-len(old[per:])-len(fresh) {
		t.Errorf("末尾の後は先頭に戻る: 先頭から %d 件", wrapped)
	}
}

// 明示した値より短い retain-until は retention_short[]、記録より TIME_SKEW_MAX を超えて長い版は retention_long[]。範囲の内は載せない。
func TestVerifyRetentionShortAndLong(t *testing.T) {
	f := newFixture(t)
	newCounting(f)
	ok := f.putRef("chunks/ok")
	short := f.putRef("chunks/short")
	short.RetainUntil = short.RetainUntil.Add(time.Second) // 1 秒でも短ければ短い(明示した値より短い)
	long := f.putRef("chunks/long")
	long.RetainUntil = long.RetainUntil.Add(-contract.TimeSkewMax - 2*time.Second)
	near := f.putRef("chunks/near")
	near.RetainUntil = near.RetainUntil.Add(-contract.TimeSkewMax + time.Minute)
	res, err := f.r.Verify(ctx, []contract.VersionRef{ok, short, long, near})
	harness.Implemented(t, err)
	if err != nil || len(res.RetentionShort) != 1 || res.RetentionShort[0].Key != "chunks/short" || len(res.RetentionLong) != 1 || res.RetentionLong[0].Key != "chunks/long" {
		t.Errorf("short=%+v long=%+v %v", res.RetentionShort, res.RetentionLong, err)
	}
	if !res.RetentionShort[0].Actual.Before(res.RetentionShort[0].Recorded) || !res.RetentionLong[0].Actual.After(res.RetentionLong[0].Recorded) {
		t.Errorf("Recorded・Actual: %+v %+v", res.RetentionShort[0], res.RetentionLong[0])
	}
}

// 取り消された verify は last_verify を進めない(最後まで終えた verify だけが境目になる)。
func TestVerifyAbortedKeepsLastVerify(t *testing.T) {
	f := newFixture(t)
	c := newCounting(f)
	refs := []contract.VersionRef{f.putRef("chunks/a")}
	cx, cancel := context.WithCancel(ctx)
	f.r.Store = &cancelRead{counting: c, cancel: cancel}
	_, err := f.r.Verify(cx, refs)
	harness.ExpectCode(t, err, contract.CodeRunAborted)
	if raw, _ := f.sf.Get(ctx, "last_verify"); len(raw) != 0 {
		t.Errorf("取り消された verify が last_verify を書いた: %s", raw)
	}
}

type cancelRead struct {
	*counting
	cancel context.CancelFunc
}

func (c *cancelRead) RetainUntil(ctx context.Context, key, vid string) (time.Time, error) {
	c.cancel()
	return c.counting.RetainUntil(ctx, key, vid)
}

// 存在と大きさ: 記録した版が無い・大きさが違う版は missing[]。非現行の版と削除マーカーの数は probe/ を除く。
func TestVerifyMissingSizeAndCounts(t *testing.T) {
	f := newFixture(t)
	a := f.putRef("chunks/a")
	f.putRef("chunks/a") // 同じキーの 2 つ目の版(記録の無い版)
	wrongSize := f.putRef("chunks/b")
	wrongSize.Size++
	p := f.putRef("probe/x")
	if _, err := f.core.Delete("b", "probe/x", "", fake.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.Delete("b", "chunks/a", "", fake.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	res, err := f.r.Verify(ctx, []contract.VersionRef{a, wrongSize, p, {Key: "chunks/gone", VersionID: "v-none"}})
	harness.Implemented(t, err)
	if err != nil || len(res.Missing) != 2 {
		t.Fatalf("missing[]: %+v %v", res.Missing, err)
	}
	if len(res.UnexpectedVersions) != 1 || res.UnexpectedVersions[0].Key != "chunks/a" {
		t.Errorf("記録した版 ID のあるキーの記録の無い版: %+v", res.UnexpectedVersions)
	}
	if res.DeleteMarkers != 1 {
		t.Errorf("削除マーカーの数は probe/ を除く: %d", res.DeleteMarkers)
	}
}
