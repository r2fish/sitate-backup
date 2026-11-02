//go:build mainpath

package mainpath

// minisign 互換の署名(方式 ED = BLAKE2b-512 の事前ハッシュに Ed25519)を、使い捨ての鍵で作る。
// 検証は実物の keys.Verifier が行う。署名側は独立に書く(食い違いを見つけるため)。

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/blake2b"
)

type signer struct {
	priv  ed25519.PrivateKey
	keyID [8]byte
}

func newSigner(t *testing.T) *signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &signer{priv: priv}
	if _, err := rand.Read(s.keyID[:]); err != nil {
		t.Fatal(err)
	}
	return s
}

// anchor は信頼の起点の公開鍵 1 行(base64 の "Ed" ∥ 鍵 ID ∥ 公開鍵)。
func (s *signer) anchor() string {
	pub := s.priv.Public().(ed25519.PublicKey)
	b := append(append([]byte("Ed"), s.keyID[:]...), pub...)
	return base64.StdEncoding.EncodeToString(b)
}

// sign は署名ファイル(4 行)を返す。trusted は "version=… arch=… path=…"。
func (s *signer) sign(msg []byte, trusted string) []byte {
	h := blake2b.Sum512(msg)
	sig := ed25519.Sign(s.priv, h[:])
	global := ed25519.Sign(s.priv, append(append([]byte(nil), sig...), trusted...))
	line := base64.StdEncoding.EncodeToString(append(append([]byte("ED"), s.keyID[:]...), sig...))
	return []byte("untrusted comment: signature from mainpath harness\n" + line + "\ntrusted comment: " + trusted + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n")
}
