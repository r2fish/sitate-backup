package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 偽の受け口: list と read-batch の出力・終了状態を試験が決める(終了状態の写像、件数の境界、読み直しの試験に使う)。
type fakeRecv struct {
	t   *testing.T
	dir string
}

func newFakeRecv(t *testing.T) *fakeRecv {
	d := t.TempDir()
	f := &fakeRecv{t, d}
	script := fmt.Sprintf(`#!/bin/sh
d=%s
case $1 in
list) cat $d/list; exit $(cat $d/list.rc 2>/dev/null || echo 0) ;;
read-batch)
	n=$(cat $d/n 2>/dev/null || echo 0); n=$((n+1)); echo $n > $d/n
	cat > $d/in.$n
	cat $d/rb.$n 2>/dev/null
	exit $(cat $d/rb.$n.rc 2>/dev/null || echo 0) ;;
esac
exit 126
`, sq(d))
	if err := os.WriteFile(filepath.Join(d, "helper"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	f.setList(nil, 0)
	return f
}

func (f *fakeRecv) path() string { return filepath.Join(f.dir, "helper") }

func (f *fakeRecv) put(name string, b []byte) {
	if err := os.WriteFile(filepath.Join(f.dir, name), b, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// setList は list の出力(根の dir が先頭になる)と終了状態を決める。
func (f *fakeRecv) setList(recs []listRec, rc int) {
	var b bytes.Buffer
	if len(recs) > 0 || rc == 0 {
		b.Write(rootRec().encode())
	}
	for _, r := range recs {
		b.Write(r.encode())
	}
	f.put("list", b.Bytes())
	f.put("list.rc", []byte(fmt.Sprint(rc)))
}

func rootRec() listRec {
	return listRec{typ: 'd', readable: true, mode: 0o755, uid: 1000, gid: 1000, mtimeNS: 1760808600_000000000, path: []byte("."), size: 4096}
}

func fileRec(path string, size int64) listRec {
	return listRec{typ: 'f', readable: true, size: size, mode: 0o644, uid: 1000, gid: 1000, mtimeNS: 1760808600_123456789, path: []byte(path)}
}

// setBatch は n 回目の read-batch の出力(tar と変化の一覧)と終了状態を決める。
func (f *fakeRecv) setBatch(n int, files map[string]string, order []string, changed []string, rc int) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, name := range order {
		body := files[name]
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg, Format: tar.FormatPAX})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	b.Write(make([]byte, 10240-b.Len()%10240)) // GNU tar の既定の record までのゼロ埋め
	b.WriteString(contract.ReadBatchChangedMarker)
	for _, c := range changed {
		b.WriteString(c)
		b.WriteByte(0)
	}
	f.put(fmt.Sprintf("rb.%d", n), b.Bytes())
	f.put(fmt.Sprintf("rb.%d.rc", n), []byte(fmt.Sprint(rc)))
}

// batchInput は n 回目の read-batch に渡したパス一覧(NUL 区切り)。
func (f *fakeRecv) batchInput(n int) [][]byte {
	b, err := os.ReadFile(filepath.Join(f.dir, fmt.Sprintf("in.%d", n)))
	if err != nil {
		return nil
	}
	if len(b) == 0 {
		return nil
	}
	return bytes.Split(b[:len(b)-1], []byte{0})
}
