package adapter

// 完了条件 7(子プロセスの環境)。試験ビルドだけの変数は env_testbuild_test.go / env_product_test.go。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// childEnvOf は子が受けた環境変数(シェルが足す PWD・SHLVL・_・OLDPWD は除く)を返す。
func childEnvOf(t *testing.T, b built) map[string]string {
	t.Helper()
	_, _ = runNotify(b)
	raw, err := os.ReadFile(filepath.Join(b.dir, "exe.env.out"))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		k, v, _ := strings.Cut(l, "=")
		switch k {
		case "PWD", "SHLVL", "_", "OLDPWD":
			continue
		}
		m[k] = v
	}
	return m
}

func keys(m map[string]string) []string {
	var r []string
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}

func TestChildEnvIsMinimal(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example.invalid:3128")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leak-me-0123456789")
	t.Setenv("HOME", "/nonexistent-home")
	b := build(t, notifySpec(`cat >/dev/null; env > "$0.env.out"`))
	// SITATE_RECV は取得アダプタだけ。notify には渡らない
	b.r.Env = map[string]string{"HOME": "/x", "SITATE_OTHER": "y", contract.EnvRecv: "/run/sitate/recv"}
	got := childEnvOf(t, b)
	if got["PATH"] != contract.AdapterEnvPATH || got["LC_ALL"] != "C" || len(got) != 2 {
		t.Errorf("PATH と LC_ALL=C だけのはず: %v", got)
	}
}

func TestChildEnvAcquireRecv(t *testing.T) {
	b := build(t, spec{kind: "acquire", name: "files", script: `cat >/dev/null; env > "$0.env.out"`})
	b.r.Env = map[string]string{contract.EnvRecv: "/run/sitate/recv", "HOME": "/x"}
	_, _ = b.r.Run(ctx, contract.RunContext{}, "acquire", "files", acquireReq(), nil)
	raw, err := os.ReadFile(filepath.Join(b.dir, "exe.env.out"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "SITATE_RECV=/run/sitate/recv\n") || strings.Contains(string(raw), "HOME=") {
		t.Errorf("取得アダプタには SITATE_RECV だけを足す: %s", raw)
	}
}
