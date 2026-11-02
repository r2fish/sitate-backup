package format

// 骨格(作業指示書 5.1): 契約のインターフェースを満たし、NOT_IMPLEMENTED を返す。

import (
	"context"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Codec は contract.Codec の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type Codec struct{}

var _ contract.Codec = (*Codec)(nil)

// Chunk は contract.Codec を満たす。
func (*Codec) Chunk(ctx context.Context, r io.Reader, emit func(contract.Chunk) error) error {
	return contract.ErrNotImplemented("format.Codec.Chunk")
}

// Seal は contract.Codec を満たす。
func (*Codec) Seal(plain, chunkKey []byte, chunkID string) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Codec.Seal")
}

// Open は contract.Codec を満たす。
func (*Codec) Open(sealed, chunkKey []byte, chunkID string) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Codec.Open")
}

// Encode は contract.Codec を満たす。
func (*Codec) Encode(kind, id string, h contract.Header, content, key []byte) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Codec.Encode")
}

// Decode は contract.Codec を満たす。
func (*Codec) Decode(kind, id string, sealed, key []byte) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Codec.Decode")
}

// Header は contract.Codec を満たす。
func (*Codec) Header(b []byte) (contract.Header, error) {
	var r0 contract.Header
	return r0, contract.ErrNotImplemented("format.Codec.Header")
}

// AAD は contract.Codec を満たす。
func (*Codec) AAD(kind, id string, h contract.Header) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Codec.AAD")
}

// Hash は contract.Codec を満たす。
func (*Codec) Hash(content []byte) string {
	panic(contract.ErrNotImplemented("format.Codec.Hash"))
}

// Crypto は contract.Crypto の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type Crypto struct{}

var _ contract.Crypto = (*Crypto)(nil)

// AEADSeal は contract.Crypto を満たす。
func (*Crypto) AEADSeal(plain, key, aad []byte) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Crypto.AEADSeal")
}

// AEADOpen は contract.Crypto を満たす。
func (*Crypto) AEADOpen(sealed, key, aad []byte) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Crypto.AEADOpen")
}

// HKDF は contract.Crypto を満たす。
func (*Crypto) HKDF(ikm, info []byte, n int) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Crypto.HKDF")
}

// Argon2id は contract.Crypto を満たす。
func (*Crypto) Argon2id(phrase, salt []byte, memKiB, iterations, parallelism int) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Crypto.Argon2id")
}

// BLAKE3 は contract.Crypto を満たす。
func (*Crypto) BLAKE3(content []byte) []byte {
	panic(contract.ErrNotImplemented("format.Crypto.BLAKE3"))
}

// SignProof は contract.Crypto を満たす。
func (*Crypto) SignProof(record []byte, key []byte) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("format.Crypto.SignProof")
}

// VerifyProof は contract.Crypto を満たす。
func (*Crypto) VerifyProof(record, sig, pub []byte) (bool, error) {
	var r0 bool
	return r0, contract.ErrNotImplemented("format.Crypto.VerifyProof")
}
