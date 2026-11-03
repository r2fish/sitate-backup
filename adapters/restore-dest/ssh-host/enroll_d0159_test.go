package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// D-0159(spec 3.2.4 の「ホスト鍵」「値の出所」)の試験。偽の宛先は初期資格情報を受けない(認証に進んだ回数 auths で見る)。

// sudoScrubStub は `sudo -n <cmd…>` で環境を消して実行する偽の sudo(実物の env_reset と同じく、SSH_CONNECTION は sudo の下で読めない)。
const sudoScrubStub = `#!/bin/bash
[ "$1" = "-n" ] && shift
exec env -i HOME="$HOME" PATH="$PATH" "$@"
`

func (f *fakeSSH) withSudoScrub(t *testing.T) *fakeSSH {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "sudo"), []byte(sudoScrubStub), 0o755); err != nil {
		t.Fatal(err)
	}
	f.bin = d + ":" + f.bin
	return f
}

// 要求の host_key_fp と違うホスト鍵の宛先では、初期資格情報を使わず(認証に進まず)何も置かずに hostkey_mismatch(見えた指紋を seen_fp)。
func TestFakeEnrollHostKeyPinMismatch(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	req := f.enrollReq("root", priv)
	req["host_key_fp"] = "SHA256:" + strings.Repeat("B", 43)
	code, res := call(t, req)
	er := wantErr(t, code, res, "hostkey_mismatch")
	if er["seen_fp"] != f.fp {
		t.Errorf("seen_fp %v want %s", er["seen_fp"], f.fp)
	}
	if f.auths.Load() != 0 || f.sessions.Load() != 0 {
		t.Errorf("不一致なのに初期資格情報を使った: auths=%d sessions=%d", f.auths.Load(), f.sessions.Load())
	}
	if _, err := os.Stat(f.home + "/.sitate"); err == nil {
		t.Errorf("不一致なのに何かが置かれた")
	}
}

// 要求の host_key_fp が合えば先へ進む(この偽の宛先は前提を持たないので prereq_missing まで)。省いたときも進む(初回の登録)。
func TestFakeEnrollHostKeyPinMatchAndOmitted(t *testing.T) {
	for _, pin := range []string{"match", "omitted"} {
		f, priv := fakeWithKey(t, false)
		req := f.enrollReq("root", priv)
		if pin == "match" {
			req["host_key_fp"] = f.fp
		}
		code, res := call(t, req)
		wantErr(t, code, res, "prereq_missing")
		if f.auths.Load() == 0 {
			t.Errorf("%s: 認証に進んでいない", pin)
		}
	}
}

// allow_ssh_from を省き、SSH_CONNECTION も読めない宛先では、何も置かず no_observed_from(終了 1)。
func TestFakeEnrollNoObservedFrom(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	req := f.enrollReq("root", priv)
	delete(req, "allow_ssh_from")
	code, res := call(t, req)
	wantErr(t, code, res, "no_observed_from")
	if _, err := os.Stat(f.home + "/.sitate"); err == nil {
		t.Errorf("読めないのに何かが置かれた")
	}
}

// allow_ssh_from が(存在して)空の要求は、観測に回さず要求の不正(隔離を作れない)。
func TestFakeEnrollEmptyAllowIsInvalid(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	req := f.enrollReq("root", priv)
	req["allow_ssh_from"] = []string{}
	code, res := runRaw(t, req)
	wantErr(t, code, res, "INTERNAL")
	if f.sessions.Load() != 0 {
		t.Errorf("接続に進んだ")
	}
}

// 接続の SSH_CONNECTION の先頭を、sudo に入る前に同じ接続で読む。sudo の下で環境が消える宛先でも読める。
func TestFakeObservedFromReadBeforeSudo(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t).withSudoScrub(t)
	f.sshConn = "192.0.2.9 51234 192.0.2.1 22"
	c := dialFake(t, f, priv)
	c.sudo = true // 管理用アカウントの接続(sudo の下では環境が消える)
	if r, err := c.run(`printf %s "${SSH_CONNECTION:-}"`, ""); err != nil || r.out != "" {
		t.Fatalf("前提: sudo の下では SSH_CONNECTION が消える偽物のはず: %v %q", err, r.out)
	}
	if got := observedFrom(c); got != "192.0.2.9" {
		t.Errorf("observedFrom = %q, want 192.0.2.9", got)
	}
	if c.sudo != true {
		t.Errorf("観測が接続の sudo の設定を変えた")
	}
	f.sshConn = ""
	if got := observedFrom(dialFake(t, f, priv)); got != "" {
		t.Errorf("読めないときは空: %q", got)
	}
	f.sshConn = "not-an-address 1 2 3"
	if got := observedFrom(dialFake(t, f, priv)); got != "" {
		t.Errorf("IP アドレスでない先頭は読めない扱い: %q", got)
	}
}

