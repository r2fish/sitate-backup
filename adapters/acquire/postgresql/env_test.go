package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 試験の道具: アダプタの実行ファイルを作り(go build)、受け口の代わりの補助コマンド(SITATE_RECV)を用意して流す。

var adapterExe string

func TestMain(m *testing.M) {
	if p := os.Getenv("SITATE_TEST_ADAPTER_EXE"); p != "" { // 作り済みの実行ファイルを使う(go の無い PostgreSQL の容器の中で試験を走らせるとき)
		adapterExe = p
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "sitate-postgresql-test-")
	if err != nil {
		panic(err)
	}
	adapterExe = filepath.Join(dir, "postgresql")
	if b, err := exec.Command("go", "build", "-o", adapterExe, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build: %v\n%s", err, b)
		os.Exit(2)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func sq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// fakeRecv: dump-pg-globals・dump-pg の出力と終了状態を試験が決める。呼ばれた操作の順を calls に残す。
type fakeRecv struct {
	t   *testing.T
	dir string
}

func newFakeRecv(t *testing.T) *fakeRecv {
	d := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
d=%s
echo "$1 $2" >> $d/calls
case $1 in
dump-pg-globals | dump-pg) cat $d/$1.out 2>/dev/null; exit $(cat $d/$1.rc 2>/dev/null || echo 0) ;;
esac
exit 126
`, sq(d))
	if err := os.WriteFile(filepath.Join(d, "helper"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeRecv{t, d}
}

func (f *fakeRecv) path() string { return filepath.Join(f.dir, "helper") }

// set は操作 op の標準出力と終了状態を決める。
func (f *fakeRecv) set(op string, out []byte, rc int) {
	if err := os.WriteFile(filepath.Join(f.dir, op+".out"), out, 0o644); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, op+".rc"), []byte(fmt.Sprint(rc)), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeRecv) calls() []string {
	b, _ := os.ReadFile(filepath.Join(f.dir, "calls"))
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

type runResult struct {
	code   int
	stdout []byte
}

func request(id string) map[string]any {
	return map[string]any{"contract": "sitate.acquire/v1",
		"target":         map[string]any{"id": id, "kind": "postgresql", "spec": map[string]any{"path": "/run/postgresql", "port": 5432}},
		"previous_index": nil}
}

// runAdapter は要求を流す。recv が空なら SITATE_RECV を渡さない。
func runAdapter(t *testing.T, recv string, req any) runResult {
	t.Helper()
	rb, _ := json.Marshal(req)
	cmd := exec.Command(adapterExe, "run")
	cmd.Stdin = bytes.NewReader(append(rb, '\n'))
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	if recv != "" {
		cmd.Env = append(cmd.Env, contract.EnvRecv+"="+recv)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
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
	return rr
}

type frame struct {
	h    contract.StreamHeader
	data []byte
}

type parsed struct {
	frames []frame
	end    contract.StreamHeader
	result contract.AcquireResult
	line   []byte
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
	p.line = line
	if err := json.Unmarshal(line, &p.result); err != nil {
		t.Fatal(err)
	}
	return p
}

// dumps は dump フレームごとに (format, 本体, item_end) を集める。
type dumpItem struct {
	format string
	body   []byte
	end    contract.StreamHeader
}

func (p parsed) dumps(t *testing.T) []dumpItem {
	t.Helper()
	var out []dumpItem
	for _, f := range p.frames {
		switch f.h.Type {
		case contract.FrameTypeDump:
			out = append(out, dumpItem{format: f.h.Format})
		case contract.FrameTypeData:
			out[len(out)-1].body = append(out[len(out)-1].body, f.data...)
		case contract.FrameTypeItemEnd:
			out[len(out)-1].end = f.h
		default:
			t.Errorf("postgresql のストリームに dump 以外のフレーム %q", f.h.Type)
		}
	}
	return out
}
