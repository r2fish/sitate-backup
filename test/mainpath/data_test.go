//go:build mainpath

package mainpath

// 合成データと、元データから計算する期待値。保存済みマニフェストを期待値にしない。

import (
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/zeebo/blake3"
)

const bigSize = 40 << 20 // ChunkAvg(4 MiB)の 10 倍。複数チャンクにまたがる

// want は元データから計算した 1 エントリの期待値。
type want struct {
	Type   string // file | dir | symlink
	Size   int64
	Hash   string // file のみ(blake3 の 16 進)
	Mode   os.FileMode
	MTime  time.Time // file のみ
	Target string    // symlink のみ
}

func pseudo(seed int64, n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

func writeFile(t *testing.T, p string, b []byte, mode os.FileMode, mt time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
}

// makeTree は根の下に合成データを作る。
func makeTree(t *testing.T, root string) {
	t.Helper()
	mt := time.Date(2026, 10, 1, 12, 34, 56, 0, time.UTC)
	writeFile(t, filepath.Join(root, "docs", "readme.txt"), []byte("hello sitate\n"), 0o644, mt)
	writeFile(t, filepath.Join(root, "docs", "empty.dat"), nil, 0o600, mt.Add(time.Hour))
	writeFile(t, filepath.Join(root, "docs", "請求書", "2026年10月分.txt"), []byte("日本語の名前のファイル\n"), 0o640, mt.Add(2*time.Hour))
	writeFile(t, filepath.Join(root, "data", "small.bin"), pseudo(1, 5000), 0o644, mt.Add(3*time.Hour))
	writeFile(t, filepath.Join(root, "data", "big.bin"), pseudo(2, bigSize), 0o600, mt.Add(4*time.Hour))
	writeFile(t, filepath.Join(root, "bin", "run.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755, mt.Add(5*time.Hour))
	if err := os.MkdirAll(filepath.Join(root, "empty-dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "empty-dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("docs/readme.txt", filepath.Join(root, "link-to-readme")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../missing-target", filepath.Join(root, "docs", "dangling")); err != nil {
		t.Fatal(err)
	}
}

func hashFile(t *testing.T, p string) string {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return hexOf(h.Sum(nil))
}

func hexOf(b []byte) string {
	const d = "0123456789abcdef"
	o := make([]byte, 0, len(b)*2)
	for _, c := range b {
		o = append(o, d[c>>4], d[c&15])
	}
	return string(o)
}

// scan は根の下(根自身を除く)を元データから計算した期待値の表にする(キーは根からの相対パス)。
func scan(t *testing.T, root string) map[string]want {
	t.Helper()
	m := map[string]want{}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		w := want{Mode: fi.Mode().Perm()}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			w.Type = "symlink"
			w.Target, _ = os.Readlink(p)
		case fi.IsDir():
			w.Type = "dir"
		default:
			w.Type, w.Size, w.MTime = "file", fi.Size(), fi.ModTime().UTC()
			w.Hash = hashFile(t, p)
		}
		m[rel] = w
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// compare は復元先と元データの期待値を比べ、食い違いを返す(ハッシュ・大きさ・モード・mtime・リンク先・余分と欠け)。
func compare(t *testing.T, label string, exp map[string]want, dest string) {
	t.Helper()
	got := scan(t, dest)
	var keys []string
	for k := range exp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := exp[k]
		g, ok := got[k]
		if !ok {
			t.Errorf("%s: %q が復元されていない", label, k)
			continue
		}
		if g.Type != e.Type {
			t.Errorf("%s: %q の種類 %s 期待 %s", label, k, g.Type, e.Type)
			continue
		}
		if e.Type == "file" {
			if g.Size != e.Size || g.Hash != e.Hash {
				t.Errorf("%s: %q の内容が違う(大きさ %d 期待 %d、ハッシュ一致=%v)", label, k, g.Size, e.Size, g.Hash == e.Hash)
			}
			if !g.MTime.Equal(e.MTime) {
				t.Errorf("%s: %q の mtime %v 期待 %v", label, k, g.MTime, e.MTime)
			}
		}
		if e.Type != "symlink" && g.Mode != e.Mode {
			t.Errorf("%s: %q のモード %o 期待 %o", label, k, g.Mode, e.Mode)
		}
		if e.Type == "symlink" && g.Target != e.Target {
			t.Errorf("%s: %q のリンク先 %q 期待 %q", label, k, g.Target, e.Target)
		}
	}
	for k := range got {
		if _, ok := exp[k]; !ok {
			t.Errorf("%s: %q が余分に復元された", label, k)
		}
	}
}
