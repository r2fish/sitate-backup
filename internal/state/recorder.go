package state

// journal/ の記録の連鎖(spec 4.2「記録の連鎖と保持」、4.3 の SITJ、4.5)。連番の割り当てと状態ディレクトリへの追記は state.lock の下で行い、
// 保管先へは放した後に割り当て済みのキーで書く(3.1.1 の規則 23)。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto/sha256"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type contextT = context.Context

// journalItemRe は journal/ の項目のキー `<seq 12 桁>-<entry_hash 64 桁>`(4.2)。
var journalItemRe = regexp.MustCompile(`^([0-9]{12})-([0-9a-f]{64})$`)

func (r *Recorder) ready() error {
	if r.State == nil || r.Codec == nil || r.KeyS == nil || r.Clock == nil {
		return internalErr()
	}
	return nil
}

func (r *Recorder) journalDir() string {
	if r.Dir != "" {
		return r.Dir
	}
	if sf, ok := r.State.(*StateFiles); ok {
		return sf.Dir
	}
	return ""
}

// stateWriter は状態ディレクトリへの置き換え書き込み(4.5)を StateFiles と同じ手順で行う。
func (r *Recorder) stateWriter() *StateFiles {
	if sf, ok := r.State.(*StateFiles); ok {
		return sf
	}
	return &StateFiles{RunID: r.RunID}
}

