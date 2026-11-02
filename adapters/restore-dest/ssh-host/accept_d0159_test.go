package main

import (
	"strings"
	"testing"
)

// D-0159 の実物の確かめ(受入試験の環境の dest。管理用アカウントは root でなく、実物の sudo が環境を消す)。
// allow_ssh_from を省いた登録で、同じ接続の SSH_CONNECTION の先頭(この試験 = バックアップサーバーの役の送信元)が結果の observed_from に返り、
// 受信許可の規則(/etc/sitate/isolation.nft)に入ること。要求の host_key_fp が違うホストでは初期資格情報を使わず何も変えないこと。
func TestAcceptEnrollObservedFromAndHostKeyPin(t *testing.T) {
	e := loadAccept(t)
	priv, pub := newKeyPair(t)
	bad := e.enrollReq(pub)
	delete(bad, "allow_ssh_from")
	bad["host_key_fp"] = "SHA256:" + strings.Repeat("B", 43)
	code, res := call(t, bad)
	er := wantErr(t, code, res, "hostkey_mismatch")
	if er["seen_fp"] != e.fp {
		t.Errorf("seen_fp %v want %s", er["seen_fp"], e.fp)
	}
	req := e.enrollReq(pub)
	delete(req, "allow_ssh_from")
	req["host_key_fp"] = e.fp
	code, res = call(t, req)
	wantOK(t, code, res)
	if res["observed_from"] != e.srcIP || res["host_key_fp"] != e.fp {
		t.Fatalf("enroll の結果: %v (送信元 %s)", res, e.srcIP)
	}
	resetDest(t, e, priv)
	r, err := e.session(t, priv).run(`cat /etc/sitate/isolation.nft`, "")
	if err != nil || r.code != 0 || !strings.Contains(r.out, e.srcIP) {
		t.Errorf("受信許可の規則に観測した送信元 %s が入っていない: %v %+v", e.srcIP, err, r)
	}
	// 初回の登録(host_key_fp を省く)は初回接続のホスト鍵を返す。
	first := e.enrollReq(pub)
	code, res = call(t, first)
	wantOK(t, code, res)
	if res["host_key_fp"] != e.fp {
		t.Errorf("初回接続のホスト鍵: %v", res)
	}
}
