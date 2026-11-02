package restore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// chunkRef は マニフェスト本体の entries.chunks[]・chunk_keys.chunks[] の要素(spec 4.4)。
type chunkRef struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	Key       string `json:"key"` // base64(32 バイト)
}

// manifestDoc はマニフェスト本体の、復元が読む項目(spec 4.4)。
type manifestDoc struct {
	ManifestID string          `json:"manifest_id"`
	DefHash    string          `json:"def_hash"`
	KeyID      string          `json:"key_id"`
	Health     string          `json:"health"`
	Definition json.RawMessage `json:"definition"`
	Targets    []struct {
		ID     string            `json:"id"`
		Status string            `json:"status"`
		Files  int64             `json:"files"`
		Dumps  []json.RawMessage `json:"dumps"`
		Bytes  int64             `json:"bytes"`
	} `json:"targets"`
	Entries struct {
		Chunks []chunkRef `json:"chunks"`
	} `json:"entries"`
	ChunkKeys struct {
		Chunks []chunkRef `json:"chunks"`
	} `json:"chunk_keys"`
}

// objReader は記録した版 ID でオブジェクトを読み、開く。
type objReader struct {
	store contract.ObjectStore
	codec contract.Codec
	keyS  contract.KeyService
	read  int64 // 保管先から読んだ暗号文の合計
}

// readAll は key の版 versionID を読む。版が無ければ contract.ErrMissing。
func (o *objReader) readAll(ctx context.Context, key, versionID string) ([]byte, error) {
	rc, err := o.store.Get(ctx, key, versionID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	o.read += int64(len(b))
	return b, err
}

// decryptErr は DECRYPT_FAILED(object: manifest / entries / key_table / config)を作る。形式の版は FORMAT_TOO_NEW / OLD。
func decryptErr(object, key, versionID, chunkID string, err error) error {
	var fe *contract.FormatError
	reason := "tag"
	switch {
	case errors.Is(err, contract.ErrMissing):
		reason = "missing"
	case errors.As(err, &fe):
		switch fe.Reason {
		case "header":
			reason = "header"
		case "too_new":
			return newErr(contract.CodeFormatTooNew, map[string]any{"required": fe.Required, "supported": nonNil(fe.Supported)})
		case "too_old":
			return newErr(contract.CodeFormatTooOld, map[string]any{"found": fe.Required, "supported": nonNil(fe.Supported)})
		}
	case isDecrypt(err):
	default:
		return err // 保管先の到達性などは呼び出し側へ
	}
	var cid, ver any
	if chunkID != "" {
		cid = chunkID
	}
	if versionID != "" {
		ver = versionID
	}
	return newErr(contract.CodeDecryptFailed, map[string]any{"object": object, "chunk_id": cid, "key": key, "version_id": ver, "reason": reason})
}

func isDecrypt(err error) bool {
	var ce *contract.Error
	return errors.As(err, &ce) && ce.Code == contract.CodeDecryptFailed
}

func nonNil(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}

// keyIDFromMark はマニフェストの見出しの印(マスター鍵の鍵 ID。4 バイトのビッグエンディアン)から "k<n>" を作る。印が無ければ空。
func keyIDFromMark(mark []byte) string {
	if len(mark) != 4 {
		return ""
	}
	n := uint32(mark[0])<<24 | uint32(mark[1])<<16 | uint32(mark[2])<<8 | uint32(mark[3])
	return "k" + strconv.FormatUint(uint64(n), 10)
}

// manifest は manifests/<ref> の版を読んで本体を開く。snapKey が nil なら見出しの包んだスナップショット鍵を KeyService で開く。
// 返す 2 つ目はスナップショット鍵(復元先へ渡すため)。
func (o *objReader) manifest(ctx context.Context, ref, versionID string, snapKey []byte) (*manifestDoc, []byte, error) {
	key := "manifests/" + ref
	sealed, err := o.readAll(ctx, key, versionID)
	if err != nil {
		return nil, nil, decryptErr("manifest", key, versionID, "", err)
	}
	if snapKey == nil {
		h, herr := o.codec.Header(sealed)
		if herr != nil {
			return nil, nil, decryptErr("manifest", key, versionID, "", herr)
		}
		if snapKey, err = o.keyS.OpenSnapshotKey(ctx, h.Wrapped, keyIDFromMark(h.Mark), ref); err != nil {
			return nil, nil, err
		}
	}
	plain, err := o.codec.Decode(contract.MagicManifest, strings.TrimPrefix(ref, "m-"), sealed, snapKey)
	if err != nil {
		return nil, nil, decryptErr("manifest", key, versionID, "", err)
	}
	var m manifestDoc
	if json.Unmarshal(plain, &m) != nil {
		return nil, nil, decryptErr("manifest", key, versionID, "", &contract.FormatError{Reason: "header"})
	}
	return &m, snapKey, nil
}

// chunkBody はチャンクを読んで開いた平文。object は DECRYPT_FAILED の分類(entries / key_table)。
func (o *objReader) chunkBody(ctx context.Context, c chunkRef, object string) ([]byte, error) {
	key := "chunks/" + c.ID[:2] + "/" + c.ID
	sealed, err := o.readAll(ctx, key, c.VersionID)
	if err != nil {
		return nil, decryptErr(object, key, c.VersionID, c.ID, err)
	}
	ck, err := base64.StdEncoding.DecodeString(c.Key)
	if err != nil {
		return nil, decryptErr(object, key, c.VersionID, c.ID, &contract.FormatError{Reason: "tag"})
	}
	plain, err := o.codec.Open(sealed, ck, c.ID)
	if err != nil {
		return nil, decryptErr(object, key, c.VersionID, c.ID, err)
	}
	return plain, nil
}

// joined はチャンク列の平文を順につないだもの(エントリ・鍵表の JSON Lines)。
func (o *objReader) joined(ctx context.Context, chunks []chunkRef, object string) ([]byte, error) {
	var buf bytes.Buffer
	for _, c := range chunks {
		if len(c.ID) < 2 {
			return nil, decryptErr(object, "", "", "", &contract.FormatError{Reason: "header"})
		}
		b, err := o.chunkBody(ctx, c, object)
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	return buf.Bytes(), nil
}

// keyRow は鍵表の 1 行(sitate.chunkkeys/v1)。
type keyRow struct {
	ID string `json:"id"`
	V  string `json:"v"`
	K  string `json:"k"`
}

// keyTable は鍵表を読む(データのチャンク ID → 版 ID・鍵)。
func (o *objReader) keyTable(ctx context.Context, m *manifestDoc) ([]keyRow, error) {
	b, err := o.joined(ctx, m.ChunkKeys.Chunks, "key_table")
	if err != nil {
		return nil, err
	}
	var rows []keyRow
	for _, ln := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var r keyRow
		if json.Unmarshal(ln, &r) != nil || r.ID == "" {
			return nil, decryptErr("key_table", "", "", "", &contract.FormatError{Reason: "header"})
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func fmtErr() error { return &contract.FormatError{Reason: "header"} }
