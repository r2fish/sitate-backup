//go:build !sitate_testbuild

package contract

// TrustAnchors は信頼の起点の表(spec 3.2.1)。製品ビルドは配布用の公開鍵だけを埋め込む。
// 使い捨ての鍵と internal/contract/fake は製品ビルドに含めない(make build が go list -deps で検査する)。
// 鍵は minisign の公開鍵(base64 で 2 バイトの方式 "Ed"、8 バイトの鍵 ID、32 バイトの鍵)。
func TrustAnchors() []string { return []string{DistributionPublicKey} }

// DistributionPublicKey は配布用の署名鍵の公開鍵(利用者が期間開始時に生成。秘密鍵はリポジトリに置かない)。
const DistributionPublicKey = "RWQAPYdjxbgseEU2kigV8tO7zPAbyybtrzLopCJhOKEMUTqqc4+niDs3"
