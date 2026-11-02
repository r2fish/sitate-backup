package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestDispatchOps(t *testing.T) {
	base := func(op string) map[string]any {
		return map[string]any{"op": op, "session_id": sidA, "dest": map[string]any{"at": "ssh://x@h:22", "key_ref": "k"}, "host_key_fp": nil,
			"resources": []any{}, "authorized_key": "ssh-ed25519 AAAA", "allow_ssh_from": []string{"192.0.2.1"}, "secrets": map[string]string{}}
	}
	// この組(起動 / 停止)に無い操作は要求の不正、後のイテレーションの操作は NOT_IMPLEMENTED(detail.feature に操作名)。
	for _, op := range []string{"create", "destroy"} {
		code, res := runRaw(t, base(op))
		e := wantErr(t, code, res, "INTERNAL")
		if e["detail"].(map[string]any)["reason"] != "request" {
			t.Errorf("%s: %v", op, e)
		}
	}
	for _, op := range []string{"expose", "release"} {
		code, res := runRaw(t, base(op))
		e := wantErr(t, code, res, "NOT_IMPLEMENTED")
		if e["detail"].(map[string]any)["feature"] != op {
			t.Errorf("%s: %v", op, e)
		}
	}
	code, res := runRaw(t, base("bogus"))
	wantErr(t, code, res, "INTERNAL")
}

func TestBadInput(t *testing.T) {
	for _, in := range []string{"", "not json\n", "{}\n", `{"contract":"sitate.restore-dest/v0","op":"stop"}` + "\n"} {
		var out bytes.Buffer
		if code := run(strings.NewReader(in), &out); code != 1 || !strings.Contains(out.String(), `"code":"INTERNAL"`) {
			t.Errorf("入力 %q: exit=%d %s", in, code, out.String())
		}
	}
	var out bytes.Buffer
	run(strings.NewReader(`{"contract":"sitate.restore-dest/v0","op":"stop"}`+"\n"), &out)
	if !strings.Contains(out.String(), `"reason":"contract"`) {
		t.Errorf("contract の版が違う要求: %s", out.String())
	}
}

// 要求の不備は、接続の前に INTERNAL(request)で止まる(秘密を使わない)。
func TestStartEnrollRequestChecks(t *testing.T) {
	good := map[string]any{"op": "start", "session_id": sidA, "dest": map[string]any{"at": "ssh://u@h.example.invalid:22", "key_ref": "k"},
		"needs": map[string]any{"disk_gb": 1}, "host_key_fp": "SHA256:abc", "authorized_key": "ssh-ed25519 AAAA", "allow_ssh_from": []string{"192.0.2.1"},
		"egress_allow": []string{"198.51.100.7"}, "egress_host": "s3.example.invalid", "secrets": map[string]string{"restore-dest-ssh": "not a key"}}
	// 秘密鍵として読めない値でも、値そのものはどこにも出ない。
	code, res := runRaw(t, copyMap(good))
	wantErr(t, code, res, "INTERNAL")
	for k, v := range map[string]any{"session_id": "x", "needs": nil, "egress_allow": []string{"bad addr"}, "egress_host": "-x", "dest": map[string]any{"at": "http://h", "key_ref": "k"}} {
		r := copyMap(good)
		r[k] = v
		code, res := runRaw(t, r)
		wantErr(t, code, res, "INTERNAL")
	}
	en := map[string]any{"op": "enroll", "dest": map[string]any{"at": "ssh://adm@h.example.invalid", "key_ref": "k"}, "authorized_key": "ssh-ed25519 AAAA",
		"allow_ssh_from": []string{}, "egress_allow": []string{"198.51.100.7"}, "egress_host": "s3.example.invalid", "secrets": map[string]string{"dest_key": "pw"}}
	code, res = runRaw(t, en)
	wantErr(t, code, res, "INTERNAL") // allow_ssh_from が空なら隔離を作れない
}

