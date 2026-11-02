//go:build sitate_testbuild

package adapter

import (
	"os"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// testBuildEnv は試験ビルドの核が子に渡す変数(試験の切り替えと追加の CA のファイル。spec 3.2.1、3.2.5、3.2.6)。
func testBuildEnv() map[string]string {
	m := map[string]string{}
	for _, k := range []string{contract.EnvTestB2AuthURL, contract.EnvExtraCAFile} {
		if v, ok := os.LookupEnv(k); ok {
			m[k] = v
		}
	}
	return m
}
