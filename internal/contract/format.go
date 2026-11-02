package contract

import "fmt"

// 保管形式の見出しの印と値(spec 4.3「オブジェクトの形式」「見出しの値と符号化」)。符号化と復号の実装は U1a(internal/format/)。
// 試験ベクトルは testdata/format/vectors.json、読み方は harness/format.go。

// オブジェクトの種類(4 バイトの ASCII。見出しの先頭と AAD の種類)。
const (
	MagicChunk       = "SITC"
	MagicManifest    = "SITM"
	MagicConfig      = "SITG"
	MagicJournal     = "SITJ"
	MagicKeyring     = "SITK"
	MagicState       = "SITS"
	MagicWrapSnap    = "WRPS" // 包んだスナップショット鍵(AAD の種類だけ。見出しの印ではない)
	MagicWrapChunk   = "WRPC" // 包んだチャンク鍵(索引。AAD の種類だけ)
	CompressNone     = 0x00
	CompressZstd     = 0x01
	NonceSize        = 24
	TagSize          = 16
	KeySize          = 32
	ChunkIDSize      = 32
	MasterKeyIDSize  = 4
	HKDFInfoPrefix   = "sitate/v1/"
	UncoveredFPLabel = "sitate/v1/uncovered"
)

// KeyPurposes は HKDF-SHA-256 の用途(info は HKDFInfoPrefix + 用途。salt は空、出力は 32 バイト)。
var KeyPurposes = []string{"chunk-id", "snapshot-wrap", "chunk-wrap", "config", "journal", "state", "proof"}

// MasterKeyIDString はマスター鍵の鍵 ID の表示(JSON と表示では "k<n>")。見出しと AAD では 4 バイトのビッグエンディアン。
func MasterKeyIDString(n uint32) string { return fmt.Sprintf("k%d", n) }