func copyMap(m map[string]any) map[string]any {
	o := map[string]any{}
	for k, v := range m {
		o[k] = v
	}
	return o
}

// 領域を作る前の確認: 前提・ユーザー・sudo・保管先のアドレスが登録時と違えば何も置かずに enroll_mismatch。
func TestFakeStartChecksBeforeCreating(t *testing.T) {
	f, priv := fakeWithKey(t, false)
	req := map[string]any{"op": "start", "session_id": sidA, "dest": f.dest(), "needs": map[string]any{"disk_gb": 0}, "host_key_fp": f.fp,
		"authorized_key": "ssh-ed25519 AAAA", "allow_ssh_from": []string{"192.0.2.1"}, "egress_allow": []string{"198.51.100.7"},
		"egress_host": "localhost", "secrets": map[string]string{"restore-dest-ssh": priv}}
	code, res := call(t, req)
	e := wantErr(t, code, res, "enroll_mismatch")
	mm, _ := e["detail"].(map[string]any)["mismatches"].([]any)
	has := func(s string) bool {
		for _, m := range mm {
			if m == s {
				return true
			}
		}
		return false
	}
	// 試験ホストは専用ユーザーでなく(user)、gocryptfs と nft を持たない(prereq)。保管先のアドレスも固定値と違う(egress_host)。
	if !has("user") || !has("prereq:gocryptfs") || !has("egress_host") {
		t.Errorf("mismatches: %v", mm)
	}
	if _, ok := e["resources"].([]any); !ok {
		t.Errorf("start の失敗は resources[] を載せる: %v", e)
	}
	if got := f.listRun(t); len(got) != 0 {
		t.Errorf("確認に落ちたのに何かが置かれた: %v", got)
	}
}

func TestParseTarget(t *testing.T) {
	cases := []struct {
		at   string
		ok   bool
		want target
	}{
		{"ssh://adm@h.example.invalid", true, target{"adm", "h.example.invalid", 22}},
		{"ssh://adm@h.example.invalid:2222", true, target{"adm", "h.example.invalid", 2222}},
		{"ssh://adm@[2001:db8::1]:2200", true, target{"adm", "2001:db8::1", 2200}},
		{"ssh://adm:pw@h:22", false, target{}},
		{"ssh://adm@h:0", false, target{}},
		{"ssh://adm@h:70000", false, target{}},
		{"http://adm@h", false, target{}},
		{"ssh://adm@h/path", false, target{}},
		{"h", false, target{}},
	}
	for _, c := range cases {
		got, ok := parseTarget(c.at)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%s: %v %+v", c.at, ok, got)
		}
	}
	if (target{host: "2001:db8::1", port: 22}).addr() != "[2001:db8::1]:22" {
		t.Errorf("addr")
	}
}

func TestRenderNft(t *testing.T) {
	r := renderNft(2222, []string{"192.0.2.1", "2001:db8::5"}, []string{"198.51.100.7", "203.0.113.0/24"}, []string{"10.0.0.2"})
	for _, want := range []string{
		"policy drop;", "ip saddr { 192.0.2.1 } tcp dport 2222 accept", "ip6 saddr { 2001:db8::5 } tcp dport 2222 accept",
		"ip daddr { 198.51.100.7, 203.0.113.0/24 } accept", "ip daddr { 10.0.0.2 } udp dport 53 accept", "ip daddr { 10.0.0.2 } tcp dport 53 accept",
		"delete table inet sitate_isolation", "ct state established,related accept",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("ルールセットに %q が無い:\n%s", want, r)
		}
	}
	// 許可していない物(任意の宛先への accept)は入らない。
	if strings.Count(r, "policy drop;") != 2 || strings.Contains(r, "ip6 daddr") {
		t.Errorf("想定外の規則:\n%s", r)
	}
	for _, ln := range strings.Split(r, "\n") {
		if strings.TrimSpace(ln) == "accept" {
			t.Errorf("条件の無い accept: %q", ln)
		}
	}
}

