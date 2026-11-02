package format

// 暗号部品(spec 4.3): XChaCha20-Poly1305、HKDF-SHA-256、Argon2id、BLAKE3、Ed25519 の薄い包み。

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Crypto は contract.Crypto の実物。
type Crypto struct{}

var _ contract.Crypto = (*Crypto)(nil)

// AEADSeal は plain を key と aad で封緘し、nonce(24) ∥ 暗号文 ∥ タグ(16) を返す。ノンスは呼び出しごとの乱数。
func (*Crypto) AEADSeal(plain, key, aad []byte) ([]byte, error) {
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("format: 鍵: %w", err)
	}
	out := make([]byte, contract.NonceSize, contract.NonceSize+len(plain)+contract.TagSize)
	if _, err := io.ReadFull(rand.Reader, out); err != nil {
		return nil, fmt.Errorf("format: 乱数: %w", err)
	}
	return a.Seal(out, out[:contract.NonceSize], plain, aad), nil
}

// AEADOpen は AEADSeal の出力を開く。鍵・AAD・中身のどれが違っても誤りを返す。
func (*Crypto) AEADOpen(sealed, key, aad []byte) ([]byte, error) {
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("format: 鍵: %w", err)
	}
	if len(sealed) < contract.NonceSize+contract.TagSize {
		return nil, errors.New("format: 暗号文が短い")
	}
	return a.Open(nil, sealed[:contract.NonceSize], sealed[contract.NonceSize:], aad)
}

// HKDF は HKDF-SHA-256(salt は空)で ikm から info の鍵を n バイト導出する。
func (*Crypto) HKDF(ikm, info []byte, n int) ([]byte, error) {
	if n <= 0 {
		return nil, errors.New("format: 出力の長さが不正")
	}
	out := make([]byte, n)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, nil, info), out); err != nil {
		return nil, fmt.Errorf("format: HKDF: %w", err)
	}
	return out, nil
}

// Argon2id は Argon2id(版 0x13)で 32 バイトの KEK を導出する。見出しの値は認証の前に読むので(偽の版を置ける)、
// 上限を超える値はメモリを確保する前に FormatError{header} で拒否する(spec 4.3)。
func (*Crypto) Argon2id(phrase, salt []byte, memKiB, iterations, parallelism int) ([]byte, error) {
	if memKiB <= 0 || iterations <= 0 || parallelism <= 0 {
		return nil, errors.New("format: Argon2id のパラメータが不正")
	}
	if memKiB > contract.Argon2idMemoryMaxKiB || iterations > contract.Argon2idIterationsMax || parallelism > contract.Argon2idParallelismMax {
		return nil, headerErr()
	}
	return argon2.IDKey(phrase, salt, uint32(iterations), uint32(memKiB), uint8(parallelism), contract.KeySize), nil
}

// BLAKE3 は content の BLAKE3(32 バイト)。
func (*Crypto) BLAKE3(content []byte) []byte {
	s := blake3.Sum256(content)
	return s[:]
}

// SignProof は record に Ed25519 で署名する。key は 32 バイトのシード。
func (*Crypto) SignProof(record []byte, key []byte) ([]byte, error) {
	if len(key) != ed25519.SeedSize {
		return nil, errors.New("format: 署名鍵のシードの長さが不正")
	}
	return ed25519.Sign(ed25519.NewKeyFromSeed(key), record), nil
}

// VerifyProof は sig が pub による record の署名かを返す。pub の長さが不正なら誤り。
func (*Crypto) VerifyProof(record, sig, pub []byte) (bool, error) {
	if len(pub) != ed25519.PublicKeySize {
		return false, errors.New("format: 公開鍵の長さが不正")
	}
	return ed25519.Verify(pub, record, sig), nil
}
