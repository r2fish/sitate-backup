package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 実物の受け口(adapters/acquire/recv/sitate-recv)を相手にした取得の試験(完了条件: files アダプタの外形と契約)。

func buildTree(t *testing.T) string {
	root := treeRoot(t)
	writeFile(t, filepath.Join(root, "a.txt"), "hello", 0o644)
	writeFile(t, filepath.Join(root, "dir", "empty"), "", 0o644)
	writeFile(t, filepath.Join(root, "dir", "日本語", "名前.txt"), "japanese", 0o644)
	writeFile(t, filepath.Join(root, "!bang"), "bang", 0o644)
	if err := os.Symlink("/etc/hostname", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	return root
}

// 基本: 全ファイルの本体・dir・symlink が契約どおりのフレームで出て、ストリームの検査(hash・bytes・順序)に通る。
func TestFilesAcquireBasic(t *testing.T) {
	root := buildTree(t)
	rr := runAdapter(t, realRecv(t, root), request("t1", root, nil))
	if rr.code != 0 {
		t.Fatalf("終了 %d: %s", rr.code, rr.stdout)
	}
	p := parseOutput(t, rr.stdout)
	if p.result.Status != "ok" || p.result.Stats.Errors != 0 {
		t.Fatalf("status=%s stats=%+v", p.result.Status, p.result.Stats)
	}
	ty := p.types()
	want := map[string]string{".": "dir", "a.txt": "file", "dir": "dir", "dir/empty": "file", "dir/日本語": "dir", "dir/日本語/名前.txt": "file", "!bang": "file", "link": "symlink"}
	for k, v := range want {
		if ty[k] != v {
			t.Errorf("%q: type=%q want %q", k, ty[k], v)
		}
	}
	if len(ty) != len(want) {
		t.Errorf("余分なフレームがある: %v", ty)
	}
	b := p.bodies("file")
	if b["a.txt"] != "hello" || b["dir/日本語/名前.txt"] != "japanese" || b["!bang"] != "bang" || b["dir/empty"] != "" {
		t.Errorf("本体が違う: %v", b)
	}
	checkItemEnds(t, p)
	if p.frames[0].h.Type != "dir" || pathOf(p.frames[0].h) != "." {
		t.Errorf("最初のフレームは根の dir(path: .): %+v", p.frames[0].h)
	}
	for _, f := range p.frames {
		if f.h.Type == "symlink" && (f.h.Target == nil || *f.h.Target != "/etc/hostname") {
			t.Errorf("symlink の target: %+v", f.h)
		}
	}
}

// 各 file の item_end の bytes と hash は本体のものと合い、exit_status は null。
func checkItemEnds(t *testing.T, p parsed) {
	t.Helper()
	var body []byte
	n := 0
	for _, f := range p.frames {
		switch f.h.Type {
		case "file", "file-retry":
			body, n = nil, n+1
		case "data":
			body = append(body, f.body...)
		case "item_end":
			s := blake3.Sum256(body)
			if f.h.Hash != harness.Blake3Tag(body) || *f.h.Bytes != int64(len(body)) || f.h.Status != "ok" || string(f.h.ExitStatus) != "null" {
				t.Errorf("item_end が本体と合わない: %+v (hash=%x)", f.h, s[:4])
			}
		}
	}
	if n == 0 {
		t.Error("file が無い")
	}
}

// 見出しの size・mode・uid・gid・mtime_ns は list の値(受け口の stat)を写す。
func TestFilesHeaderValues(t *testing.T) {
	root := treeRoot(t)
	f := filepath.Join(root, "x")
	writeFile(t, f, "12345", 0o640)
	mt := mustTime(t, f)
	rr := runAdapter(t, realRecv(t, root), request("t1", root, nil))
	p := parseOutput(t, rr.stdout)
	for _, fr := range p.frames {
		if fr.h.Type == "file" {
			if *fr.h.Size != 5 || *fr.h.Mode != 0o640 || *fr.h.MtimeNS != mt {
				t.Errorf("見出しの値: %+v mtime want %d", fr.h, mt)
			}
		}
	}
}

func mustTime(t *testing.T, p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.ModTime().UnixNano()
}

// 前回の索引: 全部同じなら file は unchanged、1 つだけ変えれば変えたものだけ本体を読む。dir・symlink は毎回送る。
func TestFilesIncremental(t *testing.T) {
	root := buildTree(t)
	recv := realRecv(t, root)
	first := parseOutput(t, runAdapter(t, recv, request("t1", root, nil)).stdout)
	idx := writePrevIndex(t, "t1", first)
	second := parseOutput(t, runAdapter(t, recv, request("t1", root, &idx)).stdout)
	ty := second.types()
	for _, f := range []string{"a.txt", "dir/empty", "dir/日本語/名前.txt", "!bang"} {
		if ty[f] != "unchanged" {
			t.Errorf("%s: %q(unchanged であるべき)", f, ty[f])
		}
	}
	if ty["dir"] != "dir" || ty["link"] != "symlink" || ty["."] != "dir" {
		t.Errorf("dir・symlink は毎回送る: %v", ty)
	}
	if len(second.bodies("file")) != 0 {
		t.Errorf("本体を読んでいる: %v", second.bodies("file"))
	}
	// 内容を変える(大きさも変える)と、そのファイルだけ file になる
	writeFile(t, filepath.Join(root, "a.txt"), "hello world", 0o644)
	third := parseOutput(t, runAdapter(t, recv, request("t1", root, &idx)).stdout)
	ty = third.types()
	if ty["a.txt"] != "file" || ty["!bang"] != "unchanged" || third.bodies("file")["a.txt"] != "hello world" {
		t.Errorf("変えたものだけ読む: %v", ty)
	}
	// 権限だけの変化も本体を読み直す(spec 3.2.2)
	if err := os.Chmod(filepath.Join(root, "!bang"), 0o664); err != nil {
		t.Fatal(err)
	}
	ty = parseOutput(t, runAdapter(t, recv, request("t1", root, &idx)).stdout).types()
	if ty["!bang"] != "file" {
		t.Errorf("mode だけの変化は file: %q", ty["!bang"])
	}
}

// 索引に無かったパス(新規)は file、索引にあって list に無いパスは何も送らない。
func TestFilesNewAndRemoved(t *testing.T) {
	root := buildTree(t)
	recv := realRecv(t, root)
	idx := writePrevIndex(t, "t1", parseOutput(t, runAdapter(t, recv, request("t1", root, nil)).stdout))
	writeFile(t, filepath.Join(root, "new.txt"), "n", 0o644)
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	ty := parseOutput(t, runAdapter(t, recv, request("t1", root, &idx)).stdout).types()
	if ty["new.txt"] != "file" {
		t.Errorf("新規は file: %v", ty)
	}
	if _, ok := ty["a.txt"]; ok {
		t.Errorf("消えたパスは送らない: %v", ty)
	}
}

// 要求の不正: contract の版違い、SITATE_RECV が無い、種別違いは終了 1・INTERNAL。
func TestFilesRequestErrors(t *testing.T) {
	root := treeRoot(t)
	good := request("t1", root, nil)
	cases := map[string]struct {
		recv, reason string
		req          any
	}{
		"SITATE_RECV なし": {"", "request", good},
		"版違い":            {"/bin/true", "contract", map[string]any{"contract": "sitate.acquire/v0", "target": good["target"], "previous_index": nil}},
		"種別違い":           {"/bin/true", "request", map[string]any{"contract": "sitate.acquire/v1", "target": map[string]any{"id": "t1", "kind": "postgresql", "spec": map[string]any{"path": "x"}}, "previous_index": nil}},
		"対象 id の形":       {"/bin/true", "request", request("x1", root, nil)},
	}
	for name, c := range cases {
		rr := runAdapter(t, c.recv, c.req)
		if rr.code != 1 || !strings.Contains(string(rr.stdout), `"code":"INTERNAL"`) || !strings.Contains(string(rr.stdout), `"reason":"`+c.reason+`"`) {
			t.Errorf("%s: 終了 %d: %s", name, rr.code, rr.stdout)
		}
		if err := contract.Validate("adapter/acquire.json#/$defs/response", rr.stdout); err != nil {
			t.Errorf("%s: 応答がスキーマに合わない: %v", name, err)
		}
	}
}

// 読めないファイルは read-batch に渡さず PERMISSION(path_b64)にし、対象は partial。読めるファイルは取れる。
func TestFilesPermission(t *testing.T) {
	root := treeRoot(t)
	writeFile(t, filepath.Join(root, "ok.txt"), "ok", 0o644)
	writeFile(t, filepath.Join(root, "secret.txt"), "secret", 0o000)
	rr := runAdapter(t, realRecv(t, root), request("t1", root, nil))
	if rr.code != 0 {
		t.Fatalf("終了 %d: %s", rr.code, rr.stdout)
	}
	p := parseOutput(t, rr.stdout)
	if p.result.Status != "partial" || p.result.Stats.Errors != 1 || len(p.result.Errors) != 1 {
		t.Fatalf("結果: %+v", p.result)
	}
	e := p.result.Errors[0]
	if e.Code != "PERMISSION" || e.PathB64 == nil || *e.PathB64 != b64([]byte("secret.txt")) {
		t.Errorf("errors[0]: %+v(UTF-8 として正しいパスも path_b64)", e)
	}
	if !strings.Contains(string(p.raw["errors"]), `"path_b64"`) || strings.Contains(string(p.raw["errors"]), `"path"`) {
		t.Errorf("errors[] の要素は path を持たない: %s", p.raw["errors"])
	}
	if p.bodies("file")["ok.txt"] != "ok" || p.bodies("file")["secret.txt"] != "" {
		t.Errorf("本体: %v", p.bodies("file"))
	}
	if _, ok := p.types()["secret.txt"]; ok {
		t.Errorf("読めないファイルのフレームは出さない")
	}
}

// 名前に \001 を含むファイルは本体を取らず、一覧を止めず、errors[] の 1 件(PERMISSION)で対象は partial(完了条件 19-b。spec 3.2.2、3.3。D-0159)。
func TestFilesSeparatorNameIsPermission(t *testing.T) {
	root := treeRoot(t)
	writeFile(t, filepath.Join(root, "ok.txt"), "ok", 0o644)
	writeFile(t, filepath.Join(root, "bad\x01name"), "x", 0o644)
	rr := runAdapter(t, realRecv(t, root), request("t1", root, nil))
	if rr.code != 0 {
		t.Fatalf("終了 %d: %s", rr.code, rr.stdout)
	}
	p := parseOutput(t, rr.stdout)
	if p.result.Status != "partial" || p.result.Stats.Errors != 1 || len(p.result.Errors) != 1 {
		t.Fatalf("結果: %+v", p.result)
	}
	e := p.result.Errors[0]
	if e.Code != "PERMISSION" || e.PathB64 == nil || *e.PathB64 != b64([]byte("bad\x01name")) {
		t.Errorf("errors[0]: %+v", e)
	}
	if p.bodies("file")["ok.txt"] != "ok" {
		t.Errorf("読めるファイルは取れる: %v", p.bodies("file"))
	}
}
