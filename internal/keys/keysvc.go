package keys

// KeyService の実物(契約 contract.KeyService)。マスター鍵(keys/master.json)は KeyService が自分で読み、値は境界を越えない。
// 用途別の鍵は保存せず、使うたびにマスター鍵から HKDF で導く(spec 4.3)。

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// entryRefLabel はエントリの ref の入力の頭(spec 4.4)。
const entryRefLabel = "sitate/v1/entry-ref"

type masterKey struct {
	ID  string
	Num uint32
	Key []byte
}

type masterFile struct {
	Current string `json:"current"`
	Keys    []struct {
		KeyID string `json:"key_id"`
		Key   string `json:"key"`
	} `json:"keys"`
}

// parseKeyID は "k<n>" の n を返す(先頭に 0 を付けた形は受けない)。
func parseKeyID(id string) (uint32, bool) {
	if !strings.HasPrefix(id, "k") || len(id) < 2 || id[1] == '0' {
		return 0, false
	}
	n, err := strconv.ParseUint(id[1:], 10, 32)
	return uint32(n), err == nil && n > 0
}

// loadMasters は keys/master.json を読み、現行の鍵と全部の鍵(現行が先頭)を返す。
func loadMasters(dir string) (cur masterKey, all []masterKey, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, contract.KeysMasterFile))
	if err != nil {
		return cur, nil, errInternal()
	}
	var mf masterFile
	if json.Unmarshal(raw, &mf) != nil {
		return cur, nil, errInternal()
	}
	found := false
	for _, e := range mf.Keys {
		b, derr := base64.StdEncoding.DecodeString(e.Key)
		n, ok := parseKeyID(e.KeyID)
		if derr != nil || len(b) != contract.KeySize || !ok {
			return cur, nil, errInternal()
		}
		mk := masterKey{ID: e.KeyID, Num: n, Key: b}
		if e.KeyID == mf.Current {
			cur, found = mk, true
			continue
		}
		all = append(all, mk)
	}
	if !found {
		return cur, nil, errInternal()
	}
	return cur, append([]masterKey{cur}, all...), nil
}

func (m masterKey) wipe() { wipeBytes(m.Key) }

func wipeMasters(all []masterKey) {
	for _, m := range all {
		m.wipe()
	}
}

func (s *KeyService) cur() (masterKey, error) {
	if s.Dir == "" {
		return masterKey{}, errInternal()
	}
	c, all, err := loadMasters(s.Dir)
	if err == nil {
		wipeMasters(all[1:]) // 旧鍵は使わない
	}
	return c, err
}

// derive はマスター鍵 mk から用途 purpose の鍵(32 バイト)を導く。
func (s *KeyService) derive(mk masterKey, purpose string) ([]byte, error) {
	if s.Crypto == nil {
		return nil, errInternal()
	}
	return s.Crypto.HKDF(mk.Key, []byte(contract.HKDFInfoPrefix+purpose), contract.KeySize)
}

// DerivedKey は用途 config / journal / state / proof の鍵を返す。マスター鍵そのもの・ID 鍵・包む鍵は境界を越えない。
func (s *KeyService) DerivedKey(purpose string) ([]byte, error) {
	switch purpose {
	case "config", "journal", "state", "proof":
	default:
		return nil, errInternal()
	}
	mk, err := s.cur()
	if err != nil {
		return nil, err
	}
	defer mk.wipe()
	return s.derive(mk, purpose)
}

// keyedHex は ID 鍵による BLAKE3 の鍵付きハッシュ(出力 32 バイト)の小文字 16 進。
// contract.Crypto の BLAKE3 には鍵付きが無いため、ここで github.com/zeebo/blake3 の NewKeyed を直接使う。
func (s *KeyService) keyedSum(in []byte) []byte {
	mk, err := s.cur()
	if err != nil {
		panic(err)
	}
	defer mk.wipe()
	idk, err := s.derive(mk, "chunk-id")
	if err != nil {
		panic(err)
	}
	defer wipeBytes(idk)
	h, err := blake3.NewKeyed(idk)
	if err != nil {
		panic(errInternal())
	}
	_, _ = h.Write(in)
	return h.Sum(nil)
}

// ChunkID はチャンク ID(圧縮前の平文の鍵付きハッシュ。64 桁の小文字 16 進)。マスター鍵が読めないときは INTERNAL の誤りで panic する(戻り値に誤りの口が無い契約)。
func (s *KeyService) ChunkID(plain []byte) string { return hex.EncodeToString(s.keyedSum(plain)) }

// EntryRef はエントリの ref(`f:` + 鍵付きハッシュの先頭 16 バイトの 32 桁。入力は sitate/v1/entry-ref ∥ 0x00 ∥ パス)。
func (s *KeyService) EntryRef(path string) string {
	in := append(append([]byte(entryRefLabel), 0), path...)
	return "f:" + hex.EncodeToString(s.keyedSum(in)[:16])
}

