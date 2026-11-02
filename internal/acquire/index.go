package acquire

// Index: チャンクの既知一覧(spec 4.5 の index/chunks、4.3「索引とチャンク鍵」)。
// 1 記録ごとに状態鍵で SITS 形式に暗号化し、各記録の前に 4 バイトのビッグエンディアンの長さを置いて追記する(先行書き込み)。
// 末尾が長さに満たないか復号に失敗する記録は書きかけとして捨てる(版 ID の無いチャンクは送り直す)。

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// IndexUnreadable は index/chunks を開けないこと(途中の記録の破損)。契約の Index には知らせる口が無いので、呼び出し側(cli)は errors.As でこれを見て
// Rebuild を起こす。Unwrap で *contract.Error(INTERNAL)にも読める。
type IndexUnreadable struct{ Err *contract.Error }

func (e *IndexUnreadable) Error() string { return "index/chunks を開けない" }
func (e *IndexUnreadable) Unwrap() error { return e.Err }

// Index は contract.Index の実物。Dir は状態ディレクトリ、KeyS は状態鍵(DerivedKey("state"))を出す。
type Index struct {
	Dir   string
	Codec contract.Codec
	KeyS  contract.KeyService
	// Store は再構成(Rebuild)がエントリと鍵表のチャンクを読む保管先(再構成を使わないなら nil でよい)。
	Store contract.ObjectStore
	Now   func() time.Time // 書き込み時刻の源(nil なら実時刻)

	mu      sync.Mutex
	loaded  bool
	m       map[string]*contract.ChunkRef
	skipped []SkippedBackup
	written map[string]time.Time // チャンク ID → 書き込み時刻(Records の WrittenAt)
	good    int64                // 読めた記録の終わりのオフセット(これより後ろは捨てる)
}

var _ contract.Index = (*Index)(nil)

const indexRel = "index/chunks"

type idxRec struct {
	Op            string    `json:"op"` // reserve | version | retain | remove | commit
	ChunkID       string    `json:"chunk_id,omitempty"`
	ChunkIDs      []string  `json:"chunk_ids,omitempty"`
	VersionID     string    `json:"version_id,omitempty"`
	WrappedKey    []byte    `json:"wrapped_key,omitempty"`
	FirstSeen     string    `json:"first_seen,omitempty"`
	Pending       bool      `json:"first_seen_pending,omitempty"`
	FormatVersion int       `json:"format_version,omitempty"`
	Size          int64     `json:"size,omitempty"`
	ETag          string    `json:"etag,omitempty"`
	RetainUntil   time.Time `json:"retain_until,omitempty"`
	WrittenAt     time.Time `json:"written_at,omitempty"`
}

func (ix *Index) path() string { return filepath.Join(ix.Dir, filepath.FromSlash(indexRel)) }

func (ix *Index) header() contract.Header {
	return contract.Header{Kind: contract.MagicState, FormatVersion: contract.FormatVersion, Mark: make([]byte, contract.MasterKeyIDSize)}
}

func (ix *Index) ready() error {
	if ix.Codec == nil || ix.KeyS == nil || ix.Dir == "" {
		return errInternal("")
	}
	return nil
}

// load は index/chunks を読み、書きかけの末尾を捨てる。
func (ix *Index) load() error {
	if ix.loaded {
		return nil
	}
	key, err := ix.KeyS.DerivedKey("state")
	if err != nil {
		return err
	}
	ix.m, ix.good, ix.written = map[string]*contract.ChunkRef{}, 0, map[string]time.Time{}
	b, err := os.ReadFile(ix.path())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	off := 0
	for off+4 <= len(b) {
		n := int(binary.BigEndian.Uint32(b[off : off+4]))
		if n <= 0 || off+4+n > len(b) {
			if n > 0 { // 長さに満たない末尾は書きかけとして捨てる
				break
			}
			return &IndexUnreadable{errInternal("output")}
		}
		last := off+4+n == len(b)
		plain, err := ix.Codec.Decode(contract.MagicState, indexRel, b[off+4:off+4+n], key)
		var r idxRec
		if err == nil {
			err = json.Unmarshal(plain, &r)
		}
		if err != nil {
			if last { // 末尾の記録の復号の失敗は書きかけとして捨てる
				break
			}
			// 後ろに記録が続く位置の破損は書きかけではない: 索引を開けない(再構成が要る)。黙って空として読まず、追記もしない。
			return &IndexUnreadable{errInternal("output")}
		}
		ix.apply(r)
		off += 4 + n
	}
	ix.good, ix.loaded = int64(off), true
	return nil
}

func (ix *Index) apply(r idxRec) {
	switch r.Op {
	case "reserve":
		ix.m[r.ChunkID] = &contract.ChunkRef{ChunkID: r.ChunkID, VersionID: r.VersionID, WrappedKey: r.WrappedKey, FirstSeen: r.FirstSeen,
			FirstSeenPending: r.Pending, FormatVersion: r.FormatVersion, Size: r.Size, ETag: r.ETag, RetainUntil: r.RetainUntil}
	case "retain":
		for _, c := range ix.m {
			if c.VersionID == r.VersionID {
				c.RetainUntil = r.RetainUntil
			}
		}
	case "version":
		if c := ix.m[r.ChunkID]; c != nil {
			ix.written[r.ChunkID] = r.WrittenAt
			c.VersionID = r.VersionID
			if r.ETag != "" {
				c.ETag = r.ETag
			}
			if !r.RetainUntil.IsZero() {
				c.RetainUntil = r.RetainUntil
			}
		}
	case "commit":
		for _, id := range r.ChunkIDs {
			if c := ix.m[id]; c != nil && c.FirstSeenPending {
				c.FirstSeen, c.FirstSeenPending = r.FirstSeen, false
			}
		}
	case "remove":
		for id, c := range ix.m {
			if c.VersionID == r.VersionID {
				delete(ix.m, id)
			}
		}
	}
}

