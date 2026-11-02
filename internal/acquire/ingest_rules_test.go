package acquire

// 取得のストリームを核が確かめる規則(spec 3.2.2): アダプタの status を信じ切らない、end の後ろ・種別に合わないフレームを受けない、
// file-retry の失敗でエントリを残さない、index/files は status: ok のときだけ置き換える。

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

const pgDef = `{"version":1,"source":{"host":"127.0.0.1","targets":[{"id":"t1","postgresql":"app"}]}}`

func (b *frameBuilder) dump(format string, body []byte, status string, exit any) *frameBuilder {
	return b.item(map[string]any{"type": "dump", "format": format}, body, status, exit)
}

func targetOf(t *testing.T, e *ingestEnv, frames []contract.Frame, res json.RawMessage) TargetOutcome {
	t.Helper()
	e.script(frames, res)
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	return r.Targets[0]
}

func TestIngestRejectsFramesAfterEnd(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	fr := newFrames().dir(".").file("a", []byte("x")).end(false)
	extra, _ := json.Marshal(map[string]any{"type": "dir", "path": "evil", "mode": 493, "uid": 0, "gid": 0, "mtime_ns": 1})
	fr = append(fr, contract.Frame{Type: "dir", Meta: extra})
	if tg := targetOf(t, e, fr, okResult("ok")); tg.Status != "failed" {
		t.Errorf("end の後ろのフレームは契約違反: %+v", tg)
	}
}

func TestIngestFilesTargetNeedsRootEntry(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	if tg := targetOf(t, e, newFrames().file("a", []byte("x")).end(false), okResult("ok")); tg.Status != "failed" {
		t.Errorf("根のエントリが無いのに ok: %+v", tg)
	}
	e = newIngestEnv(t, ingestDef)
	if tg := targetOf(t, e, newFrames().end(false), okResult("ok")); tg.Status != "failed" {
		t.Errorf("空のストリームが ok: %+v", tg)
	}
}

func TestIngestFrameKindMustMatchTarget(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	if tg := targetOf(t, e, newFrames().dir(".").dump("globals", []byte("g"), "ok", 0).end(false), okResult("ok")); tg.Status != "failed" {
		t.Errorf("files の対象に dump: %+v", tg)
	}
	e = newIngestEnv(t, pgDef)
	if tg := targetOf(t, e, newFrames().file("a", []byte("x")).end(false), okResult("ok")); tg.Status != "failed" {
		t.Errorf("DB の対象に file: %+v", tg)
	}
}

// DB の対象: アダプタが ok と言っても、受け取った dump で確かめる(全項目の失敗は failed、一部は partial、両方そろえば ok)。
func TestIngestDumpStatusIsVerified(t *testing.T) {
	g, c := []byte("globals"), []byte("custom dump body")
	cases := []struct {
		name   string
		frames func() []contract.Frame
		want   string
		dumps  []string
	}{
		{"両方そろう", func() []contract.Frame {
			return newFrames().dump("globals", g, "ok", 0).dump("custom", c, "ok", 0).end(false)
		}, "ok", []string{"globals", "custom"}},
		{"両方 error", func() []contract.Frame {
			return newFrames().dump("globals", nil, "error", 1).dump("custom", nil, "error", 1).end(false)
		}, "failed", nil},
		{"globals だけ", func() []contract.Frame { return newFrames().dump("globals", g, "ok", 0).end(false) }, "partial", []string{"globals"}},
		{"custom の終了状態が 0 でない", func() []contract.Frame {
			return newFrames().dump("globals", g, "ok", 0).dump("custom", c, "ok", 3).end(false)
		}, "partial", []string{"globals"}},
		{"dump が 1 つも無い", func() []contract.Frame { return newFrames().end(false) }, "failed", nil},
	}
	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			e := newIngestEnv(t, pgDef)
			tg := targetOf(t, e, cs.frames(), okResult("ok"))
			if tg.Status != cs.want {
				t.Errorf("status は %s: %+v", cs.want, tg)
			}
			if len(tg.Dumps) != len(cs.dumps) {
				t.Errorf("dumps[] は取れた項目だけ: %v", tg.Dumps)
			}
		})
	}
}

