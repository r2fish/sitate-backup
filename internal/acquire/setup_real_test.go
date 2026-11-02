package acquire

// 核が生成した台本(setupScript)を U2a の実物の source-setup・sitate-recv で流して噛み合うかの試験。
// 実物の置き場は兄弟の worktree(SITATE_U2A_RECV で上書きできる)。無ければ skip。実際に流すのは root で
// SITATE_DESTRUCTIVE_SETUP=1 のときだけ(sitate-ro のアカウントなどを作るため、使い捨ての容器の中で)。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func u2aRead(t *testing.T) func(string) ([]byte, error) {
	t.Helper()
	dir := os.Getenv("SITATE_U2A_RECV")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "U2a", "adapters", "acquire", "recv")
	}
	if _, err := os.Stat(filepath.Join(dir, "source-setup")); err != nil {
		t.Skip("U2a の実物が無い: " + dir)
	}
	return func(n string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, n)) }
}

const realDefFiles = `{"source":{"host":"127.0.0.1","targets":[{"id":"t1","files":"/srv/data"}]}}`

func TestSetupScriptWithRealSourceSetup(t *testing.T) {
	read := u2aRead(t)
	def, _ := parseCheckDef([]byte(realDefFiles))
	pub := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMM"
	script, hash, err := setupScriptFrom(read, def, nil, pub, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	sh := exec.Command("sh", "-n")
	sh.Stdin = bytes.NewReader(script)
	if out, err := sh.CombinedOutput(); err != nil {
		t.Fatalf("台本が sh -n を通らない: %v %s", err, out)
	}
	// 埋め込んだ受け口は、BEGIN-EMBEDDED〜END-EMBEDDED を置き換えた形で、そのバイト列の SHA-256 が receiver_hash。
	rcv := longestQuoted(string(script))
	sum := sha256.Sum256([]byte(rcv))
	if hash != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Errorf("receiver_hash は SITATE_RECEIVER のバイト列の SHA-256")
	}
	for _, want := range []string{"SITATE_AUTH_LINE='restrict,from=\"203.0.113.9\",command=\"/usr/local/libexec/sitate-recv\" " + pub + "'",
		"SITATE_TARGETS='t1\tfiles\t/srv/data\t\"/srv/data\"'", "SITATE_VERSION='" + Version + "'"} {
		if !strings.Contains(rcv, want) {
			t.Errorf("受け口に %q が無い", want)
		}
	}
	for _, want := range []string{"SITATE_PULL_KEY='" + pub + "'", "SITATE_FROM='203.0.113.9'", "SITATE_TARGETS='t1\tfiles"} {
		if !strings.Contains(string(script), want) {
			t.Errorf("台本に %q が無い", want)
		}
	}
	if os.Geteuid() != 0 || os.Getenv("SITATE_DESTRUCTIVE_SETUP") != "1" {
		t.Skip("実行は root かつ SITATE_DESTRUCTIVE_SETUP=1 のときだけ(使い捨ての容器)。ここまでは検査した。実行は未検証")
	}
	if err := os.MkdirAll("/srv/data", 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // 2 回目は全段 unchanged
		c := exec.Command("sh", "-s")
		c.Stdin = bytes.NewReader(script)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("source-setup が失敗: %v\n%s", err, out)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var last map[string]any
		if json.Unmarshal([]byte(lines[len(lines)-1]), &last) != nil || last["step"] != "done" {
			t.Fatalf("最後の行は done: %s", out)
		}
		if i == 1 && last["result"] != "unchanged" {
			t.Errorf("再実行は全段 unchanged: %s", out)
		}
	}
	got, err := os.ReadFile("/usr/local/libexec/sitate-recv")
	if err != nil || string(got) != rcv {
		t.Errorf("置かれた受け口が核の埋め込んだ中身と違う(receiver_hash が合わない)")
	}
}
