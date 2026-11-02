package keys

// keys/ の配置の読み書き(spec 4.5「keys/ の配置」)。置き換えて更新するファイルは同じディレクトリの一時ファイルに書いて fsync し、rename して
// ディレクトリを fsync する。ディレクトリは 0700、ファイルは 0600。

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
)

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// writeAtomic は path を置き換える(一時ファイル → fsync → rename → ディレクトリの fsync)。
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	rnd := make([]byte, 4)
	if _, err := rand.Read(rnd); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".tmp-keys-"+hex.EncodeToString(rnd))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(dir)
}

// pendingFiles は keys/pending/ の中身。Master と Repo は無いことがある(フレーズ再発行など)。
type pendingFiles struct {
	Op                  string
	Keyring, Repo, Mstr []byte
}

func readOpt(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// readPending は保留を読む。保留が無ければ found = false。
func readPending(dir string) (p pendingFiles, found bool, err error) {
	pd := filepath.Join(dir, contract.KeysPendingDir)
	meta, err := readOpt(filepath.Join(pd, contract.KeysPendingMeta))
	if err != nil || meta == nil {
		return p, false, err
	}
	p.Op = metaOp(meta)
	if p.Keyring, err = os.ReadFile(filepath.Join(pd, contract.KeysPendingKeyring)); err != nil {
		return p, false, err
	}
	if p.Repo, err = readOpt(filepath.Join(pd, contract.KeysPendingRepo)); err != nil {
		return p, false, err
	}
	if p.Mstr, err = readOpt(filepath.Join(pd, contract.KeysPendingMaster)); err != nil {
		return p, false, err
	}
	return p, true, nil
}

// savePending は保留を作る(既にあれば捨てて作り直す)。pending.json は最後に書く(書きかけの保留を保留と見なさない)。
func savePending(dir string, p pendingFiles) error {
	pd := filepath.Join(dir, contract.KeysPendingDir)
	if err := os.RemoveAll(pd); err != nil {
		return err
	}
	if err := os.MkdirAll(pd, dirMode); err != nil {
		return err
	}
	for name, b := range map[string][]byte{contract.KeysPendingKeyring: p.Keyring, contract.KeysPendingRepo: p.Repo, contract.KeysPendingMaster: p.Mstr} {
		if b == nil {
			continue
		}
		if err := writeAtomic(filepath.Join(pd, name), b); err != nil {
			return err
		}
	}
	return writeAtomic(filepath.Join(pd, contract.KeysPendingMeta), pendingMeta(p.Op))
}

// placeCommitted は確定の後のファイルを置く: マスター鍵を置き換え、keyring.latest・repo.latest を置く。保留はまだ消さない。
// 途中で止まっても、同じフレーズでの phrase verify の再実行が続きを行う。
func placeCommitted(dir string, p pendingFiles) error {
	if p.Mstr != nil {
		if err := writeAtomic(filepath.Join(dir, contract.KeysMasterFile), p.Mstr); err != nil {
			return err
		}
	}
	if err := writeAtomic(filepath.Join(dir, contract.KeysKeyringLatest), p.Keyring); err != nil {
		return err
	}
	if p.Repo != nil {
		if err := writeAtomic(filepath.Join(dir, contract.KeysRepoLatest), p.Repo); err != nil {
			return err
		}
	}
	return nil
}

// removePending は確定の最後に保留を消す。
func removePending(dir string) error {
	pd := filepath.Join(dir, contract.KeysPendingDir)
	if err := os.RemoveAll(pd); err != nil {
		return err
	}
	return syncDir(filepath.Dir(pd))
}

func pendingMeta(op string) []byte {
	b, _ := json.Marshal(map[string]any{"op": op, "change_id": nil})
	return b
}

func metaOp(meta []byte) string {
	var m struct {
		Op string `json:"op"`
	}
	_ = json.Unmarshal(meta, &m)
	return m.Op
}
