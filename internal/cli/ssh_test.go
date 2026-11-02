package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 試験用の最小の SSH サーバー: 公開鍵認証を何でも通し、exec を数えて「ran:<コマンド>」と終了状態 3 を返す。
func sshServer(t *testing.T) (host string, port int, fp string, execs *atomic.Int32) {
	t.Helper()
	execs = &atomic.Int32{}
	_, hk, _ := ed25519.GenerateKey(rand.Reader)
	hsigner, _ := ssh.NewSignerFromKey(hk)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return &ssh.Permissions{}, nil }}
	cfg.AddHostKey(hsigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				defer sc.Close()
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, creqs, _ := nc.Accept()
					for r := range creqs {
						if r.Type != "exec" {
							_ = r.Reply(false, nil)
							continue
						}
						execs.Add(1)
						_ = r.Reply(true, nil)
						_, _ = ch.Write(append([]byte("ran:"), r.Payload[4:]...))
						_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 3})
						_ = ch.Close()
					}
				}
			}()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port, ssh.FingerprintSHA256(hsigner.PublicKey()), execs
}

func testKey(t *testing.T) *contract.Secret {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	blk, err := ssh.MarshalPrivateKey(k, "")
	if err != nil {
		t.Fatal(err)
	}
	return contract.NewSecret(pem.EncodeToMemory(blk))
}

// ホスト鍵が PinnedFP と違えば何も実行せず SSHHostKeyError。合えば実行して終了状態をそのまま返す。空なら照らさず SeenFP を返す。
func TestSSHClientPinnedFingerprint(t *testing.T) {
	host, port, fp, execs := sshServer(t)
	conn := contract.SSHConn{Target: contract.SSHTarget{Host: host, Port: port, User: "u"}, CredRef: "k", Cred: testKey(t)}
	c := SSHClient{}

	bad := conn
	bad.PinnedFP = "SHA256:AAAA"
	_, err := c.Run(bg, contract.SSHRequest{SSHConn: bad, Argv: []string{"echo", "x"}})
	var he *contract.SSHHostKeyError
	if !errors.As(err, &he) || he.Seen != fp || he.Pinned != "SHA256:AAAA" || execs.Load() != 0 {
		t.Fatalf("不一致: %v execs=%d", err, execs.Load())
	}
	var out bytes.Buffer
	good := conn
	good.PinnedFP = fp
	res, err := c.Run(bg, contract.SSHRequest{SSHConn: good, Argv: []string{"echo", "x"}, Stdout: &out})
	if err != nil || res.ExitStatus != 3 || res.SeenFP != fp || out.String() != "ran:echo x" || execs.Load() != 1 {
		t.Fatalf("一致: %v %+v %q", err, res, out.String())
	}
	res, err = c.Run(bg, contract.SSHRequest{SSHConn: conn, Script: []byte("true"), Sudo: true, Stdout: &out})
	if err != nil || res.SeenFP != fp || !bytes.Contains(out.Bytes(), []byte("sudo -n sh -s")) {
		t.Fatalf("Script/Sudo: %v %+v %q", err, res, out.String())
	}
	// 鍵として読めない資格情報と、届かない接続先。
	broken := conn
	broken.Cred = contract.NewSecret([]byte("not a key"))
	var se *contract.SSHError
	if _, err = c.Run(bg, contract.SSHRequest{SSHConn: broken, Argv: []string{"x"}}); !errors.As(err, &se) || se.Reason != contract.SSHReasonKeyUnparsable {
		t.Errorf("key_unparsable: %v", err)
	}
	dead := conn
	dead.Target.Port = 1
	if _, err = c.Run(bg, contract.SSHRequest{SSHConn: dead, Argv: []string{"x"}}); !errors.As(err, &se) || se.Reason != contract.SSHReasonConnect {
		t.Errorf("connect: %v", err)
	}
}

func TestQuoteArgv(t *testing.T) {
	got := quoteArgv([]string{"sitate", "restore", "--to", "/dest/a b", "", "it's", "a;b", "k=v,w:x@y%z+1"})
	want := `sitate restore --to '/dest/a b' '' 'it'\''s' 'a;b' k=v,w:x@y%z+1`
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}
