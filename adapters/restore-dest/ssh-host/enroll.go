package main

import (
	"context"
	"net"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// opEnroll は復元先の登録(spec 3.5 の「復元先の登録」)。管理用アカウント(dest.at の <user>)に初期資格情報(枠 dest_key)で入り、
// 専用ユーザー sitate-restore・その authorized_keys(restrict 付き)・/etc/hosts の固定・nft による隔離を置く。
// 終了 0 の結果は前提の全項目が ok のときだけ。途中まで置いた物は戻さない(再登録が同じ状態に揃える)。
func opEnroll(req *contract.RestoreDestRequest) (map[string]any, *adapterErr) {
	t, ok := parseTarget(req.Dest.At)
	auth, hasAuth := authFromSecret(req.Secrets["dest_key"], true)
	if !ok || t.user == "" || !hasAuth || !pubKeyRe.MatchString(req.AuthorizedKey) ||
		req.AllowSSHFrom != nil && (len(req.AllowSSHFrom) == 0 || !validAddrList(req.AllowSSHFrom)) || // 省くのは良いが、空の一覧では隔離を作れない
		!validAddrList(req.EgressAllow) || !hostRe.MatchString(req.EgressHost) {
		return nil, internalErr("request")
	}
	// 要求の host_key_fp があれば、接続したホストのホスト鍵を照らしてから初期資格情報を使う(違えば認証に進まず hostkey_mismatch。spec 3.2.4)。
	pin := ""
	if req.HostKeyFP != nil {
		pin = *req.HostKeyFP
	}
	c, e := dial(context.Background(), t, t.user, auth, pin)
	if e != nil {
		return nil, e
	}
	defer c.close()
	allow, observed, e := resolveAllow(c, req.AllowSSHFrom) // sudo に入る前に、同じ接続で読む(sudo の下では環境が消える)
	if e != nil {
		return nil, e
	}
	c.sudo = t.user != "root"
	pr, e := probeAdmin(c)
	if e != nil {
		return nil, e
	}
	if m := missing(pr.prereqs, "subuid"); len(m) > 0 {
		return nil, prereqErr(pr.prereqs)
	}
	rules := renderNft(t.port, allow, req.EgressAllow, pr.ns)
	if e := setupHost(c, req, rules); e != nil {
		return nil, e
	}
	pr, e = probeAdmin(c)
	if e != nil {
		return nil, e
	}
	if len(missing(pr.prereqs)) > 0 {
		return nil, prereqErr(pr.prereqs)
	}
	var obs any // 読めなければ null
	if observed != "" {
		obs = observed
	}
	return map[string]any{"host_key_fp": c.seenFP, "user": restoreUser, "observed_from": obs, "prereq": pr.prereqs}, nil
}

// observedFrom は接続の SSH_CONNECTION の先頭(この接続の送信元アドレス)を、sudo を介さずに読む。読めない・アドレスでないときは空。
func observedFrom(c *conn) string {
	s := c.sudo
	c.sudo = false
	defer func() { c.sudo = s }()
	r, err := c.run(`printf '%s' "${SSH_CONNECTION:-}"`, "")
	if err != nil || r.code != 0 {
		return ""
	}
	f := strings.Fields(r.out)
	if len(f) == 0 || net.ParseIP(f[0]) == nil || len(f[0]) > 45 {
		return ""
	}
	return f[0]
}

// resolveAllow は受信を許す送信元を決める。要求の allow_ssh_from があればそれ、無ければ観測した送信元(読めなければ no_observed_from)。
// 2 つ目の返り値は観測した送信元(結果の observed_from。読めなければ空)。
func resolveAllow(c *conn, given []string) ([]string, string, *adapterErr) {
	obs := observedFrom(c)
	if given != nil {
		return given, obs, nil
	}
	if obs == "" {
		return nil, "", codeErr("no_observed_from")
	}
	return []string{obs}, obs, nil
}

func probeAdmin(c *conn) (probeInfo, *adapterErr) {
	r, err := c.run(probeScript, "")
	if err != nil || r.code != 0 {
		return probeInfo{}, codeErr("enroll_failed")
	}
	return parseProbe(r.out), nil
}

// setupHost は専用ユーザー・鍵・/etc/hosts・隔離を置く。
func setupHost(c *conn, req *contract.RestoreDestRequest, rules string) *adapterErr {
	var ips []string
	for _, x := range req.EgressAllow {
		if net.ParseIP(x) != nil { // CIDR は /etc/hosts に固定しない
			ips = append(ips, x)
		}
	}
	r, err := c.run(enrollSetupScript, rules, "restrict "+req.AuthorizedKey, req.EgressHost, strings.Join(ips, " "))
	if err != nil || r.code != 0 {
		return codeErr("enroll_failed")
	}
	return nil
}
