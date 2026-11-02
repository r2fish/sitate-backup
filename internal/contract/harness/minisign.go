package harness

import (
	"crypto/ed25519"
	"encoding/base64"
	"golang.org/x/crypto/blake2b"
)

// MinisignKey は使い捨ての署名鍵(契約試験用)。公開鍵の符号化は spec 3.2.1(Ed ∥ 鍵 ID(8) ∥ 公開鍵(32) の base64)。
type MinisignKey struct {
	priv  ed25519.PrivateKey
	keyID [8]byte
}

// NewMinisignKey は seed(32 バイト)から鍵を作る。
func NewMinisignKey(seed []byte) *MinisignKey {
	k := &MinisignKey{priv: ed25519.NewKeyFromSeed(seed)}
	copy(k.keyID[:], seed[:8])
	return k
}

// Public は公開鍵の 1 行(56 文字)。
func (k *MinisignKey) Public() string {
	b := append(append([]byte("Ed"), k.keyID[:]...), k.priv.Public().(ed25519.PublicKey)...)
	return base64.StdEncoding.EncodeToString(b)
}

// Sign は対象の署名ファイルの中身を返す(方式 ED: BLAKE2b-512 に署名し、trusted comment も署名する)。
func (k *MinisignKey) Sign(target []byte, trustedComment string) []byte {
	h := blake2b.Sum512(target)
	sig := ed25519.Sign(k.priv, h[:])
	sigB := append(append([]byte("ED"), k.keyID[:]...), sig...)
	global := ed25519.Sign(k.priv, append(append([]byte{}, sig...), trustedComment...))
	out := "untrusted comment: signature from test key\n" + base64.StdEncoding.EncodeToString(sigB) +
		"\ntrusted comment: " + trustedComment + "\n" + base64.StdEncoding.EncodeToString(global) + "\n"
	return []byte(out)
}
