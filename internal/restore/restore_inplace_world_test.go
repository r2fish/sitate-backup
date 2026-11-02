package restore

// restore の (a) と materialize の判定(作業指示書 5.6 の完了条件 7、spec 3.1.2.4 の流れ 3)。
// 偽の保管先に実際の世代を置き、置いたものを置き場から読み直して元データのハッシュと突き合わせる。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const mt = int64(1_700_000_000_000_000_000)

var (
	dataA  = []byte("hello restore\n")
	dataB  = []byte("binary\x00\x01\x02 data")
	dump1  = []byte("PGDMP-fake-custom-dump")
	dump2  = []byte("-- globals\n")
	outTag = []byte("outside must stay untouched")
)

func goodEntries() []specEntry {
	return []specEntry{
		{Type: "dir", T: "t1", Path: "t1", Mode: 0o1777, Mtime: mt},
		{Type: "dir", T: "t1", Path: "t1/sub", Mode: 0o2755, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/a.txt", Data: dataA, Mode: 0o640, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/sub/b.bin", Data: dataB, Mode: 0o4755, Mtime: mt + 5},
		{Type: "symlink", T: "t1", Path: "t1/ln", Target: "../../does/not/exist", Mode: 0o777, Mtime: mt},
		{Type: "dump", T: "t3", Path: "t3/db.dump", Format: "custom", Data: dump1, Mode: 0o600, Mtime: mt},
		{Type: "dump", T: "t3", Path: "t3/globals.sql", Format: "globals", Data: dump2, Mode: 0o600, Mtime: mt},
	}
}

func okResult(t *testing.T, g *genWorld, to string) map[string]json.RawMessage {
	t.Helper()
	rep, err := g.restorer().Restore(ctx, g.restoreCall(to))
	harness.Implemented(t, err)
	if err != nil {
		t.Fatalf("復元が失敗: %v", err)
	}
	return resultOf(t, rep, "restore")
}

// 世代を空のディレクトリへ書き出す: 内容は元データのとおり、setuid・setgid・sticky は落ち、mtime が戻り、シンボリックリンクはリンクのまま。
func TestRestoreInPlaceMaterializes(t *testing.T) {
	g := newGenWorld(t)
	g.build(goodEntries())
	to := filepath.Join(t.TempDir(), "to")
	m := okResult(t, g, to)
	wantRaw(t, m, "hash_mismatch", "0")
	wantRaw(t, m, "files", "4")
	for rel, want := range map[string][]byte{"t1/a.txt": dataA, "t1/sub/b.bin": dataB, "t3/db.dump": dump1, "t3/globals.sql": dump2} {
		got, err := os.ReadFile(filepath.Join(to, rel))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s の内容が元データと違う: %q %v", rel, got, err)
		}
	}
	modes := map[string]os.FileMode{"t1/a.txt": 0o640, "t1/sub/b.bin": 0o755, "t1": 0o777, "t1/sub": 0o755}
	for rel, want := range modes {
		fi, err := os.Lstat(filepath.Join(to, rel))
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky); got != want {
			t.Errorf("%s のモード = %v, want %v(setuid・setgid・sticky は落とす)", rel, got, want)
		}
	}
	if fi, _ := os.Stat(filepath.Join(to, "t1/sub/b.bin")); fi.ModTime().UnixNano() != mt+5 {
		t.Errorf("mtime が戻っていない: %d", fi.ModTime().UnixNano())
	}
	if fi, _ := os.Stat(filepath.Join(to, "t1")); fi.ModTime().UnixNano() != mt {
		t.Errorf("ディレクトリの mtime は中身を置いた後に戻す: %d", fi.ModTime().UnixNano())
	}
	if tgt, err := os.Readlink(filepath.Join(to, "t1/ln")); err != nil || tgt != "../../does/not/exist" {
		t.Errorf("シンボリックリンクがリンクとして置かれていない: %q %v", tgt, err)
	}
	// 照合の期待値は元データから作る(置いたマニフェストの hash を使わない)。
	var exp, act []contract.RestoreVerifyFile
	for i, e := range []struct {
		rel  string
		data []byte
	}{{"t1/a.txt", dataA}, {"t1/sub/b.bin", dataB}, {"t3/db.dump", dump1}, {"t3/globals.sql", dump2}} {
		b, _ := os.ReadFile(filepath.Join(to, e.rel))
		exp = append(exp, contract.RestoreVerifyFile{EntryRef: string(rune('a' + i)), Size: int64(len(e.data)), Hash: g.hash(e.data)})
		act = append(act, contract.RestoreVerifyFile{EntryRef: string(rune('a' + i)), Size: int64(len(b)), Hash: g.hash(b)})
	}
	if ok, _, _, n, mm := verifyFiles(exp, act); !ok || n != 4 || len(mm) != 0 {
		t.Errorf("元データのハッシュと置いたファイルが合わない: %v %d %v", ok, n, mm)
	}
}

// シンボリックリンクをたどらない: リンクの下に置くエントリは、何も書かずに止める(リンクの先に書かない)。
func TestRestoreSymlinkNotFollowed(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "keep"), outTag, 0o600); err != nil {
		t.Fatal(err)
	}
	g := newGenWorld(t)
	g.build([]specEntry{
		{Type: "symlink", T: "t1", Path: "t1/ln", Target: out, Mode: 0o777, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/ln/evil", Data: dataA, Mode: 0o644, Mtime: mt},
	})
	to := filepath.Join(t.TempDir(), "to")
	_, err := g.restorer().Restore(ctx, g.restoreCall(to))
	harness.ExpectCode(t, err, contract.CodeInternal)
	if des, _ := os.ReadDir(out); len(des) != 1 {
		t.Errorf("リンクの先に書いた: %v", des)
	}
	if _, serr := os.Stat(to); serr == nil {
		t.Errorf("判定は書き出しの前: 何も書かないはず(置き場ができている)")
	}
}

