package adapter

// 取得アダプタのストリーム(spec 3.2.2): フレームを結果の前に onFrame へ渡す。

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const acquireOK = `{"contract":"sitate.acquire/v1","status":"ok","stats":{"files":1,"bytes":5,"errors":0,"errors_omitted":0,"warnings_omitted":0,"changed_during_read":0},"errors":[],"warnings":[]}`

func acquireReq() json.RawMessage {
	return json.RawMessage(`{"contract":"sitate.acquire/v1","target":{"id":"t1","kind":"files","spec":{"path":"/srv/app"}},"previous_index":null}`)
}

func i64(v int64) *int64   { return &v }
func i(v int) *int         { return &v }
func str(s string) *string { return &s }

// frameBytes はフレーム列(見出しの JSON 1 行と本体)を組む。
func frameBytes(t *testing.T) []byte {
	t.Helper()
	zero := "blake3:" + strings.Repeat("0", 64)
	var b bytes.Buffer
	w := func(h contract.StreamHeader, body []byte) {
		if err := harness.WriteFrame(&b, h, body); err != nil {
			t.Fatal(err)
		}
	}
	w(contract.StreamHeader{Type: "dir", Path: str("."), Mode: i(493), UID: i64(0), GID: i64(0), MtimeNS: i64(1)}, nil)
	w(contract.StreamHeader{Type: "file", Path: str("a"), Size: i64(5), Mode: i(420), UID: i64(0), GID: i64(0), MtimeNS: i64(1)}, nil)
	w(contract.StreamHeader{Type: "data", Size: i64(5)}, []byte("hello"))
	w(contract.StreamHeader{Type: "item_end", Bytes: i64(5), Hash: zero, Status: "ok", ExitStatus: json.RawMessage("null")}, nil)
	w(contract.StreamHeader{Type: "end", Bytes: i64(5), Hash: zero, ExitStatus: json.RawMessage("0")}, nil)
	return b.Bytes()
}

func acquireWith(t *testing.T, stream []byte, tail string) built {
	t.Helper()
	s := spec{kind: "acquire", name: "files", script: "cat >/dev/null; cat \"$0.out\"; " + tail}
	b := build(t, s)
	if err := os.WriteFile(filepath.Join(b.dir, "exe.out"), stream, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRunAcquireFrames(t *testing.T) {
	b := acquireWith(t, frameBytes(t), "echo '"+acquireOK+"'")
	var types []string
	var data []byte
	res, err := b.r.Run(ctx, contract.RunContext{}, "acquire", "files", acquireReq(), func(f contract.Frame) error {
		types = append(types, f.Type)
		data = append(data, f.Data...)
		if !json.Valid(f.Meta) || bytes.Contains(f.Meta, []byte("\n")) {
			t.Errorf("Meta は見出しの JSON 1 つ(改行なし): %q", f.Meta)
		}
		return nil
	})
	if err != nil || string(res) != acquireOK {
		t.Fatalf("結果: %s %v", res, err)
	}
	if got := strings.Join(types, ","); got != "dir,file,data,item_end,end" || string(data) != "hello" {
		t.Errorf("フレーム: %s %q", got, data)
	}
}

func TestRunAcquireChildExitsMidStream(t *testing.T) {
	st := frameBytes(t)
	n := bytes.Index(st, []byte(`{"type":"data"`))
	b := acquireWith(t, st[:n], "exit 3")
	var types []string
	_, err := b.r.Run(ctx, contract.RunContext{}, "acquire", "files", acquireReq(), func(f contract.Frame) error { types = append(types, f.Type); return nil })
	internalWith(t, err, "exit_code")
	if strings.Join(types, ",") != "dir,file" {
		t.Errorf("途中までのフレームは onFrame へ渡す: %v", types)
	}
}

func TestRunAcquireTruncatedBodyExit0IsOutput(t *testing.T) {
	st := frameBytes(t)
	n := bytes.Index(st, []byte("hello"))
	b := acquireWith(t, st[:n+2], "")
	_, err := b.r.Run(ctx, contract.RunContext{}, "acquire", "files", acquireReq(), nil)
	internalWith(t, err, "output")
}

func TestRunAcquireBadHeaders(t *testing.T) {
	cases := map[string]string{
		"未知の type":      `{"type":"bogus"}`,
		"パスに ..":        `{"type":"unchanged","path":"a/../b","size":1,"mode":420,"uid":0,"gid":0,"mtime_ns":1}`,
		"パスに空の要素":       `{"type":"unchanged","path":"a//b","size":1,"mode":420,"uid":0,"gid":0,"mtime_ns":1}`,
		"path_b64 に ..": `{"type":"unchanged","path_b64":"Li4=","size":1,"mode":420,"uid":0,"gid":0,"mtime_ns":1}`,
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			b := acquireWith(t, []byte(h+"\n"), "echo '"+acquireOK+"'")
			_, err := b.r.Run(ctx, contract.RunContext{}, "acquire", "files", acquireReq(), nil)
			internalWith(t, err, "output")
		})
	}
}
