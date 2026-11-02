package restore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sort"
	"syscall"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// fileFailure は 1 ファイルの内容を作れなかったこと(データのチャンクの破損・欠け・内容ハッシュの不一致)。置かずに続け、まとめて RESTORE_MISMATCH にする。
type fileFailure struct {
	ChunkID, VersionID, Reason string
}

func (f *fileFailure) Error() string { return "file: " + f.Reason }

// content は file・dump エントリの内容を w に流す(チャンクごと。全体をメモリに載せない)。*fileFailure はそのファイルだけの失敗。
type content func(e entry, w io.Writer) error

// hasher は書いたファイルを読み直したバイト列の内容ハッシュ("blake3:" 付き)。
type hasher func(r io.Reader) (string, error)

// materializer は書き出しの規則(spec 3.1.2.4 の流れ 3)でエントリを置く。<to> の下だけを os.Root 経由で操作する。
type materializer struct {
	root     *os.Root
	isRoot   bool
	hash     hasher
	failures []fileFailure
	failRefs map[int][]string
	actual   []contract.RestoreVerifyFile // 置き場から読み直した実測(ref・大きさ・内容ハッシュ)
}

// materialize はエントリを `<to>/<path>` に置く。パスの検査は書き出しの前に全エントリについて済ませる(validateEntries)。
// ファイルは同じディレクトリの一時名に 0600 で書き、読み直して大きさと内容ハッシュが合ったものだけを rename してから属性を当てる(mode 000 でも照合できる)。
// dump は 0600 のまま置き、mode・uid・gid・mtime を当てない。setuid・setgid・sticky は落とす(root でも)。
// シンボリックリンクはリンクとして置き、置く先と途中のリンクはたどらない。核が root のときだけ uid・gid を戻す。ディレクトリの属性は中身の後。
func materialize(ctx context.Context, to string, entries []entry, get content, hash hasher) (*materializer, error) {
	if err := validateEntries(entries); err != nil {
		return nil, err
	}
	isRoot := os.Geteuid() == 0
	if err := os.MkdirAll(to, 0o700); err != nil {
		return nil, err
	}
	if isRoot {
		if err := rootOwned(to); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(to)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	m := &materializer{root: root, isRoot: isRoot, hash: hash, failRefs: map[int][]string{}}
	var dirs []entry
	for _, e := range entries {
		if e.Type == "dir" {
			dirs = append(dirs, e)
			if err := root.MkdirAll(joinRel(e), 0o700); err != nil {
				return nil, err
			}
		}
	}
	for _, e := range entries {
		if e.Type == "dir" {
			continue
		}
		if ctx.Err() != nil {
			return nil, abortError(ctx)
		}
		if err := m.place(e, get); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(dirs, func(i, j int) bool { return len(relPath(dirs[i])) > len(relPath(dirs[j])) })
	for _, d := range dirs {
		if err := m.finish(joinRel(d), d, true); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// rootOwned は核が root で動くとき、<to> が root 所有で他者が書けないことを確かめる(置き換えの競合で root がリンクの先へ書くのを防ぐ)。
func rootOwned(to string) error {
	fi, err := os.Lstat(to)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || !fi.IsDir() || st.Uid != 0 || fi.Mode().Perm()&0o022 != 0 {
		return argInvalid("--to", "mismatch")
	}
	return nil
}

func joinRel(e entry) string {
	b, _ := e.pathBytes()
	return string(b)
}

func tmpName(rel string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	dir := ""
	for i := len(rel) - 1; i >= 0; i-- {
		if rel[i] == '/' {
			dir = rel[:i+1]
			break
		}
	}
	return dir + ".sitate-part-" + hex.EncodeToString(b[:])
}

func (m *materializer) place(e entry, get content) error {
	rel := joinRel(e)
	comps := relPath(e)
	if len(comps) > 1 {
		if err := m.root.MkdirAll(rel[:len(rel)-len(comps[len(comps)-1])-1], 0o700); err != nil {
			return err
		}
	}
	if e.Type == "symlink" {
		if err := m.root.Symlink(string(e.targetBytes()), rel); err != nil {
			return err
		}
		if m.isRoot {
			_ = m.root.Lchown(rel, e.UID, e.GID)
		}
		return nil
	}
	tmp := tmpName(rel)
	f, err := m.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	err = get(e, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = m.checkTmp(e, tmp)
	}
	var ff *fileFailure
	if errors.As(err, &ff) {
		_ = m.root.Remove(tmp) // 壊れた内容は残さない
		m.failures = append(m.failures, *ff)
		m.failRefs[len(m.failures)-1] = []string{e.Ref}
		return nil
	}
	if err != nil {
		_ = m.root.Remove(tmp)
		return err
	}
	if err := m.root.Rename(tmp, rel); err != nil {
		_ = m.root.Remove(tmp)
		return err
	}
	if e.Type == "dump" {
		return nil
	}
	return m.finish(rel, e, false)
}

// checkTmp は一時名のファイルを読み直して、大きさと内容ハッシュが合うことを確かめ、実測に足す。
func (m *materializer) checkTmp(e entry, tmp string) error {
	f, err := m.root.Open(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	h, err := m.hash(f)
	if err != nil {
		return err
	}
	m.actual = append(m.actual, contract.RestoreVerifyFile{EntryRef: e.Ref, Size: fi.Size(), Hash: h})
	if fi.Size() != e.Size || h != e.Hash {
		ff := &fileFailure{Reason: "hash"}
		if len(e.Ranges) > 0 {
			ff.ChunkID, ff.VersionID = e.Ranges[0].ID, e.Ranges[0].VersionID
		}
		return ff
	}
	return nil
}

// finish はモード(setuid・setgid・sticky を落とす)・所有・時刻を戻す。
func (m *materializer) finish(rel string, e entry, isDir bool) error {
	if m.isRoot {
		if err := m.root.Lchown(rel, e.UID, e.GID); err != nil {
			return err
		}
	}
	t := time.Unix(0, e.MtimeNS)
	if err := m.root.Chtimes(rel, t, t); err != nil {
		return err
	}
	return m.root.Chmod(rel, os.FileMode(e.Mode&0o777))
}
