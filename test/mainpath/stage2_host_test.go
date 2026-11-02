//go:build mainpath

package mainpath

// 第 2 段のホストへの SSH(実物)。受入試験の環境の source・backup・dest に管理用の鍵で入る。

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// host は受入試験の環境のホスト 1 つ。ホスト鍵の指紋は起動時に記録した値と照らす。
type host struct {
	role, addr, fp string
	t              *testing.T
	holdStdin      bool // run が標準入力を終わりまで閉じない
}

type execResult struct {
	Stdout, Stderr string
	Code           int
}

func accEnv(t *testing.T, k string) string {
	t.Helper()
	v := os.Getenv(k)
	if v == "" {
		t.Fatalf("受入試験の環境の変数 %s が無い(accept-env.sh up の後に ACCEPT=<NAME> を付けて実行)", k)
	}
	return v
}

func newHost(t *testing.T, role string) *host {
	t.Helper()
	up := strings.ToUpper(role)
	return &host{role: role, t: t, fp: accEnv(t, "SITATE_ACCEPT_"+up+"_HOST_KEY_FP"),
		addr: net.JoinHostPort(accEnv(t, "SITATE_ACCEPT_"+up+"_HOST"), accEnv(t, "SITATE_ACCEPT_"+up+"_PORT"))}
}

func adminSigner(t *testing.T) ssh.Signer {
	t.Helper()
	b, err := os.ReadFile(accEnv(t, "SITATE_ACCEPT_ADMIN_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.ParsePrivateKey(b)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// run は user として cmd を 1 つ実行する(強制コマンドの鍵ではその鍵のコマンドが走り、cmd は SSH_ORIGINAL_COMMAND になる)。
func (h *host) run(user string, key ssh.Signer, cmd string, stdin []byte) (execResult, error) {
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(key)}, Timeout: 30 * time.Second,
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519}, // 起動時に記録した指紋は ed25519 のホスト鍵のもの
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(k); got != h.fp {
				return fmt.Errorf("ホスト鍵の指紋が記録と違う: %s", got)
			}
			return nil
		}}
	c, err := ssh.Dial("tcp", h.addr, cfg)
	if err != nil {
		return execResult{}, err
	}
	defer c.Close()
	s, err := c.NewSession()
	if err != nil {
		return execResult{}, err
	}
	defer s.Close()
	var so, se bytes.Buffer
	s.Stdout, s.Stderr = &so, &se
	if h.holdStdin && stdin != nil {
		// 標準入力の 1 行目の後も開いたままにする(核は標準入力の終わりを実行の取り消しとして扱う。呼び出し側の SSH は実行中ずっと開いている)。
		pr, pw := io.Pipe()
		s.Stdin = pr
		go func() { _, _ = pw.Write(stdin) }()
		defer pw.Close()
	} else if stdin != nil {
		s.Stdin = bytes.NewReader(stdin)
	}
	err = s.Run(cmd)
	r := execResult{Stdout: so.String(), Stderr: se.String()}
	var ee *ssh.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.Code = ee.ExitStatus()
		err = nil
	}
	return r, err
}

// root は管理用アカウントで入って sudo -n sh -s に script を流す。失敗(終了コード≠0)は試験を止める。
func (h *host) root(script string) string {
	h.t.Helper()
	r, err := h.run(adminUser(h.t), adminSigner(h.t), "sudo -n sh -s", []byte("set -eu\n"+script+"\n"))
	if err != nil {
		h.t.Fatalf("%s: 管理アクセスで実行できない: %v", h.role, err)
	}
	if r.Code != 0 {
		h.t.Fatalf("%s: 終了 %d\n%s\n%s", h.role, r.Code, r.Stdout, r.Stderr)
	}
	return r.Stdout
}

// rootTry は root と同じだが失敗しても止めず結果を返す。
func (h *host) rootTry(script string) execResult {
	h.t.Helper()
	r, err := h.run(adminUser(h.t), adminSigner(h.t), "sudo -n sh -s", []byte(script+"\n"))
	if err != nil {
		h.t.Fatalf("%s: 管理アクセスで実行できない: %v", h.role, err)
	}
	return r
}

func adminUser(t *testing.T) string { return accEnv(t, "SITATE_ACCEPT_ADMIN_USER") }

// put は root 所有でなく owner のファイルとして data を path に置く。
func (h *host) put(path string, data []byte, mode, owner string) {
	h.t.Helper()
	r, err := h.run(adminUser(h.t), adminSigner(h.t),
		fmt.Sprintf("sudo -n sh -c 'install -D -m %s -o %s -g %s /dev/stdin %s'", mode, owner, owner, shq(path)), data)
	if err != nil || r.Code != 0 {
		h.t.Fatalf("%s: %s を置けない: %v %s", h.role, path, err, r.Stderr)
	}
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