// 読み直し(file-retry)が失敗したら、先の読みの内容を残さない(壊れているかもしれない)。
func TestIngestFailedRetryDropsEarlierRead(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	fr := newFrames().dir(".").file("f", []byte("torn")).item(map[string]any{"type": "file-retry", "path": "f", "size": 4, "mode": 420, "uid": 0, "gid": 0, "mtime_ns": 9}, nil, "error", nil).end(false)
	e.script(fr, okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := entriesOf(t, e, r)["t1/f"]; has {
		t.Error("読み直しが失敗したファイルのエントリが残った")
	}
	if r.Targets[0].Status != "partial" {
		t.Errorf("item_end の error があれば partial: %+v", r.Targets[0])
	}
}

// index/files は status: ok のときだけ置き換える(partial・failed では置き換えない。spec 4.5)。
func TestIngestIndexReplacedOnlyWhenOK(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", []byte("one")).end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(e.indexFile("t1"))
	e.script(newFrames().dir(".").file("a", []byte("two two")).end(false), okResult("partial", map[string]any{"code": "PERMISSION", "path_b64": b64("x")}))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(e.indexFile("t1")); !bytes.Equal(before, after) {
		t.Error("partial で index/files を置き換えた")
	}
	e.script(newFrames().dir(".").file("a", []byte("three")).end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(e.indexFile("t1")); bytes.Equal(before, after) {
		t.Error("ok で index/files を置き換えない")
	}
}

// 送り直したチャンクの新しい版を、前回から変化の無いファイルの写しも指す(古い版は延長されず prune に消される)。
func TestIngestUnchangedRangesFollowResentVersion(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	ne := &noExtend{ObjectStore: e.a.Store}
	e.a.Store = ne
	body := bytes.Repeat([]byte("shared "), 30)
	e.script(newFrames().dir(".").file("u", body).end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	e.a.Clock.(*fake.Clock).Advance(30 * 24 * 3600 * 1e9)
	// u は変化なし、w は同じ内容の新しいファイル(延ばせないので送り直す)。
	e.script(newFrames().dir(".").unchanged("u", len(body)).file("w", body).end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	en := entriesOf(t, e, r)
	if r.ChunkKeysMissing != 0 {
		t.Fatalf("送り直したので鍵は揃う: %d", r.ChunkKeysMissing)
	}
	u, w := *en["t1/u"].Ranges, *en["t1/w"].Ranges
	if len(u) == 0 || u[0].VID != w[0].VID {
		t.Errorf("u の範囲が古い版を指す: u %+v w %+v", u, w)
	}
	if got := content(t, e, r, en["t1/u"]); !bytes.Equal(got, body) {
		t.Error("u の内容を戻せる")
	}
}

// 送り直しで、確定済みの first_seen を仮の値に戻さない(汚染疑いの引き継ぎの起点)。
func TestIngestResendKeepsConfirmedFirstSeen(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	ne := &noExtend{ObjectStore: e.a.Store}
	e.a.Store = ne
	body := bytes.Repeat([]byte("seen "), 40)
	e.script(newFrames().dir(".").file("a", body).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	const first = "m-0123456789abcdef0123456789abcdef"
	ix := e.a.Index.(*Index)
	if err := ix.Commit(context.Background(), first, r1.ReferencedChunkIDs); err != nil {
		t.Fatal(err)
	}
	e.a.Clock.(*fake.Clock).Advance(30 * 24 * 3600 * 1e9)
	e.script(newFrames().dir(".").file("a", body).end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	for _, id := range r1.ReferencedChunkIDs[:1] {
		if ref, _ := ix.Lookup(context.Background(), id); ref == nil || ref.FirstSeen != first || ref.FirstSeenPending {
			t.Errorf("送り直しで first_seen が変わった: %+v", ref)
		}
	}
}

// 文脈の取り消しで延長が失敗しても「延ばせない」と読まない: RUN_ABORTED で止まり、対象の索引を捨てない。
func TestIngestCancelDuringExtendKeepsIndex(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	body := []byte("cancel me please")
	e.script(newFrames().dir(".").file("u", body).end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.a.Store = &cancelOnExtend{ObjectStore: e.a.Store, cancel: cancel}
	e.a.Clock.(*fake.Clock).Advance(30 * 24 * 3600 * 1e9)
	e.script(newFrames().dir(".").unchanged("u", len(body)).end(false), okResult("ok"))
	_, err := e.a.Ingest(ctx, contract.Call{Words: []string{"backup"}, RunID: testRunID})
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeRunAborted {
		t.Fatalf("RUN_ABORTED: %v", err)
	}
	if !fileExists(e.indexFile("t1")) {
		t.Error("取り消しで対象の索引を捨てた")
	}
}

type cancelOnExtend struct {
	contract.ObjectStore
	cancel context.CancelFunc
}

func (c *cancelOnExtend) Extend(ctx context.Context, key, vid string, until time.Time) error {
	c.cancel()
	return ctx.Err()
}
