//go:build mainpath

package mainpath

// backup の流れ(spec 7.5。acquire.Ingest のコメントの 1〜5)をハーネスの中で行う。マニフェストの本体は spec 4.4 とスキーマから独自に組む(cli/manifest.go は読まない)。

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/acquire"
	"github.com/r2fish/sitate-backup/internal/contract"
)

type gen struct {
	MID     string
	Ing     acquire.IngestResult
	SnapKey []byte
	Version string // マニフェストの版 ID
	Size    int64
	Exp     map[string]want // 取得時点の元データから計算した期待値
}

func (w *world) jsonOf(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		w.t.Fatal(err)
	}
	return b
}

// backup は 1 世代を取って保管する。parent は直前の世代(無ければ空)。
func (w *world) backup(parent string) gen {
	t := w.t
	t.Helper()
	exp := scan(t, w.src)
	ing, err := w.acq.Ingest(w.ctx, contract.Call{Words: []string{"backup"}, RunID: "r-" + time.Now().UTC().Format("20060102T150405Z") + "-ab12"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	healthy, tg := targetsOf(t, ing)
	// 2. 参照の存在の照合。
	missing, err := w.ret.MissingRefs(w.ctx, w.refsOf(ing))
	if err != nil {
		t.Fatalf("MissingRefs: %v", err)
	}
	if len(missing) > 0 {
		t.Errorf("MissingRefs が欠けた参照を返した: %v", missing)
		healthy = false
	}
	if ing.ChunkKeysMissing > 0 {
		healthy = false
	}
	now := time.Now().UTC().Truncate(time.Second)
	class, err := w.ret.Class(contract.ClassInput{CreatedAt: now, Healthy: healthy, Timezone: "Asia/Tokyo"})
	if err != nil {
		t.Fatalf("Class: %v", err)
	}
	// 4. スナップショット鍵とマニフェスト本体。
	snap, wrapped, keyID, err := w.keyS.NewSnapshotKey(w.ctx, ing.ManifestID)
	if err != nil {
		t.Fatalf("NewSnapshotKey: %v", err)
	}
	health := "pass"
	if !healthy {
		health = "incomplete"
	}
	body := w.jsonOf(map[string]any{"format": "sitate.manifest/v1", "manifest_id": ing.ManifestID, "repo_id": w.repoID, "created_at": now.Format(time.RFC3339),
		"parent": parentOf(parent), "def_hash": w.defHash, "binary_version": w.binVer, "key_id": keyID, "retention_class": string(class),
		"definition": json.RawMessage(w.def), "targets": tg,
		"entries":    map[string]any{"format": "sitate.entries/v1", "count": ing.EntriesCount, "chunks": ing.EntriesChunks},
		"chunk_keys": map[string]any{"format": "sitate.chunkkeys/v1", "count": ing.KeyTableCount, "chunks": ing.KeyTableChunks},
		"checks":     []any{}, "health": health,
		"stats": map[string]any{"chunks_referenced": ing.ChunksReferenced, "chunks_new": ing.ChunksNew, "bytes_new": ing.BytesNew}})
	if err := contract.Validate("data/manifest.json", body); err != nil {
		t.Fatalf("マニフェストがスキーマに合わない: %v\n%s", err, body)
	}
	sealed := w.sealManifest(ing.ManifestID, keyID, wrapped, snap, body)
	res, err := w.put.Put(w.ctx, "manifests/"+ing.ManifestID, bytes.NewReader(sealed), class)
	if err != nil {
		t.Fatalf("Put manifests: %v", err)
	}
	// 5. 状態に世代を足し、索引を確定する。
	w.recordGen(ing, res, now, class, health)
	if err := w.idx.Commit(w.ctx, ing.ManifestID, ing.ReferencedChunkIDs); err != nil {
		t.Fatalf("Index.Commit: %v", err)
	}
	w.lastMID = ing.ManifestID
	return gen{MID: ing.ManifestID, Ing: ing, SnapKey: snap, Version: res.VersionID, Size: res.Size, Exp: exp}
}

func parentOf(parent string) any {
	if parent == "" {
		return nil
	}
	return parent
}

// targetsOf は対象の結果をマニフェストの targets[] に写し、全対象が ok かを返す。
func targetsOf(t *testing.T, ing acquire.IngestResult) (bool, []map[string]any) {
	t.Helper()
	healthy := true
	var tg []map[string]any
	for _, o := range ing.Targets {
		if o.Status != "ok" {
			healthy = false
			t.Errorf("対象 %s の status=%s error_code=%s errors=%v", o.ID, o.Status, o.ErrorCode, o.Errors)
		}
		tg = append(tg, map[string]any{"id": o.ID, "kind": o.Kind, "status": o.Status, "bytes": o.Bytes, "files": o.Files, "changed_during_read": o.ChangedDuringRead})
	}
	return healthy, tg
}

// recordGen は状態の snapshots[] に世代を足す。
func (w *world) recordGen(ing acquire.IngestResult, res contract.PutResult, now time.Time, class contract.RetentionClass, health string) {
	var snaps []json.RawMessage
	if raw, _ := w.st.Get(w.ctx, "snapshots"); len(raw) > 0 {
		_ = json.Unmarshal(raw, &snaps)
	}
	snaps = append(snaps, w.jsonOf(map[string]any{"ref": ing.ManifestID, "version_id": res.VersionID, "size": res.Size, "etag": res.ETag,
		"retain_until": res.RetainUntil.UTC().Format(time.RFC3339), "created_at": now.Format(time.RFC3339), "stored_at": now.Format(time.RFC3339),
		"retention_class": string(class), "retention_until": res.RetainUntil.UTC().Format(time.RFC3339), "health": health, "targets_ok": health == "pass",
		"restore_verified": nil, "refs_verified": nil, "restore_failed": false, "def_copy_mismatch": false, "baseline": nil, "format_version": contract.FormatVersion}))
	w.setItem("snapshots", snaps)
}

func (w *world) refsOf(ing acquire.IngestResult) []contract.VersionRef {
	var refs []contract.VersionRef
	add := func(l []acquire.ChunkInfo) {
		for _, c := range l {
			refs = append(refs, contract.VersionRef{Key: "chunks/" + c.ID[:2] + "/" + c.ID, VersionID: c.VersionID})
		}
	}
	add(ing.EntriesChunks)
	add(ing.KeyTableChunks)
	return refs
}

// sealManifest は Codec.Encode(MagicManifest, ...)。見出しの印は spec 4.3 の「マスター鍵の鍵 ID(4 バイトのビッグエンディアン)」。
func (w *world) sealManifest(mid, keyID string, wrapped, snap, body []byte) []byte {
	n, err := strconv.Atoi(strings.TrimPrefix(keyID, "k"))
	if err != nil {
		w.t.Fatalf("鍵 ID %q", keyID)
	}
	h := contract.Header{Kind: contract.MagicManifest, FormatVersion: contract.FormatVersion, Mark: binary.BigEndian.AppendUint32(nil, uint32(n)), Wrapped: wrapped}
	out, err := w.codec.Encode(contract.MagicManifest, strings.TrimPrefix(mid, "m-"), h, body, snap)
	if err != nil {
		w.t.Fatalf("Codec.Encode(SITM): %v", err)
	}
	return out
}

// restoreTo は (a) その場の restore を実物の Restore に通す。snapKey は包みを開いた値(別の鍵の試験で差し替える)。
func (w *world) restoreTo(backup string, snapKey []byte, to string) (json.RawMessage, error) {
	line := contract.DestKeyLine{SnapshotKey: b64(snapKey)}
	line.ReadKey.KeyID, line.ReadKey.Secret = w.s3.rk, w.s3.rs
	lb, _ := json.Marshal(line)
	r, err := w.rst.Restore(w.ctx, contract.Call{Words: []string{"restore"}, SnapshotKeyIn: contract.NewSecret(lb), Args: map[string][]string{
		contract.ArgTo: {to}, contract.ArgBackup: {backup}, contract.ArgEndpoint: {w.s3.ep}, contract.ArgBucket: {w.s3.bucket}, contract.ArgPrefix: {w.prefix}}})
	return r.Result, err
}

func b64(b []byte) string { return base64Std.EncodeToString(b) }

var base64Std = base64.StdEncoding

// forgeManifest は gen のマニフェストを復号して edit で書き換え、同じスナップショット鍵で別の manifest_id として(新しいスナップショット鍵で)置き直す。新しい ID と鍵を返す。
func (w *world) forgeManifest(g gen, edit func(m map[string]any)) (string, []byte) {
	t := w.t
	rc, err := w.put.Get(w.ctx, "manifests/"+g.MID, g.Version)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := io.ReadAll(rc)
	rc.Close()
	plain, err := w.codec.Decode(contract.MagicManifest, strings.TrimPrefix(g.MID, "m-"), sealed, g.SnapKey)
	if err != nil {
		t.Fatalf("自分のマニフェストを Decode できない: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(plain, &m); err != nil {
		t.Fatal(err)
	}
	nid := "m-" + strings.Repeat("cd", 16)
	m["manifest_id"] = nid
	edit(m)
	body, _ := json.Marshal(m)
	snap, wrapped, keyID, err := w.keyS.NewSnapshotKey(w.ctx, nid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.put.Put(w.ctx, "manifests/"+nid, bytes.NewReader(w.sealManifest(nid, keyID, wrapped, snap, body)), contract.RetentionDaily); err != nil {
		t.Fatal(err)
	}
	return nid, snap
}

// forgeEntries はエントリの先頭のチャンクの、範囲を持つ最初のエントリの先頭の範囲の版 ID を存在しない値にしたチャンクを、
// 同じチャンク ID の新しい版として置き、それを指す世代(別の manifest_id)を返す。
func (w *world) forgeEntries(g gen) (string, []byte) {
	t := w.t
	ref := g.Ing.EntriesChunks[0]
	key := "chunks/" + ref.ID[:2] + "/" + ref.ID
	rc, err := w.put.Get(w.ctx, key, ref.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := io.ReadAll(rc)
	rc.Close()
	ck, _ := base64Std.DecodeString(ref.Key)
	plain, err := w.codec.Open(sealed, ck, ref.ID)
	if err != nil {
		t.Fatalf("エントリのチャンクを開けない: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(plain), "\n"), "\n")
	done := false
	for i, l := range lines {
		var e map[string]any
		if json.Unmarshal([]byte(l), &e) != nil {
			t.Fatalf("エントリの行: %s", l)
		}
		rs, ok := e["ranges"].([]any)
		if !ok || len(rs) == 0 || done {
			continue
		}
		rs[0].([]any)[1] = "00000000-0000-0000-0000-000000000000"
		nb, _ := json.Marshal(e)
		lines[i], done = string(nb), true
	}
	if !done {
		t.Fatalf("範囲を持つエントリが先頭のチャンクに無い")
	}
	forged, err := w.codec.Seal([]byte(strings.Join(lines, "\n")+"\n"), ck, ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.put.Put(w.ctx, key, bytes.NewReader(forged), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	return w.forgeManifest(g, func(m map[string]any) {
		m["entries"].(map[string]any)["chunks"].([]any)[0].(map[string]any)["version_id"] = res.VersionID
	})
}
