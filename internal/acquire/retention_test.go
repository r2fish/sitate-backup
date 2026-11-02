package acquire

// 重複排除のときの保持(spec 4.3「索引と prune の整合」、4.2 の延長の目標 A): 書き込みの応答の値を索引に記録し、
// 既存のチャンクを参照するとき記録の retain-until が目標 A に足りなければ版を指定して延ばしてから参照する。延ばせなければ参照せず送り直す。

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// noExtend は Extend が失敗する保管先。
type noExtend struct {
	contract.ObjectStore
	extends int
}

func (n *noExtend) Extend(context.Context, string, string, time.Time) error {
	n.extends++
	return errors.New("延長できない")
}

func (e *ingestEnv) records(t *testing.T) map[string]contract.VersionRef {
	t.Helper()
	l, err := e.a.Index.Records(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]contract.VersionRef{}
	for _, r := range l {
		m[r.Key] = r
	}
	return m
}

func TestIngestRecordsPutResponseInIndex(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", bytes.Repeat([]byte("r"), 100)).end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	recs := e.records(t)
	if int64(len(recs)) < r.ChunksNew {
		t.Fatalf("送ったチャンクの記録: %d < %d", len(recs), r.ChunksNew)
	}
	for k, v := range recs {
		if v.VersionID == "" || v.Size <= 0 || v.ETag == "" || v.RetainUntil.IsZero() || v.WrittenAt.IsZero() {
			t.Errorf("%s の記録に書き込みの応答の値が無い: %+v", k, v)
		}
		info, _ := e.a.Store.ListVersions(context.Background(), k)
		if len(info) != 1 || info[0].Size != v.Size {
			t.Errorf("%s の大きさが保管先と違う: %+v %+v", k, v, info)
		}
	}
}

// 2 回目の実行で時計が進み、記録の retain-until が目標 A に足りないとき、延ばして記録を更新し、送り直さない。
func TestIngestExtendsShortRetentionBeforeReuse(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	body := bytes.Repeat([]byte("keep "), 60)
	e.script(newFrames().dir(".").file("a", body).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	before := e.records(t)
	e.a.Clock.(*fake.Clock).Advance(30 * 24 * time.Hour)
	e.script(newFrames().dir(".").file("a", body).end(false), okResult("ok"))
	r2, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if r2.ChunksNew >= r1.ChunksNew {
		t.Errorf("延ばせたので送り直さない: %d → %d", r1.ChunksNew, r2.ChunksNew)
	}
	want := e.a.Clock.(*fake.Clock).T.AddDate(0, 0, contract.RetentionDailyDays+7)
	grew := 0
	for k, v := range e.records(t) {
		if old, ok := before[k]; ok && v.RetainUntil.After(old.RetainUntil) {
			grew++
			if v.RetainUntil.Before(want.Add(-time.Second)) {
				t.Errorf("目標 A(%v)に足りない: %v", want, v.RetainUntil)
			}
		}
	}
	if grew == 0 {
		t.Error("索引の retain-until を更新していない")
	}
	if got := content(t, e, r2, entriesOf(t, e, r2)["t1/a"]); !bytes.Equal(got, body) {
		t.Error("延ばしたチャンクから戻せる")
	}
}

// 延ばせないときは参照せずに送り直す。
func TestIngestResendsWhenExtendFails(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	ne := &noExtend{ObjectStore: e.a.Store}
	e.a.Store = ne
	body := bytes.Repeat([]byte("resend "), 50)
	e.script(newFrames().dir(".").file("a", body).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	e.a.Clock.(*fake.Clock).Advance(30 * 24 * time.Hour)
	e.script(newFrames().dir(".").file("a", body).end(false), okResult("ok"))
	r2, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if ne.extends == 0 || r2.ChunksNew < r1.ChunksNew {
		t.Errorf("延ばせないので送り直す: extends %d, %d → %d", ne.extends, r1.ChunksNew, r2.ChunksNew)
	}
	if got := content(t, e, r2, entriesOf(t, e, r2)["t1/a"]); !bytes.Equal(got, body) {
		t.Error("送り直したチャンクから戻せる")
	}
}

// 前回から変化の無いファイル(unchanged)の範囲も同じ: 延ばせなければ読み直していないので送り直せず、鍵の無いチャンクとして数え、対象の索引を捨てる。
func TestIngestUnchangedRangesNeedRetentionToo(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	ne := &noExtend{ObjectStore: e.a.Store}
	e.a.Store = ne
	body := []byte("unchanged body")
	e.script(newFrames().dir(".").file("u", body).end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	e.a.Clock.(*fake.Clock).Advance(30 * 24 * time.Hour)
	e.script(newFrames().dir(".").unchanged("u", len(body)).end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if r.ChunkKeysMissing == 0 {
		t.Error("延ばせないチャンクを参照した")
	}
	if fileExists(e.indexFile("t1")) {
		t.Error("次の backup が全部を読み直せるよう、対象の索引を捨てる")
	}
	// 延ばせる保管先なら、unchanged の範囲も延ばして参照できる。
	e2 := newIngestEnv(t, ingestDef)
	e2.script(newFrames().dir(".").file("u", body).end(false), okResult("ok"))
	_, _ = e2.run(t)
	e2.a.Clock.(*fake.Clock).Advance(30 * 24 * time.Hour)
	e2.script(newFrames().dir(".").unchanged("u", len(body)).end(false), okResult("ok"))
	r2, err := e2.run(t)
	if err != nil || r2.ChunkKeysMissing != 0 {
		t.Fatalf("延ばして参照できる: %v %+v", err, r2.ChunkKeysMissing)
	}
	if got := content(t, e2, r2, entriesOf(t, e2, r2)["t1/u"]); !bytes.Equal(got, body) {
		t.Error("内容を戻せる")
	}
}

// マニフェストを書いた後の Commit: 仮の first_seen(印あり)の行を、その世代に付け替えて印を外す。印の無い行は変えない。
func TestIndexCommitFirstSeen(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", []byte("x")).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	ix := e.a.Index.(*Index)
	id := r1.ReferencedChunkIDs[0]
	if ref, _ := ix.Lookup(context.Background(), id); ref == nil || !ref.FirstSeenPending {
		t.Fatalf("書いただけの行は仮: %+v", ref)
	}
	if err := ix.Commit(context.Background(), "m-"+"0123456789abcdef0123456789abcdef", r1.ReferencedChunkIDs); err != nil {
		t.Fatal(err)
	}
	for _, id := range r1.ReferencedChunkIDs {
		ref, _ := newIndex2(ix).Lookup(context.Background(), id)
		if ref == nil || ref.FirstSeenPending || ref.FirstSeen != "m-0123456789abcdef0123456789abcdef" {
			t.Errorf("確定していない: %+v", ref)
		}
	}
	// 確定済みの行は次の Commit で付け替えない。
	if err := ix.Commit(context.Background(), "m-ffffffffffffffffffffffffffffffff", r1.ReferencedChunkIDs); err != nil {
		t.Fatal(err)
	}
	if ref, _ := ix.Lookup(context.Background(), id); ref.FirstSeen != "m-0123456789abcdef0123456789abcdef" {
		t.Errorf("確定済みの first_seen を付け替えた: %s", ref.FirstSeen)
	}
}

func newIndex2(ix *Index) *Index { return &Index{Dir: ix.Dir, Codec: ix.Codec, KeyS: ix.KeyS} }
