package harness

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/zeebo/blake3"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"io"
)

// 保管形式の試験ベクトル(testdata/format/vectors.json)の参照。暗号の部品だけを持ち、見出しと AAD の並びは
// ベクトルの側に手で書いてある(U1a が仕様から作ったものと突き合わせるため)。製品の Codec ではない。

// FormatSeal は object = header ∥ nonce ∥ 暗号文 ∥ タグ を作る(XChaCha20-Poly1305。aad はベクトルの値)。
func FormatSeal(header, key, nonce, aad, plaintext []byte) ([]byte, error) {
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	out := append(append([]byte{}, header...), nonce...)
	return a.Seal(out, nonce, plaintext, aad), nil
}

// FormatOpen は header の長さ hlen を除いた nonce ∥ 暗号文 ∥ タグ を開く。
func FormatOpen(object []byte, hlen int, key, aad []byte) ([]byte, error) {
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(object) < hlen+contract.NonceSize+contract.TagSize {
		return nil, fmt.Errorf("短すぎる")
	}
	body := object[hlen:]
	return a.Open(nil, body[:contract.NonceSize], body[contract.NonceSize:], aad)
}

// FormatClassify は先頭の印と形式の版から、開けない理由を分ける(Codec の返す理由。spec 4.3「読めないときの扱い」)。
// 値: ok / header(印が種類と違う)/ too_new / too_old。読める版は readable。
func FormatClassify(object []byte, magic string, readable []int) string {
	if len(object) < 5 || string(object[:4]) != magic {
		return "header"
	}
	v := int(object[4])
	for _, r := range readable {
		if v == r {
			return "ok"
		}
	}
	if v > readable[len(readable)-1] {
		return "too_new"
	}
	return "too_old"
}

// FormatDerive は HKDF-SHA-256(salt 空、info = sitate/v1/<用途>)で 32 バイトの鍵を導出する。
func FormatDerive(master []byte, purpose string) ([]byte, error) {
	out := make([]byte, contract.KeySize)
	_, err := io.ReadFull(hkdf.New(sha256.New, master, nil, []byte(contract.HKDFInfoPrefix+purpose)), out)
	return out, err
}

// FormatChunkID は BLAKE3 の鍵付きハッシュ(鍵 = ID 鍵)の小文字 16 進 64 文字。
func FormatChunkID(idKey, plaintext []byte) (string, error) {
	h, err := blake3.NewKeyed(idKey)
	if err != nil {
		return "", err
	}
	h.Write(plaintext)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FormatUncoveredFP は候補の指紋(ID 鍵の鍵付き BLAKE3 を、ASCII の sitate/v1/uncovered ∥ 0x00 ∥ 種別 ∥ 0x00 ∥ パスのバイト列 に掛ける)。
func FormatUncoveredFP(idKey []byte, kind string, path []byte) (string, error) {
	in := bytes.Join([][]byte{[]byte(contract.UncoveredFPLabel), []byte(kind), path}, []byte{0})
	return FormatChunkID(idKey, in)
}

// FormatKEK は Argon2id(版 0x13)でフレーズの BIP-39 のエントロピーから KEK(32 バイト)を導く。
func FormatKEK(entropy, salt []byte, memKiB, iters uint32, par uint8) []byte {
	return argon2.IDKey(entropy, salt, iters, memKiB, par, contract.KeySize)
}

// HMACSHA256 は独立の確認用(HKDF の試験ベクトルを標準の hmac で作り直す)。
func HMACSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}