// パスの拒否: 1 つでも形に合わないエントリがあれば INTERNAL で止め、良いエントリも含めて何も書かない。
func TestRestorePathRejectedWritesNothing(t *testing.T) {
	bad := map[string]specEntry{
		"先頭の /":           {Type: "file", T: "t1", Path: "/t1/x", Data: dataA, Mode: 0o644, Mtime: mt},
		".. の成分":          {Type: "file", T: "t1", Path: "t1/../x", Data: dataA, Mode: 0o644, Mtime: mt},
		". の成分":           {Type: "file", T: "t1", Path: "t1/./x", Data: dataA, Mode: 0o644, Mtime: mt},
		"空の成分":            {Type: "file", T: "t1", Path: "t1//x", Data: dataA, Mode: 0o644, Mtime: mt},
		"対象 id で始まらない":    {Type: "file", T: "t1", Path: "t2/x", Data: dataA, Mode: 0o644, Mtime: mt},
		"dump が固定名でない":    {Type: "dump", T: "t3", Path: "t3/other.dump", Format: "custom", Data: dump1, Mode: 0o600, Mtime: mt},
		"dump の形式と名前の不一致": {Type: "dump", T: "t3", Path: "t3/globals.sql", Format: "custom", Data: dump1, Mode: 0o600, Mtime: mt},
		"未知の type":        {Type: "fifo", T: "t1", Path: "t1/p", Mode: 0o644, Mtime: mt},
	}
	for name, b := range bad {
		g := newGenWorld(t)
		g.build([]specEntry{{Type: "file", T: "t1", Path: "t1/good", Data: dataB, Mode: 0o644, Mtime: mt}, b})
		to := filepath.Join(t.TempDir(), "to")
		_, err := g.restorer().Restore(ctx, g.restoreCall(to))
		ce := harness.ExpectCode(t, err, contract.CodeInternal)
		var d struct{ Component string }
		_ = json.Unmarshal(ce.Detail, &d)
		if d.Component != "restore" {
			t.Errorf("%s: detail.component = %q", name, d.Component)
		}
		if _, serr := os.Stat(to); serr == nil {
			t.Errorf("%s: 判定は書き出しの前のはずなのに置き場ができている", name)
		}
	}
}

// データのチャンクの壊れ(内容ハッシュ違い)と欠け(版が無い)は RESTORE_MISMATCH で、影響した範囲を detail に置く。
func TestRestoreMismatchOnCorruptAndMissing(t *testing.T) {
	g := newGenWorld(t)
	g.build([]specEntry{
		{Type: "file", T: "t1", Path: "t1/ok", Data: dataA, Mode: 0o644, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/bad-hash", Data: dataB, HashOf: []byte("other content"), Mode: 0o644, Mtime: mt},
		{Type: "file", T: "t1", Path: "t1/missing", Data: dump2, BadVersion: true, Mode: 0o644, Mtime: mt},
	})
	_, err := g.restorer().Restore(ctx, g.restoreCall(filepath.Join(t.TempDir(), "to")))
	ce := harness.ExpectCode(t, err, contract.CodeRestoreMismatch)
	if verr := contract.Validate("cli/detail.json#/$defs/RESTORE_MISMATCH", ce.Detail); verr != nil {
		t.Fatalf("detail がスキーマに合わない: %v\n%s", verr, ce.Detail)
	}
	var d struct {
		HashMismatch []struct{ Reason string } `json:"hash_mismatch"`
		FilesOK      int                       `json:"files_ok"`
		FilesFailed  int                       `json:"files_failed"`
	}
	_ = json.Unmarshal(ce.Detail, &d)
	reasons := map[string]bool{}
	for _, h := range d.HashMismatch {
		reasons[h.Reason] = true
	}
	if !reasons["hash"] || !reasons["missing"] || d.FilesOK != 1 || d.FilesFailed != 2 {
		t.Errorf("影響の範囲が違う: %s", ce.Detail)
	}
}

// 空でない --to は ARG_INVALID(flag --to、reason mismatch)で、保管先を開かず何も変えない(監督の確定。spec 3.1.2.4)。
func TestRestoreToNotEmptyArgInvalid(t *testing.T) {
	g := newGenWorld(t)
	g.build(goodEntries())
	to := t.TempDir()
	if err := os.WriteFile(filepath.Join(to, "keep"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := g.restorer()
	opened := false
	inner := r.OpenStore
	r.OpenStore = func(c context.Context, a contract.StoreAddr, k string, s *contract.Secret) (contract.ObjectStore, error) {
		opened = true
		return inner(c, a, k, s)
	}
	_, err := r.Restore(ctx, g.restoreCall(to))
	ce := harness.ExpectCode(t, err, contract.CodeArgInvalid)
	if string(ce.Detail) != `{"flag":"--to","reason":"mismatch"}` || opened {
		t.Errorf("detail=%s opened=%v", ce.Detail, opened)
	}
	if des, _ := os.ReadDir(to); len(des) != 1 {
		t.Errorf("既存の中身が変わった: %v", des)
	}
}
