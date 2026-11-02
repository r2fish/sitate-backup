package acquire

// 取得アダプタの容器の引数列と、SITATE_RECV の中継(補助コマンド ↔ 核 ↔ 受け口)の試験。実際の容器の試験は container_podman_test.go。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestContainerArgs(t *testing.T) {
	c := &Container{RecvExe: "/opt/sitate/sitate"}
	td := taskDir{Root: "/w/r-1/t1/rootfs", Pipe: "/w/r-1/t1/pipe", PrevIdx: "/w/r-1/t1.prev.idx"}
	env := []string{"PATH=/usr/bin", "LC_ALL=C", "SITATE_RECV=/host/evil", "SITATE_RECVPIPE=/host/evil2", "ANTHROPIC_API_KEY=leak"}
	a, err := c.args(testRunID, td, "t1.prev.idx", "/opt/adapters/files", env)
	if err != nil {
		t.Fatal(err)
	}
	j := strings.Join(a, " ")
	for _, want := range []string{"podman run --rm -i", "--network none", "--read-only", "--cap-drop all", "--security-opt no-new-privileges", "--user 65534:65534",
		"--memory " + strconv.FormatInt(contract.ContainerMemoryMax, 10), "--label sitate-run=" + testRunID, "--rootfs",
		"src=/opt/adapters/files,dst=/sitate/adapter,ro", "src=/opt/sitate/sitate,dst=/sitate/recv,ro",
		"src=/w/r-1/t1.prev.idx,dst=/run/sitate/work/t1.prev.idx,ro", "-e SITATE_RECV=/sitate/recv", "-e SITATE_RECVPIPE=/run/sitate/pipe", "-e PATH=/usr/bin"} {
		if !strings.Contains(j, want) {
			t.Errorf("引数列に %q が無い: %s", want, j)
		}
	}
	if strings.Contains(j, "/host/evil") || strings.Contains(j, "ANTHROPIC") {
		t.Errorf("実行器の環境の SITATE_RECV を上書きできる / 余計な変数: %s", j)
	}
	// ルートの位置引数が最後の 2 つ(ルート、実行ファイル)で、それ以降にオプションが無い。
	if n := len(a); a[n-2] != td.Root || a[n-1] != "/sitate/adapter" {
		t.Errorf("末尾: %v", a[len(a)-3:])
	}
	for _, bad := range []string{"--privileged", "--network host", "--volume", " -v ", "keys", "secrets", "sitate-pull"} {
		if strings.Contains(j, bad) {
			t.Errorf("渡してはいけないもの %q: %s", bad, j)
		}
	}
	if _, err := c.args(testRunID, td, "x", "", nil); err == nil {
		t.Error("実行ファイルが空でも組めた")
	}
}

func TestContainerPrepareRootIsMinimal(t *testing.T) {
	td := taskDir{Root: filepath.Join(t.TempDir(), "rootfs"), PrevIdx: "x"}
	if err := prepareRoot(td, "t1.prev.idx"); err != nil {
		t.Fatal(err)
	}
	var got []string
	_ = filepath.Walk(td.Root, func(p string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			got = append(got, strings.TrimPrefix(p, td.Root))
			if info.Size() != 0 {
				t.Errorf("マウント点の空のファイルだけ: %s", p)
			}
		}
		return nil
	})
	if len(got) != 5 {
		t.Errorf("ルートのファイル: %v", got)
	}
}

// relayEnv は偽の受け口(ポート 22)に対する中継を、FIFO の上で起こす。
func relayEnv(t *testing.T, reply func(execRec) (string, int)) (*recvRelay, string, *fakeSource) {
	t.Helper()
	src := startSource22(t, reply)
	dir := filepath.Join(t.TempDir(), "pipe")
	conn := contract.SSHConn{Target: contract.SSHTarget{Host: "127.0.0.1", Port: 22, User: "sitate-ro"}, Cred: contract.NewSecret([]byte(newAdminKey(t))), PinnedFP: src.FP}
	r, err := startRelay(context.Background(), dir, "t1", routeSSH{}, conn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.stop)
	return r, dir, src
}

