package cli

// まとめレビュー F7: 握手の時限切れは Reason: timeout(STORAGE_UNREACHABLE の reason: timeout。plans.go の分岐が働く)。届かない・拒まれるのは connect のまま。

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestSSHHandshakeTimeoutReason(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { // 受け付けるが何も話さない
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	_, err = SSHClient{ConnectTimeout: 300 * time.Millisecond}.Run(bg, contract.SSHRequest{SSHConn: contract.SSHConn{Target: contract.SSHTarget{Host: "127.0.0.1", Port: port, User: "u"}, PinnedFP: "SHA256:x"}})
	var se *contract.SSHError
	if !errors.As(err, &se) || se.Reason != contract.SSHReasonTimeout {
		t.Errorf("握手の時限切れ: %v", err)
	}
	// 閉じたポートは connect。
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln2.Addr().(*net.TCPAddr).Port
	ln2.Close()
	_, err = SSHClient{ConnectTimeout: time.Second}.Run(bg, contract.SSHRequest{SSHConn: contract.SSHConn{Target: contract.SSHTarget{Host: "127.0.0.1", Port: closed, User: "u"}, PinnedFP: "SHA256:x"}})
	if !errors.As(err, &se) || se.Reason != contract.SSHReasonConnect {
		t.Errorf("拒まれた接続: %v", err)
	}
}
