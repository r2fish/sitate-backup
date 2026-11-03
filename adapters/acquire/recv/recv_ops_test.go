package recv

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 受け口の動作の試験(許可リストを埋め込んだ実物を sh で動かす。root で走るときは nobody の権限で動かす)。

func sq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func renderRecv(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile("sitate-recv")
	if err != nil {
		t.Fatal(err)
	}
	rec := fmt.Sprintf("t1\tfiles\t%s\t\"%s\"", root, root)
	src := strings.Replace(string(b), "SITATE_TARGETS=''", "SITATE_TARGETS="+sq(rec), 1)
	dir := t.TempDir()
	if os.Geteuid() == 0 {
		_ = os.Chmod(dir, 0o755)
	}
	p := filepath.Join(dir, "recv.sh")
	if err := os.WriteFile(p, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func openTree(t *testing.T) string {
	base := t.TempDir()
	if os.Geteuid() == 0 {
		for d := base; d != os.TempDir() && d != "/"; d = filepath.Dir(d) {
			_ = os.Chmod(d, 0o755)
		}
	}
	root := filepath.Join(base, "data")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func put(t *testing.T, p, c string, m os.FileMode) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(c), m); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(p, m)
}

func cmdFor(script, op string) *exec.Cmd {
	args := []string{script}
	name := "sh"
	if os.Geteuid() == 0 {
		name, args = "setpriv", append([]string{"--reuid=65534", "--regid=65534", "--clear-groups", "sh"}, args...)
	}
	c := exec.Command(name, args...)
	c.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin", "SSH_ORIGINAL_COMMAND=" + op}
	return c
}

func call(t *testing.T, script, op string, stdin []byte) (int, []byte) {
	t.Helper()
	c := cmdFor(script, op)
	c.Stdin = bytes.NewReader(stdin)
	var o bytes.Buffer
	c.Stdout = &o
	err := c.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, o.Bytes()
}

// 許可リストの対象でも、操作と対象の種別が合わない(files への dump-pg)・未知の id・書式の誤りは 126 で何も出さない。
func TestRecvRefusesWithAllowlist(t *testing.T) {
	s := renderRecv(t, openTree(t))
	for _, op := range []string{"dump-pg t1", "dump-pg-globals t1", "dump-my t1", "list t2", "list t01", "list T1", "read-batch t1 x", "list  t1;id", "list t1\nid"} {
		if code, out := call(t, s, op, nil); code != 126 || len(out) != 0 {
			t.Errorf("%q: 終了 %d 出力 %q", op, code, out)
		}
	}
}

// list: 根 "." が先頭、残りは path のバイト順(! で始まる名前、a < a-b < a.txt < a/b)。FIFO は出さない。読めない物は readable 0。
func TestRecvListOrderAndTypes(t *testing.T) {
	root := openTree(t)
	for _, n := range []string{"!bang", "a-b", "a.txt", "a/b", "z"} {
		put(t, filepath.Join(root, n), "x", 0o644)
	}
	put(t, filepath.Join(root, "secret"), "s", 0)
	_ = exec.Command("mkfifo", filepath.Join(root, "fifo")).Run()
	_ = os.Symlink("/etc/hostname", filepath.Join(root, "lnk"))
	code, out := call(t, renderRecv(t, root), "list t1", nil)
	if code != 0 {
		t.Fatalf("終了 %d", code)
	}
	fields := bytes.Split(out, []byte{0})
	if len(fields) < 2 || (len(fields)-1)%9 != 0 {
		t.Fatalf("9 欄ずつでない: %d", len(fields))
	}
	var paths []string
	typ := map[string]string{}
	rd := map[string]string{}
	for i := 0; i+9 <= len(fields)-1; i += 9 {
		p := string(fields[i+7])
		paths = append(paths, p)
		typ[p], rd[p] = string(fields[i]), string(fields[i+1])
	}
	want := []string{".", "!bang", "a", "a-b", "a.txt", "a/b", "lnk", "secret", "z"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Errorf("順序: %v want %v", paths, want)
	}
	if !sort.SliceIsSorted(paths[1:], func(i, j int) bool { return paths[1:][i] < paths[1:][j] }) {
		t.Errorf("バイト順でない")
	}
	if typ["lnk"] != "l" || typ["a"] != "d" || typ["z"] != "f" || (os.Geteuid() != 0 && rd["secret"] != "0") {
		t.Errorf("type/readable: %v %v", typ, rd)
	}
	if string(fields[8]) != "" || !bytes.Contains(out, []byte("/etc/hostname\x00")) {
		t.Errorf("symlink の target")
	}
	if c, _ := call(t, renderRecv(t, filepath.Join(root, "none")), "list t1", nil); c != 3 {
		t.Errorf("根が無い: 終了 %d(3)", c)
	}
}

func tarNames(t *testing.T, b []byte) ([]string, map[string]string, []byte) {
	tr := tar.NewReader(bytes.NewReader(b))
	var names []string
	body := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		d, _ := io.ReadAll(tr)
		names = append(names, h.Name)
		body[h.Name] = string(d)
	}
	i := bytes.Index(b, []byte(contract.ReadBatchChangedMarker))
	if i < 0 {
		t.Fatalf("変化の一覧の区切りが無い")
	}
	return names, body, b[i+len(contract.ReadBatchChangedMarker):]
}

