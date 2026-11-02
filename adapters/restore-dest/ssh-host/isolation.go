package main

import (
	"fmt"
	"net"
	"strings"
)

// renderNft は隔離の nft のルールセットを作る(spec 3.5 の「復元先の登録」の (3))。
// 受信は sshFrom からの SSH(sshPort)だけ、送信は egress と名前解決(resolvers の 53)だけ。確立済みの応答と lo は通す。
// IPv6 の近隣探索(ICMPv6)は入力と出力で通す(通さないと許可した相手とも IPv6 で通信できない。IPv4 の ARP はこの表を通らない)。経路外からの偽装は hoplimit 255 で落とす。
// DHCP の更新(udp 67/68、547/546)は通す(通さないとリースが切れて到達できなくなる)。専用のテーブルだけを作り直し、ほかの規則には触れない。
const ndRule = "    icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-advert, nd-router-solicit } ip6 hoplimit 255 accept\n"

func renderNft(sshPort int, sshFrom, egress, resolvers []string) string {
	var b strings.Builder
	b.WriteString("table inet sitate_isolation\ndelete table inet sitate_isolation\ntable inet sitate_isolation {\n")
	b.WriteString("  chain input {\n    type filter hook input priority 0; policy drop;\n    iifname \"lo\" accept\n    ct state established,related accept\n")
	b.WriteString(ndRule)
	for _, f := range split46(sshFrom) {
		fmt.Fprintf(&b, "    %s saddr { %s } tcp dport %d accept\n", f.fam, strings.Join(f.addrs, ", "), sshPort)
	}
	b.WriteString("    udp sport 67 udp dport 68 accept\n    udp sport 547 udp dport 546 accept\n  }\n")
	b.WriteString("  chain output {\n    type filter hook output priority 0; policy drop;\n    oifname \"lo\" accept\n    ct state established,related accept\n")
	b.WriteString(ndRule)
	b.WriteString("    udp sport 68 udp dport 67 accept\n    udp sport 546 udp dport 547 accept\n")
	for _, f := range split46(resolvers) {
		fmt.Fprintf(&b, "    %s daddr { %s } udp dport 53 accept\n    %s daddr { %s } tcp dport 53 accept\n", f.fam, strings.Join(f.addrs, ", "), f.fam, strings.Join(f.addrs, ", "))
	}
	for _, f := range split46(egress) {
		fmt.Fprintf(&b, "    %s daddr { %s } accept\n", f.fam, strings.Join(f.addrs, ", "))
	}
	b.WriteString("  }\n}\n")
	return b.String()
}

type famAddrs struct {
	fam   string // ip / ip6
	addrs []string
}

// split46 は IP アドレスか CIDR の一覧を IPv4 と IPv6 に分ける。空の族は出さない。
func split46(xs []string) []famAddrs {
	var v4, v6 []string
	for _, x := range xs {
		ip := net.ParseIP(x)
		if ip == nil {
			ip, _, _ = net.ParseCIDR(x)
		}
		switch {
		case ip == nil:
		case ip.To4() != nil:
			v4 = append(v4, x)
		default:
			v6 = append(v6, x)
		}
	}
	var out []famAddrs
	if len(v4) > 0 {
		out = append(out, famAddrs{"ip", v4})
	}
	if len(v6) > 0 {
		out = append(out, famAddrs{"ip6", v6})
	}
	return out
}
