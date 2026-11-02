package cli

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

// writeFileAtomic は同じディレクトリの一時ファイル(O_EXCL、0600)に書いて fsync し、rename で置き換えてディレクトリを fsync する(spec 4.5)。
func writeFileAtomic(path string, data []byte) error { return writeFileAtomicMode(path, data, 0o600) }

// writeFileAtomicMode は writeFileAtomic の権限指定つき(定義ファイルは読める形で置く)。
func writeFileAtomicMode(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	var r [4]byte
	_, _ = rand.Read(r[:])
	tmp := filepath.Join(dir, ".tmp-cli-"+hex.EncodeToString(r[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
