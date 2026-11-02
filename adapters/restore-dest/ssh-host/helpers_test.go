package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"golang.org/x/crypto/ssh"
)

// call はアダプタを(プロセスの中で)1 回呼び、終了コードと結果の JSON を返す。標準出力は 1 行の JSON でなければならない。
func call(t *testing.T, req map[string]any) (int, map[string]any) {
	t.Helper()
	req["contract"] = contractVersion
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate("adapter/restore-dest.json#/$defs/request", b); err != nil {
		t.Fatalf("試験の要求がスキーマに合わない: %v", err)
	}
	var out bytes.Buffer
	code := run(bytes.NewReader(append(b, '\n')), &out)
	s := out.String()
	if !strings.HasSuffix(s, "\n") || strings.Count(strings.TrimRight(s, "\n"), "\n") != 0 {
		t.Fatalf("標準出力が改行で終わる 1 行でない: %q", s)
	}
	if err := contract.Validate("adapter/restore-dest.json#/$defs/response", []byte(s)); err != nil {
		t.Fatalf("結果が応答のスキーマに合わない: %v\n%s", err, s)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(s), &res); err != nil {
		t.Fatalf("標準出力が JSON でない: %q", s)
	}
	return code, res
}

func errOf(res map[string]any) map[string]any {
	e, _ := res["error"].(map[string]any)
	return e
}

// wantErr は終了 1 の error.code を確かめる。
func wantErr(t *testing.T, code int, res map[string]any, want string) map[string]any {
	t.Helper()
	e := errOf(res)
	if code != 1 || e == nil || e["code"] != want {
		t.Fatalf("error.code %s を期待: exit=%d %v", want, code, res)
	}
	return e
}

func wantOK(t *testing.T, code int, res map[string]any) {
	t.Helper()
	if code != 0 || res["error"] != nil {
		t.Fatalf("成功を期待: exit=%d %v", code, res)
	}
}

// newKeyPair は使い捨ての Ed25519 鍵(OpenSSH 形式の秘密鍵と公開鍵の 1 行)を作る。
func newKeyPair(t *testing.T) (priv, pub string) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := ssh.MarshalPrivateKey(sk, "")
	if err != nil {
		t.Fatal(err)
	}
	spk, err := ssh.NewPublicKey(pk)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(blk)), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(spk)))
}

func localAddrTo(t *testing.T, host string, port int) string {
	t.Helper()
	c, err := net.Dial("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runRaw はスキーマの検査をせずに要求を渡す(スキーマが許さない要求にもアダプタが何もしないことを確かめる)。
func runRaw(t *testing.T, req map[string]any) (int, map[string]any) {
	t.Helper()
	req["contract"] = contractVersion
	b, _ := json.Marshal(req)
	var out bytes.Buffer
	code := run(bytes.NewReader(append(b, '\n')), &out)
	var res map[string]any
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("標準出力が JSON でない: %q", out.String())
	}
	return code, res
}

func mustPub(t *testing.T, line string) ssh.PublicKey {
	t.Helper()
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func dialFake(t *testing.T, f *fakeSSH, priv string) *conn {
	t.Helper()
	auth, _ := authFromSecret(priv, false)
	c, ae := dial(t.Context(), target{user: restoreUser, host: f.host, port: f.port}, restoreUser, auth, f.fp)
	if ae != nil {
		t.Fatalf("接続できない: %s", ae.code)
	}
	t.Cleanup(c.close)
	return c
}

func parseIP(s string) net.IP { return net.ParseIP(s) }
