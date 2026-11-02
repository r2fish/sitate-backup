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
	if !ok || t.user == "" || !hasAuth || !pubKeyRe.MatchString(req.AuthorizedKey) || len(req.AllowSSHFrom) == 0 ||
		!validAddrList(req.AllowSSHFrom) || !validAddrList(req.EgressAllow) || !hostRe.MatchString(req.EgressHost) {
		return nil, internalErr("request")
	}
	c, e := dial(context.Background(), t, t.user, auth, "")
	if e != nil {
		return nil, e
	}
	defer c.close()
	c.sudo = t.user != "root"
	pr, e := probeAdmin(c)
	if e != nil {
		return nil, e
	}
	if m := missing(pr.prereqs, "subuid"); len(m) > 0 {
		return nil, prereqErr(pr.prereqs)
	}
	rules := renderNft(t.port, req.AllowSSHFrom, req.EgressAllow, pr.ns)
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
	return map[string]any{"host_key_fp": c.seenFP, "user": restoreUser, "prereq": pr.prereqs}, nil
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
