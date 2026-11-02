package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func fakeWithKey(t *testing.T, rsaHost bool) (*fakeSSH, string) {
	t.Helper()
	f := startFakeSSH(t, rsaHost)
	priv, pub := newKeyPair(t)
	f.userKey = mustPub(t, pub)
	return f, priv
}

// ホスト鍵が固定した指紋と違えば、認証に進まず(鍵を渡さず)失敗し、見えた指紋を seen_fp に載せる。
func TestFakeHostKeyPinned(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	f.mkRun(t, sidA)
	wrong := "SHA256:" + strings.Repeat("B", 43)
	for _, op := range []string{"stop", "inspect"} {
		code, res := call(t, f.req(op, sidA, priv, wrong, nil))
		er := wantErr(t, code, res, "hostkey_mismatch")
		if er["seen_fp"] != f.fp {
			t.Errorf("%s: seen_fp %v want %s", op, er["seen_fp"], f.fp)
		}
	}
	if f.auths.Load() != 0 || f.sessions.Load() != 0 {
		t.Errorf("不一致なのに認証・実行に進んだ: auths=%d sessions=%d", f.auths.Load(), f.sessions.Load())
	}
	if got := f.listRun(t); !reflect.DeepEqual(got, []string{sidA}) {
		t.Errorf("資源が変わった: %v", got)
	}
	// 一致すれば進む。
	code, res := call(t, f.req("inspect", sidA, priv, f.fp, nil))
	wantOK(t, code, res)
	if res["state"] != "present" {
		t.Errorf("inspect: %v", res)
	}
}

// 鍵種は Ed25519 に限る(RSA のホスト鍵しか持たないホストには、固定した指紋が一致しても鍵を渡さない)。
// 見えた指紋は seen_fp に必ず載る。enroll でも hostkey_mismatch になる。
func TestFakeRSAHostKeyRefused(t *testing.T) {
	f, priv := fakeWithKey(t, true)
	for _, pin := range []string{f.fp, "SHA256:" + strings.Repeat("C", 43)} {
		code, res := call(t, f.req("inspect", sidA, priv, pin, nil))
		er := wantErr(t, code, res, "hostkey_mismatch")
		if er["seen_fp"] != f.fp {
			t.Errorf("seen_fp %v want %s", er["seen_fp"], f.fp)
		}
	}
	code, res := call(t, f.enrollReq("root", priv))
	er := wantErr(t, code, res, "hostkey_mismatch")
	if er["seen_fp"] != f.fp {
		t.Errorf("enroll の seen_fp %v want %s", er["seen_fp"], f.fp)
	}
	if f.auths.Load() != 0 {
		t.Errorf("認証に進んだ")
	}
}

func TestFakeAuthRejected(t *testing.T) {
	f, _ := fakeWithKey(t, false)
	other, _ := newKeyPair(t)
	code, res := call(t, f.req("stop", sidA, other, f.fp, nil))
	wantErr(t, code, res, "auth")
}

