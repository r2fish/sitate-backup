package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// opStart は登録時の設定の確認と使い捨て暗号化領域の作成(spec 3.2.4 の「start の確認」)。電源は入れない。
// ホスト鍵は要求の host_key_fp(登録時の固定値)と一致するときだけ接続する。領域を作る前に確認がすべて通ること。
func opStart(req *contract.RestoreDestRequest) (map[string]any, *adapterErr) {
	t, ok := parseTarget(req.Dest.At)
	if !ok || req.SessionID == nil || !validSessionID(*req.SessionID) || req.Needs == nil || req.Needs.DiskGB < 0 ||
		!validAddrList(req.EgressAllow) || !hostRe.MatchString(req.EgressHost) {
		return nil, internalErr("request")
	}
	sid := *req.SessionID
	c, e := dedicated(req, req.HostKeyFP)
	if e != nil {
		return nil, e.withResources(nil)
	}
	defer c.close()
	pr, e := probeUser(c)
	if e != nil {
		return nil, e.withResources(nil)
	}
	if e := checkRegistered(pr, req); e != nil {
		return nil, e.withResources(nil)
	}
	if free := pr.freeKB * 1024; free < req.Needs.DiskGB*bytesPerGB {
		return nil, diskShortErr(req.Needs.DiskGB, free/bytesPerGB).withResources(nil)
	}
	w, res, e := createScratch(c, sid)
	if e != nil {
		return nil, e.withResources(res)
	}
	return map[string]any{
		"session_id":  sid,
		"ssh":         map[string]any{"host": t.host, "port": t.port, "user": restoreUser},
		"host_key_fp": c.seenFP,
		"scratch":     map[string]any{"path": w + "/scratch", "method": contract.RestoreScratchMethod},
		"resources":   res,
	}, nil
}

func probeUser(c *conn) (probeInfo, *adapterErr) {
	r, err := c.run(probeScript, "")
	if err != nil || r.code != 0 {
		return probeInfo{}, unreachableErr("connect")
	}
	return parseProbe(r.out), nil
}

// checkRegistered は登録時の状態との一致を確かめる: 専用ユーザーが root でなく sudo を持たないこと、前提(enroll の prereq[] と同じ)、
// egress_host をこの(バックアップサーバー)側で名前解決した IP アドレスが egress_allow(登録時の固定値)に 1 つも含まれないときだけ保管先のアドレスが変わったとみなす。
func checkRegistered(pr probeInfo, req *contract.RestoreDestRequest) *adapterErr {
	var mm []string
	if pr.uid == 0 || pr.user != restoreUser {
		mm = append(mm, "user")
	}
	if pr.sudo != "none" || contains(pr.groups, "sudo") || contains(pr.groups, "wheel") || contains(pr.groups, "admin") ||
		contains(pr.groups, "docker") || contains(pr.groups, "lxd") || contains(pr.groups, "disk") {
		mm = append(mm, "sudo")
	}
	for _, n := range missing(pr.prereqs) {
		mm = append(mm, "prereq:"+n)
	}
	if !egressUnchanged(req.EgressHost, req.EgressAllow) {
		mm = append(mm, "egress_host")
	}
	if len(mm) > 0 {
		return mismatchErr(mm)
	}
	return nil
}

func egressUnchanged(host string, fixed []string) bool {
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil || len(ips) == 0 {
		return false // 確かめられないときは変わったとみなして止める
	}
	for _, ip := range ips {
		if ipInList(ip.IP, fixed) {
			return true
		}
	}
	return false
}

// createScratch は作業ディレクトリと使い捨ての gocryptfs 領域を作る。パスワードはここで作り、SSH の標準入力で渡すだけでどこにも書かない。
// 失敗しても、そこまでに作った資源を返す(核が同じ session_id で stop する)。
func createScratch(c *conn, sid string) (string, []contract.AdapterResource, *adapterErr) {
	none := []contract.AdapterResource{}
	r, err := c.run(scratchCreateScript, "", sid)
	if err != nil || r.code != 0 {
		if err != nil || r.code == scratchAfterWorkdir { // 作業ディレクトリを作った後の失敗(切断を含む)は、作った物を返して核の stop に任せる
			return "", []contract.AdapterResource{{Type: "workdir", ID: sid}}, scratchErr(r.code, err)
		}
		return "", none, scratchErr(r.code, err)
	}
	w := firstLine(r.out)
	res := []contract.AdapterResource{{Type: "workdir", ID: sid}}
	pw, err := genPassword()
	if err != nil {
		return "", res, internalErr("request")
	}
	if r, err := c.run(scratchInitScript, pw+"\n", sid); err != nil || r.code != 0 {
		return "", res, scratchErr(r.code, err)
	}
	res = append(res, contract.AdapterResource{Type: "scratch", ID: sid})
	if r, err := c.run(scratchMountScript, pw+"\n", sid); err != nil || r.code != 0 {
		return "", res, scratchErr(r.code, err)
	}
	return w, res, nil
}

// scratchAfterWorkdir は scratchCreateScript が作業ディレクトリを作った後に失敗したときの終了コード。
const scratchAfterWorkdir = 5

func scratchErr(code int, err error) *adapterErr {
	if err != nil {
		return unreachableErr("connect")
	}
	if code == 3 {
		return customErr("SESSION_EXISTS")
	}
	return customErr("SCRATCH_FAILED")
}

// genPassword は領域のパスワードの作り方(試験が既知の値に差し替えて、どこにも書かれないことを確かめる)。
var genPassword = newPassword

func newPassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
