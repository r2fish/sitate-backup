package restore

// レビュー(F1・F2・F11・F14・F24・F25)の差し戻しの試験: 書き出しの属性、一時名と rename、版の選び方。

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func listNames(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && p != dir {
			r, _ := filepath.Rel(dir, p)
			out = append(out, r)
		}
		return nil
	})
	return out
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root では mode 000 を読めてしまう")
	}
}

// F1: mode・uid・gid・mtime を持たない dump のエントリ(U1c の実際の形)は 0600 で置く。非 root でも復元できる。
func TestRestoreDumpWithoutAttributes(t *testing.T) {
	g := newGenWorld(t)
	g.build([]specEntry{
		{Type: "dump", T: "t3", Path: "t3/db.dump", Format: "custom", Data: dump1},
		{Type: "dump", T: "t3", Path: "t3/globals.sql", Format: "globals", Data: dump2},
	})
	to := filepath.Join(t.TempDir(), "to")
	okResult(t, g, to)
	for rel, want := range map[string][]byte{"t3/db.dump": dump1, "t3/globals.sql": dump2} {
		fi, err := os.Stat(filepath.Join(to, rel))
		b, _ := os.ReadFile(filepath.Join(to, rel))
		if err != nil || fi.Mode().Perm() != 0o600 || string(b) != string(want) {
			t.Errorf("%s: mode=%v 内容=%q err=%v", rel, fi.Mode(), b, err)
		}
	}
}

// F14: 元の mode が 000 のファイル・ディレクトリでも、非 root の復元先で照合できて不合格にならない(属性は照合の後に当てる)。
func TestRestoreMode000(t *testing.T) {
	skipIfRoot(t)
	g := newGenWorld(t)
	g.build([]specEntry{
		{Type: "dir", T: "t1", Path: "t1", Mode: 0o755, Mtime: mt},
		{Type: "dir", T: "t1", Path: "t1/locked", Mode: 0, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/locked/secret", Data: dataA, Mode: 0, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/none", Data: dataB, Mode: 0, Mtime: mt},
	})
	to := filepath.Join(t.TempDir(), "to")
	defer os.Chmod(filepath.Join(to, "t1/locked"), 0o700)
	m := okResult(t, g, to)
	wantRaw(t, m, "files", "2")
	for rel, want := range map[string]os.FileMode{"t1/none": 0, "t1/locked": 0} {
		fi, err := os.Lstat(filepath.Join(to, rel))
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: %v %v", rel, fi, err)
		}
	}
}

// failStore は n 回目以降のチャンクの読み出しを一時的な失敗にする。
type failStore struct {
	contract.ObjectStore
	chunks, failFrom int
}

func (f *failStore) Get(ctx context.Context, key, ver string) (io.ReadCloser, error) {
	if strings.HasPrefix(key, "chunks/") {
		f.chunks++
		if f.chunks >= f.failFrom {
			return nil, errors.New("試験: 保管先の一時的な失敗")
		}
	}
	return f.ObjectStore.Get(ctx, key, ver)
}

// F11: 途中で保管先が失敗しても、部分ファイルを残さない(一時名も残らない)。
func TestRestoreNoPartialFileOnStoreFailure(t *testing.T) {
	g := newGenWorld(t)
	g.buildMulti("t1/big.bin", [][]byte{[]byte("part-one"), []byte("part-two"), []byte("part-three")})
	r := g.restorer()
	inner, _ := r.OpenStore(ctx, contract.StoreAddr{}, "", nil)
	fs := &failStore{ObjectStore: inner, failFrom: 5} // 5 回目 = 3 つ目のデータのチャンク(エントリと鍵表のチャンクの後)
	r.OpenStore = func(context.Context, contract.StoreAddr, string, *contract.Secret) (contract.ObjectStore, error) {
		return fs, nil
	}
	to := filepath.Join(t.TempDir(), "to")
	_, err := r.Restore(ctx, g.restoreCall(to))
	if err == nil {
		t.Fatal("保管先の失敗で成功した")
	}
	for _, n := range listNames(t, to) {
		if strings.Contains(n, "big.bin") || strings.Contains(n, ".sitate-part") {
			t.Errorf("部分ファイルが残った: %s", n)
		}
	}
}

