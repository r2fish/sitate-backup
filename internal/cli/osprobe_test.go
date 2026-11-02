package cli

import (
	"context"
	"github.com/r2fish/sitate-backup/internal/contract"
	"os"
	"path/filepath"
	"testing"
)

func probeRoot(t *testing.T, files map[string]string) *HostProbe {
	t.Helper()
	root := t.TempDir()
	writeBundle(t, root, files)
	return &HostProbe{Root: root, StateDir: filepath.Join(root, "state")}
}

func check(t *testing.T, p *HostProbe, name string) bool {
	t.Helper()
	ok, _, err := p.Check(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestProbeDirectoryJoin(t *testing.T) {
	p := probeRoot(t, map[string]string{"etc/nsswitch.conf": "passwd: files systemd\ngroup: files [SUCCESS=merge] systemd\n"})
	if !check(t, p, "directory_join") {
		t.Error("files systemd は合格")
	}
	p = probeRoot(t, map[string]string{"etc/nsswitch.conf": "passwd: files sss\ngroup: files\n"})
	if check(t, p, "directory_join") {
		t.Error("sss は外れ")
	}
}

func TestProbeListeningPorts(t *testing.T) {
	head := "  sl  local_address rem_address   st\n"
	ok := head + "   0: 00000000:0016 00000000:0000 0A 0\n   1: 0100007F:1F90 00000000:0000 0A 0\n"
	p := probeRoot(t, map[string]string{"proc/net/tcp": ok})
	if !check(t, p, "listening_ports") {
		t.Error("ssh と loopback だけは合格")
	}
	bad := ok + "   2: 00000000:0050 00000000:0000 0A 0\n"
	p = probeRoot(t, map[string]string{"proc/net/tcp": bad})
	if o, d, _ := p.Check(context.Background(), "listening_ports"); o || d == nil {
		t.Error("80 番は外れで detail に材料を持つ")
	}
	p = probeRoot(t, map[string]string{"proc/net/tcp": "  h\n   0: 00000000:0050 00000000:0000 0A 0\n", "etc/ssh/sshd_config": "Port 80\n"})
	if !check(t, p, "listening_ports") {
		t.Error("Port 80 の sshd")
	}
}

func TestProbeAdminAccessConfigAndSwap(t *testing.T) {
	conf := "etc/ssh/sshd_config.d/00-sitate.conf"
	p := probeRoot(t, map[string]string{conf: "PasswordAuthentication no\nPermitRootLogin no\n"})
	if check(t, p, "admin_access_closed") { // State が無ければ記録が確かめられず外れ
		t.Error("閉じた記録が無いのに合格")
	}
	p = probeRoot(t, map[string]string{"proc/swaps": "Filename Type Size\n", "x": ""})
	if !check(t, p, "swap_disabled") {
		t.Error("スワップ無し")
	}
	p = probeRoot(t, map[string]string{"proc/swaps": "Filename Type Size\n/dev/sda2 partition 1 0 -2\n"})
	if check(t, p, "swap_disabled") {
		t.Error("スワップ有り")
	}
}

func TestProbeCheckImagesAndUnknown(t *testing.T) {
	p := probeRoot(t, nil)
	ok, d, _ := p.Check(context.Background(), "check_images")
	if !ok || string(d) != `{"count":0}` {
		t.Errorf("check_images: %v %s", ok, d)
	}
	if _, _, err := p.Check(context.Background(), "state_dir_fs"); err == nil {
		t.Error("state_dir_fs は Check の担当でない")
	}
}

func TestProbeFileModes(t *testing.T) {
	p := probeRoot(t, nil)
	d := filepath.Join(p.StateDir, "keys")
	_ = os.MkdirAll(filepath.Join(p.StateDir, "secrets"), 0o700)
	_ = os.MkdirAll(d, 0o700)
	_ = os.Chmod(d, 0o700)
	_ = os.Chmod(filepath.Join(p.StateDir, "secrets"), 0o700)
	f := filepath.Join(d, "master.json")
	_ = os.WriteFile(f, []byte("{}"), 0o600)
	if !check(t, p, "file_modes") {
		t.Error("0700/0600 は合格")
	}
	_ = os.Chmod(f, 0o640)
	if check(t, p, "file_modes") {
		t.Error("グループ権限は外れ")
	}
}

func TestStoreOpenerArgs(t *testing.T) {
	op := NewStoreOpener(&Deps{})
	if _, err := op(context.Background(), storeAddr("", "b", ""), "k", nil); err == nil {
		t.Error("endpoint 無しが成功した")
	}
}

func storeAddr(e, b, p string) (a contract.StoreAddr) {
	a.Endpoint, a.Bucket, a.Prefix = e, b, p
	return
}
