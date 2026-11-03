package state

// マスター鍵の確定の前の記録(spec 4.5「keys/」「keys/pending/」、4.2 の核自身の中断した書き込み)。
//   - keys/pending/master.json だけがある間(init の後、phrase verify の確定の前): 記録はその鍵で封じて状態ディレクトリに残し、
//     保管先へは書かない。確定を捨てて作り直す(init の再実行)とき、保管先に新しい鍵で読めない項目を残さないため。確定の後の SyncRemote が写す。
//   - マスター鍵がまだ無い間(init の前の init-storage): write_intent だけを journal/pre-master に行ごとの JSON で退避し、
//     鍵ができた最初の追記の前に連番の項目として移す。保管先へ書く前の予定の記録を落とさず、鍵の無い封じ方も作らない。
//     中身は probe/ のキー名(乱数)だけで秘密を含まない。ほかの種類の記録(run_end など)は従来どおり鍵が無ければ書けない。

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const preMasterFile = "pre-master" // journal/ の下。journalItemRe に合わないので項目として列挙されない。

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// masterPhase は状態ディレクトリのマスター鍵の有無。Dir が無い(メモリの Recorder)ときは常に cur。
func (r *Recorder) masterPhase() (cur, pending bool) {
	dir := r.journalDir()
	if dir == "" {
		return true, false
	}
	return fileExists(filepath.Join(dir, contract.KeysMasterFile)),
		fileExists(filepath.Join(dir, contract.KeysPendingDir, contract.KeysPendingMaster))
}

func (r *Recorder) masterUncommitted() bool {
	cur, pend := r.masterPhase()
	return !cur && pend
}

// deferWithoutMaster はマスター鍵がまだ無いときの write_intent を退避する。退避したら deferred を返す。
func (r *Recorder) deferWithoutMaster(obj map[string]json.RawMessage, entry []byte) (deferred bool, err error) {
	cur, pend := r.masterPhase()
	if cur || pend {
		return false, nil
	}
	var kind string
	if json.Unmarshal(obj["kind"], &kind) != nil || kind != "write_intent" {
		return false, nil
	}
	compact := new(bytes.Buffer)
	if json.Compact(compact, entry) != nil {
		return false, internalErr()
	}
	path := filepath.Join(r.journalDir(), "journal", preMasterFile)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, internalErr()
	}
	if err := r.stateWriter().writeAtomic(path, append(old, append(compact.Bytes(), '\n')...)); err != nil {
		return false, internalErr()
	}
	return true, nil
}

// flushPreMaster は退避した write_intent を、鍵ができた最初の追記の前に連番の項目として移す。
func (r *Recorder) flushPreMaster(_ context.Context) error {
	dir := r.journalDir()
	if dir == "" {
		return nil
	}
	path := filepath.Join(dir, "journal", preMasterFile)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return internalErr()
	}
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		obj := map[string]json.RawMessage{}
		if json.Unmarshal(line, &obj) != nil || obj == nil {
			return internalErr()
		}
		if _, _, err := r.appendOne(obj); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return internalErr()
	}
	return nil
}