// read-batch: 通常の読める物だけ tar に入れ、根の外へ出る物(途中のシンボリックリンク・リンクの先)・ディレクトリ・無い物は飛ばす。
func TestRecvReadBatchSafety(t *testing.T) {
	root := openTree(t)
	outside := t.TempDir()
	if os.Geteuid() == 0 {
		_ = os.Chmod(outside, 0o755)
	}
	put(t, filepath.Join(outside, "leak"), "LEAK", 0o644)
	put(t, filepath.Join(root, "ok"), "OK", 0o644)
	put(t, filepath.Join(root, "sub", "in"), "IN", 0o644)
	_ = os.Symlink(outside, filepath.Join(root, "esc"))
	_ = os.Symlink(filepath.Join(outside, "leak"), filepath.Join(root, "leaklink"))
	in := "ok\x00sub/in\x00esc/leak\x00leaklink\x00sub\x00missing\x00"
	code, out := call(t, renderRecv(t, root), "read-batch t1", []byte(in))
	if code != 0 {
		t.Fatalf("終了 %d", code)
	}
	names, body, changed := tarNames(t, out)
	if fmt.Sprint(names) != "[ok sub/in]" || body["ok"] != "OK" || bytes.Contains(out, []byte("LEAK")) || len(changed) != 0 {
		t.Errorf("tar: %v 変化=%q", names, changed)
	}
}

// read-batch の入力の誤りは何も出さず 126(契約の試験ベクトルの input)。空の一覧は空の tar と空の変化の一覧。
func TestRecvReadBatchInput(t *testing.T) {
	var d struct {
		Input []struct {
			Name  string
			Paths []string
			Ok    bool
		}
	}
	harness.LoadVectors(t, "receiver/read-batch.json", &d)
	s := renderRecv(t, openTree(t))
	for _, v := range d.Input {
		in := ""
		for _, p := range v.Paths {
			in += p + "\x00"
		}
		code, out := call(t, s, "read-batch t1", []byte(in))
		if v.Ok != (code == 0) || (!v.Ok && len(out) != 0) {
			t.Errorf("%s: 終了 %d 出力 %d バイト(ok=%v)", v.Name, code, len(out), v.Ok)
		}
	}
	if c, _ := call(t, s, "read-batch t1", []byte("a\x00b")); c != 126 {
		t.Errorf("NUL で終わらない入力: %d", c)
	}
}

// 読み取り中に変化したファイル: tar の書き出しを止めている間に伸ばすと、変化の一覧にそのパスが載る。
func TestRecvReadBatchChanged(t *testing.T) {
	root := openTree(t)
	big := filepath.Join(root, "big")
	put(t, big, strings.Repeat("A", 8<<20), 0o666)
	put(t, filepath.Join(root, "small"), "S", 0o644)
	c := cmdFor(renderRecv(t, root), "read-batch t1")
	c.Stdin = strings.NewReader("big\x00small\x00")
	pipe, _ := c.StdoutPipe()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 1<<16)
	_, _ = io.ReadFull(pipe, head) // 読んだ所で tar はパイプの詰まりで止まる
	f, err := os.OpenFile(big, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("more")
	f.Close()
	rest, _ := io.ReadAll(pipe)
	if err := c.Wait(); err != nil {
		t.Fatal(err)
	}
	_, _, changed := tarNames(t, append(head, rest...))
	if string(changed) != "big\x00" {
		t.Errorf("変化の一覧: %q(big だけ)", changed)
	}
}

