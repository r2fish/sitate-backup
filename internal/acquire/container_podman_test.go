package acquire

// 実物の rootless podman での試験(開発用コンテナ: SITATE_NESTED=1 make shell-linux ...)。podman が無いか、容器を起動できない環境では skip する
// (skip は合格に数えない: 起動できない理由を skip の文に出す)。

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const probeSrc = `package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"time"
)

func main() {
	if os.Getenv("PROBE_MODE") == "recv" {
		out, err := exec.Command(os.Getenv("SITATE_RECV"), "list", "t1").CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"out": string(out), "code": code})
		return
	}
	if os.Getenv("PROBE_MODE") == "sleep" {
		time.Sleep(time.Minute)
		return
	}
	ents, _ := os.ReadDir("/")
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	_, shErr := os.Stat("/bin/sh")
	_, dialErr := net.DialTimeout("tcp", "127.0.0.1:9", time.Second)
	_, ifaces := net.Interfaces()
	werr := os.WriteFile("/x", []byte("x"), 0o644)
	json.NewEncoder(os.Stdout).Encode(map[string]any{"root": names, "sh": shErr == nil, "dial_failed": dialErr != nil, "iface_err": ifaces != nil, "write_failed": werr != nil,
		"uid": os.Getuid(), "recv": os.Getenv("SITATE_RECV"), "env": os.Environ()})
}
`

type podmanEnv struct {
	runID string // 空なら testRunID
	c     *Container
	probe string
	root  taskDir
}

