//go:build sitate_testbuild

package contract

import (
	"os"
	"strings"
)

// TrustAnchors は試験ビルドの信頼の起点: test/env/ の起動時に生成した使い捨ての公開鍵だけ(配布用の鍵は埋め込まない)。
// 公開鍵は EnvTestTrustPubKeyFile が指す minisign の .pub ファイル(2 行目)から読む。
func TrustAnchors() []string {
	b, err := os.ReadFile(os.Getenv(EnvTestTrustPubKeyFile))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 2 {
		return nil
	}
	return []string{strings.TrimSpace(lines[1])}
}
