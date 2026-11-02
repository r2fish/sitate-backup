package harness

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	cdc "github.com/PlakarKorp/go-cdc-chunkers"
	_ "github.com/PlakarKorp/go-cdc-chunkers/chunkers/fastcdc"
	"github.com/klauspost/compress/zstd"
	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/zeebo/blake3"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// RefCodec は contract.Codec の参照実装(契約試験のための組み立て用。製品に入らない。U1a は自分で実装する)。
type RefCodec struct{}

// RefCrypto は contract.Crypto の参照実装。
type RefCrypto struct{}

var _ contract.Codec = (*RefCodec)(nil)
var _ contract.Crypto = (*RefCrypto)(nil)

func (*RefCodec) Chunk(ctx context.Context, r io.Reader, emit func(contract.Chunk) error) error {
	ch, err := cdc.NewChunker("fastcdc", r, &cdc.ChunkerOpts{MinSize: contract.ChunkMin, NormalSize: contract.ChunkAvg, MaxSize: contract.ChunkMax})
	if err != nil {
		return err
	}
	for {
		b, err := ch.Next()
		if len(b) > 0 {
			if e := emit(contract.Chunk{Data: append([]byte{}, b...), Length: len(b)}); e != nil {
				return e
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func refAAD(kind, id string, h contract.Header) ([]byte, error) {
	out := append([]byte{byte(h.FormatVersion)}, kind...)
	if kind == contract.MagicState {
		out = binary.BigEndian.AppendUint16(out, uint16(len(id)))
		out = append(out, id...)
	} else {
		b, err := hex.DecodeString(id)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return append(out, h.Mark...), nil
}
func (*RefCodec) AAD(kind, id string, h contract.Header) ([]byte, error) { return refAAD(kind, id, h) }

func (*RefCrypto) AEADSeal(plain, key, aad []byte) ([]byte, error) {
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	n := make([]byte, 24)
	rand.Read(n)
	return a.Seal(n, n, plain, aad), nil
}
func (*RefCrypto) AEADOpen(sealed, key, aad []byte) ([]byte, error) {
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < 40 {
		return nil, fmt.Errorf("short")
	}
	return a.Open(nil, sealed[:24], sealed[24:], aad)
}
func (*RefCrypto) HKDF(ikm, info []byte, n int) ([]byte, error) {
	out := make([]byte, n)
	_, err := io.ReadFull(hkdf.New(sha256.New, ikm, nil, info), out)
	return out, err
}
func (*RefCrypto) Argon2id(p, s []byte, m, i, par int) ([]byte, error) {
	return argon2.IDKey(p, s, uint32(i), uint32(m), uint8(par), 32), nil
}
func (*RefCrypto) BLAKE3(c []byte) []byte { s := blake3.Sum256(c); return s[:] }
func (*RefCrypto) SignProof(rec, key []byte) ([]byte, error) {
	return ed25519.Sign(ed25519.NewKeyFromSeed(key), rec), nil
}
func (*RefCrypto) VerifyProof(rec, sig, pub []byte) (bool, error) {
	return ed25519.Verify(pub, rec, sig), nil
}
func (*RefCodec) Hash(c []byte) string {
	s := blake3.Sum256(c)
	return "blake3:" + hex.EncodeToString(s[:])
}

func refSeal(head []byte, kind, id string, h contract.Header, plain, key []byte) ([]byte, error) {
	aad, err := refAAD(kind, id, h)
	if err != nil {
		return nil, err
	}
	s, err := (&RefCrypto{}).AEADSeal(plain, key, aad)
	return append(append([]byte{}, head...), s...), err
}

var zenc, _ = zstd.NewWriter(nil)
var zdec, _ = zstd.NewReader(nil)

func (*RefCodec) Seal(plain, key []byte, id string) ([]byte, error) {
	flag := byte(contract.CompressNone)
	body := plain
	if z := zenc.EncodeAll(plain, nil); len(z) < len(plain) {
		flag, body = contract.CompressZstd, z
	}
	h := contract.Header{FormatVersion: contract.FormatVersion, Mark: []byte{flag}}
	return refSeal(append([]byte(contract.MagicChunk), byte(h.FormatVersion), flag), contract.MagicChunk, id, h, body, key)
}
func (c *RefCodec) Open(sealed, key []byte, id string) ([]byte, error) {
	h, err := c.Header(sealed)
	if err != nil {
		return nil, err
	}
	b, err := c.open(sealed[6:], contract.MagicChunk, id, h, key)
	if err != nil {
		return nil, err
	}
	if h.Mark[0] == contract.CompressZstd {
		return zdec.DecodeAll(b, nil)
	}
	return b, nil
}
func (c *RefCodec) open(body []byte, kind, id string, h contract.Header, key []byte) ([]byte, error) {
	aad, err := refAAD(kind, id, h)
	if err != nil {
		return nil, err
	}
	b, err := (&RefCrypto{}).AEADOpen(body, key, aad)
	if err != nil {
		return nil, &contract.FormatError{Reason: "tag"}
	}
	return b, nil
}
func (*RefCodec) Header(b []byte) (contract.Header, error) {
	if len(b) < 5 {
		return contract.Header{}, &contract.FormatError{Reason: "header"}
	}
	k := string(b[:4])
	h := contract.Header{Kind: k, FormatVersion: int(b[4])}
	switch k {
	case "SITC":
		h.Mark = b[5:6]
	case "SITM":
		h.Mark, h.Wrapped = b[5:9], b[9:9+72]
	case "SITG", "SITJ", "SITS":
		h.Mark = b[5:9]
	case "SITK":
		h.Mark = b[5+16 : 5+16+25]
	default:
		return h, &contract.FormatError{Reason: "header"}
	}
	switch {
	case h.FormatVersion > contract.FormatVersion:
		return h, &contract.FormatError{Reason: "too_new", Required: h.FormatVersion, Supported: []int{contract.FormatVersion}}
	case h.FormatVersion < 1:
		return h, &contract.FormatError{Reason: "too_old"}
	}
	return h, nil
}
func refHlen(h contract.Header) int {
	switch h.Kind {
	case "SITC":
		return 6
	case "SITM":
		return 9 + 72
	case "SITK":
		return 5 + 16 + 25
	}
	return 9
}
func (c *RefCodec) Encode(kind, id string, h contract.Header, content, key []byte) ([]byte, error) {
	var head []byte
	switch kind {
	case "SITK":
		b, _ := hex.DecodeString(id)
		head = append(append([]byte(kind), byte(h.FormatVersion)), b...)
		head = append(head, h.Mark...)
	default:
		head = append(append([]byte(kind), byte(h.FormatVersion)), h.Mark...)
		head = append(head, h.Wrapped...)
	}
	return refSeal(head, kind, id, h, content, key)
}
func (c *RefCodec) Decode(kind, id string, sealed, key []byte) ([]byte, error) {
	h, err := c.Header(sealed)
	if err != nil {
		return nil, err
	}
	if h.Kind != kind {
		return nil, &contract.FormatError{Reason: "header"}
	}
	return c.open(sealed[refHlen(h):], kind, id, h, key)
}
