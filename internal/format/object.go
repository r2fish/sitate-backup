package format

// オブジェクトの見出し・AAD・符号化と復号(spec 4.3「オブジェクトの形式」「AAD の各部」「読めないときの扱い」)。

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// readableVersions は読める(= 書ける)形式の版。n = 1 では [1](spec 4.3「形式の版と後方互換」)。
var readableVersions = []int{contract.FormatVersion}

const (
	wrappedLen  = contract.NonceSize + contract.KeySize + contract.TagSize // 包んだスナップショット鍵
	keyringMark = 25                                                       // メモリ(4)∥ 反復(4)∥ 並列(1)∥ ソルト(16)
	keyringID   = 16
)

// layout は種類ごとの見出しの形。idInHeader は見出しに ID の生のバイトを置く長さ、idLen は AAD の ID の長さ(0 は可変 = SITS)。
type layout struct {
	idInHeader, markLen, wrappedLen, idLen int
}

var layouts = map[string]layout{
	contract.MagicChunk:    {markLen: 1, idLen: contract.ChunkIDSize},
	contract.MagicManifest: {markLen: 4, wrappedLen: wrappedLen, idLen: 16},
	contract.MagicConfig:   {markLen: 4, idLen: 32},
	contract.MagicJournal:  {markLen: 4, idLen: 8},
	contract.MagicKeyring:  {idInHeader: keyringID, markLen: keyringMark, idLen: keyringID},
	contract.MagicState:    {markLen: 4},
}

func (l layout) headerLen() int { return 5 + l.idInHeader + l.markLen + l.wrappedLen }

// checkPrefix は長さ 5 以上と種類の一致の後、見出しの長さを見る前に形式の版を検査する。
func checkPrefix(b []byte, kind string) error {
	if len(b) < 5 || string(b[:4]) != kind {
		return headerErr()
	}
	return checkVersion(int(b[4]))
}

func headerErr() error { return &contract.FormatError{Reason: "header"} }
func tagErr() error    { return &contract.FormatError{Reason: "tag"} }

// checkVersion は形式の版が読める版かを見て、読めなければ too_new / too_old を返す。
func checkVersion(v int) error {
	for _, r := range readableVersions {
		if v == r {
			return nil
		}
	}
	reason := "too_old"
	if v > readableVersions[len(readableVersions)-1] {
		reason = "too_new"
	}
	return &contract.FormatError{Reason: reason, Required: v, Supported: append([]int(nil), readableVersions...)}
}

// Header は b の先頭から見出しを読む。印が種類と違う・短いときは FormatError{header}。形式の版は検査しない。
func (*Codec) Header(b []byte) (contract.Header, error) {
	if len(b) < 5 {
		return contract.Header{}, headerErr()
	}
	kind := string(b[:4])
	l, ok := layouts[kind]
	if !ok || len(b) < l.headerLen() {
		return contract.Header{}, headerErr()
	}
	h := contract.Header{Kind: kind, FormatVersion: int(b[4])}
	p := 5 + l.idInHeader
	h.Mark = append([]byte(nil), b[p:p+l.markLen]...)
	if l.wrappedLen > 0 {
		h.Wrapped = append([]byte(nil), b[p+l.markLen:p+l.markLen+l.wrappedLen]...)
	}
	return h, nil
}

// AAD は 形式の版(1)∥ 種類(4)∥ ID ∥ 見出しの印 を返す。id の読み方は contract.Codec の注釈による。
func (*Codec) AAD(kind, id string, h contract.Header) ([]byte, error) {
	l, ok := layouts[kind]
	if !ok {
		l, ok = wrapLayout(kind)
		if !ok {
			return nil, fmt.Errorf("format: 種類 %q を知らない", kind)
		}
	}
	if h.FormatVersion < 0 || h.FormatVersion > 255 {
		return nil, errors.New("format: 形式の版が不正")
	}
	if len(h.Mark) != l.markLen {
		return nil, fmt.Errorf("format: %s の見出しの印は %d バイト", kind, l.markLen)
	}
	idb, err := aadID(kind, id, l)
	if err != nil {
		return nil, err
	}
	aad := make([]byte, 0, 5+len(idb)+len(h.Mark))
	aad = append(aad, byte(h.FormatVersion))
	aad = append(aad, kind...)
	aad = append(aad, idb...)
	return append(aad, h.Mark...), nil
}

