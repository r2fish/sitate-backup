package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"golang.org/x/crypto/ssh"
)

// 時限・再試行(spec 3.2.1。作業指示書の固定値の表の ADAPTER_CONNECT_TIMEOUT など)。変数なのは試験で短くするため。
var (
	connectTimeout = contract.AdapterConnectTimeout
	retryMax       = contract.AdapterRetryMax
	backoffBase    = contract.AdapterBackoffBase
	outputMax      = contract.AdapterResultMax
)

// conn は 1 本の SSH 接続。ホスト鍵は Ed25519 に限り、seenFP に見えた指紋を持つ。
type conn struct {
	cl     *ssh.Client
	seenFP string
	sudo   bool // true なら run は `sudo -n` で root として実行する(enroll の管理用アカウント)
}

func (c *conn) close() {
	if c != nil && c.cl != nil {
		_ = c.cl.Close()
	}
}

// authFromSecret は秘密(OpenSSH 形式の秘密鍵)から認証方式を作る。値はどこにも書かない。
// allowPassword のとき(enroll の初期資格情報だけ)、秘密鍵の形でない値をパスワードとして使う。
func authFromSecret(secret string, allowPassword bool) (ssh.AuthMethod, bool) {
	if secret == "" {
		return nil, false
	}
	if strings.Contains(secret, "PRIVATE KEY") {
		signer, err := ssh.ParsePrivateKey([]byte(secret))
		if err != nil {
			return nil, false
		}
		return ssh.PublicKeys(signer), true
	}
	if !allowPassword {
		return nil, false
	}
	return ssh.Password(secret), true
}

// dial は接続する。pinFP が空でなければ、ホスト鍵がその指紋と違うとき hostkey_mismatch(見えた指紋を seen_fp)で失敗し、鍵を渡さない。
// 接続の失敗は ADAPTER_RETRY_MAX 回まで指数の間隔(0〜計算値の揺らぎ)で再試行する(spec 3.2.1)。認証の失敗とホスト鍵の不一致は再試行しない。
func dial(ctx context.Context, t target, user string, auth ssh.AuthMethod, pinFP string) (*conn, *adapterErr) {
	var last *adapterErr
	for attempt := 0; attempt <= retryMax; attempt++ {
		if attempt > 0 {
			wait := time.Duration(rand.Int64N(int64(backoffBase<<(attempt-1)) + 1))
			select {
			case <-ctx.Done():
				return nil, unreachableErr("timeout")
			case <-time.After(wait):
			}
		}
		c, e := dialOnce(ctx, t, user, auth, pinFP)
		if e == nil {
			return c, nil
		}
		if e.code != "unreachable" {
			return nil, e
		}
		last = e
	}
	return nil, last
}

func dialOnce(ctx context.Context, t target, user string, auth ssh.AuthMethod, pinFP string) (*conn, *adapterErr) {
	c := &conn{}
	mismatch := false
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{auth},
		// Ed25519 を先頭に、他の鍵種も受けて見える。指紋を取ってから、Ed25519 でなければコールバックで拒む(認証の前に切る)。
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
		Timeout:           connectTimeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			c.seenFP = ssh.FingerprintSHA256(key)
			if key.Type() != ssh.KeyAlgoED25519 {
				mismatch = true
				return errors.New("host key type")
			}
			if pinFP != "" && c.seenFP != pinFP {
				mismatch = true
				return errors.New("host key mismatch")
			}
			return nil
		},
	}
	d := net.Dialer{Timeout: connectTimeout}
	nc, err := d.DialContext(ctx, "tcp", t.addr())
	if err != nil {
		return nil, unreachableErr(netReason(err))
	}
	_ = nc.SetDeadline(time.Now().Add(connectTimeout))
	sc, chans, reqs, err := ssh.NewClientConn(nc, t.addr(), cfg)
	if err != nil {
		_ = nc.Close()
		return nil, handshakeErr(err, mismatch, c.seenFP)
	}
	_ = nc.SetDeadline(time.Time{})
	c.cl = ssh.NewClient(sc, chans, reqs)
	return c, nil
}

func netReason(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "connect"
}

// handshakeErr は SSH の握手の失敗を error.code に分ける。
func handshakeErr(err error, mismatch bool, seen string) *adapterErr {
	switch {
	case mismatch:
		return hostkeyErr(seen)
	case strings.Contains(err.Error(), "unable to authenticate"), strings.Contains(err.Error(), "no supported methods"):
		return codeErr("auth")
	}
	return unreachableErr(netReason(err))
}

// runResult はリモートのコマンドの結果。
type runResult struct {
	out  string
	code int
}

// run はリモートで `bash -c <script> sitate <args...>` を実行する。stdin は標準入力(秘密はここだけで渡す)。
// 標準エラーは診断にも使わず捨てる(秘密を出さないため)。
func (c *conn) run(script, stdin string, args ...string) (runResult, error) {
	s, err := c.cl.NewSession()
	if err != nil {
		return runResult{}, err
	}
	defer s.Close()
	var out limitedBuf
	out.max = outputMax
	s.Stdout = &out
	s.Stderr = io.Discard
	s.Stdin = strings.NewReader(stdin)
	cmd := "bash -c " + shQuote(script) + " sitate"
	if c.sudo {
		cmd = "sudo -n " + cmd
	}
	for _, a := range args {
		cmd += " " + shQuote(a)
	}
	err = s.Run(cmd)
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		return runResult{out: out.String(), code: ee.ExitStatus()}, nil
	}
	if err != nil {
		return runResult{}, err
	}
	return runResult{out: out.String()}, nil
}

type limitedBuf struct {
	bytes.Buffer
	max int
}

func (b *limitedBuf) Write(p []byte) (int, error) {
	n := len(p)
	if room := b.max - b.Len(); n > room {
		p = p[:max(0, room)]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

// shQuote は POSIX シェルの単一引用符で包む。
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
