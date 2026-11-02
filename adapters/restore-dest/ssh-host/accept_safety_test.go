package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// listRun は専用ユーザーの ~/.sitate/run の直下の名前(印の無い物も含む)。
func listRun(t *testing.T, e acceptEnv, priv string) string {
	t.Helper()
	c := e.session(t, priv)
	defer c.close()
	r, err := c.run(`ls -A "$HOME/.sitate/run" | sort | tr '\n' ' '`, "")
	if err != nil || r.code != 0 {
		t.Fatalf("ls: %v %+v", err, r)
	}
	return strings.TrimSpace(r.out)
}

// resetDest は前の試験の残りを片付け、終わりにも片付ける(session A・B と decoy)。
func resetDest(t *testing.T, e acceptEnv, priv string) {
	t.Helper()
	clean := func() {
		for _, sid := range []string{sidA, sidB} {
			call(t, e.teardownReq("stop", sid, priv, e.fp, nil))
		}
		if c, ae := dial(context.Background(), target{user: restoreUser, host: e.host, port: e.port}, restoreUser, mustAuth(priv), e.fp); ae == nil {
			_, _ = c.run(`rm -rf "$HOME/.sitate/run/decoy" "$HOME/pwn"`, "")
			c.close()
		}
	}
	clean()
	t.Cleanup(clean)
}

func mustAuth(priv string) ssh.AuthMethod {
	a, _ := authFromSecret(priv, false)
	return a
}

func startOK(t *testing.T, e acceptEnv, sid, priv, pub string) {
	t.Helper()
	code, res := call(t, e.startReq(sid, priv, pub, e.fp, 1))
	wantOK(t, code, res)
}

// 台帳の資源の識別が誤っても別の資源を破棄しない(spec 3.2.4。申し送り)。
func TestAcceptStopNeverTouchesOtherResources(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	resetDest(t, e, priv)
	startOK(t, e, sidA, priv, pub)
	startOK(t, e, sidB, priv, pub)
	// 印の無いディレクトリ(run_id の形でない名前)は、列挙も破棄もされない。
	c := e.session(t, priv)
	if r, err := c.run(`mkdir -p "$HOME/.sitate/run/decoy" && echo keep > "$HOME/.sitate/run/decoy/f"`, ""); err != nil || r.code != 0 {
		t.Fatal(err, r)
	}
	c.close()
	before := listRun(t, e, priv)
	if before != "decoy "+sidA+" "+sidB {
		t.Fatalf("前提の状態: %q", before)
	}
	rejectBadIdentities(t, e, priv, before)
	// (4) 正しい stop は自分の session_id の物だけを消す。
	code, res = call(t, e.teardownReq("stop", sidA, priv, e.fp, []map[string]any{{"type": "workdir", "id": sidA}}))
	wantOK(t, code, res)
	if res["done"] != true {
		t.Fatalf("stop: %v", res)
	}
	if got := listRun(t, e, priv); got != "decoy "+sidB {
		t.Fatalf("他の資源が巻き込まれた: %q", got)
	}
	// 掃引(session_id: null)は印の付いた B だけを列挙し、decoy は列挙しない。
	code, res = call(t, e.teardownReq("inspect", "", priv, e.fp, nil))
	wantOK(t, code, res)
	got := strings.Join(resIDs(res, "resources"), ",")
	if !strings.HasPrefix(got, "workdir:"+sidB+",scratch:"+sidB) || strings.Contains(got, sidA) || strings.Contains(got, "decoy") {
		t.Fatalf("掃引: %v", res) // B の領域を保つ gocryptfs の処理(process)も B の資源として列挙される
	}
	for _, r := range res["resources"].([]any) {
		if r.(map[string]any)["session_id"] != sidB {
			t.Errorf("session_id の違う資源: %v", r)
		}
	}
	code, res = call(t, e.teardownReq("stop", sidB, priv, e.fp, nil))
	wantOK(t, code, res)
	if got := listRun(t, e, priv); got != "decoy" {
		t.Fatalf("後始末: %q", got)
	}
	c = e.session(t, priv)
	_, _ = c.run(`rm -rf "$HOME/.sitate/run/decoy"`, "")
}

// rejectBadIdentities は誤った識別(台帳の id の食い違い、空・形の違う session_id)では何も触らないことを確かめる。
func rejectBadIdentities(t *testing.T, e acceptEnv, priv, before string) {
	t.Helper()
	// (1) 台帳の resources[] の id が session_id と違う: 何も触らず INTERNAL。
	bad := []map[string]any{{"type": "workdir", "id": sidB}}
	code, res := call(t, e.teardownReq("stop", sidA, priv, e.fp, bad))
	wantErr(t, code, res, "INTERNAL")
	// (2) session_id が空・形の違うもの・host_key_fp が無いもの: 何も触らず INTERNAL。
	for _, sid := range []string{"", ".", "..", "r-*", "decoy", sidA + "/..", "../" + sidA} {
		code, res = runRaw(t, e.teardownReq("stop", sid, priv, e.fp, nil))
		wantErr(t, code, res, "INTERNAL")
	}
	nofp := e.teardownReq("stop", sidA, priv, e.fp, nil)
	nofp["host_key_fp"] = nil
	code, res = runRaw(t, nofp)
	wantErr(t, code, res, "INTERNAL")
	// (3) リモートのスクリプトそのものも、形の違う session_id では何も消さない(終了 64)。
	c := e.session(t, priv)
	for _, sid := range []string{"", ".", "..", "r-*", "decoy", "../" + sidA, sidA + "/..", "$(touch $HOME/pwn)", sidA + "; touch $HOME/pwn"} {
		for _, sc := range []string{teardownScript, inspectScript, scratchCreateScript} {
			if sc == inspectScript && sid == "" {
				continue // 空は掃引(全 session_id の列挙。何も消さない)
			}
			if r, err := c.run(sc, "", sid); err != nil || r.code != 64 {
				t.Errorf("session_id %q: 終了 64 を期待: %v %+v", sid, err, r)
			}
		}
	}
	if r, _ := c.run(`test ! -e "$HOME/pwn"`, ""); r.code != 0 {
		t.Errorf("session_id の文字列がシェルとして実行された")
	}
	c.close()
	if got := listRun(t, e, priv); got != before {
		t.Fatalf("誤った識別で資源が変わった: %q -> %q", before, got)
	}
}

