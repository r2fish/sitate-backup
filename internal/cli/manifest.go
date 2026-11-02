package cli

// backup の流れの 4(spec 7.5): マニフェストの組み立てと書き込みの置き場。取得の入口(acquire.Ingest)の出力は、結線(完了条件 24)のときに
// ManifestMaterial へ写す(cli のこのファイルは acquire を import しない)。健常性の決定のうち汚染疑いの引き継ぎ・チェックの結果は
// イテレーション 1 には無い(チェックの無い世代。spec 8.2、作業指示書 5.17 の完了条件 23)。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ChunkRefMaterial はエントリと鍵表のチャンクの参照(マニフェストの entries.chunks[]・chunk_keys.chunks[])。
type ChunkRefMaterial struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	Key       string `json:"key"`
}

// TargetMaterial は対象ごとの取得の結果(マニフェストの targets[] の元)。
type TargetMaterial struct {
	ID                string
	Kind              string // files / postgresql
	Status            string // ok / partial / failed
	Files, Bytes      int64
	ChangedDuringRead int64
	Dumps             []string // postgresql のダンプの形式(送った順)
}

// ManifestMaterial はマニフェストを組む材料。
type ManifestMaterial struct {
	ManifestID     string
	RepoID         string
	CreatedAt      time.Time
	Parent         string // 無ければ空(最初の世代)
	DefHash        string
	BinaryVersion  string
	Definition     json.RawMessage // 承認済みの定義の正規化した写し
	Targets        []TargetMaterial
	EntriesCount   int
	EntriesChunks  []ChunkRefMaterial
	KeyTableCount  int
	KeyTableChunks []ChunkRefMaterial

	ChunksReferenced int
	ChunksNew        int64
	BytesNew         int64
	ChunkKeysMissing int
	// RefsMissing は参照の存在の照合(Retention.MissingRefs)で欠けた参照があったこと。健常性を incomplete にする。
	RefsMissing bool
}

// manifestHealth はイテレーション 1 の健常性(チェックが無い): 全対象が ok で鍵の無いチャンクが無ければ pass、それ以外は incomplete。
// failed の対象は取得していないので、そのことは targets[] の status が示す(spec 4.4)。
func manifestHealth(m ManifestMaterial) string {
	if m.ChunkKeysMissing > 0 || m.RefsMissing {
		return "incomplete"
	}
	for _, t := range m.Targets {
		if t.Status != "ok" {
			return "incomplete"
		}
	}
	return "pass"
}

// allTargetsFailed は全対象が failed か(マニフェストを書かずに ALL_TARGETS_FAILED で終える。spec 4.4)。
func allTargetsFailed(ts []TargetMaterial) bool {
	if len(ts) == 0 {
		return false
	}
	for _, t := range ts {
		if t.Status != "failed" {
			return false
		}
	}
	return true
}

// buildManifestBody はマニフェストの本体 JSON(sitate.manifest/v1)を組む。健常性 pass の判定は manifestHealth。
func buildManifestBody(m ManifestMaterial, keyID string, class contract.RetentionClass) ([]byte, string, error) {
	health := manifestHealth(m)
	targets := make([]map[string]any, 0, len(m.Targets))
	for _, t := range m.Targets {
		e := map[string]any{"id": t.ID, "kind": t.Kind, "status": t.Status, "bytes": t.Bytes}
		if t.Kind == "postgresql" {
			dumps := make([]map[string]string, 0, len(t.Dumps))
			for _, f := range t.Dumps {
				dumps = append(dumps, map[string]string{"format": f})
			}
			e["dumps"] = dumps
		} else {
			e["files"], e["changed_during_read"] = t.Files, t.ChangedDuringRead
		}
		targets = append(targets, e)
	}
	var parent any
	if m.Parent != "" {
		parent = m.Parent
	}
	refs := func(l []ChunkRefMaterial) []ChunkRefMaterial {
		if l == nil {
			return []ChunkRefMaterial{}
		}
		return l
	}
	body := map[string]any{
		"format": "sitate.manifest/v1", "manifest_id": m.ManifestID, "repo_id": m.RepoID,
		"created_at": m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), "parent": parent, "def_hash": m.DefHash,
		"binary_version": m.BinaryVersion, "key_id": keyID, "retention_class": string(class), "definition": m.Definition,
		"targets":    targets,
		"entries":    map[string]any{"format": "sitate.entries/v1", "count": m.EntriesCount, "chunks": refs(m.EntriesChunks)},
		"chunk_keys": map[string]any{"format": "sitate.chunkkeys/v1", "count": m.KeyTableCount, "chunks": refs(m.KeyTableChunks)},
		"checks":     []any{}, "health": health,
		"stats": map[string]any{"chunks_referenced": m.ChunksReferenced, "chunks_new": m.ChunksNew, "bytes_new": m.BytesNew},
	}
	b, err := json.Marshal(body)
	return b, string(health), err
}

// ManifestWriter はマニフェストの符号化と書き込み(backup の流れの 4)。部品は結線のときに注入する。
type ManifestWriter struct {
	Codec contract.Codec
	KeyS  contract.KeyService
	Store contract.ObjectStore
}

// ManifestWritten は書いたマニフェストの記録(snapshots[] に足す元)。
type ManifestWritten struct {
	Ref    string
	Health string
	Class  contract.RetentionClass
	Put    contract.PutResult
}

// Write はスナップショット鍵を作って包み、本体を符号化して manifests/<manifest_id> に保持区分つきで書く。
// 保持区分の決定(Retention.Class)は呼び出し側が行い class で渡す。
func (w ManifestWriter) Write(ctx context.Context, m ManifestMaterial, class contract.RetentionClass) (ManifestWritten, error) {
	key, wrapped, keyID, err := w.KeyS.NewSnapshotKey(ctx, m.ManifestID)
	if err != nil {
		return ManifestWritten{}, err
	}
	defer wipe(key)
	body, health, err := buildManifestBody(m, keyID, class)
	if err != nil {
		return ManifestWritten{}, internalErr()
	}
	mark, err := keyIDMark(keyID)
	if err != nil {
		return ManifestWritten{}, internalErr()
	}
	h := contract.Header{Kind: contract.MagicManifest, FormatVersion: contract.FormatVersion, Mark: mark, Wrapped: wrapped}
	sealed, err := w.Codec.Encode(contract.MagicManifest, strings.TrimPrefix(m.ManifestID, "m-"), h, body, key)
	if err != nil {
		return ManifestWritten{}, internalErr()
	}
	put, err := w.Store.Put(ctx, "manifests/"+m.ManifestID, bytes.NewReader(sealed), class)
	if err != nil {
		return ManifestWritten{}, err
	}
	return ManifestWritten{Ref: m.ManifestID, Health: health, Class: class, Put: put}, nil
}

// keyIDMark は "k<n>" を見出しの印(n の 4 バイトのビッグエンディアン。spec 4.3)にする。
func keyIDMark(keyID string) ([]byte, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(keyID, "k"), 10, 32)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(n))
	return b, nil
}
