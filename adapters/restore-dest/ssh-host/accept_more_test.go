package main

import (
	"context"
	"strings"
	"testing"
)

// admin は管理用アカウントの接続(root として実行する。試験の準備と確認だけに使う)。
func (e acceptEnv) admin(t *testing.T) *conn {
	t.Helper()
	auth, ok := authFromSecret(e.adminKey, true)
	if !ok {
		t.Fatal("管理用アカウントの資格情報")
	}
	c, ae := dial(context.Background(), target{user: e.adminUser, host: e.host, port: e.port}, e.adminUser, auth, e.fp)
	if ae != nil {
		t.Fatalf("管理用アカウントで接続できない: %s", ae.code)
	}
	c.sudo = e.adminUser != "root"
	t.Cleanup(c.close)
	return c
}

func mustRun(t *testing.T, c *conn, script, stdin string, args ...string) string {
	t.Helper()
	r, err := c.run(script, stdin, args...)
	if err != nil || r.code != 0 {
		t.Fatalf("実行に失敗: err=%v rc=%d out=%q\nscript=%s", err, r.code, r.out, script)
	}
	return strings.TrimSpace(r.out)
}

// F1: 再登録の root は、専用ユーザーが置いたシンボリックリンクを辿らない(リンク先の所有者・内容が変わらず、正しく置き直される)。
func TestAcceptReenrollDoesNotFollowLinks(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	enroll := func() {
		code, res := call(t, e.enrollReq(pub))
		wantOK(t, code, res)
	}
	enroll()
	adm := e.admin(t)
	clean := func() {
		mustRun(t, adm, `h=$(getent passwd sitate-restore | cut -d: -f6); rm -rf /etc/u2d-dir /etc/u2d-pwn "$h/.sitate" "$h/.ssh/authorized_keys" "$h/.ssh/authorized_keys.new"`, "")
	}
	t.Cleanup(func() { // 壊れた状態を後の試験に残さない
		clean()
		code, res := call(t, e.enrollReq(pub))
		wantOK(t, code, res)
	})
	mustRun(t, adm, `mkdir /etc/u2d-dir && chmod 0755 /etc/u2d-dir`, "")
	u := e.session(t, priv)
	mustRun(t, u, `rm -rf "$HOME/.sitate" && ln -s /etc/u2d-dir "$HOME/.sitate" && ln -sfn /etc/u2d-pwn "$HOME/.ssh/authorized_keys.new"`, "")
	u.close()
	enroll()
	if got := mustRun(t, adm, `stat -c '%U %a' /etc/u2d-dir; test ! -e /etc/u2d-pwn && echo nopwn`, ""); got != "root 755\nnopwn" {
		t.Fatalf("リンク先が変わった: %q", got)
	}
	u = e.session(t, priv)
	got := mustRun(t, u, `test -d "$HOME/.sitate/run" && test ! -L "$HOME/.sitate" && test -f "$HOME/.ssh/authorized_keys" && test ! -L "$HOME/.ssh/authorized_keys" && echo ok; cut -c1-30 "$HOME/.ssh/authorized_keys"`, "")
	if !strings.HasPrefix(got, "ok\nrestrict ssh-ed25519 ") {
		t.Errorf("置き直した結果(authorized_keys は restrict 付き): %q", got)
	}
	u.close()
	resetDest(t, e, priv)
	startOK(t, e, sidA, priv, pub)
}

// F3: 隔離のユニットは有効で、動いていて、規則が入っていて、順序の循環を作らない。
func TestAcceptIsolationUnit(t *testing.T) {
	e := loadAccept(t)
	_, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	adm := e.admin(t)
	const unit = "sitate-isolation.service"
	got := mustRun(t, adm, `systemctl is-enabled `+unit+`; systemctl is-active `+unit+`; systemctl show `+unit+` -p DefaultDependencies --value;
systemctl show `+unit+` -p Before --value; test -e /etc/systemd/system/sysinit.target.wants/`+unit+` && echo wants-sysinit;
nft list table inet sitate_isolation >/dev/null && echo rules`, "")
	for _, want := range []string{"enabled\n", "active\n", "no\n", "network-pre.target", "wants-sysinit", "rules"} {
		if !strings.Contains(got+"\n", want) {
			t.Errorf("ユニットの状態に %q が無い:\n%s", want, got)
		}
	}
	out := mustRun(t, adm, `systemd-analyze verify /etc/systemd/system/`+unit+` 2>&1 || true`, "")
	if strings.Contains(out, "ordering cycle") {
		t.Errorf("順序の循環: %s", out)
	}
}