// listLocal は journal/ の項目名を連番の順に返す。
func (r *Recorder) listLocal() ([]string, error) {
	var names []string
	if dir := r.journalDir(); dir == "" {
		r.mu.Lock()
		for n := range r.mem {
			names = append(names, n)
		}
		r.mu.Unlock()
	} else {
		es, err := os.ReadDir(dir + "/journal")
		if err != nil && !os.IsNotExist(err) {
			return nil, internalErr()
		}
		for _, e := range es {
			if journalItemRe.MatchString(e.Name()) {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

func (r *Recorder) readLocal(name string) ([]byte, error) {
	if dir := r.journalDir(); dir != "" {
		b, err := os.ReadFile(dir + "/journal/" + name)
		if err != nil {
			return nil, internalErr()
		}
		return b, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.mem[name]
	if !ok {
		return nil, internalErr()
	}
	return b, nil
}

func (r *Recorder) writeLocal(name string, b []byte) error {
	if dir := r.journalDir(); dir != "" {
		if err := r.stateWriter().writeAtomic(dir+"/journal/"+name, b); err != nil {
			return internalErr()
		}
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mem == nil {
		r.mem = map[string][]byte{}
	}
	r.mem[name] = b
	return nil
}

func seqID(seq uint64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], seq)
	return hex.EncodeToString(b[:])
}

// entryHash は保管先に置くバイト列の BLAKE3 を 16 進の小文字 64 文字にしたもの(4.2)。
func (r *Recorder) entryHash(sealed []byte) string {
	return strings.TrimPrefix(r.Codec.Hash(sealed), "blake3:")
}

// journalMark は SITJ の見出しの印(マスター鍵の鍵 ID 4 バイト。4.3)。契約の KeyService が鍵 ID を返さないため、記録鍵の SHA-256 の先頭 4 バイトで代える。
func journalMark(key []byte) []byte {
	h := sha256.Sum256(key)
	return h[:4]
}

// Record は contract.Recorder を満たす。state.lock の下で追記し、放した後に保管先へ書く。1 つ前の項目の entry_hash(seq 1 では空)を返す。
// 保管先へ書けないときは追記済みのまま誤りを返す(状態ディレクトリの項目は次にロックを取る実行が SyncRemote で保管先へ写す)。
func (r *Recorder) Record(ctx context.Context, entry []byte) (string, error) {
	if err := r.ready(); err != nil {
		return "", err
	}
	h, err := r.State.Lock(ctx)
	if err != nil {
		return "", err
	}
	name, prev, err := r.appendLocked(ctx, entry)
	h.Release()
	if err != nil || name == "" {
		return "", err
	}
	return prev, r.WriteRemote(ctx, []byte(name))
}

// AppendLocked は contract.Recorder を満たす。state.lock を持つ呼び出し側が使う(再入しない)。割り当てた項目のキー `<seq>-<entry_hash>` を返す。
// 呼び出し側は state.lock を放した後に WriteRemote([]byte(キー)) で保管先へ書く。
func (r *Recorder) AppendLocked(ctx context.Context, held contract.StateLockHandle, entry []byte) (string, error) {
	if err := r.ready(); err != nil {
		return "", err
	}
	if sf, ok := r.State.(*StateFiles); ok {
		if err := sf.heldBy(held); err != nil {
			return "", err
		}
	} else if held == nil {
		return "", internalErr()
	}
	name, _, err := r.appendLocked(ctx, entry)
	return name, err
}

func (r *Recorder) appendLocked(ctx context.Context, entry []byte) (name, prev string, err error) {
	obj := map[string]json.RawMessage{}
	if json.Unmarshal(entry, &obj) != nil || obj == nil {
		return "", "", newErr(contract.CodeArgInvalid, map[string]any{"flag": "entry", "reason": "format"})
	}
	if deferred, err := r.deferWithoutMaster(obj, entry); deferred || err != nil {
		return "", "", err
	}
	if err := r.flushPreMaster(ctx); err != nil {
		return "", "", err
	}
	return r.appendOne(obj)
}

// appendOne は 1 項目を連番の次に封じて状態ディレクトリへ置く。
func (r *Recorder) appendOne(obj map[string]json.RawMessage) (name, prev string, err error) {
	names, err := r.listLocal()
	if err != nil {
		return "", "", err
	}
	seq := uint64(1)
	obj["prev_hash"] = json.RawMessage("null")
	if n := len(names); n > 0 {
		m := journalItemRe.FindStringSubmatch(names[n-1])
		last, _ := strconv.ParseUint(m[1], 10, 64)
		seq, prev = last+1, m[2]
		obj["prev_hash"], _ = json.Marshal(prev)
	}
	obj["seq"] = json.RawMessage(strconv.FormatUint(seq, 10))
	plain, _ := json.Marshal(obj)
	key, err := r.KeyS.DerivedKey("journal")
	if err != nil {
		return "", "", err
	}
	sealed, err := r.Codec.Encode(contract.MagicJournal, seqID(seq), contract.Header{Kind: contract.MagicJournal, FormatVersion: contract.FormatVersion, Mark: journalMark(key)}, plain, key)
	if err != nil {
		return "", "", err
	}
	name = fmt.Sprintf("%012d-%s", seq, r.entryHash(sealed))
	return name, prev, r.writeLocal(name, sealed)
}

// WriteRemote は contract.Recorder を満たす。item は AppendLocked が割り当てた項目のキー `<seq>-<entry_hash>` で、
// 状態ディレクトリの同名の項目(保管先と同じバイト列)を保管先の journal/ に同じキーで書く。保管先に同じキーがあれば書かない(冪等)。
func (r *Recorder) WriteRemote(ctx context.Context, item []byte) error {
	if r.State == nil || r.Store == nil {
		return internalErr()
	}
	if r.masterUncommitted() {
		return nil // 確定の前の項目は保管先へ書かない(確定の後の SyncRemote が写す)
	}
	name := string(item)
	if !journalItemRe.MatchString(name) {
		return newErr(contract.CodeArgInvalid, map[string]any{"flag": "item", "reason": "format"})
	}
	b, err := r.readLocal(name)
	if err != nil {
		return err
	}
	if r.entryHash(b) != name[13:] {
		return internalErr()
	}
	key := "journal/" + name
	vs, err := r.Store.ListVersions(ctx, key)
	if err != nil {
		return err
	}
	now, err := r.Clock.Now(ctx)
	if err != nil {
		return err
	}
	for _, v := range vs {
		if v.Key == key && !v.DeleteMarker {
			return RecordObject(ctx, r.State, ObjectRec{Key: key, VersionID: v.VersionID, Size: v.Size, WrittenAt: now.UTC().Format(time.RFC3339)})
		}
	}
	res, err := r.Store.Put(ctx, key, bytes.NewReader(b), contract.RetentionDaily)
	if err != nil {
		return err
	}
	return RecordObject(ctx, r.State, ObjectFromPut(key, res, now))
}

// SyncRemote は状態ディレクトリの journal/ にあって保管先に無い項目を、連番の順に保管先へ写す。
// 記録の締め切りの内に保管先へ書けなかった実行の項目を、次にロックを取る実行が写す(3.1.1 の規則 21)。
func (r *Recorder) SyncRemote(ctx context.Context) error {
	if r.State == nil || r.Store == nil {
		return internalErr()
	}
	if r.masterUncommitted() {
		return nil
	}
	names, err := r.listLocal()
	if err != nil {
		return err
	}
	objs, err := ReadObjects(ctx, r.State)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, o := range objs {
		done[o.Key] = true
	}
	for _, n := range names { // objects[] に版を記録した項目は保管先へ写し済み。保管先を引かない(呼び出し数を未写しの項目の数に比例させる)
		if done["journal/"+n] {
			continue
		}
		if err := r.WriteRemote(ctx, []byte(n)); err != nil {
			return err
		}
	}
	return nil
}