// 台帳の識別が誤っていても、session_id が指す物以外は消えない。
func TestFakeStopOnlyOwnSession(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	f.mkRun(t, sidA, sidB, "decoy", "r-2026")
	for _, bad := range []struct {
		sid string
		rs  []map[string]any
	}{
		{sidA, []map[string]any{{"type": "workdir", "id": sidB}}},
		{sidA, []map[string]any{{"type": "scratch", "id": "decoy"}}},
		{"", nil}, {".", nil}, {"..", nil}, {"decoy", nil}, {"r-2026", nil}, {"*", nil}, {sidA + "/../" + sidB, nil},
	} {
		code, res := runRaw(t, f.req("stop", bad.sid, priv, f.fp, bad.rs))
		wantErr(t, code, res, "INTERNAL")
	}
	if f.sessions.Load() != 0 {
		t.Fatalf("不正な要求でリモートの実行に進んだ")
	}
	code, res := call(t, f.req("stop", sidA, priv, f.fp, []map[string]any{{"type": "workdir", "id": sidA}}))
	wantOK(t, code, res)
	// この試験のホストは開発用コンテナの同じユーザーで、ほかの処理を持つ。session の資源(workdir・scratch)が残っていないことを確かめる。
	for _, r := range resIDs(res, "remaining") {
		if strings.HasPrefix(r, "workdir:") || strings.HasPrefix(r, "scratch:") {
			t.Fatalf("stop: %v", res)
		}
	}
	if got := f.listRun(t); !reflect.DeepEqual(got, []string{sidB, "decoy", "r-2026"}) && !sameSet(got, []string{sidB, "decoy", "r-2026"}) {
		t.Fatalf("残るべき物が変わった: %v", got)
	}
	// 親(~/.sitate/run)を消す経路が無い: どの形の session_id でもスクリプトは終了 64 で何も消さない。
	c := dialFake(t, f, priv)
	for _, sid := range []string{"", ".", "..", "/", "~", "decoy", "$HOME", "r-*"} {
		if r, err := c.run(teardownScript, "", sid); err != nil || r.code != 64 {
			t.Errorf("teardown %q: %v %+v", sid, err, r)
		}
	}
	if got := f.listRun(t); len(got) != 3 {
		t.Fatalf("親か兄弟が消えた: %v", got)
	}
}

func TestFakeInspectSweep(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	f.withStubs(t)
	f.mkRun(t, sidA, sidB, "decoy")
	code, res := call(t, f.req("inspect", "", priv, f.fp, nil))
	wantOK(t, code, res)
	got := resIDs(res, "resources")
	if !sameSet(got, []string{"workdir:" + sidA, "workdir:" + sidB}) {
		t.Fatalf("掃引(印の無い decoy は列挙しない): %v", got)
	}
	code, res = call(t, f.req("inspect", sidB, priv, f.fp, nil))
	wantOK(t, code, res)
	if got := resIDs(res, "resources"); !sameSet(got, []string{"workdir:" + sidB}) {
		t.Fatalf("session_id を指定: %v", got)
	}
	code, res = call(t, f.req("inspect", "r-20261103T010299Z-0000", priv, f.fp, nil))
	wantOK(t, code, res)
	if res["state"] != "absent" {
		t.Errorf("無い session_id は absent: %v", res)
	}
}

func sameSet(a, b []string) bool {
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

func (f *fakeSSH) enrollReq(user, secret string) map[string]any {
	d := f.dest()
	d["at"] = strings.Replace(d["at"].(string), restoreUser+"@", user+"@", 1)
	return map[string]any{"op": "enroll", "dest": d, "authorized_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexample", "allow_ssh_from": []string{"192.0.2.1"},
		"egress_allow": []string{"198.51.100.7"}, "egress_host": "s3.example.invalid", "secrets": map[string]string{"dest_key": secret}}
}

// 前提が欠けるホストの enroll は、何も置かずに prereq_missing(全項目の name・ok)で失敗する。
func TestFakeEnrollPrereqMissing(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	code, res := call(t, f.enrollReq("root", priv)) // root なら sudo を介さず、この試験ホスト(前提を持たない)で実行される
	e := wantErr(t, code, res, "prereq_missing")
	ps, _ := e["detail"].(map[string]any)["prereq"].([]any)
	names := map[string]any{}
	for _, p := range ps {
		names[p.(map[string]any)["name"].(string)] = p.(map[string]any)["ok"]
	}
	if names["gocryptfs"] != false || names["nft"] != false || len(ps) != 7 {
		t.Errorf("prereq: %v", ps)
	}
	if _, err := os.Stat(f.home + "/.sitate"); err == nil {
		t.Errorf("前提が欠けるのに何かが置かれた")
	}
}

// 管理用アカウントが sudo を使えなければ、置く操作は enroll_failed。初期資格情報の拒否は auth。
func TestFakeEnrollFailures(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	code, res := call(t, f.enrollReq("adm", priv))
	wantErr(t, code, res, "enroll_failed")
	other, _ := newKeyPair(t)
	code, res = call(t, f.enrollReq("adm", other))
	wantErr(t, code, res, "auth")
}