func TestRecvRelayForwardsAndFramesOutput(t *testing.T) {
	big := strings.Repeat("x", contract.StreamFragmentMax+10)
	_, dir, src := relayEnv(t, func(r execRec) (string, int) {
		if strings.HasPrefix(r.Cmd, "read-batch") {
			return big, 0
		}
		return "", 5
	})
	var out bytes.Buffer
	if code := RunRecvHelper([]string{"read-batch", "t1"}, strings.NewReader("a\x00b\x00"), &out, dir); code != 0 || out.String() != big {
		t.Fatalf("read-batch: code %d, out %d バイト", code, out.Len())
	}
	ex := src.Execs()
	if len(ex) != 1 || ex[0].User != "sitate-ro" || ex[0].Cmd != "read-batch t1" || ex[0].Stdin != "a\x00b\x00" {
		t.Errorf("受け口への呼び出し: %+v", ex)
	}
	if code := RunRecvHelper([]string{"list", "t1"}, strings.NewReader(""), io.Discard, dir); code != 5 {
		t.Errorf("受け口の終了状態をそのまま返す: %d", code)
	}
}

// selftest と別の対象 id は、受け口へ送らずに 126。補助コマンド自身も形の合わない呼び出しを 126 にする。形式を破った要求も流さない。
func TestRecvRelayDeniesWithoutForwarding(t *testing.T) {
	_, dir, src := relayEnv(t, nil)
	for _, args := range [][]string{{"selftest"}, {"survey"}, {"selftest", "t1"}, {"list"}, {"list", "t1", "x"}, {"list", "t9"}, {"rm", "t1"}} {
		if code := RunRecvHelper(args, strings.NewReader(""), io.Discard, dir); code != contract.RecvExitDenied && !(len(args) == 2 && args[0] == "list" && args[1] == "t9") {
			t.Errorf("%v は 126: %d", args, code)
		}
	}
	// 補助コマンドを通さず、中継へ直接送る(別の対象 id・selftest・形式違反)。
	for _, raw := range []string{`{"format":"sitate.recvpipe/v1","op":"list","target":"t9"}`, `{"format":"sitate.recvpipe/v1","op":"selftest","target":"t1"}`,
		`{"format":"x","op":"list","target":"t1"}`} {
		req, _ := os.OpenFile(filepath.Join(dir, pipeReqName), os.O_WRONLY, 0)
		rep, _ := os.OpenFile(filepath.Join(dir, pipeRepName), os.O_RDONLY, 0)
		_, _ = io.WriteString(req, raw+"\n{\"in\":0}\n")
		req.Close()
		var out bytes.Buffer
		if code := readHelperReply(newReader(rep), &out); code != contract.RecvExitDenied {
			t.Errorf("%s は 126: %d", raw, code)
		}
		rep.Close()
	}
	if len(src.Execs()) != 0 {
		t.Errorf("拒否した操作を受け口へ流した: %+v", src.Execs())
	}
}

func TestRecvRelayUnreachableIs255(t *testing.T) {
	r, dir, src := relayEnv(t, nil)
	_ = r
	src.ln.Close()
	if code := RunRecvHelper([]string{"list", "t1"}, strings.NewReader(""), io.Discard, dir); code != contract.RecvExitUnreachable {
		t.Errorf("受け口に届かないときは 255: %d", code)
	}
}

func TestRecvRelayHostKeyMismatchIsFatal(t *testing.T) {
	r, dir, _ := relayEnv(t, nil)
	r.conn.PinnedFP = fpA
	if code := RunRecvHelper([]string{"list", "t1"}, strings.NewReader(""), io.Discard, dir); code != contract.RecvExitUnreachable {
		t.Errorf("ホスト鍵が違えば 255: %d", code)
	}
	var d map[string]any
	if ce, ok := r.fatalErr().(*contract.Error); !ok || ce.Code != contract.CodeSourceHostkeyMismatch || json.Unmarshal(ce.Detail, &d) != nil || d["expected_fp"] != fpA {
		t.Errorf("実行の失敗 SOURCE_HOSTKEY_MISMATCH を残す: %v", r.fatalErr())
	}
}

func newReader(r io.Reader) *bufio.Reader { return bufio.NewReader(r) }

func TestRunCleanupPsArgsAreOneQuery(t *testing.T) {
	a := (&RunCleanup{}).psArgs([]string{"label=" + RunLabel, "status=exited", "status=created"})
	if strings.Join(a, " ") != "ps -a --no-trunc --filter label=sitate-run --filter status=exited --filter status=created --format {{.ID}}" {
		t.Errorf("1 回の ps にフィルタを重ねる: %v", a)
	}
}
