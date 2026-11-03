package keys

// 確定の前の init の再実行で保留(keys/pending/)を作り直すときの journal/ の扱い(E3、F5、F9)。
// 保留中のマスター鍵で封じた journal/ の項目は、鍵が変わると読めなくなり、残すと連鎖(Entries)が INTERNAL になるので捨てる。
// ただし write_intent は保管先に実在する版(probe/・repo.json・keyring/ など)を指すので、捨てる前に開いて keys[] を journal/pre-master に戻し、
// 新しい鍵ができた最初の追記の前に項目として移す(Recorder)。ほかの種類の項目(run_end など)は確定を捨てる試みの記録で、保管先へ書いていないので捨てる。
// 削除・退避・保留の作り直しは state.lock の下で行う(ロックを要しない操作の Record と交ざらないように。3.1.1 の規則 23)。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// journalItemName は journal/ の項目のファイル名(`<seq 12 桁>-<entry_hash 64 桁>`。spec 4.2)。
var journalItemName = regexp.MustCompile(`^([0-9]{12})-[0-9a-f]{64}$`)

const preMasterName = "pre-master" // internal/state の Recorder と同じ名前

// replacePending は state.lock の下で、古い保留の write_intent を退避し、封じた項目を捨て、新しい保留を置く。
func (k *Keys) replacePending(ctx context.Context, p pendingFiles) error {
	h, err := k.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	carried := k.collectIntents()
	if err := dropSealedJournal(k.Dir); err != nil {
		return err
	}
	if err := savePending(k.Dir, p); err != nil {
		return errInternal()
	}
	return appendPreMaster(k.Dir, carried)
}

// collectIntents は今の journal 鍵(保留中のマスター鍵。無ければ空)で開ける write_intent の項目を連番の順に返す(at・kind・run_id・body だけ)。
func (k *Keys) collectIntents() [][]byte {
	if k.KeyS == nil || k.Codec == nil {
		return nil
	}
	key, err := k.KeyS.DerivedKey("journal")
	if err != nil {
		return nil
	}
	es, err := os.ReadDir(filepath.Join(k.Dir, "journal"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range es {
		if journalItemName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out [][]byte
	for _, n := range names {
		sealed, err := os.ReadFile(filepath.Join(k.Dir, "journal", n))
		if err != nil {
			continue
		}
		var seq uint64
		for _, c := range journalItemName.FindStringSubmatch(n)[1] {
			seq = seq*10 + uint64(c-'0')
		}
		plain, err := k.Codec.Decode(contract.MagicJournal, seqID(seq), sealed, key)
		if err != nil {
			continue
		}
		var it map[string]json.RawMessage
		if json.Unmarshal(plain, &it) != nil {
			continue
		}
		var kind string
		if json.Unmarshal(it["kind"], &kind) != nil || kind != "write_intent" {
			continue
		}
		b, err := json.Marshal(map[string]json.RawMessage{"at": it["at"], "kind": it["kind"], "run_id": it["run_id"], "body": it["body"]})
		if err == nil {
			out = append(out, b)
		}
	}
	return out
}

func seqID(seq uint64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], seq)
	return hex.EncodeToString(b[:])
}

// appendPreMaster は退避した write_intent を journal/pre-master の末尾に足す。
func appendPreMaster(dir string, lines [][]byte) error {
	if len(lines) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(dir, "journal"), 0o700); err != nil {
		return errInternal()
	}
	f, err := os.OpenFile(filepath.Join(dir, "journal", preMasterName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return errInternal()
	}
	defer f.Close()
	if _, err := f.Write(append(bytes.Join(lines, []byte{'\n'}), '\n')); err != nil {
		return errInternal()
	}
	return f.Sync()
}

// dropSealedJournal は保留のマスター鍵で封じた journal/ の項目を捨てる(新しい鍵では読めず、連鎖が壊れるため)。
// 確定の前の項目は保管先へ書かない(Recorder)ので、項目そのものは捨てても保管先に残らない。write_intent が指す版は collectIntents が戻す。
// マスター鍵が確定している(keys/master.json がある)ときは init はここへ来ない(ALREADY_INITIALIZED)。鍵を待つ退避(journal/pre-master)は封じていないので残す。
func dropSealedJournal(dir string) error {
	es, err := os.ReadDir(filepath.Join(dir, "journal"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errInternal()
	}
	for _, e := range es {
		if journalItemName.MatchString(e.Name()) {
			if err := os.Remove(filepath.Join(dir, "journal", e.Name())); err != nil {
				return errInternal()
			}
		}
	}
	return nil
}