// F4: IPv6 の許可した相手とは近隣探索を経て通信でき、許可外の相手とは通信できない(netns と veth で実測)。
func TestAcceptIPv6NeighborDiscovery(t *testing.T) {
	e := loadAccept(t)
	adm := e.admin(t)
	const setup = `ip netns del u2da 2>/dev/null; ip netns del u2db 2>/dev/null; ip netns add u2da && ip netns add u2db &&
ip link add vu2a type veth peer name vu2b && ip link set vu2a netns u2da && ip link set vu2b netns u2db &&
ip -n u2da addr add fd00:77::1/64 dev vu2a nodad && ip -n u2da link set vu2a up && ip -n u2da link set lo up &&
ip -n u2db addr add fd00:77::2/64 dev vu2b nodad && ip -n u2db addr add fd00:77::3/64 dev vu2b nodad && ip -n u2db link set vu2b up`
	t.Cleanup(func() { _, _ = adm.run(`pkill -f u2d-listener; ip netns del u2da; ip netns del u2db; true`, "") })
	if r, err := adm.run(setup, ""); err != nil || r.code != 0 {
		t.Fatalf("netns を作れない: %v %+v", err, r)
	}
	rules := renderNft(2222, []string{"fd00:77::2"}, nil, nil)
	mustRun(t, adm, `ip netns exec u2da nft -f -`, rules)
	mustRun(t, adm, `(ip netns exec u2da perl -MIO::Socket::IP -e '$0="u2d-listener"; $l=IO::Socket::IP->new(Listen=>5,LocalAddr=>"::",LocalPort=>2222,ReuseAddr=>1) or die; while($c=$l->accept){close $c}' >/dev/null 2>&1 &) ; sleep 1`, "")
	const client = `ip netns exec u2db perl -MIO::Socket::IP -e 'exit(IO::Socket::IP->new(PeerHost=>"fd00:77::1",PeerPort=>2222,LocalHost=>$ARGV[0],Timeout=>4)?0:1)' "$1"`
	if r, err := adm.run(client, "", "fd00:77::2"); err != nil || r.code != 0 {
		t.Errorf("許可した相手(IPv6)に届かない: %v %+v", err, r)
	}
	if r, err := adm.run(client, "", "fd00:77::3"); err != nil || r.code == 0 {
		t.Errorf("許可外の相手(IPv6)に届いた: %v %+v", err, r)
	}
}

// F2: podman が失敗する復元先で、stop は done: true にならず、inspect は absent を返さない。
func TestAcceptPodmanFailure(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	resetDest(t, e, priv)
	startOK(t, e, sidA, priv, pub)
	u := e.session(t, priv)
	mustRun(t, u, `mkdir -p "$HOME/.local/share/containers" && chmod 000 "$HOME/.local/share/containers"`, "")
	u.close()
	t.Cleanup(func() {
		c := e.session(t, priv)
		_, _ = c.run(`chmod 700 "$HOME/.local/share/containers"`, "")
		c.close()
	})
	code, res = call(t, e.teardownReq("stop", sidA, priv, e.fp, nil))
	if code == 0 && res["done"] == true {
		t.Errorf("podman が失敗したのに done: true: %v", res)
	}
	code, res = call(t, e.teardownReq("inspect", sidA, priv, e.fp, nil))
	wantOK(t, code, res)
	if res["state"] == "absent" {
		t.Errorf("podman が失敗したのに absent: %v", res)
	}
}

