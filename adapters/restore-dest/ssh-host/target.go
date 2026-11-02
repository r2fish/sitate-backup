package main

import (
	"net"
	"net/url"
	"regexp"
	"strconv"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	restoreUser   = "sitate-restore" // 専用ユーザー(spec 3.5 の「復元先の登録」)
	bytesPerGB    = int64(1) << 30
	defaultSSHTCP = 22
)

// sessionIDRe は run_id の形(spec 3.1.1)。資源の識別に使えるのはこの形の session_id だけ。
// 空・パスの区切りを含むもの・形の違うものは、ここで拒むことで別の資源の破棄に繋がらないようにする。
var sessionIDRe = regexp.MustCompile(`^r-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$`)

func validSessionID(s string) bool { return sessionIDRe.MatchString(s) }

// target は dest.at(ssh://<user>@<host>:<port>)の中身。
type target struct {
	user string
	host string
	port int
}

func (t target) addr() string { return net.JoinHostPort(t.host, strconv.Itoa(t.port)) }

// parseTarget は dest.at を読む。ポートの省略は 22、IPv6 は [...] で囲む(spec 3.5)。
func parseTarget(at string) (target, bool) {
	u, err := url.Parse(at)
	if err != nil || u.Scheme != "ssh" || u.Hostname() == "" || u.Path != "" && u.Path != "/" {
		return target{}, false
	}
	t := target{user: u.User.Username(), host: u.Hostname(), port: defaultSSHTCP}
	if _, has := u.User.Password(); has {
		return target{}, false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return target{}, false
		}
		t.port = n
	}
	return t, true
}

// validAddrList は allow_ssh_from・egress_allow の各要素(IP アドレスか CIDR)を確かめる。
func validAddrList(xs []string) bool {
	for _, x := range xs {
		if net.ParseIP(x) == nil {
			if _, _, err := net.ParseCIDR(x); err != nil {
				return false
			}
		}
	}
	return true
}

// ipInList は ip が一覧(IP アドレスか CIDR)のどれかに含まれるか。
func ipInList(ip net.IP, xs []string) bool {
	for _, x := range xs {
		if p := net.ParseIP(x); p != nil {
			if p.Equal(ip) {
				return true
			}
		} else if _, n, err := net.ParseCIDR(x); err == nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

// resourcesConsistent は要求の resources[] を session_id と突き合わせる。workdir・scratch の id は session_id と一致しなければならず、
// 食い違い(台帳の識別の誤り)は要求の不正として何も触らない。container・process の id は破棄の対象の決定に使わない。
func resourcesConsistent(sid string, rs []contract.AdapterResource) bool {
	for _, r := range rs {
		if (r.Type == "workdir" || r.Type == "scratch") && r.ID != sid {
			return false
		}
	}
	return true
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)
