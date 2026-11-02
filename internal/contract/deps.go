//go:build sitate_deps

// 依存の先行追加(作業指示書 5.1、GB-85)。go mod tidy で消えないよう、ビルドタグ付きの空 import で固定する。
// 使う WP は各自のパッケージで import する。このファイルは通常のビルドに入らない。
package contract

import (
	_ "github.com/PlakarKorp/go-cdc-chunkers"
	_ "github.com/PlakarKorp/go-cdc-chunkers/chunkers/fastcdc"
	_ "github.com/aws/aws-sdk-go-v2/aws"
	_ "github.com/aws/aws-sdk-go-v2/config"
	_ "github.com/aws/aws-sdk-go-v2/credentials"
	_ "github.com/aws/aws-sdk-go-v2/service/s3"
	_ "github.com/gowebpki/jcs"
	_ "github.com/klauspost/compress/zstd"
	_ "github.com/santhosh-tekuri/jsonschema/v6"
	_ "github.com/spf13/cobra"
	_ "github.com/tyler-smith/go-bip39"
	_ "github.com/zeebo/blake3"
	_ "golang.org/x/crypto/argon2"
	_ "golang.org/x/crypto/chacha20poly1305"
	_ "golang.org/x/crypto/hkdf"
	_ "golang.org/x/crypto/ssh"
	_ "gopkg.in/yaml.v3"
)
