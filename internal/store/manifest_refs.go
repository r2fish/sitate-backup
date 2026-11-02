package store

// マニフェストが参照するオブジェクトの列挙(spec 4.4、4.2 の延長の目標 A・B と prune の「参照されない」の判定)。
// 本体を開き(スナップショット鍵)、エントリと鍵表のチャンクの参照と、鍵表(チャンク鍵で暗号化したチャンク)からデータのチャンクの参照を得る。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type chunkRef struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	Key       string `json:"key"`
}

type manifestBody struct {
	DefHash       string `json:"def_hash"`
	BinaryVersion string `json:"binary_version"`
	Entries       struct {
		Chunks []chunkRef `json:"chunks"`
	} `json:"entries"`
	ChunkKeys struct {
		Chunks []chunkRef `json:"chunks"`
	} `json:"chunk_keys"`
}

// mfInfo はマニフェストから引いた参照。
type mfInfo struct {
	DefHash, BinaryVersion string
	Refs                   []objID // エントリ・鍵表・データのチャンク
}

func chunkKey(id string) string {
	if len(id) < 2 {
		return "chunks/" + id
	}
	return "chunks/" + id[:2] + "/" + id
}

func (r *Retention) readAll(ctx context.Context, key, versionID string) ([]byte, error) {
	rc, err := r.Store.Get(ctx, key, versionID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// decryptFail は開けないオブジェクトを DECRYPT_FAILED にする(spec 3.1.3)。
func decryptFail(object string, chunkID *string, key, versionID, reason string) error {
	var k, v any
	if key != "" {
		k = key
	}
	if versionID != "" {
		v = versionID
	}
	return newErr(contract.CodeDecryptFailed, map[string]any{"object": object, "chunk_id": chunkID, "key": k, "version_id": v, "reason": reason})
}

var zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

// plainJSON は本体が zstd の圧縮のままなら伸ばす(Codec が伸ばして返す実装でも、返さない実装でも読めるように)。
func plainJSON(b []byte) ([]byte, error) {
	if !bytes.HasPrefix(b, zstdMagic) {
		return b, nil
	}
	d, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	return d.DecodeAll(b, nil)
}

// walk はマニフェスト ref(版 versionID)が参照するオブジェクトを引く。
func (r *Retention) walk(ctx context.Context, ref, versionID string) (mfInfo, error) {
	var info mfInfo
	if r.Codec == nil || r.KeyS == nil {
		return info, nilDep()
	}
	mkey := "manifests/" + ref
	sealed, err := r.readAll(ctx, mkey, versionID)
	if err != nil {
		if isMissing(err) {
			return info, decryptFail("manifest", nil, mkey, versionID, "missing")
		}
		return info, err
	}
	h, err := r.Codec.Header(sealed)
	if err != nil || len(h.Mark) < 4 {
		return info, decryptFail("manifest", nil, mkey, versionID, "header")
	}
	keyID := "k" + strconv.FormatUint(uint64(binary.BigEndian.Uint32(h.Mark[:4])), 10)
	snapKey, err := r.KeyS.OpenSnapshotKey(ctx, h.Wrapped, keyID, ref)
	if err != nil {
		return info, err
	}
	plain, err := r.Codec.Decode(contract.MagicManifest, strings.TrimPrefix(ref, "m-"), sealed, snapKey)
	if err == nil {
		plain, err = plainJSON(plain)
	}
	var body manifestBody
	if err == nil {
		err = json.Unmarshal(plain, &body)
	}
	if err != nil {
		return info, decryptFail("manifest", nil, mkey, versionID, "tag")
	}
	info.DefHash, info.BinaryVersion = body.DefHash, body.BinaryVersion
	for _, c := range body.Entries.Chunks {
		info.Refs = append(info.Refs, objID{chunkKey(c.ID), c.VersionID})
	}
	for _, c := range body.ChunkKeys.Chunks {
		info.Refs = append(info.Refs, objID{chunkKey(c.ID), c.VersionID})
	}
	data, err := r.keyTable(ctx, body.ChunkKeys.Chunks)
	info.Refs = append(info.Refs, data...) // 読めた範囲の参照は、誤りでも返す(呼び出し側が延ばしてから failed[] に載せる)
	return info, err
}

// keyTable は鍵表のチャンクを順に開いて連結し、行(JSON Lines の {"id","v","k"})に分けて、データのチャンクの参照を返す。
// 鍵表は FastCDC で切るので行の途中で切れる。読めないチャンクがあれば、そこまでの完全な行だけを返して誤りを返す。
func (r *Retention) keyTable(ctx context.Context, chunks []chunkRef) ([]objID, error) {
	var all []byte
	var ferr error
	for _, c := range chunks {
		plain, err := r.openChunk(ctx, "key_table", c)
		if err != nil {
			ferr = err
			if i := bytes.LastIndexByte(all, '\n'); i >= 0 {
				all = all[:i+1]
			} else {
				all = nil
			}
			break
		}
		all = append(all, plain...)
	}
	var out []objID
	for _, line := range bytes.Split(all, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row struct {
			ID string `json:"id"`
			V  string `json:"v"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return out, decryptFail("key_table", nil, "", "", "tag")
		}
		out = append(out, objID{chunkKey(row.ID), row.V})
	}
	return out, ferr
}

func (r *Retention) openChunk(ctx context.Context, object string, c chunkRef) ([]byte, error) {
	ck := chunkKey(c.ID)
	sealed, err := r.readAll(ctx, ck, c.VersionID)
	if err != nil {
		if isMissing(err) {
			return nil, decryptFail(object, &c.ID, ck, c.VersionID, "missing")
		}
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(c.Key)
	if err != nil {
		return nil, decryptFail(object, &c.ID, ck, c.VersionID, "tag")
	}
	plain, err := r.Codec.Open(sealed, key, c.ID)
	if err != nil {
		return nil, decryptFail(object, &c.ID, ck, c.VersionID, "tag")
	}
	return plain, nil
}
