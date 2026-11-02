package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// proc は試験が起こす偽の「専用ユーザーの処理」。終わったら gone が閉じる。
type proc struct {
	cmd  *exec.Cmd
	gone chan struct{}
}

func (p *proc) alive() bool {
	select {
	case <-p.gone:
		return false
	default:
		return true
	}
}

func spawn(t *testing.T, dir, script string, extra ...*os.File) *proc {
	t.Helper()
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	cmd.ExtraFiles = extra
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &proc{cmd: cmd, gone: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.gone) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	time.Sleep(300 * time.Millisecond)
	return p
}

func (f *fakeSSH) runDir(sid string) string { return filepath.Join(f.home, ".sitate", "run", sid) }

func stopRes(t *testing.T, f *fakeSSH, priv, sid string) map[string]any {
	t.Helper()
	code, res := call(t, f.req("stop", sid, priv, f.fp, nil))
	wantOK(t, code, res)
	return res
}

// podman の失敗(rc 非 0、雑音だけの出力)を「容器なし」にしない: stop は done: true にならず、inspect は absent を返さない。
func TestFakePodmanFailureIsNotAbsent(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	f.mkRun(t, sidA)
	for _, mode := range []string{"fail", "noise"} {
		f.setHome(t, ".podmode", mode)
		code, res := call(t, f.req("stop", sidA, priv, f.fp, nil))
		if code == 0 && res["done"] == true {
			t.Errorf("%s: podman が失敗したのに done: true: %v", mode, res)
		}
		if code != 0 {
			wantErr(t, code, res, "adapter:ssh-host:TEARDOWN_FAILED")
		}
		code, res = call(t, f.req("inspect", sidA, priv, f.fp, nil))
		wantOK(t, code, res)
		if res["state"] != "unknown" {
			t.Errorf("%s: inspect は unknown を期待: %v", mode, res)
		}
		code, res = call(t, f.req("inspect", "", priv, f.fp, nil))
		wantOK(t, code, res)
		if res["state"] != "unknown" {
			t.Errorf("%s: 掃引は unknown を期待: %v", mode, res)
		}
	}
	for _, rm := range []string{f.readHome(".podrm")} {
		if strings.TrimSpace(rm) != "" {
			t.Errorf("雑音の語が podman rm に渡った: %q", rm)
		}
	}
}

// 容器は自分の session_id の印の付いた物だけを消す。消せなければ remaining に container として載る。
func TestFakeContainersOnlyOwnLabel(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	f.mkRun(t, sidA)
	f.setHome(t, ".pod", "aaaaaaaaaaaa "+sidA+"\nbbbbbbbbbbbb "+sidB+"\ncccccccccccc -\n")
	code, res := call(t, f.req("inspect", sidA, priv, f.fp, nil))
	wantOK(t, code, res)
	if got := resIDs(res, "resources"); !sameSet(got, []string{"workdir:" + sidA, "container:aaaaaaaaaaaa"}) {
		t.Fatalf("inspect: %v", got)
	}
	code, res = call(t, f.req("inspect", "", priv, f.fp, nil))
	wantOK(t, code, res)
	if got := resIDs(res, "resources"); !sameSet(got, []string{"workdir:" + sidA, "container:aaaaaaaaaaaa", "container:bbbbbbbbbbbb"}) {
		t.Fatalf("掃引: %v", got)
	}
	f.setHome(t, ".podmode", "stuck")
	res = stopRes(t, f, priv, sidA)
	if res["done"] != false || !sameSet(resIDs(res, "remaining"), []string{"container:aaaaaaaaaaaa"}) {
		t.Fatalf("消せない容器は remaining に載る: %v", res)
	}
	f.setHome(t, ".podmode", "")
	f.setHome(t, ".podrm", "")
	res = stopRes(t, f, priv, sidA)
	if res["done"] != true {
		t.Fatalf("stop: %v", res)
	}
	if rm := strings.Fields(f.readHome(".podrm")); len(rm) != 1 || rm[0] != "aaaaaaaaaaaa" {
		t.Errorf("podman rm の対象は自分の印の容器だけ: %v", rm)
	}
	if left := f.readHome(".pod"); !strings.Contains(left, "bbbbbbbbbbbb") || !strings.Contains(left, "cccccccccccc") || strings.Contains(left, "aaaaaaaaaaaa") {
		t.Errorf("容器の残り: %q", left)
	}
}

