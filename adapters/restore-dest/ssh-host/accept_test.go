package main

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
)

// 受入試験の環境(test/env。実物の sshd を持つ dest コンテナ)に対する試験。
// `bash test/env/bin/accept-env.sh up <NAME>` の後に `make shell-linux ACCEPT=<NAME> CMD="go test ..."` で走らせる。
// 環境が無いときは skip する(skip は合格に数えない。報告に「未実行」と書く)。
type acceptEnv struct {
	host, adminUser, adminKey, fp string
	port                          int
	hookHost                      string
	hookIP                        string
	srcIP                         string // この試験(= バックアップサーバーの役)の送信元
}

func loadAccept(t *testing.T) acceptEnv {
	t.Helper()
	h := os.Getenv("SITATE_ACCEPT_DEST_HOST")
	if h == "" {
		t.Skip("受入試験の環境が無い(SITATE_ACCEPT_DEST_HOST が空): accept-env.sh up の後に make shell-linux ACCEPT=<NAME> で走らせる")
	}
	port, _ := strconv.Atoi(os.Getenv("SITATE_ACCEPT_DEST_PORT"))
	e := acceptEnv{host: h, port: port, adminUser: os.Getenv("SITATE_ACCEPT_ADMIN_USER"), fp: os.Getenv("SITATE_ACCEPT_DEST_HOST_KEY_FP"), hookHost: "hook"}
	e.adminKey = readFile(t, os.Getenv("SITATE_ACCEPT_ADMIN_KEY_FILE"))
	ips, err := net.LookupIP(e.hookHost)
	if err != nil || len(ips) == 0 {
		t.Fatalf("hook を名前解決できない: %v", err)
	}
	e.hookIP = ips[0].String()
	e.srcIP = localAddrTo(t, h, port)
	return e
}

func (e acceptEnv) at(user string) map[string]any {
	return map[string]any{"at": "ssh://" + user + "@" + net.JoinHostPort(e.host, strconv.Itoa(e.port)), "key_ref": "dest-key"}
}

func (e acceptEnv) enrollReq(pub string) map[string]any {
	return map[string]any{"op": "enroll", "dest": e.at(e.adminUser), "authorized_key": pub, "allow_ssh_from": []string{e.srcIP},
		"egress_allow": []string{e.hookIP}, "egress_host": e.hookHost, "secrets": map[string]string{"dest_key": e.adminKey}}
}

func (e acceptEnv) startReq(sid, priv, pub, fp string, gb int) map[string]any {
	return map[string]any{"op": "start", "session_id": sid, "dest": e.at("sitate-restore"), "needs": map[string]any{"disk_gb": gb}, "host_key_fp": fp,
		"authorized_key": pub, "allow_ssh_from": []string{e.srcIP}, "egress_allow": []string{e.hookIP}, "egress_host": e.hookHost,
		"secrets": map[string]string{"restore-dest-ssh": priv}}
}

func (e acceptEnv) teardownReq(op, sid, priv, fp string, rs []map[string]any) map[string]any {
	var s any = sid
	if sid == "" && op == "inspect" {
		s = nil
	}
	if rs == nil {
		rs = []map[string]any{}
	}
	return map[string]any{"op": op, "session_id": s, "dest": e.at("sitate-restore"), "host_key_fp": fp, "resources": rs,
		"secrets": map[string]string{"restore-dest-ssh": priv}}
}

// session は専用ユーザーの接続(試験の側から。ホスト鍵は固定した指紋と照らす)。
func (e acceptEnv) session(t *testing.T, priv string) *conn {
	t.Helper()
	auth, _ := authFromSecret(priv, false)
	c, ae := dial(context.Background(), target{user: restoreUser, host: e.host, port: e.port}, restoreUser, auth, e.fp)
	if ae != nil {
		t.Fatalf("専用ユーザーで接続できない: %s", ae.code)
	}
	t.Cleanup(c.close)
	return c
}

const sidA, sidB = "r-20261103T010203Z-3f9a", "r-20261103T010204Z-3f9b"

func resIDs(res map[string]any, key string) []string {
	var out []string
	rs, _ := res[key].([]any)
	for _, r := range rs {
		m := r.(map[string]any)
		out = append(out, m["type"].(string)+":"+m["id"].(string))
	}
	return out
}

