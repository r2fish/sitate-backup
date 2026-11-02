package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// doctor の adapter_dir_modes(spec 3.2.1 の「置き場の所有者とモード」。D-0159): /usr/libexec/sitate/ の下は所有者 root、ディレクトリは 0755、ファイルは root 以外が書けない。
// 試験は root で動かないため、期待する所有者は LibexecUID(0 が実機の root)で差し替える。

func adapterRoot(t *testing.T) (*HostProbe, string) {
	t.Helper()
	p := probeRoot(t, nil)
	p.LibexecUID = os.Getuid()
	dir := filepath.Join(p.Root, "usr/libexec/sitate")
	sub := filepath.Join(dir, "adapters/x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, filepath.Join(dir, "adapters"), sub} {
		if err := os.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f := filepath.Join(sub, "adapter.json")
	if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f, 0o755); err != nil {
		t.Fatal(err)
	}
	return p, f
}

func TestProbeAdapterDirModes(t *testing.T) {
	p, f := adapterRoot(t)
	if !check(t, p, "adapter_dir_modes") {
		t.Fatal("所有者・0755・root 以外が書けないファイルは合格")
	}
	p.LibexecUID = os.Getuid() + 1
	if check(t, p, "adapter_dir_modes") {
		t.Error("所有者が root でなければ外れ")
	}
	p.LibexecUID = os.Getuid()
	dir := filepath.Dir(f)
	_ = os.Chmod(dir, 0o775)
	if check(t, p, "adapter_dir_modes") {
		t.Error("ディレクトリが 0755 でなければ外れ(グループが書ける)")
	}
	_ = os.Chmod(dir, 0o700)
	if check(t, p, "adapter_dir_modes") {
		t.Error("ディレクトリが 0755 でなければ外れ(0700)")
	}
	_ = os.Chmod(dir, 0o755)
	for _, m := range []os.FileMode{0o775, 0o757, 0o666} {
		_ = os.Chmod(f, m)
		if check(t, p, "adapter_dir_modes") {
			t.Errorf("ファイルを root 以外が書ける %o は外れ", m)
		}
	}
	_ = os.Chmod(f, 0o555)
	if !check(t, p, "adapter_dir_modes") {
		t.Error("0555 は合格")
	}
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", f); err != nil {
		t.Fatal(err)
	}
	if check(t, p, "adapter_dir_modes") {
		t.Error("シンボリックリンクは外れ")
	}
}

func TestProbeAdapterDirMissing(t *testing.T) {
	p := probeRoot(t, nil)
	if check(t, p, "adapter_dir_modes") {
		t.Error("置き場が無ければ外れ")
	}
}