// stop が止められない処理は remaining[] に process として載り(done: false、終了 0)、止めた後の stop は done: true。
// 帰属しない処理(この確かめの SSH のほかの処理)は止めない。
func TestFakeStopRemainingShape(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	f.mkRun(t, sidA, sidB)
	stray := spawn(t, t.TempDir(), "exec sleep 300")
	other := spawn(t, f.runDir(sidB)+"/scratch", "exec sleep 300") // 別の session の物
	res := stopRes(t, f, priv, sidA)
	rem := resIDs(res, "remaining")
	if res["done"] != false || res["verified_by"] != "ssh" || len(rem) != 1 || !strings.HasPrefix(rem[0], "process:") {
		t.Fatalf("残存の形: %v", res)
	}
	if !stray.alive() || !other.alive() {
		t.Errorf("帰属しない処理・別の session の処理を止めた")
	}
	_ = stray.cmd.Process.Kill()
	<-stray.gone
	res = stopRes(t, f, priv, sidA)
	if res["done"] != true || len(res["remaining"].([]any)) != 0 {
		t.Errorf("別の session の処理は A の残存ではない: %v", res)
	}
	if !other.alive() {
		t.Errorf("別の session の処理を止めた")
	}
}

// 相対パスの cmdline・実行ファイル・開いたファイルだけで session に帰属する処理も止める。SIGTERM を無視する処理は強制終了する。
func TestFakeStopAttribution(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	f.mkRun(t, sidA)
	w := f.runDir(sidA)
	if err := exec.Command("cp", "/bin/sleep", w+"/sl").Run(); err != nil {
		t.Fatal(err)
	}
	opened, err := os.Open(w + "/f")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	loop := `trap "" TERM; while :; do sleep 1; done`
	here := t.TempDir()
	cases := map[string]*proc{
		"cwd + TERM を無視":     spawn(t, w+"/scratch", loop),
		"相対パスの cmdline":      spawn(t, here, `exec -a .sitate/run/`+sidA+`/x sleep 300`),
		"実行ファイル":             spawn(t, here, `exec -a sl `+w+`/sl 300`),
		"開いたファイル + TERM を無視": spawn(t, here, loop, opened),
	}
	res := stopRes(t, f, priv, sidA)
	if res["done"] != true {
		t.Errorf("stop: %v", res)
	}
	for name, p := range cases {
		if p.alive() {
			t.Errorf("%s: 処理が残った", name)
		}
	}
}

// 作業ディレクトリがリンクなら、inspect は workdir として列挙し、stop はリンクだけを消す(リンク先は消さない)。
func TestFakeLinkedWorkdir(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	target := t.TempDir()
	if err := os.WriteFile(target+"/keep", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.home, ".sitate", "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, f.runDir(sidA)); err != nil {
		t.Fatal(err)
	}
	code, res := call(t, f.req("inspect", sidA, priv, f.fp, nil))
	wantOK(t, code, res)
	if res["state"] != "present" || !sameSet(resIDs(res, "resources"), []string{"workdir:" + sidA}) {
		t.Fatalf("リンクの作業ディレクトリを列挙する: %v", res)
	}
	res = stopRes(t, f, priv, sidA)
	if res["done"] != true {
		t.Errorf("stop: %v", res)
	}
	if _, err := os.Lstat(f.runDir(sidA)); err == nil {
		t.Errorf("リンクが残った")
	}
	if b, err := os.ReadFile(target + "/keep"); err != nil || string(b) != "x" {
		t.Errorf("リンク先に触れた: %v", err)
	}
	code, res = call(t, f.req("inspect", sidA, priv, f.fp, nil))
	wantOK(t, code, res)
	if res["state"] != "absent" {
		t.Errorf("stop の後の inspect: %v", res)
	}
}

// 作業ディレクトリを作った後の失敗は、作った資源を返す(核が同じ session_id で stop できる)。
func TestFakeCreateScratchReturnsResources(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	c := dialFake(t, f, priv)
	f.setHome(t, ".mkfail", "")
	_, res, e := createScratch(c, sidA)
	if e == nil || !sameSet(resTypes(res), []string{"workdir:" + sidA}) {
		t.Fatalf("mkdir cipher の失敗: res=%v e=%v", res, e)
	}
	if err := os.Remove(filepath.Join(f.home, ".mkfail")); err != nil {
		t.Fatal(err)
	}
	stopRes(t, f, priv, sidA)
	_, res, e = createScratch(c, sidA) // gocryptfs の -init は通り、マウントは失敗する
	if e == nil || !sameSet(resTypes(res), []string{"workdir:" + sidA, "scratch:" + sidA}) {
		t.Fatalf("マウントの失敗: res=%v e=%v", res, e)
	}
	_, res, e = createScratch(c, sidA)
	if e == nil || e.code != "adapter:ssh-host:SESSION_EXISTS" || len(res) != 0 {
		t.Errorf("同じ session_id の再作成は何も返さず SESSION_EXISTS: res=%v e=%v", res, e)
	}
}

func resTypes(rs []contract.AdapterResource) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Type+":"+r.ID)
	}
	return out
}