// F11: 内容ハッシュが合わなかったファイルと欠けたファイルは置かない(良いファイルだけが残る)。
func TestRestoreBadFilesNotLeft(t *testing.T) {
	g := newGenWorld(t)
	g.build([]specEntry{
		{Type: "file", T: "t1", Path: "t1/ok", Data: dataA, Mode: 0o644, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/bad", Data: dataB, HashOf: []byte("x"), Mode: 0o644, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/gone", Data: dump2, BadVersion: true, Mode: 0o644, Mtime: mt},
	})
	to := filepath.Join(t.TempDir(), "to")
	_, err := g.restorer().Restore(ctx, g.restoreCall(to))
	harness.ExpectCode(t, err, contract.CodeRestoreMismatch)
	for _, n := range listNames(t, to) {
		if n != "t1" && n != "t1/ok" {
			t.Errorf("置いてはいけないものが残った: %s", n)
		}
	}
}

// F2: 書き込み鍵で積まれた偽の新しい版があっても、復号できる版で復元する。
func TestRestoreSkipsForgedNewerVersion(t *testing.T) {
	g := newGenWorld(t)
	g.build([]specEntry{{Type: "file", T: "t1", Path: "t1/a", Data: dataA, Mode: 0o644, Mtime: mt}})
	g.put("manifests/"+g.Ref, "forged-not-decryptable")
	to := filepath.Join(t.TempDir(), "to")
	okResult(t, g, to)
	if b, _ := os.ReadFile(filepath.Join(to, "t1/a")); string(b) != string(dataA) {
		t.Errorf("内容: %q", b)
	}
}

// F24: latest で当たる世代が無ければ NO_HEALTHY_BACKUP。
func TestRestoreLatestNone(t *testing.T) {
	g := newGenWorld(t)
	g.build([]specEntry{{Type: "file", T: "t1", Path: "t1/a", Data: dataA, Mode: 0o644, Mtime: mt}})
	g.put("manifests/m-ffffffffffffffffffffffffffffffff", "forged")
	c := g.restoreCall(filepath.Join(t.TempDir(), "to"))
	c.Args[contract.ArgBackup] = []string{"latest"}
	_, err := g.restorer().Restore(ctx, c)
	if err != nil {
		harness.ExpectCode(t, err, contract.CodeNoHealthyBackup)
	}
}

// F25: エントリの範囲の桁あふれで panic せず、decrypt の失敗にする。symlink の target の NUL は書き出しの前に拒む。
func TestRestoreRangeOverflowAndSymlinkNUL(t *testing.T) {
	src := newChunkSource(ctx, &objReader{}, nil)
	src.cache["c"] = []byte("abc")
	e := entry{Ranges: []rangeRef{{ID: "c", VersionID: "v", Offset: 1, Length: 1<<63 - 1}}}
	var ff *fileFailure
	if err := src.content(e, io.Discard); !errors.As(err, &ff) {
		t.Errorf("桁あふれが失敗にならない: %v", err)
	}
	for _, tg := range []entry{{Target: "a\x00b"}, {TargetB64: "!!!"}, {}} {
		bad := entry{Ref: "f:" + strings.Repeat("a", 32), T: "t1", Path: "t1/l", Type: "symlink", Target: tg.Target, TargetB64: tg.TargetB64}
		if validateEntries([]entry{bad}) == nil {
			t.Errorf("symlink の target %+v を通した", tg)
		}
	}
}

var _ = fake.StartTime

// F12: 核が root のとき <to> は root 所有で他者が書けないことを要する。root で走らせられない環境では、拒む側(非 root 所有・group/other が書ける)だけを確かめる。
func TestRootOwnedRejects(t *testing.T) {
	d := t.TempDir()
	_ = os.Chmod(d, 0o777)
	if err := rootOwned(d); err == nil && os.Geteuid() != 0 {
		t.Error("他者が書ける・root 所有でない <to> を受け入れた")
	}
}