// newPodmanEnv は静的な試験用の実行ファイルを作り、容器を起動できなければ skip する。
func newPodmanEnv(t *testing.T) *podmanEnv {
	t.Helper()
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman が無い")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "probe.go")
	if err := os.WriteFile(src, []byte(probeSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe")
	b := exec.Command("go", "build", "-o", probe, src)
	b.Dir = dir
	b.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := b.CombinedOutput(); err != nil {
		t.Fatalf("試験用の実行ファイルを作れない: %v %s", err, out)
	}
	_ = os.Chmod(dir, 0o755)
	e := &podmanEnv{c: &Container{RecvExe: probe}, probe: probe,
		root: taskDir{Root: filepath.Join(dir, "rootfs"), Pipe: filepath.Join(dir, "pipe")}}
	if err := os.MkdirAll(e.root.Pipe, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{pipeReqName, pipeRepName} { // 容器の起動の確認用(中継の試験では同じ FIFO を中継が開く)
		if err := syscall.Mkfifo(filepath.Join(e.root.Pipe, n), 0o666); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(filepath.Join(e.root.Pipe, n), 0o666)
	}
	if err := prepareRoot(e.root, "x"); err != nil {
		t.Fatal(err)
	}
	if out, err := e.run(t, nil, 20*time.Second).CombinedOutput(); err != nil {
		t.Skipf("この環境では容器を起動できない: %v: %s", err, lastLines(string(out)))
	}
	return e
}

func lastLines(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > 3 {
		l = l[len(l)-3:]
	}
	return strings.Join(l, " | ")
}

func (e *podmanEnv) run(t *testing.T, env []string, d time.Duration) *exec.Cmd {
	e.c.ExtraEnv = env
	id := e.runID
	if id == "" {
		id = testRunID
	}
	a, err := e.c.args(id, e.root, "x", e.probe, []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LC_ALL=C"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return exec.CommandContext(ctx, a[0], a[1:]...)
}

// 容器の中に実行ファイル以外のコマンドが無く、ネットワークが無く、ルートが読み取り専用で、非特権ユーザーで動く。
func TestPodmanContainerIsolation(t *testing.T) {
	e := newPodmanEnv(t)
	var out bytes.Buffer
	cmd := e.run(t, nil, 30*time.Second)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Root        []string
		Sh          bool
		DialFailed  bool `json:"dial_failed"`
		WriteFailed bool `json:"write_failed"`
		UID         int
		Recv        string
		Env         []string
	}
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if r.Sh || !r.DialFailed || !r.WriteFailed || r.UID == 0 || r.Recv != containerRecv {
		t.Errorf("隔離: %+v", r)
	}
	for _, n := range r.Root {
		switch n {
		case "sitate", "run", "proc", "dev", "sys", "etc", "tmp", "var": // 後ろの 5 つは podman が作る空の場所(/bin・/usr・/lib は無い)
		default:
			t.Errorf("ルートに余計なもの %q(実行ファイル以外のコマンドを置かない)", n)
		}
	}
	for _, kv := range r.Env {
		if strings.Contains(kv, "KEY") || strings.Contains(kv, "SECRET") {
			t.Errorf("秘密らしい環境変数: %s", kv)
		}
	}
}

// run_id のラベルで RunCleanup が容器を片付ける(クライアントを SIGKILL しても容器が残る場合を含む)。
func TestPodmanRunCleanupByLabel(t *testing.T) {
	e := newPodmanEnv(t)
	cmd := e.run(t, []string{"PROBE_MODE=sleep"}, time.Minute)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	rc := &RunCleanup{}
	ids := func() []string {
		l, _, _ := rc.listContainers(context.Background(), "label="+RunLabel+"="+testRunID)
		return l
	}
	for i := 0; i < 50 && len(ids()) == 0; i++ {
		time.Sleep(200 * time.Millisecond)
	}
	if len(ids()) == 0 {
		t.Fatal("ラベル付きの容器が見えない")
	}
	_ = cmd.Process.Kill() // クライアントだけを落とす
	cl, un, err := rc.Cleanup(context.Background(), testRunID, "")
	if err != nil || len(un) != 0 { // クライアントを落とすと容器が先に消えることもある(cl が空でもよい)
		t.Fatalf("Cleanup: %v %v %v", cl, un, err)
	}
	if left := ids(); len(left) != 0 {
		t.Errorf("片付けた後に残る: %v", left)
	}
}

// 容器の中の補助コマンドから、パイプ(FIFO の bind マウント)を通って核の中継、受け口(偽の SSH)まで届く。
func TestPodmanRecvThroughPipes(t *testing.T) {
	e := newPodmanEnv(t)
	hb := filepath.Join(filepath.Dir(e.probe), "recvhelper")
	_, self, _, _ := runtime.Caller(0)
	b := exec.Command("go", "build", "-o", hb, "./testdata/recvhelper")
	b.Dir = filepath.Dir(self)
	b.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := b.CombinedOutput(); err != nil {
		t.Fatalf("補助コマンドを作れない: %v %s", err, out)
	}
	e.c.RecvExe = hb
	src := startSource22(t, func(r execRec) (string, int) { return "listing-from-receiver", 0 })
	conn := contract.SSHConn{Target: contract.SSHTarget{Host: "127.0.0.1", Port: 22, User: "sitate-ro"}, Cred: contract.NewSecret([]byte(newAdminKey(t))), PinnedFP: src.FP}
	r, err := startRelay(context.Background(), e.root.Pipe, "t1", routeSSH{}, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer r.stop()
	var out bytes.Buffer
	cmd := e.run(t, []string{"PROBE_MODE=recv"}, 30*time.Second)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	var res struct {
		Out  string
		Code int
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil || res.Code != 0 || res.Out != "listing-from-receiver" {
		t.Errorf("容器の中から受け口まで届かない: %s (%v)", out.String(), err)
	}
	if ex := src.Execs(); len(ex) != 1 || ex[0].Cmd != "list t1" || ex[0].User != "sitate-ro" {
		t.Errorf("受け口の呼び出し: %+v", ex)
	}
}

// run_id が分からない掃除(interrupted が空)は、動いている他の実行の容器を消さない。
func TestPodmanCleanupLeavesOtherRunsAlone(t *testing.T) {
	e := newPodmanEnv(t)
	e.runID = "r-20261018T173000Z-aaaa"
	cmd := e.run(t, []string{"PROBE_MODE=sleep"}, time.Minute)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	rc := &RunCleanup{}
	other := "label=" + RunLabel + "=" + e.runID
	for i := 0; i < 50; i++ {
		if l, _, _ := rc.listContainers(context.Background(), other); len(l) > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, _, err := rc.Cleanup(context.Background(), "r-20261018T173000Z-bbbb", ""); err != nil {
		t.Fatal(err)
	}
	if l, _, _ := rc.listContainers(context.Background(), other); len(l) != 1 {
		t.Errorf("他の実行の動いている容器を消した: %v", l)
	}
	if _, _, err := rc.Cleanup(context.Background(), e.runID, e.runID); err != nil {
		t.Fatal(err)
	}
	if l, _, _ := rc.listContainers(context.Background(), other); len(l) != 0 {
		t.Errorf("own の掃除で消える: %v", l)
	}
	_ = cmd.Process.Kill()
}
