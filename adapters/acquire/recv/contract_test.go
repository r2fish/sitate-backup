package recv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 受け口の契約試験(操作ごとの出力形式。作業指示書 3.2、spec 3.3「受け口が許す操作」「受け口の動作」)。
// 受け口の sh を一時ディレクトリへ写し、強制コマンドの環境変数 SSH_ORIGINAL_COMMAND だけを渡して sh で動かす(許可リストを埋め込む前の雛形。
// 埋め込みの仕組み(list・read-batch・dump 系の出力)は U2a が決めるため、ここでは許可リストが空の雛形で決まる出力だけを縛る)。
// 骨格は NOT_IMPLEMENTED を返し、空の実装(何も出さない・常に 0)は下の値の比較で落ちる。

func runRecv(t *testing.T, name, cmd string) (code int, stdout, stderr string, script []byte) {
	t.Helper()
	script, err := Files.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, script, 0o755); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("sh", p)
	c.Dir = dir
	c.Env = []string{"PATH=/usr/bin:/bin", "SSH_ORIGINAL_COMMAND=" + cmd}
	var o, e bytes.Buffer
	c.Stdout, c.Stderr = &o, &e
	err = c.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode()
	default:
		t.Fatal(err)
	}
	if strings.Contains(e.String(), "NOT_IMPLEMENTED") {
		t.Fatalf("%s %s", harness.NotImplementedMarker, name)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("受け口が作業ディレクトリに書いた(元サーバーのどこにも書かない): %d 件", len(ents))
	}
	return code, o.String(), e.String(), script
}

// 許す操作以外・語の数の違い・許可リストに無い対象 id は拒否して終了 126、標準出力には何も出さない(spec 3.3「受け口が許す操作」「引数」)。
func TestContractRecvRefuses(t *testing.T) {
	for _, cmd := range []string{
		"", "rm -rf /", "bash", "list", "list t1 extra", "list ../x", "list t9", "read-batch", "read-batch t9",
		"dump-pg t9", "dump-pg-globals t9", "dump-my t9", "selftest extra", "survey extra", "SELFTEST",
		"selftest; id", "list t1;id", "list $(id)", "list `id`",
	} {
		code, out, _, _ := runRecv(t, "sitate-recv", cmd)
		if code != 126 || out != "" {
			t.Errorf("%q: 終了 %d・標準出力 %q(126 で何も出さない)", cmd, code, out)
		}
	}
}

// selftest: 終了 0・標準出力は sitate.recv-selftest/v1 の JSON 1 行で、receiver_hash は受け口のファイルのバイト列の SHA-256(spec 3.3 の receiver_hash)。
func TestContractRecvSelftest(t *testing.T) {
	code, out, _, script := runRecv(t, "sitate-recv", "selftest")
	if code != 0 {
		t.Fatalf("selftest の終了 %d", code)
	}
	if !strings.HasSuffix(out, "\n") || strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Fatalf("標準出力が改行で終わる JSON 1 行でない: %q", out)
	}
	if err := contract.Validate("receiver/selftest.json", []byte(out)); err != nil {
		t.Fatalf("selftest の出力が receiver/selftest.json に合わない: %v\n%s", err, out)
	}
	var st struct {
		Hash    string            `json:"receiver_hash"`
		Targets []json.RawMessage `json:"targets"`
	}
	_ = json.Unmarshal([]byte(out), &st)
	sum := sha256.Sum256(script)
	if st.Hash != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Errorf("receiver_hash が自分のファイルの SHA-256 でない: %s", st.Hash)
	}
	if len(st.Targets) != 0 {
		t.Errorf("許可リストが空の雛形の targets は空: %s", out)
	}
}
