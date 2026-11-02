package prices

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract/harness"
	"golang.org/x/crypto/blake2b"
)

// 署名の検証(spec 7.2 の価格表の形。配布署名は minisign の方式 ED、trusted comment は version=<版> arch=any path=prices/prices.json)。
// 検証器はここに最小のものを持つ(他 WP のパッケージを import しない。作業指示書 1 章)。

const trusted = "version=0.0.0-test arch=any path=prices/prices.json"

func verifySig(pub string, target, sig []byte, wantComment string) bool {
	pb, err := base64.StdEncoding.DecodeString(pub)
	if err != nil || len(pb) != 42 || string(pb[:2]) != "Ed" {
		return false
	}
	lines := strings.Split(strings.TrimRight(string(sig), "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[2], "trusted comment: ") {
		return false
	}
	sb, err1 := base64.StdEncoding.DecodeString(lines[1])
	gb, err2 := base64.StdEncoding.DecodeString(lines[3])
	if err1 != nil || err2 != nil || len(sb) != 74 || string(sb[:2]) != "ED" || string(sb[2:10]) != string(pb[2:10]) {
		return false
	}
	comment := strings.TrimPrefix(lines[2], "trusted comment: ")
	if comment != wantComment {
		return false
	}
	h := blake2b.Sum512(target)
	key := ed25519.PublicKey(pb[10:])
	return ed25519.Verify(key, h[:], sb[10:]) && ed25519.Verify(key, append(append([]byte(nil), sb[10:]...), comment...), gb)
}

func TestPricesSignature(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	key := harness.NewMinisignKey(seed)
	body := readFile(t, "prices.json")
	sig := key.Sign(body, trusted)
	if !verifySig(key.Public(), body, sig, trusted) {
		t.Fatal("使い捨ての鍵の署名が検証できない")
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)/2] ^= 1
	other := harness.NewMinisignKey(append([]byte{9}, seed[1:]...))
	cases := map[string]bool{
		"本体を 1 バイト変えた":               verifySig(key.Public(), tampered, sig, trusted),
		"trusted comment の path が違う": verifySig(key.Public(), body, key.Sign(body, strings.Replace(trusted, "prices/", "x/", 1)), trusted),
		"trusted comment の版が違う":      verifySig(key.Public(), body, key.Sign(body, strings.Replace(trusted, "0.0.0-test", "9.9.9", 1)), trusted),
		"別の鍵の署名":                     verifySig(other.Public(), body, sig, trusted),
	}
	// 署名の行を書き換えた(検証器は comment・方式・key id・global signature を別々に見る)。
	evil := strings.Replace(trusted, "prices/prices.json", "prices/other.json", 1)
	cases["comment の行だけ書き換えた(global signature は元のまま)"] = verifySig(key.Public(), body, rewriteSig(t, sig, func(l []string, _ []byte) { l[2] = "trusted comment: " + evil }), evil)
	cases["方式が Ed の署名"] = verifySig(key.Public(), body, rewriteSig(t, sig, func(l []string, sb []byte) { sb[0] = 'E'; sb[1] = 'd'; l[1] = base64.StdEncoding.EncodeToString(sb) }), trusted)
	cases["key id が違う署名"] = verifySig(key.Public(), body, rewriteSig(t, sig, func(l []string, sb []byte) { sb[2] ^= 1; l[1] = base64.StdEncoding.EncodeToString(sb) }), trusted)
	cases["global signature だけ壊した"] = verifySig(key.Public(), body, rewriteSig(t, sig, func(l []string, _ []byte) {
		gb, _ := base64.StdEncoding.DecodeString(l[3])
		gb[0] ^= 1
		l[3] = base64.StdEncoding.EncodeToString(gb)
	}), trusted)
	for name, ok := range cases {
		if ok {
			t.Errorf("検証が通ってしまった: %s", name)
		}
	}
}

// rewriteSig は署名ファイルの 4 行(l)と本体署名の 74 バイト(sb)を f で書き換えて組み直す。
func rewriteSig(t *testing.T, sig []byte, f func(lines []string, sb []byte)) []byte {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(sig), "\n"), "\n")
	sb, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	f(lines, sb)
	return []byte(strings.Join(lines, "\n") + "\n")
}