// 登録 → 起動 → 領域に書く → 検出 → 停止 → 消去の確認、までの主経路。
func TestAcceptLifecycle(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	if res["host_key_fp"] != e.fp || res["user"] != restoreUser {
		t.Fatalf("enroll の結果: %v", res)
	}
	for _, p := range res["prereq"].([]any) {
		if p.(map[string]any)["ok"] != true {
			t.Errorf("前提が満たされていない: %v", p)
		}
	}
	resetDest(t, e, priv)
	code, res = call(t, e.startReq(sidA, priv, pub, e.fp, 1))
	wantOK(t, code, res)
	scratch := res["scratch"].(map[string]any)
	if scratch["method"] != "gocryptfs" || !strings.HasSuffix(scratch["path"].(string), "/"+sidA+"/scratch") {
		t.Fatalf("scratch: %v", scratch)
	}
	if got := resIDs(res, "resources"); strings.Join(got, ",") != "workdir:"+sidA+",scratch:"+sidA {
		t.Fatalf("resources: %v", got)
	}
	c := e.session(t, priv)
	w := strings.TrimSuffix(scratch["path"].(string), "/scratch")
	r, err := c.run(`echo plaintext-marker-xyz > "$1/scratch/hello.txt" && ls "$1/cipher" && grep -rl plaintext-marker-xyz "$1/cipher" | wc -l`, "", w)
	if err != nil || r.code != 0 || !strings.HasSuffix(strings.TrimSpace(r.out), "\n0") && !strings.HasSuffix(strings.TrimSpace(r.out), "0") {
		t.Fatalf("領域への書き込みと暗号文の確認: %v %+v", err, r)
	}
	if strings.Contains(r.out, "hello.txt") {
		t.Errorf("暗号文のディレクトリに平文のファイル名が見える: %s", r.out)
	}
	c.close() // 他の SSH の接続が残っていると、stop はそれを残存の処理として報告する(仕様どおり)
	code, res = call(t, e.teardownReq("inspect", sidA, priv, e.fp, nil))
	wantOK(t, code, res)
	if res["state"] != "present" {
		t.Errorf("inspect: %v", res)
	}
	code, res = call(t, e.teardownReq("stop", sidA, priv, e.fp, nil))
	wantOK(t, code, res)
	if res["done"] != true || res["verified_by"] != "ssh" || len(res["remaining"].([]any)) != 0 {
		t.Fatalf("stop: %v", res)
	}
	code, res = call(t, e.teardownReq("inspect", sidA, priv, e.fp, nil))
	wantOK(t, code, res)
	if res["state"] != "absent" || len(res["resources"].([]any)) != 0 {
		t.Errorf("stop の後の inspect: %v", res)
	}
	if r, _ := e.session(t, priv).run(`test ! -e "$1"`, "", w); r.code != 0 {
		t.Errorf("作業ディレクトリが残っている")
	}
}

// stop が消せない物(この確かめの SSH のほかに残る専用ユーザーの処理)は remaining[] に載せ、done: false で終了コード 0(spec 3.2.4)。
func TestAcceptStopRemaining(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	resetDest(t, e, priv)
	code, res = call(t, e.startReq(sidA, priv, pub, e.fp, 1))
	wantOK(t, code, res)
	other := e.session(t, priv) // 別の SSH の接続と、その上で動き続ける処理
	s, err := other.cl.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start("sleep 60"); err != nil {
		t.Fatal(err)
	}
	code, res = call(t, e.teardownReq("stop", sidA, priv, e.fp, nil))
	wantOK(t, code, res)
	rem := resIDs(res, "remaining")
	if res["done"] != false || res["verified_by"] != "ssh" || len(rem) == 0 {
		t.Fatalf("残存があるときは done: false と remaining[]: %v", res)
	}
	for _, r := range rem {
		if !strings.HasPrefix(r, "process:") {
			t.Errorf("session の資源は消えているはず: %v", rem)
		}
	}
	// 切れた接続の後も動き続ける処理は、どの session_id の物とも言えないので stop は止めない。人が止めるまで remaining に残る。
	_, _ = other.run(`pkill -x sleep`, "")
	other.close()
	code, res = call(t, e.teardownReq("stop", sidA, priv, e.fp, nil))
	wantOK(t, code, res)
	if res["done"] != true {
		t.Errorf("処理を止めた後の stop: %v", res)
	}
}
