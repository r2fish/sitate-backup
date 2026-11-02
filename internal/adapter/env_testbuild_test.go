//go:build sitate_testbuild

package adapter

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 試験ビルドの核は、試験の切り替えと追加の CA の変数を自分の環境から子へ渡す(これ以外は渡さない)。
func TestTestBuildPassesTestVars(t *testing.T) {
	t.Setenv(contract.EnvExtraCAFile, "/srv/ca.pem")
	t.Setenv(contract.EnvTestB2AuthURL, "http://b2.example.invalid")
	t.Setenv("HTTPS_PROXY", "http://proxy.example.invalid:3128")
	b := build(t, notifySpec(`cat >/dev/null; env > "$0.env.out"`))
	got := childEnvOf(t, b)
	if got[contract.EnvExtraCAFile] != "/srv/ca.pem" || got[contract.EnvTestB2AuthURL] != "http://b2.example.invalid" || len(got) != 4 {
		t.Errorf("試験ビルドの子の環境: %v", keys(got))
	}
}