// appendRec は記録を暗号化して追記し、保管先への送信の前に確かに残すため fsync する。
func (ix *Index) appendRec(ctx context.Context, r idxRec) error {
	if e := ctxErr(ctx); e != nil {
		return e
	}
	key, err := ix.KeyS.DerivedKey("state")
	if err != nil {
		return err
	}
	plain, _ := json.Marshal(r)
	sealed, err := ix.Codec.Encode(contract.MagicState, indexRel, ix.header(), plain, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ix.path()), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(ix.path(), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(ix.good); err != nil { // 書きかけの末尾を取り除く
		return err
	}
	rec := binary.BigEndian.AppendUint32(nil, uint32(len(sealed)))
	rec = append(rec, sealed...)
	if _, err := f.WriteAt(rec, ix.good); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ix.good += int64(len(rec))
	ix.apply(r)
	return nil
}

// Lookup は contract.Index を満たす。未知のチャンクは (nil, nil)。
func (ix *Index) Lookup(ctx context.Context, chunkID string) (*contract.ChunkRef, error) {
	if err := ix.ready(); err != nil {
		return nil, err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.load(); err != nil {
		return nil, err
	}
	c := ix.m[chunkID]
	if c == nil {
		return nil, nil
	}
	cp := *c
	cp.WrappedKey = append([]byte(nil), c.WrappedKey...)
	return &cp, nil
}

// Reserve は contract.Index を満たす。送る前に ID と包んだ鍵を記録する(版 ID は送信の応答の後)。
func (ix *Index) Reserve(ctx context.Context, ref contract.ChunkRef) error {
	if err := ix.ready(); err != nil {
		return err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.load(); err != nil {
		return err
	}
	return ix.appendRec(ctx, idxRec{Op: "reserve", ChunkID: ref.ChunkID, VersionID: ref.VersionID, WrappedKey: ref.WrappedKey, FirstSeen: ref.FirstSeen,
		Pending: ref.FirstSeenPending, FormatVersion: ref.FormatVersion, Size: ref.Size, ETag: ref.ETag, RetainUntil: ref.RetainUntil})
}

// SetVersion は contract.Index を満たす。先行書き込みの無いチャンクへの追記は誤り。
func (ix *Index) SetVersion(ctx context.Context, chunkID, versionID string) error {
	if err := ix.ready(); err != nil {
		return err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.load(); err != nil {
		return err
	}
	if ix.m[chunkID] == nil {
		return errInternal("")
	}
	return ix.appendRec(ctx, idxRec{Op: "version", ChunkID: chunkID, VersionID: versionID, WrittenAt: ix.now()})
}

// Remove は contract.Index を満たす。prune が消した版のチャンクを索引から外す(再び参照されたときは送り直す)。
func (ix *Index) Remove(ctx context.Context, versionID string) error {
	if err := ix.ready(); err != nil {
		return err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.load(); err != nil {
		return err
	}
	return ix.appendRec(ctx, idxRec{Op: "remove", VersionID: versionID})
}

// Commit は、マニフェストを書いた世代 manifestID が参照するチャンク(chunkIDs)のうち、仮の first_seen(印 first_seen_pending)の行を
// manifestID に付け替えて印を外す(spec 4.5。付け替えるのは印のある行だけ)。contract.Index には無い口で、cli が backup の流れの 5 でマニフェストを書いた後に呼ぶ。
func (ix *Index) Commit(ctx context.Context, manifestID string, chunkIDs []string) error {
	if err := ix.ready(); err != nil {
		return err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.load(); err != nil {
		return err
	}
	return ix.appendRec(ctx, idxRec{Op: "commit", FirstSeen: manifestID, ChunkIDs: chunkIDs})
}

func (ix *Index) now() time.Time {
	if ix.Now != nil {
		return ix.Now().UTC()
	}
	return time.Now().UTC()
}

// SetRetain は contract.Index を満たす。版の retain-until の記録を更新する(書き込みの応答と延長の後)。
func (ix *Index) SetRetain(ctx context.Context, versionID string, until time.Time) error {
	if err := ix.ready(); err != nil {
		return err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.load(); err != nil {
		return err
	}
	return ix.appendRec(ctx, idxRec{Op: "retain", VersionID: versionID, RetainUntil: until})
}

// Records は contract.Index を満たす。版 ID のある全チャンクの記録(Key は chunks/ のキー。ID の順)。
func (ix *Index) Records(ctx context.Context) ([]contract.VersionRef, error) {
	if err := ix.ready(); err != nil {
		return nil, err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := ix.load(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(ix.m))
	for id, c := range ix.m {
		if c.VersionID != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]contract.VersionRef, 0, len(ids))
	for _, id := range ids {
		c := ix.m[id]
		out = append(out, contract.VersionRef{Key: chunkKeyOf(id), VersionID: c.VersionID, Size: c.Size, ETag: c.ETag, RetainUntil: c.RetainUntil, WrittenAt: ix.written[id]})
	}
	return out, nil
}
