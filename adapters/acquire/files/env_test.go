package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 試験の道具: アダプタの実行ファイルを作り(go build)、受け口の代わりの補助コマンド(SITATE_RECV)を用意して流す。

var adapterExe string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sitate-files-test-")
	if err != nil {
		panic(err)
	}
	adapterExe = filepath.Join(dir, "files")
	if b, err := exec.Command("go", "build", "-o", adapterExe, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build: %v\n%s", err, b)
		os.Exit(2)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func recvSource(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "recv", "sitate-recv"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// realRecv は実物の受け口に許可リスト(対象 t1 = root)を埋め込み、`<op> <id>` を SSH_ORIGINAL_COMMAND にして実行する補助コマンドを作る。
// root で走るときは nobody の権限で動かす(root は権限の検査を通り抜けるため)。
func realRecv(t *testing.T, root string) string {
	t.Helper()
	dir := t.TempDir()
	src := string(recvSource(t))
	rec := fmt.Sprintf("t1\tfiles\t%s\t%s", root, mustJSON(root))
	src = strings.Replace(src, "SITATE_TARGETS=''", "SITATE_TARGETS="+sq(rec), 1)
	script := filepath.Join(dir, "recv.sh")
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "helper")
	pre := ""
	if os.Geteuid() == 0 {
		pre = "setpriv --reuid=65534 --regid=65534 --clear-groups "
	}
	body := fmt.Sprintf("#!/bin/sh\nSSH_ORIGINAL_COMMAND=\"$1 $2\" exec %ssh %s\n", pre, script)
	if err := os.WriteFile(helper, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		_ = os.Chmod(dir, 0o755)
	}
	return helper
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// treeRoot は試験用の木の根を作る(root で走るときも nobody が通れるように 0755)。
func treeRoot(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	openPath(t, base)
	root := filepath.Join(base, "data")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeFile(t *testing.T, p, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(p, mode)
}

type runResult struct {
	code   int
	stdout []byte
	rss    int64 // 最大常駐サイズ(バイト。Linux)
}

// runAdapter は要求を流す。recv が空なら SITATE_RECV を渡さない。
func runAdapter(t *testing.T, recv string, req any) runResult {
	t.Helper()
	rb, _ := json.Marshal(req)
	cmd := exec.Command(adapterExe, "run")
	cmd.Stdin = bytes.NewReader(append(rb, '\n'))
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C", "TMPDIR=" + t.TempDir()}
	if recv != "" {
		cmd.Env = append(cmd.Env, contract.EnvRecv+"="+recv)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err := cmd.Run()
	if errb.Len() > 0 {
		t.Logf("アダプタの標準エラー: %s", errb.String())
	}
	rr := runResult{stdout: out.Bytes()}
	if ee, ok := err.(*exec.ExitError); ok {
		rr.code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		rr.rss = int64(ru.Maxrss)
		if runtime.GOOS == "linux" {
			rr.rss *= 1024
		}
	}
	return rr
}

func request(id, root string, prev *string) map[string]any {
	return map[string]any{"contract": "sitate.acquire/v1", "target": map[string]any{"id": id, "kind": "files", "spec": map[string]any{"path": root}}, "previous_index": prev}
}

// frame は読み出したフレーム 1 つ。
type frame struct {
	h    contract.StreamHeader
	body []byte
}

type parsed struct {
	end    contract.StreamHeader
	frames []frame
	result contract.AcquireResult
	raw    map[string]json.RawMessage
}

// parseOutput は標準出力を sitate.stream/v1 の検査(hash・bytes・順序)つきで読み、最終行の結果 JSON を返す。
func parseOutput(t *testing.T, out []byte) parsed {
	t.Helper()
	br := bufio.NewReader(bytes.NewReader(out))
	var p parsed
	end, err := harness.ReadStream(br, func(f contract.Frame) error {
		var h contract.StreamHeader
		if err := json.Unmarshal(f.Meta, &h); err != nil {
			return err
		}
		p.frames = append(p.frames, frame{h, f.Data})
		return nil
	})
	if err != nil {
		t.Fatalf("ストリームが契約に反する: %v", err)
	}
	p.end = end
	line, err := br.ReadBytes('\n')
	if err != nil {
		t.Fatalf("結果の行が無い: %v", err)
	}
	if rest, _ := br.ReadByte(); rest != 0 {
		t.Errorf("結果の後に何かある")
	}
	if err := contract.Validate("adapter/acquire.json#/$defs/result", line); err != nil {
		t.Fatalf("結果がスキーマに合わない: %v\n%s", err, line)
	}
	_ = json.Unmarshal(line, &p.result)
	_ = json.Unmarshal(line, &p.raw)
	return p
}

// files は file(と file-retry)のフレームの path → 本体を集める。
func (p parsed) bodies(typ string) map[string]string {
	m := map[string]string{}
	var cur string
	for _, f := range p.frames {
		switch f.h.Type {
		case typ:
			cur = pathOf(f.h)
			m[cur] = ""
		case contract.FrameTypeData:
			if cur != "" {
				m[cur] += string(f.body)
			}
		case contract.FrameTypeItemEnd:
			cur = ""
		}
	}
	return m
}

func pathOf(h contract.StreamHeader) string {
	b, _ := harness.StreamPathBytes(h)
	return string(b)
}

func (p parsed) types() map[string]string {
	m := map[string]string{}
	for _, f := range p.frames {
		switch f.h.Type {
		case "file", "file-retry", "unchanged", "dir", "symlink":
			m[pathOf(f.h)] = f.h.Type
		}
	}
	return m
}

// writePrevIndex は取得の出力から前回の索引(JSON Lines。spec 3.2.2 の previous_index)を作る。
func writePrevIndex(t *testing.T, id string, p parsed) string {
	t.Helper()
	type ent struct {
		path []byte
		line map[string]any
	}
	var ents []ent
	for _, f := range p.frames {
		switch f.h.Type {
		case "file", "file-retry", "unchanged", "dir", "symlink":
			pb, _ := harness.StreamPathBytes(f.h)
			typ := f.h.Type
			if typ == "unchanged" || typ == "file-retry" {
				typ = "file"
			}
			l := map[string]any{"type": typ, "size": deref64(f.h.Size), "mtime_ns": deref64(f.h.MtimeNS), "mode": derefInt(f.h.Mode), "uid": deref64(f.h.UID), "gid": deref64(f.h.GID)}
			if utf8Valid(pb) {
				l["path"] = string(pb)
			} else {
				l["path_b64"] = b64(pb)
			}
			ents = append(ents, ent{pb, l})
		}
	}
	sort.Slice(ents, func(i, j int) bool { return pathCmp(ents[i].path, ents[j].path) < 0 })
	var b bytes.Buffer
	fmt.Fprintf(&b, "{\"format\":\"sitate.previdx/v1\",\"target\":%q}\n", id)
	for _, e := range ents {
		j, _ := json.Marshal(e.line)
		b.Write(j)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), id+".prev.idx")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func deref64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func utf8Valid(b []byte) bool { return utf8.Valid(b) }
func b64(b []byte) string     { return base64.StdEncoding.EncodeToString(b) }

// openPath は root で走るとき、nobody が base までの各ディレクトリを通れるようにする。
func openPath(t *testing.T, base string) {
	if os.Geteuid() != 0 {
		return
	}
	for d := base; d != os.TempDir() && d != "/" && d != "."; d = filepath.Dir(d) {
		if err := os.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}
