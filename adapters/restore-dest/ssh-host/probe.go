package main

import (
	"net"
	"strconv"
	"strings"
)

// probeInfo は probeScript の出力。
type probeInfo struct {
	uid     int
	user    string
	home    string
	groups  []string
	prereqs []prereq
	sudo    string // yes / none(専用ユーザーのときだけ)
	freeKB  int64
	ns      []string // /etc/resolv.conf の nameserver(enroll の送信の許可に使う)
}

func parseProbe(out string) probeInfo {
	p := probeInfo{uid: -1}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "uid":
			if n, err := strconv.Atoi(f[1]); err == nil {
				p.uid = n
			}
		case "user":
			p.user = f[1]
		case "home":
			p.home = f[1]
		case "groups":
			p.groups = strings.Split(f[1], ",")
		case "p":
			if len(f) == 3 {
				p.prereqs = append(p.prereqs, prereq{Name: f[1], OK: f[2] == "ok"})
			}
		case "sudo":
			p.sudo = f[1]
		case "free_kb":
			p.freeKB, _ = strconv.ParseInt(f[1], 10, 64)
		case "ns":
			if net.ParseIP(f[1]) != nil {
				p.ns = append(p.ns, f[1])
			}
		}
	}
	return p
}

// missing は ok でない前提の名前の一覧。skip の名前は数えない。
func missing(ps []prereq, skip ...string) []string {
	var out []string
	for _, p := range ps {
		if p.OK || contains(skip, p.Name) {
			continue
		}
		out = append(out, p.Name)
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
