package cli

// 継ぎ目 SSH の実物(spec 7.5。D-0157)。接続先のホスト鍵の指紋が PinnedFP と違えば、認証の前に接続を切って何も実行しない(標準入力も読まない)。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// SSHClient は contract.SSH の実物。ConnectTimeout が 0 なら 30 秒。
type SSHClient struct{ ConnectTimeout time.Duration }

var _ contract.SSH = SSHClient{}

var errHostKeyPinned = errors.New("host key mismatch")

// Run は contract.SSH を満たす。1 接続・1 コマンド。Script は `sh -s`(Sudo が真なら `sudo -n sh -s`)の標準入力へ流す。
func (c SSHClient) Run(ctx context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
	var res contract.SSHResult
	// 資格情報なしで固定値を指定した呼び出しは、ホスト鍵の指紋の取得だけ(認証の前に切る。D-0166)。
	var auth []ssh.AuthMethod
	switch {
	case req.Cred != nil:
		signer, err := ssh.ParsePrivateKey(req.Cred.Reveal())
		if err != nil {
			return res, &contract.SSHError{Reason: contract.SSHReasonKeyUnparsable}
		}
		auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	case req.PinnedFP == "":
		return res, &contract.SSHError{Reason: contract.SSHReasonKeyUnparsable}
	}
	var seen string
	cfg := &ssh.ClientConfig{
		User: req.Target.User,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = ssh.FingerprintSHA256(key)
			if req.PinnedFP != "" && seen != req.PinnedFP {
				return errHostKeyPinned
			}
			return nil
		},
	}
	port := req.Target.Port
	if port == 0 {
		port = 22
	}
	timeout := c.ConnectTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dctx, "tcp", net.JoinHostPort(req.Target.Host, strconv.Itoa(port)))
	if err != nil {
		return res, &contract.SSHError{Reason: connectReason(err)}
	}
	_ = conn.SetDeadline(time.Now().Add(timeout)) // 握手の間だけ
	cc, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(req.Target.Host, strconv.Itoa(port)), cfg)
	res.SeenFP = seen
	if err != nil {
		_ = conn.Close()
		switch {
		case errors.Is(err, errHostKeyPinned) || strings.Contains(err.Error(), errHostKeyPinned.Error()):
			return res, &contract.SSHHostKeyError{Pinned: req.PinnedFP, Seen: seen}
		case strings.Contains(err.Error(), "unable to authenticate"):
			return res, &contract.SSHError{Reason: contract.SSHReasonAuth}
		}
		return res, &contract.SSHError{Reason: connectReason(err)}
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()
	return c.exec(ctx, client, req, res)
}

// connectReason は接続・握手の失敗の理由: 時限内に終わらなければ timeout、それ以外(届かない・拒まれた・名前解決)は connect。
func connectReason(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) || strings.Contains(err.Error(), "i/o timeout") {
		return contract.SSHReasonTimeout
	}
	return contract.SSHReasonConnect
}

func (SSHClient) exec(ctx context.Context, client *ssh.Client, req contract.SSHRequest, res contract.SSHResult) (contract.SSHResult, error) {
	sess, err := client.NewSession()
	if err != nil {
		return res, &contract.SSHError{Reason: contract.SSHReasonConnect}
	}
	defer sess.Close()
	cmd, stdin := quoteArgv(req.Argv), req.Stdin
	if len(req.Script) > 0 {
		cmd, stdin = "sh -s", bytes.NewReader(req.Script)
		if req.Sudo {
			cmd = "sudo -n sh -s"
		}
	}
	if req.Stdout != nil {
		sess.Stdout = req.Stdout
	} else {
		sess.Stdout = io.Discard
	}
	sess.Stderr = io.Discard
	if stdin != nil {
		sess.Stdin = stdin
	}
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = sess.Close()
		_ = client.Close()
		return res, ctx.Err()
	case err = <-done:
	}
	var ee *ssh.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.ExitStatus = ee.ExitStatus()
	default:
		return res, &contract.SSHError{Reason: contract.SSHReasonConnect}
	}
	return res, nil
}

// quoteArgv は Argv を空白で結んだ 1 つのコマンド文字列にする(contract.SSHRequest の規則)。
// 安全な文字([A-Za-z0-9_./:=@%+,-])だけの語はそのまま、それ以外と空の語は単一引用符で引用する(語の中の ' は '\” にする)。
func quoteArgv(argv []string) string {
	out := make([]string, len(argv))
	for i, w := range argv {
		out[i] = quoteWord(w)
	}
	return strings.Join(out, " ")
}

func quoteWord(w string) string {
	safe := w != ""
	for _, r := range w {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:=@%+,-", r)) {
			safe = false
			break
		}
	}
	if safe {
		return w
	}
	return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
}
