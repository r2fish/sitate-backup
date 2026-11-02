package keys

// 信頼の起点(spec 3.2.1 の配布署名の形): minisign 互換、方式 ED(BLAKE2b-512 に署名)。trusted comment は version・arch・path の 3 つを持ち、
// 期待値と完全に一致しなければ署名が合っても使わない。公開鍵が複数あるときは、署名ファイルの鍵 ID と一致する鍵で検証し、一致する鍵が無ければ使わない。

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/blake2b"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	sigAlgoPrehash   = "ED"
	sigKeyIDLen      = 8
	sigUntrustedHead = "untrusted comment:"
	sigTrustedHead   = "trusted comment: "
)

type anchorKey struct {
	id  [sigKeyIDLen]byte
	pub ed25519.PublicKey
}

// parseAnchor は minisign の公開鍵 1 行(base64 の 2 バイトの方式 "Ed"、8 バイトの鍵 ID、32 バイトの鍵)を読む。
func parseAnchor(line string) (anchorKey, bool) {
	var a anchorKey
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line))
	if err != nil || len(b) != 2+sigKeyIDLen+ed25519.PublicKeySize || string(b[:2]) != "Ed" {
		return a, false
	}
	copy(a.id[:], b[2:2+sigKeyIDLen])
	a.pub = ed25519.PublicKey(append([]byte(nil), b[2+sigKeyIDLen:]...))
	return a, true
}

type parsedSig struct {
	keyID          [sigKeyIDLen]byte
	sig            []byte
	trustedComment string
	global         []byte
}

// parseSig は署名ファイル(4 行: untrusted comment、署名、trusted comment、全体の署名)を読む。
func parseSig(file []byte) (parsedSig, bool) {
	var p parsedSig
	lines := strings.Split(strings.TrimRight(string(file), "\r\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], sigUntrustedHead) || !strings.HasPrefix(lines[2], sigTrustedHead) {
		return p, false
	}
	sb, err1 := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	gb, err2 := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err1 != nil || err2 != nil || len(sb) != 2+sigKeyIDLen+ed25519.SignatureSize || string(sb[:2]) != sigAlgoPrehash || len(gb) != ed25519.SignatureSize {
		return p, false
	}
	copy(p.keyID[:], sb[2:2+sigKeyIDLen])
	p.sig = sb[2+sigKeyIDLen:]
	p.trustedComment = strings.TrimPrefix(lines[2], sigTrustedHead)
	p.global = gb
	return p, true
}

// Verify は target に対する署名ファイル sig を信頼の起点の鍵で検証し、trusted comment が want と一致することも確かめる。
// 署名として不正・鍵が起点に無い・期待値と違うときは (false, nil)。
func (v *Verifier) Verify(_ context.Context, target, sig []byte, want contract.TrustedComment) (bool, error) {
	anchors := v.Anchors
	if anchors == nil {
		anchors = contract.TrustAnchors()
	}
	p, ok := parseSig(sig)
	if !ok {
		return false, nil
	}
	if p.trustedComment != "version="+want.Version+" arch="+want.Arch+" path="+want.Path {
		return false, nil
	}
	h := blake2b.Sum512(target)
	for _, line := range anchors {
		a, ok := parseAnchor(line)
		if !ok || a.id != p.keyID {
			continue
		}
		if !ed25519.Verify(a.pub, h[:], p.sig) {
			return false, nil
		}
		return ed25519.Verify(a.pub, append(append([]byte(nil), p.sig...), p.trustedComment...), p.global), nil
	}
	return false, nil
}
