//go:build mainpath

package mainpath

// 第 1 段の SSH: 受け口をローカルに SSH_ORIGINAL_COMMAND を付けて起動する小さな実装。実物の SSH は第 2 段。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/adapters/acquire/recv"
	"github.com/r2fish/sitate-backup/internal/contract"
)

const fakeSeenFP = "SHA256:mainpathharnesshostkeyfingerprint0000000"

type localSSH struct {
	script string // 埋め込み済みの受け口のパス
	t      *testing.T
	calls  []string
}

// embedBlock は sitate-recv の BEGIN-EMBEDDED〜END-EMBEDDED の間を許可リストなどに置き換える(ハーネスの独自実装)。
func embedBlock(src, targets, authLine, authFile string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	i := strings.Index(src, "# BEGIN-EMBEDDED\n")
	j := strings.Index(src, "# END-EMBEDDED\n")
	blk := "# BEGIN-EMBEDDED\nSITATE_VERSION=" + q("0.0.0-mainpath") + "\nSITATE_AUTH_LINE=" + q(authLine) +
		"\nSITATE_TARGETS=" + q(targets) + "\nSITATE_AUTHKEYS_FILE=" + q(authFile) + "\n"
	return src[:i] + blk + src[j:]
}

// newLocalSSH は受け口(許可リスト: id → 根)を dir に書き、その receiver_hash(sha256:<16 進>。受け口の $0 の SHA-256)を返す。
func newLocalSSH(t *testing.T, dir string, roots map[string]string, authLine string) (*localSSH, string) {
	t.Helper()
	src, err := recv.Files.ReadFile("sitate-recv")
	if err != nil {
		t.Fatal(err)
	}
	var tg []string
	for id, root := range roots {
		tg = append(tg, id+"\tfiles\t"+root+"\t"+jsonStr(root))
	}
	authFile := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(authFile, []byte(authLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := embedBlock(string(src), strings.Join(tg, "\n"), authLine, authFile)
	p := filepath.Join(dir, "sitate-recv")
	if err := os.WriteFile(p, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(body))
	return &localSSH{script: p, t: t}, "sha256:" + hex.EncodeToString(h[:])
}

func jsonStr(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// quoteWord は contract.SSHRequest の注記どおり、安全な文字だけの語はそのまま、それ以外は単一引用符で引用する。
func quoteWord(w string) string {
	ok := w != ""
	for _, r := range w {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:=@%+,-", r)) {
			ok = false
		}
	}
	if ok {
		return w
	}
	return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
}

func (s *localSSH) Run(ctx context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
	if req.PinnedFP != "" && req.PinnedFP != fakeSeenFP {
		return contract.SSHResult{}, &contract.SSHHostKeyError{Pinned: req.PinnedFP, Seen: fakeSeenFP}
	}
	if len(req.Argv) == 0 {
		return contract.SSHResult{}, &contract.SSHError{Reason: "unsupported"}
	}
	var ws []string
	for _, a := range req.Argv {
		ws = append(ws, quoteWord(a))
	}
	cmdline := strings.Join(ws, " ")
	s.calls = append(s.calls, cmdline)
	c := exec.CommandContext(ctx, "sh", s.script)
	c.Env = []string{"SSH_ORIGINAL_COMMAND=" + cmdline, "PATH=/usr/bin:/bin"}
	c.Stdin = req.Stdin
	var errb bytes.Buffer
	c.Stderr = &errb
	if req.Stdout != nil {
		c.Stdout = req.Stdout
	}
	err := c.Run()
	res := contract.SSHResult{SeenFP: fakeSeenFP}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.ExitStatus = ee.ExitCode()
	case ctx.Err() != nil:
		return res, ctx.Err()
	default:
		return res, &contract.SSHError{Reason: "unreachable"}
	}
	return res, nil
}