// 受け口のスクリプトは sh -n を通り、eval を使わない。
func TestRecvScriptsSyntaxNoEval(t *testing.T) {
	for _, n := range []string{"sitate-recv", "source-setup", "source-survey"} {
		if out, err := exec.Command("sh", "-n", n).CombinedOutput(); err != nil {
			t.Errorf("%s: sh -n: %v %s", n, err, out)
		}
		b, _ := os.ReadFile(n)
		for i, l := range strings.Split(string(b), "\n") {
			code := strings.SplitN(l, "#", 2)[0]
			if strings.Contains(" "+code+" ", " eval ") || strings.Contains(code, "`eval") {
				t.Errorf("%s:%d: eval を使っている", n, i+1)
			}
		}
	}
}

// selftest: スキーマに合い、ファイルの対象の root_state が分かれる。
func TestRecvSelftestTargets(t *testing.T) {
	root := openTree(t)
	code, out := call(t, renderRecv(t, root), "selftest", nil)
	if code != 0 || contract.Validate("receiver/selftest.json", out) != nil || !bytes.Contains(out, []byte(`"root_state":"ok"`)) {
		t.Fatalf("selftest: %d %s", code, out)
	}
	_, out = call(t, renderRecv(t, filepath.Join(root, "none")), "selftest", nil)
	if !bytes.Contains(out, []byte(`"root_state":"not_found"`)) {
		t.Errorf("not_found: %s", out)
	}
}

// シンボリックリンクの中身の \001 で list のレコードを偽造できない(終了 5 で止まる)。ディレクトリの名前の \001 も同じ。
func TestRecvListRefusesSeparatorInLinkOrDirName(t *testing.T) {
	for _, mk := range []func(root string){
		func(root string) {
			_ = os.Symlink("x\x01f\x011\x015\x01644\x010\x010\x011.000000000\x01forged-file\x01", filepath.Join(root, "lnk"))
		},
		func(root string) { put(t, filepath.Join(root, "d\x01x", "f"), "x", 0o644) },
	} {
		root := openTree(t)
		put(t, filepath.Join(root, "ok"), "x", 0o644)
		mk(root)
		code, out := call(t, renderRecv(t, root), "list t1", nil)
		if code != 5 || bytes.Contains(out, []byte("forged-file\x00")) {
			t.Errorf("終了 %d(5)・偽のレコードが出ている: %v", code, bytes.Contains(out, []byte("forged-file")))
		}
	}
}

// 名前に \001 を含むファイルは本体を取らず readable 0 のレコードとして出し、一覧は止まらない(spec 3.3。D-0159。完了条件 19-b)。
// レコードの欄の区切りは崩れず、名前の中の \001 はそのまま残り、ほかのレコードは path のバイト順に並ぶ。
func TestRecvListSeparatorFileNameIsUnreadableRecord(t *testing.T) {
	root := openTree(t)
	for _, n := range []string{"a", "a\x01b", "a\x01\x01", "b", "c/z"} {
		put(t, filepath.Join(root, n), "x", 0o644)
	}
	code, out := call(t, renderRecv(t, root), "list t1", nil)
	if code != 0 {
		t.Fatalf("終了 %d(0): %q", code, out)
	}
	f := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
	if len(f)%9 != 0 {
		t.Fatalf("欄の数 %d が 9 の倍数でない: %q", len(f), out)
	}
	type rec struct{ typ, rd, path string }
	var got []rec
	for i := 0; i < len(f); i += 9 {
		got = append(got, rec{string(f[i]), string(f[i+1]), string(f[i+7])})
	}
	want := []rec{{"d", "1", "."}, {"f", "1", "a"}, {"f", "0", "a\x01\x01"}, {"f", "0", "a\x01b"}, {"f", "1", "b"}, {"d", "1", "c"}, {"f", "1", "c/z"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("レコード %q\n期待 %q", got, want)
	}
}

// source-setup が持つ「根に置けない場所」の一覧は internal/contract の一覧(spec 4.1 の (i)(ii)(iii))と同じ。
func TestSetupForbiddenRootsMatchContract(t *testing.T) {
	b, _ := os.ReadFile("source-setup")
	s := string(b)
	for _, p := range append(append([]string{contract.RootForbiddenI}, contract.RootForbiddenII...), contract.RootForbiddenIII...) {
		if p != "/" && !strings.Contains(s, p) {
			t.Errorf("source-setup の一覧に %s が無い", p)
		}
	}
}