// 受信許可の規則: allow_ssh_from があればそれ、無ければ観測した先頭(結果の observed_from と同じ値)。
func TestAllowFromRule(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.sshConn = "192.0.2.9 51234 192.0.2.1 22"
	c := dialFake(t, f, priv)
	got, obs, e := resolveAllow(c, nil)
	if e != nil || len(got) != 1 || got[0] != "192.0.2.9" || obs != "192.0.2.9" {
		t.Errorf("省いたとき: %v %q %v", got, obs, e)
	}
	got, obs, e = resolveAllow(c, []string{"198.51.100.0/24"})
	if e != nil || len(got) != 1 || got[0] != "198.51.100.0/24" || obs != "192.0.2.9" {
		t.Errorf("指定したとき: %v %q %v", got, obs, e)
	}
	f.sshConn = ""
	if _, obs, e = resolveAllow(dialFake(t, f, priv), nil); e == nil || e.code != "no_observed_from" || obs != "" {
		t.Errorf("読めないとき: %q %v", obs, e)
	}
	if got, obs, e = resolveAllow(dialFake(t, f, priv), []string{"192.0.2.5"}); e != nil || obs != "" || got[0] != "192.0.2.5" {
		t.Errorf("指定ありで読めないとき(エラーにしない): %v %q %v", got, obs, e)
	}
}

// D-0165: dry_run の enroll は接続してホスト鍵を返し、初期資格情報の受理と前提を読むだけで何も置かない。前提の欠けは失敗にせず prereq[] の ok で返す。
// 復元先の鍵・許可する送信元・送信先の一覧は要らない(定義を組み立てる前の導入の手順 3)。
func TestFakeEnrollDryRunPlacesNothing(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	req := f.enrollReq("root", priv)
	for _, k := range []string{"authorized_key", "allow_ssh_from", "egress_allow", "egress_host"} {
		delete(req, k)
	}
	req["dry_run"] = true
	code, res := call(t, req)
	if code != 0 {
		t.Fatalf("終了 %d: %v", code, res)
	}
	if res["host_key_fp"] != f.fp || res["user"] != restoreUser {
		t.Errorf("結果 %v", res)
	}
	ps, _ := res["prereq"].([]any)
	if len(ps) != 7 {
		t.Errorf("prereq %v", ps)
	}
	bad := 0
	for _, p := range ps {
		if p.(map[string]any)["ok"] == false {
			bad++
		}
	}
	for _, p := range ps {
		if m := p.(map[string]any); m["name"] == "subuid" && m["ok"] != true {
			t.Errorf("subuid は enroll が作る項目で、登録の前は欠けに数えない: %v", m)
		}
	}
	if bad == 0 {
		t.Error("この偽の宛先は前提を持たないので、欠けが ok: false で載るはず")
	}
	if f.auths.Load() == 0 {
		t.Error("初期資格情報の受理を確かめていない")
	}
	if _, err := os.Stat(f.home + "/.sitate"); err == nil {
		t.Error("dry_run が何かを置いた")
	}
}

// dry_run でも、要求の host_key_fp と違えば認証に進まず hostkey_mismatch(dry_run でない enroll に鍵と送信先が要ることは要求のスキーマが守る)。
func TestFakeEnrollDryRunPin(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	req := f.enrollReq("root", priv)
	req["dry_run"], req["host_key_fp"] = true, "SHA256:"+strings.Repeat("B", 43)
	code, res := call(t, req)
	wantErr(t, code, res, "hostkey_mismatch")
	if f.auths.Load() != 0 {
		t.Error("不一致なのに認証に進んだ")
	}
}