// wrapLayout は見出しを持たない包んだ鍵(WRPS・WRPC)の AAD の形。
func wrapLayout(kind string) (layout, bool) {
	switch kind {
	case contract.MagicWrapSnap:
		return layout{markLen: 4, idLen: 16}, true
	case contract.MagicWrapChunk:
		return layout{markLen: 4, idLen: contract.ChunkIDSize}, true
	}
	return layout{}, false
}

func aadID(kind, id string, l layout) ([]byte, error) {
	if kind == contract.MagicState {
		if !utf8.ValidString(id) || len(id) > 0xffff {
			return nil, errors.New("format: 状態ファイルの相対パスが不正")
		}
		return append(binary.BigEndian.AppendUint16(nil, uint16(len(id))), id...), nil
	}
	b, err := hex.DecodeString(id)
	if err != nil || len(b) != l.idLen || hex.EncodeToString(b) != id { // 小文字の 16 進だけ

		return nil, fmt.Errorf("format: %s の ID は %d バイトの 16 進", kind, l.idLen)
	}
	return b, nil
}

// Encode は content を key で封緘し、見出し ∥ nonce ∥ 暗号文 ∥ タグ のオブジェクトにする。圧縮はしない(マニフェストの圧縮は呼び出し側)。
// チャンクは Seal を使う。形式の版は h.FormatVersion(読める版だけ書ける)。
func (c *Codec) Encode(kind, id string, h contract.Header, content, key []byte) ([]byte, error) {
	l, ok := layouts[kind]
	if !ok || kind == contract.MagicChunk {
		return nil, fmt.Errorf("format: %q は Encode の対象でない", kind)
	}
	if h.Kind != kind {
		return nil, fmt.Errorf("format: 見出しの種類 %q と kind %q が違う", h.Kind, kind)
	}
	if err := checkVersion(h.FormatVersion); err != nil {
		return nil, err
	}
	if len(h.Wrapped) != l.wrappedLen {
		return nil, fmt.Errorf("format: 包んだ鍵は %d バイト", l.wrappedLen)
	}
	aad, err := c.AAD(kind, id, h)
	if err != nil {
		return nil, err
	}
	if kind == contract.MagicManifest { // 本体は常に zstd で圧縮する。呼び出し側は JSON を渡す
		content = zEnc().EncodeAll(content, nil)
	}
	body, err := (&Crypto{}).AEADSeal(content, key, aad)
	if err != nil {
		return nil, err
	}
	out := append([]byte(kind), byte(h.FormatVersion))
	if l.idInHeader > 0 {
		idb, _ := hex.DecodeString(id) // AAD が検査済み
		out = append(out, idb...)
	}
	out = append(out, h.Mark...)
	out = append(out, h.Wrapped...)
	return append(out, body...), nil
}

// Decode は kind のオブジェクトを開く。開けない理由は *contract.FormatError(header / too_new / too_old / tag)。
func (c *Codec) Decode(kind, id string, sealed, key []byte) ([]byte, error) {
	l, ok := layouts[kind]
	if !ok || kind == contract.MagicChunk {
		return nil, fmt.Errorf("format: %q は Decode の対象でない", kind)
	}
	if err := checkPrefix(sealed, kind); err != nil {
		return nil, err
	}
	h, err := c.Header(sealed)
	if err != nil {
		return nil, headerErr()
	}
	if kind == contract.MagicKeyring && hex.EncodeToString(sealed[5:5+keyringID]) != id {
		return nil, tagErr() // 見出しの keyring_id が呼び出し側の ID と違う
	}
	aad, err := c.AAD(kind, id, h)
	if err != nil {
		return nil, tagErr() // 呼び出し側の ID が形を成さない = どのオブジェクトの ID でもない
	}
	plain, err := (&Crypto{}).AEADOpen(sealed[l.headerLen():], key, aad)
	if err != nil {
		return nil, tagErr()
	}
	if kind == contract.MagicManifest { // 本体は zstd で圧縮した JSON。展開の上限は ChunkMax
		if plain, err = zDec().DecodeAll(plain, nil); err != nil {
			return nil, tagErr()
		}
	}
	return plain, nil
}

// Hash は content の BLAKE3 を "blake3:" ∥ 小文字の 16 進 64 文字で返す。
func (*Codec) Hash(content []byte) string {
	return "blake3:" + hex.EncodeToString((&Crypto{}).BLAKE3(content))
}
