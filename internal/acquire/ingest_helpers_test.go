package acquire

// 取得の入口(Ingest・Run)の試験の道具: 偽の取得アダプタが流すストリームを組み立てる。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// frameBuilder は sitate.stream/v1 のフレーム列と、end の hash・bytes を組み立てる。
type frameBuilder struct {
	frag   int // データの断片の大きさ(0 なら 5 バイト)
	frames []contract.Frame
	all    *blake3.Hasher
	total  int64
}

func newFrames() *frameBuilder { return &frameBuilder{all: blake3.New()} }

func (b *frameBuilder) add(h map[string]any, body []byte) {
	meta, _ := json.Marshal(h)
	b.frames = append(b.frames, contract.Frame{Type: h["type"].(string), Meta: meta, Data: body})
	b.all.Write(meta)
	b.all.Write([]byte("\n"))
	if len(body) > 0 {
		b.all.Write(body)
		b.total += int64(len(body))
	}
}

func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func (b *frameBuilder) dir(path string) *frameBuilder {
	b.add(map[string]any{"type": "dir", "path": path, "mode": 493, "uid": 0, "gid": 0, "mtime_ns": 1}, nil)
	return b
}

func (b *frameBuilder) symlink(path, target string) *frameBuilder {
	b.add(map[string]any{"type": "symlink", "path": path, "target": target, "uid": 0, "gid": 0, "mtime_ns": 1}, nil)
	return b
}

// item は file / file-retry / dump の始まり・本体(断片に分ける)・item_end。
func (b *frameBuilder) item(head map[string]any, body []byte, status string, exit any) *frameBuilder {
	b.add(head, nil)
	frag := b.frag
	if frag == 0 {
		frag = 5
	}
	for off := 0; off < len(body); off += frag {
		end := off + frag
		if end > len(body) {
			end = len(body)
		}
		b.add(map[string]any{"type": "data", "size": end - off}, body[off:end])
	}
	b.add(map[string]any{"type": "item_end", "bytes": len(body), "hash": harness.Blake3Tag(body), "status": status, "exit_status": exit}, nil)
	return b
}

func (b *frameBuilder) file(path string, body []byte) *frameBuilder {
	return b.item(map[string]any{"type": "file", "path": path, "size": len(body), "mode": 420, "uid": 1000, "gid": 1000, "mtime_ns": 7}, body, "ok", nil)
}

func (b *frameBuilder) retry(path string, body []byte, status string) *frameBuilder {
	return b.item(map[string]any{"type": "file-retry", "path": path, "size": len(body), "mode": 420, "uid": 1000, "gid": 1000, "mtime_ns": 8}, body, status, nil)
}

func (b *frameBuilder) unchanged(path string, size int) *frameBuilder {
	b.add(map[string]any{"type": "unchanged", "path": path, "size": size, "mode": 420, "uid": 1000, "gid": 1000, "mtime_ns": 7}, nil)
	return b
}

// end は end のフレームを足して、フレーム列を返す。bad が真なら hash を壊す。
func (b *frameBuilder) end(bad bool) []contract.Frame {
	h := "blake3:" + fmt.Sprintf("%x", b.all.Sum(nil))
	if bad {
		h = "blake3:" + fmt.Sprintf("%064d", 0)
	}
	meta, _ := json.Marshal(map[string]any{"type": "end", "bytes": b.total, "hash": h, "exit_status": 0})
	return append(b.frames, contract.Frame{Type: "end", Meta: meta})
}

func okResult(status string, errs ...map[string]any) json.RawMessage {
	if errs == nil {
		errs = []map[string]any{}
	}
	return rawJSON(map[string]any{"contract": "sitate.acquire/v1", "status": status, "stats": map[string]any{"files": 1, "bytes": 1, "errors": len(errs),
		"errors_omitted": 0, "warnings_omitted": 0, "changed_during_read": 0}, "errors": errs, "warnings": []any{}})
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

const ingestDef = `{"version":1,"source":{"host":"127.0.0.1","targets":[{"id":"t1","files":"/srv/app"}]}}`

// ingestEnv は取得の入口の試験の環境(偽の保管先・本物に近い切り方と封緘・状態ディレクトリ付き)。
type ingestEnv struct {
	a  *Acquire
	ad *fake.AdapterRunner
}

func newIngestEnv(t *testing.T, def string) *ingestEnv {
	t.Helper()
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	dir := t.TempDir()
	codec := &harness.RefCodec{}
	st := &fake.StateFiles{Def: []byte(def), Items: map[string]json.RawMessage{"initialized": json.RawMessage("true"), "sealed": json.RawMessage("true")}}
	ad := fake.NewAdapterRunner()
	a := &Acquire{Store: store, State: st, Index: &Index{Dir: dir, Codec: codec, KeyS: fake.KeyService{}}, Codec: codec, KeyS: fake.KeyService{},
		Adapters: ad, Clock: &fake.Clock{T: fake.StartTime}, Dir: dir}
	return &ingestEnv{a: a, ad: ad}
}

func (e *ingestEnv) script(frames []contract.Frame, result json.RawMessage) {
	e.ad.Script("acquire", fake.Step{Frames: frames, Result: result})
}

func (e *ingestEnv) run(t *testing.T) (IngestResult, error) {
	t.Helper()
	return e.a.Ingest(context.Background(), contract.Call{Words: []string{"backup"}, RunID: testRunID})
}

// readChunk は保管先から版を指定してチャンクを読み、鍵で開く。
func (e *ingestEnv) readChunk(t *testing.T, id, vid, keyB64 string) []byte {
	t.Helper()
	rc, err := e.a.Store.Get(context.Background(), chunkKeyOf(id), vid)
	if err != nil {
		t.Fatalf("チャンク %s を読めない: %v", id, err)
	}
	defer rc.Close()
	var sealed []byte
	buf := make([]byte, 1<<16)
	for {
		n, err := rc.Read(buf)
		sealed = append(sealed, buf[:n]...)
		if err != nil {
			break
		}
	}
	key, _ := base64.StdEncoding.DecodeString(keyB64)
	plain, err := e.a.Codec.Open(sealed, key, id)
	if err != nil {
		t.Fatalf("チャンク %s を開けない: %v", id, err)
	}
	return plain
}

// concat はチャンクの並びを順に読んでつなぐ(エントリ・鍵表の本文)。
func (e *ingestEnv) concat(t *testing.T, l []ChunkInfo) []byte {
	var out []byte
	for _, c := range l {
		out = append(out, e.readChunk(t, c.ID, c.VersionID, c.Key)...)
	}
	return out
}

func (e *ingestEnv) indexFile(tid string) string {
	return filepath.Join(e.a.Dir, "index", "files", tid)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