// CandidateFingerprint は取得候補の指紋(入力は sitate/v1/uncovered ∥ 0x00 ∥ 種別 ∥ 0x00 ∥ パス)。
func (s *KeyService) CandidateFingerprint(kind, path string) string {
	in := append(append(append(append([]byte(contract.UncoveredFPLabel), 0), kind...), 0), path...)
	return hex.EncodeToString(s.keyedSum(in))
}

func keyMark(n uint32) []byte { return binary.BigEndian.AppendUint32(nil, n) }

func (s *KeyService) wrapAAD(kind, id string, mk masterKey) ([]byte, error) {
	if s.Codec == nil {
		return nil, errInternal()
	}
	return s.Codec.AAD(kind, id, contract.Header{FormatVersion: contract.FormatVersion, Mark: keyMark(mk.Num)})
}

func (s *KeyService) wrap(mk masterKey, purpose, kind, id string, plain []byte) ([]byte, error) {
	if s.Crypto == nil {
		return nil, errInternal()
	}
	wk, err := s.derive(mk, purpose)
	if err != nil {
		return nil, err
	}
	defer wipeBytes(wk)
	aad, err := s.wrapAAD(kind, id, mk)
	if err != nil {
		return nil, err
	}
	return s.Crypto.AEADSeal(plain, wk, aad)
}

// unwrap は現行の鍵、無ければ旧鍵の順に試す(包みの形に鍵 ID が無いチャンク鍵の包みのため)。
func (s *KeyService) unwrap(keys []masterKey, purpose, kind, id string, wrapped []byte) ([]byte, error) {
	if s.Crypto == nil {
		return nil, errInternal()
	}
	var last error = errInternal()
	for _, mk := range keys {
		wk, err := s.derive(mk, purpose)
		if err != nil {
			return nil, err
		}
		aad, err := s.wrapAAD(kind, id, mk)
		if err != nil {
			return nil, err
		}
		plain, oerr := s.Crypto.AEADOpen(wrapped, wk, aad)
		wipeBytes(wk)
		if oerr == nil {
			return plain, nil
		}
		last = mkErr(contract.CodeDecryptFailed, map[string]any{"object": "key_table", "chunk_id": nil, "key": nil, "version_id": nil, "reason": "tag"})
	}
	return nil, last
}

// WrapChunkKey はチャンク鍵を chunk-wrap 鍵で包む(索引に置く形。AAD は WRPC ∥ チャンク ID ∥ マスター鍵の鍵 ID)。
func (s *KeyService) WrapChunkKey(chunkKey []byte, chunkID string) ([]byte, error) {
	mk, err := s.cur()
	if err != nil {
		return nil, err
	}
	defer mk.wipe()
	return s.wrap(mk, "chunk-wrap", contract.MagicWrapChunk, chunkID, chunkKey)
}

// UnwrapChunkKey は包んだチャンク鍵を開く。別のチャンク ID では開けない。
func (s *KeyService) UnwrapChunkKey(wrapped []byte, chunkID string) ([]byte, error) {
	if s.Dir == "" {
		return nil, errInternal()
	}
	_, all, err := loadMasters(s.Dir)
	if err != nil {
		return nil, err
	}
	defer wipeMasters(all)
	return s.unwrap(all, "chunk-wrap", contract.MagicWrapChunk, chunkID, wrapped)
}

// NewSnapshotKey は乱数 32 バイトのスナップショット鍵を作り、snapshot-wrap 鍵で包んで返す(AAD は WRPS ∥ manifest_id の乱数部 ∥ 鍵 ID)。
func (s *KeyService) NewSnapshotKey(_ context.Context, manifestID string) (key, wrapped []byte, keyID string, err error) {
	mk, err := s.cur()
	if err != nil {
		return nil, nil, "", err
	}
	defer mk.wipe()
	key = make([]byte, contract.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, nil, "", errInternal()
	}
	wrapped, err = s.wrap(mk, "snapshot-wrap", contract.MagicWrapSnap, strings.TrimPrefix(manifestID, "m-"), key)
	if err != nil {
		return nil, nil, "", err
	}
	return key, wrapped, mk.ID, nil
}

// OpenSnapshotKey は keyID のマスター鍵で包みを開く。別の manifest_id では開けない。
func (s *KeyService) OpenSnapshotKey(_ context.Context, wrapped []byte, keyID, manifestID string) ([]byte, error) {
	if s.Dir == "" {
		return nil, errInternal()
	}
	_, all, err := loadMasters(s.Dir)
	if err != nil {
		return nil, err
	}
	defer wipeMasters(all)
	for _, mk := range all {
		if mk.ID == keyID {
			return s.unwrap([]masterKey{mk}, "snapshot-wrap", contract.MagicWrapSnap, strings.TrimPrefix(manifestID, "m-"), wrapped)
		}
	}
	return nil, mkErr(contract.CodeDecryptFailed, map[string]any{"object": "manifest", "chunk_id": nil, "key": nil, "version_id": nil, "reason": "tag"})
}
