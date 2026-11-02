//go:build !sitate_testbuild

package adapter

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 製品ビルドの試験では、試験の切り替えと追加の CA の変数を核が自分の環境に持っていても子へ渡らない。
func TestProductBuildDoesNotPassTestVars(t *testing.T) {
	t.Setenv(contract.EnvExtraCAFile, "/srv/ca.pem")
	t.Setenv(contract.EnvTestB2AuthURL, "http://b2.example.invalid")
	b := build(t, notifySpec(`cat >/dev/null; env > "$0.env.out"`))
	b.r.Env = map[string]string{contract.EnvExtraCAFile: "/srv/ca.pem", contract.EnvTestB2AuthURL: "http://b2.example.invalid"}
	got := childEnvOf(t, b)
	if len(got) != 2 {
		t.Errorf("製品ビルドでは PATH と LC_ALL だけ: %v", keys(got))
	}
}
