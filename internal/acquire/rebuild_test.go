package acquire

// 索引の再構成(Index.Rebuild)の試験: 取得で作った世代のマニフェストの本体から、重複排除に使える索引と index/files/<id> を作り直す。

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func manifestBodyOf(r IngestResult, created, def string) []byte {
	b, _ := json.Marshal(map[string]any{"manifest_id": r.ManifestID, "created_at": created, "definition": json.RawMessage(def), "targets": []map[string]any{{"id": "t1", "status": "ok"}},
		"entries":    map[string]any{"chunks": r.EntriesChunks},
		"chunk_keys": map[string]any{"chunks": r.KeyTableChunks}})
	return b
}

func TestIndexRebuildFromManifests(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	body := bytes.Repeat([]byte("rebuild me "), 40)
	e.script(newFrames().dir(".").file("a", body).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	// 索引を壊す(無くす)。
	if err := os.RemoveAll(filepath.Join(e.a.Dir, "index")); err != nil {
		t.Fatal(err)
	}
	ix := e.a.Index.(*Index)
	ix.Store, ix.loaded = e.a.Store, false
	bad := []byte(`{"manifest_id":"m-bad","created_at":"2026-01-01T00:00:00Z","entries":{"chunks":[{"id":"` + r1.EntriesChunks[0].ID + `","version_id":"nope","key":"AAAA"}]},"chunk_keys":{"chunks":[]}}`)
	if err := ix.Rebuild(context.Background(), [][]byte{bad, manifestBodyOf(r1, "2026-11-02T00:00:00Z", ingestDef)}, []byte(ingestDef)); err != nil {
		t.Fatal(err)
	}
	if sk := ix.Skipped(); len(sk) != 1 || sk[0].Ref != "m-bad" || sk[0].Reason != "missing" {
		t.Errorf("読めない世代は飛ばして理由を返す: %+v", sk)
	}
	ref, err := ix.Lookup(context.Background(), r1.EntriesChunks[0].ID)
	if err != nil || ref == nil || ref.VersionID != r1.EntriesChunks[0].VersionID || ref.FirstSeen != r1.ManifestID {
		t.Fatalf("エントリのチャンクの記録: %+v %v", ref, err)
	}
	if !fileExists(e.indexFile("t1")) {
		t.Error("定義が同じ世代から index/files/t1 を作る")
	}
	// 再構成した索引で、同じ内容は重複排除される(データのチャンクを送り直さない)。
	e.script(newFrames().dir(".").file("a", body).end(false), okResult("ok"))
	r2, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if r2.ChunksNew >= r1.ChunksNew {
		t.Errorf("再構成した索引で重複排除が効く: %d → %d", r1.ChunksNew, r2.ChunksNew)
	}
}

// 根が今の定義と違う世代からは index/files/<id> を作らない(チャンクの記録は作る)。読める世代が無ければ誤りで、古い索引を残す。
func TestIndexRebuildRespectsDefinitionAndKeepsOldOnFailure(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", []byte("x")).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	ix := e.a.Index.(*Index)
	ix.Store = e.a.Store
	other := `{"version":1,"source":{"host":"127.0.0.1","targets":[{"id":"t1","files":"/other"}]}}`
	_ = os.Remove(e.indexFile("t1"))
	if err := ix.Rebuild(context.Background(), [][]byte{manifestBodyOf(r1, "2026-11-02T00:00:00Z", other)}, []byte(ingestDef)); err != nil {
		t.Fatal(err)
	}
	if fileExists(e.indexFile("t1")) {
		t.Error("根の違う世代から index/files を作った")
	}
	if err := ix.Rebuild(context.Background(), [][]byte{[]byte("garbage")}, nil); err == nil {
		t.Error("読める世代が無いのに成功した")
	}
	if ref, _ := ix.Lookup(context.Background(), r1.EntriesChunks[0].ID); ref == nil {
		t.Error("失敗したとき古い索引を残す")
	}
}