// F6・M9: 領域を外せないとき、stop は scratch と workdir を remaining に載せて done: false(消したと偽らない)。
// 外せるようになれば、次の stop で消える。SIGTERM を無視して領域に居る処理は、止めた後に平文が読めない。
func TestAcceptStopUnmountFails(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	resetDest(t, e, priv)
	code, res = call(t, e.startReq(sidA, priv, pub, e.fp, 1))
	wantOK(t, code, res)
	w := strings.TrimSuffix(res["scratch"].(map[string]any)["path"].(string), "/scratch")
	u := e.session(t, priv)
	mustRun(t, u, `echo secret-marker > "$1/scratch/f"; cd "$1/scratch" && (setsid nohup bash -c 'trap "" TERM; while :; do sleep 1; done' >/dev/null 2>&1 </dev/null &)
printf '#!/bin/sh\nexit 1\n' > "$HOME/failfm"; chmod 755 "$HOME/failfm"
printf 'mkdir -p "$HOME/stubbin"; ln -sf "$HOME/failfm" "$HOME/stubbin/fusermount3"; PATH="$HOME/stubbin:$PATH"\n' > "$HOME/.bashrc"`, "", w)
	u.close()
	stubOff := func() {
		c := e.session(t, priv)
		_, _ = c.run(`rm -rf "$HOME/.bashrc" "$HOME/stubbin" "$HOME/failfm"`, "")
		c.close()
	}
	defer stubOff()
	code, res = call(t, e.teardownReq("stop", sidA, priv, e.fp, nil))
	wantOK(t, code, res)
	rem := resIDs(res, "remaining")
	if res["done"] != false || !sameSet(rem, []string{"scratch:" + sidA, "workdir:" + sidA}) {
		t.Fatalf("領域を外せないときの stop: %v", res)
	}
	stubOff()
	code, res = call(t, e.teardownReq("stop", sidA, priv, e.fp, nil))
	wantOK(t, code, res)
	if res["done"] != true {
		t.Errorf("外せるようになった後の stop: %v", res)
	}
	c := e.session(t, priv)
	defer c.close()
	if got := mustRun(t, c, `for d in /proc/[0-9]*; do readlink "$d/cwd" 2>/dev/null; done | grep -cF -- "$1"; grep -rl secret-marker "$1" 2>/dev/null | wc -l; test ! -e "$1" && echo gone`, "", w); got != "0\n0\ngone" {
		t.Errorf("stop の後に処理か平文が残った: %q", got)
	}
}

// M7: 領域のパスワードは標準入力だけで渡り、専用ユーザーのホームにもコマンド行にも一時領域にも書かれない。
func TestAcceptPasswordNeverWritten(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	resetDest(t, e, priv)
	const marker = "PWMARKERu2dq7Zk3Vx9"
	old := genPassword
	genPassword = func() (string, error) { return marker, nil }
	defer func() { genPassword = old }()
	startOK(t, e, sidA, priv, pub)
	c := e.session(t, priv)
	defer c.close()
	got := mustRun(t, c, `{ grep -rlF -- "$1" "$HOME" /tmp /var/tmp 2>/dev/null; for p in $(pgrep -u "$(id -u)" gocryptfs); do tr '\0' ' ' < /proc/$p/cmdline | grep -F -- "$1"; done; } | wc -l`, "", marker)
	if got != "0" {
		t.Errorf("パスワードがファイルかコマンド行に現れた: %s 件", got)
	}
}

// M13: 前回の固定が無い状態からの登録で、/etc/hosts に egress_host の固定を置く。
func TestAcceptEgressHostsPinned(t *testing.T) {
	e := loadAccept(t)
	_, pub := newKeyPair(t)
	adm := e.admin(t)
	mustRun(t, adm, `t=$(mktemp); { grep -v '# sitate-egress$' /etc/hosts || true; } > "$t"; cat "$t" > /etc/hosts; rm -f "$t"`, "")
	code, res := call(t, e.enrollReq(pub))
	wantOK(t, code, res)
	if got := mustRun(t, adm, `grep '# sitate-egress$' /etc/hosts`, ""); got != e.hookIP+" "+e.hookHost+" # sitate-egress" {
		t.Errorf("/etc/hosts の固定: %q", got)
	}
}