func TestSessionID(t *testing.T) {
	for s, ok := range map[string]bool{sidA: true, "": false, ".": false, "..": false, "r-20261103T010203Z-3f9A": false, sidA + "x": false, " " + sidA: false, "r-*": false} {
		if validSessionID(s) != ok {
			t.Errorf("%q", s)
		}
	}
}

func TestShQuote(t *testing.T) {
	if got := shQuote("a'b$(x)"); got != `'a'\''b$(x)'` {
		t.Errorf("%s", got)
	}
}

func TestIPInList(t *testing.T) {
	l := []string{"198.51.100.7", "203.0.113.0/24"}
	if !ipInList(parseIP("198.51.100.7"), l) || !ipInList(parseIP("203.0.113.9"), l) || ipInList(parseIP("198.51.100.8"), l) {
		t.Errorf("ipInList")
	}
}

// IPv6 の近隣探索を落とすと、許可した相手とも通信できない(IPv4 の ARP は inet の規則を通らない)。入力と出力の両方で許す。
func TestRenderNftNeighborDiscovery(t *testing.T) {
	r := renderNft(22, []string{"2001:db8::5"}, []string{"2001:db8::9"}, []string{"2001:db8::53"})
	line := "icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-advert, nd-router-solicit } ip6 hoplimit 255 accept"
	if strings.Count(r, line) != 2 {
		t.Errorf("近隣探索の許可が入力と出力に 1 つずつ要る:\n%s", r)
	}
}

// 隔離のユニットは、ネットワークの設定より前に動くよう DefaultDependencies=no で sysinit.target に結ぶ。
func TestIsolationUnitText(t *testing.T) {
	for _, want := range []string{"DefaultDependencies=no", "Wants=network-pre.target", "Before=network-pre.target shutdown.target",
		"Conflicts=shutdown.target", "WantedBy=sysinit.target", "systemctl enable sitate-isolation.service", "systemctl restart sitate-isolation.service"} {
		if !strings.Contains(enrollSetupScript, want) {
			t.Errorf("enroll の台本に %q が無い", want)
		}
	}
	if strings.Contains(enrollSetupScript, "multi-user.target") {
		t.Errorf("multi-user.target に結んではいけない")
	}
}

// 時限・再試行は contract の定数で受ける(数値の直書きをしない)。
func TestConnConstants(t *testing.T) {
	if connectTimeout != contract.AdapterConnectTimeout || retryMax != contract.AdapterRetryMax || backoffBase != contract.AdapterBackoffBase {
		t.Errorf("時限の定数が contract と違う")
	}
}

// 専用ユーザーが root 相当(sudo、root と同等の群、root そのもの)なら登録時の状態と一致しない。
func TestCheckRegisteredPrivilege(t *testing.T) {
	req := &contract.RestoreDestRequest{EgressHost: "localhost", EgressAllow: []string{"127.0.0.1", "::1"}}
	ok := probeInfo{uid: 1000, user: restoreUser, sudo: "none", groups: []string{restoreUser}}
	if e := checkRegistered(ok, req); e != nil {
		t.Fatalf("正常な状態が拒否された: %v", e.detail)
	}
	for name, mut := range map[string]func(p *probeInfo){
		"sudo yes": func(p *probeInfo) { p.sudo = "yes" },
		"uid 0":    func(p *probeInfo) { p.uid = 0 },
		"群 sudo":   func(p *probeInfo) { p.groups = append(p.groups, "sudo") },
		"群 docker": func(p *probeInfo) { p.groups = append(p.groups, "docker") },
		"前提の欠け":    func(p *probeInfo) { p.prereqs = []prereq{{Name: "nft"}} },
	} {
		p := ok
		mut(&p)
		if e := checkRegistered(p, req); e == nil || e.code != "enroll_mismatch" {
			t.Errorf("%s: enroll_mismatch を期待", name)
		}
	}
}
