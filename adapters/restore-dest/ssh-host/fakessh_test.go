package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeSSH は試験用の SSH サーバー(プロセスの中。実物の sshd ではない)。exec の要求を、HOME を差し替えたローカルの bash で実行する。
// ホスト鍵の固定、認証、スクリプトの安全(session_id の形、他の session の資源に触れないこと)を、環境なしで確かめるために使う。
type fakeSSH struct {
	host     string
	port     int
	fp       string
	home     string
	bin      string // 試験用の podman・gocryptfs・mkdir を置いた PATH の先頭(stubs_test.go)
	userKey  ssh.PublicKey
	auths    atomic.Int32 // 認証に進んだ回数(ホスト鍵の不一致では 0 のまま = 鍵を渡さない)
	sessions atomic.Int32
	sshConn  string // 空でなければ exec の環境に SSH_CONNECTION として置く(実物の sshd と同じ。sudo の下では消える偽物は stubs_test.go)
}

func startFakeSSH(t *testing.T, rsaHostKey bool) *fakeSSH {
	t.Helper()
	var signer ssh.Signer
	if rsaHostKey {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		signer, _ = ssh.NewSignerFromKey(k)
	} else {
		_, sk, _ := ed25519.GenerateKey(rand.Reader)
		signer, _ = ssh.NewSignerFromKey(sk)
	}
	f := &fakeSSH{host: "127.0.0.1", fp: ssh.FingerprintSHA256(signer.PublicKey()), home: t.TempDir()}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		f.auths.Add(1)
		if f.userKey != nil && string(k.Marshal()) == string(f.userKey.Marshal()) {
			return &ssh.Permissions{}, nil
		}
		return nil, io.ErrUnexpectedEOF
	}}
	cfg.AddHostKey(signer)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f.port = l.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(nc, cfg)
		}
	}()
	return f
}

func (f *fakeSSH) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			return
		}
		go f.session(ch, creqs)
	}
}

func (f *fakeSSH) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for r := range reqs {
		if r.Type != "exec" {
			_ = r.Reply(false, nil)
			continue
		}
		n := binary.BigEndian.Uint32(r.Payload)
		cmdline := string(r.Payload[4 : 4+n])
		_ = r.Reply(true, nil)
		f.sessions.Add(1)
		cmd := exec.Command("bash", "-c", cmdline)
		cmd.Env = []string{"HOME=" + f.home, "PATH=" + f.path(), "SITATE_TEST_PID=" + strconv.Itoa(os.Getpid())}
		if f.sshConn != "" {
			cmd.Env = append(cmd.Env, "SSH_CONNECTION="+f.sshConn)
		}
		cmd.Stdin, cmd.Stdout = ch, ch
		code := 0
		if err := cmd.Run(); err != nil {
			code = 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{uint32(code)}))
		return
	}
}

func (f *fakeSSH) dest() map[string]any {
	return map[string]any{"at": "ssh://" + restoreUser + "@" + net.JoinHostPort(f.host, strconv.Itoa(f.port)), "key_ref": "dest-key"}
}

func (f *fakeSSH) req(op, sid, priv, fp string, rs []map[string]any) map[string]any {
	var s any = sid
	if sid == "" && op == "inspect" {
		s = nil
	}
	if rs == nil {
		rs = []map[string]any{}
	}
	return map[string]any{"op": op, "session_id": s, "dest": f.dest(), "host_key_fp": fp, "resources": rs,
		"secrets": map[string]string{"restore-dest-ssh": priv}}
}

// mkRun は偽ホストの HOME の下に ~/.sitate/run/<name>/f を作る。
func (f *fakeSSH) mkRun(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(f.home, ".sitate", "run", n)
		if err := os.MkdirAll(filepath.Join(p, "scratch"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "f"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fakeSSH) listRun(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(filepath.Join(f.home, ".sitate", "run"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// withStubs は偽ホストの PATH の先頭に試験用の podman・gocryptfs・mkdir を置く(stubs_test.go)。
func (f *fakeSSH) withStubs(t *testing.T) *fakeSSH {
	f.bin = stubBin(t)
	return f
}

func (f *fakeSSH) path() string {
	if f.bin == "" {
		return os.Getenv("PATH")
	}
	return f.bin + ":" + os.Getenv("PATH")
}
