package adapter

// RunContext.Wrap(容器の口): 検証の後にだけ呼ばれ、返した引数列を起動する。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestWrapCalledAfterVerificationAndRuns(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; echo '"+okResult+"'"))
	var gotExe string
	var gotEnv []string
	rc := contract.RunContext{Wrap: func(exe string, env []string) ([]string, error) {
		gotExe, gotEnv = exe, env
		if started(b) {
			t.Error("Wrap の時点でアダプタが起動済み")
		}
		return []string{"/bin/sh", exe}, nil // 実行ファイルまでの引数列(Runner が run を足す)
	}}
	if _, err := b.r.Run(ctx, rc, "notify", "webhook", request("sitate.notify/v1"), nil); err != nil {
		t.Fatal(err)
	}
	if gotExe != filepath.Join(b.dir, "exe") || len(gotEnv) != 2 || !started(b) {
		t.Errorf("Wrap の引数: %q %v", gotExe, gotEnv)
	}
}

func TestWrapNotCalledWhenVerificationFails(t *testing.T) {
	cases := map[string]func(b built){
		"署名":     func(b built) { b.r.Verifier = &wantVerifier{} },
		"sha256": func(b built) { _ = os.WriteFile(filepath.Join(b.dir, "exe"), []byte("#!/bin/sh\n"), 0o755) },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			b := build(t, notifySpec("cat >/dev/null; echo '"+okResult+"'"))
			mod(b)
			called := false
			rc := contract.RunContext{Wrap: func(string, []string) ([]string, error) { called = true; return []string{"/bin/sh"}, nil }}
			if _, err := b.r.Run(ctx, rc, "notify", "webhook", request("sitate.notify/v1"), nil); err == nil || called || started(b) {
				t.Errorf("検証に失敗したのに Wrap を呼んだか起動した: %v called=%v", err, called)
			}
		})
	}
}

func TestWrapErrorNotStarted(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; echo '"+okResult+"'"))
	rc := contract.RunContext{Wrap: func(string, []string) ([]string, error) { return nil, errors.New("no container") }}
	_, err := b.r.Run(ctx, rc, "notify", "webhook", request("sitate.notify/v1"), nil)
	if err == nil || started(b) {
		t.Errorf("Wrap の誤りで起動した: %v", err)
	}
}