func TestAcceptStartFailures(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	resetDest(t, e, priv)
	empty := func(t *testing.T, er map[string]any) {
		t.Helper()
		rs, ok := er["resources"].([]any)
		if !ok || len(rs) != 0 {
			t.Errorf("何も作っていない失敗は resources: [] を載せる: %v", er)
		}
		if got := listRun(t, e, priv); got != "" {
			t.Errorf("失敗なのに何かが置かれた: %q", got)
		}
	}
	// ホスト鍵が固定値と違えば何も置かずに失敗し、見えた指紋を seen_fp に載せる。
	wrong := "SHA256:" + strings.Repeat("A", 43)
	code, res = call(t, e.startReq(sidA, priv, pub, wrong, 1))
	er := wantErr(t, code, res, "hostkey_mismatch")
	if er["seen_fp"] != e.fp {
		t.Errorf("seen_fp: %v", er["seen_fp"])
	}
	empty(t, er)
	// 領域の空きが足りなければ領域を作らずに失敗する。
	code, res = call(t, e.startReq(sidA, priv, pub, e.fp, 1<<30))
	er = wantErr(t, code, res, "disk_short")
	if d := er["detail"].(map[string]any); d["needed_gb"] != float64(1<<30) || d["free_gb"] == nil {
		t.Errorf("disk_short の detail: %v", d)
	}
	empty(t, er)
	// 保管先のアドレスが変わった(egress_host の名前解決が固定値に 1 つも含まれない)。
	r := e.startReq(sidA, priv, pub, e.fp, 1)
	r["egress_allow"] = []string{"192.0.2.99"}
	code, res = call(t, r)
	er = wantErr(t, code, res, "enroll_mismatch")
	if m, _ := er["detail"].(map[string]any)["mismatches"].([]any); len(m) != 1 || m[0] != "egress_host" {
		t.Errorf("mismatches: %v", er["detail"])
	}
	empty(t, er)
	// 認証の拒否。
	other, _ := newKeyPair(t)
	code, res = call(t, e.startReq(sidA, other, pub, e.fp, 1))
	empty(t, wantErr(t, code, res, "auth"))
}

func TestAcceptUnreachable(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	backoffBase, retryMax, connectTimeout = time.Millisecond, 1, time.Second
	defer func() { backoffBase, retryMax, connectTimeout = time.Second, 3, 30*time.Second }()
	// 待ち受けていないポート(拒否される): reason connect。
	e.host, e.port = os.Getenv("SITATE_ACCEPT_SOURCE_HOST"), 2
	code, res := call(t, e.startReq(sidA, priv, pub, e.fp, 1))
	er := wantErr(t, code, res, "unreachable")
	if er["detail"].(map[string]any)["reason"] != "connect" {
		t.Errorf("detail: %v", er["detail"])
	}
	// 隔離が破棄するポート(応答が無い): reason timeout。
	e.host = os.Getenv("SITATE_ACCEPT_DEST_HOST")
	code, res = call(t, e.startReq(sidA, priv, pub, e.fp, 1))
	er = wantErr(t, code, res, "unreachable")
	if er["detail"].(map[string]any)["reason"] != "timeout" {
		t.Errorf("detail: %v", er["detail"])
	}
	code, res = call(t, e.teardownReq("inspect", "", priv, e.fp, nil))
	wantOK(t, code, res)
	if res["state"] != "unknown" {
		t.Errorf("到達できない inspect は unknown: %v", res)
	}
}

// 隔離: 送信は egress と名前解決だけ。確かめるのは専用ユーザーとしての実測(許可した宛先に届き、許可外に届かない)。
func TestAcceptIsolationEffective(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	c := e.session(t, priv)
	tcp := func(host, port string) int {
		r, err := c.run(`timeout 4 bash -c 'exec 3<>/dev/tcp/'"$1"'/'"$2"`, "", host, port)
		if err != nil {
			t.Fatal(err)
		}
		return r.code
	}
	if rc := tcp("hook", "8443"); rc != 0 {
		t.Errorf("許可した宛先(hook)に届かない rc=%d", rc)
	}
	if rc := tcp("source", "22"); rc == 0 {
		t.Errorf("許可外の宛先(source)に届いた")
	}
	if r, _ := c.run(`getent hosts source >/dev/null`, ""); r.code != 0 {
		t.Errorf("名前解決ができない")
	}
	if r, _ := c.run(`grep -c '# sitate-egress$' /etc/hosts`, ""); strings.TrimSpace(r.out) != "1" {
		t.Errorf("/etc/hosts の固定: %q", r.out)
	}
}
