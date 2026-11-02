package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// doctor の timer_permission(spec 3.1.2 の表。D-0159): systemd ではそのユーザーの単位を常駐させられる(linger が有効)、cron では sitate の crontab が許されている。
// どちらの方式を使う環境かで判定し、片方が通れば合格にはしない。

func probeWithRun(t *testing.T, systemd bool, run func(name string, args ...string) ([]byte, error)) *HostProbe {
	t.Helper()
	p := probeRoot(t, nil)
	if systemd {
		if err := os.MkdirAll(filepath.Join(p.Root, "run/systemd/system"), 0o755); err != nil {
			t.Fatal(err)
		}
		bin := t.TempDir()
		if err := os.WriteFile(filepath.Join(bin, "systemctl"), nil, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin)
	}
	p.Run = func(_ context.Context, name string, args ...string) ([]byte, error) { return run(name, args...) }
	return p
}

func TestProbeTimerPermissionSystemdLinger(t *testing.T) {
	linger := func(v string) func(string, ...string) ([]byte, error) {
		return func(name string, args ...string) ([]byte, error) {
			if name == "loginctl" {
				return []byte("Linger=" + v + "\n"), nil
			}
			return nil, errors.New("使わない")
		}
	}
	if !check(t, probeWithRun(t, true, linger("yes")), "timer_permission") {
		t.Error("systemd で linger が有効なら合格")
	}
	if check(t, probeWithRun(t, true, linger("no")), "timer_permission") {
		t.Error("systemd で linger が無効なら外れ")
	}
	// systemd の環境で cron が許されていても、タイマーは systemd の単位なので合格にしない。
	p := probeWithRun(t, true, func(name string, _ ...string) ([]byte, error) {
		if name == "crontab" {
			return nil, nil
		}
		return []byte("Linger=no\n"), nil
	})
	writeBundle(t, p.Root, map[string]string{"etc/cron.allow": "sitate\n"})
	if check(t, p, "timer_permission") {
		t.Error("systemd の環境で cron の許可があっても linger が無ければ外れ")
	}
}

func TestProbeTimerPermissionCrontab(t *testing.T) {
	crontab := func(out string, err error) func(string, ...string) ([]byte, error) {
		return func(name string, args ...string) ([]byte, error) {
			if name != "crontab" {
				return nil, errors.New("使わない")
			}
			return []byte(out), err
		}
	}
	if !check(t, probeWithRun(t, false, crontab("", nil)), "timer_permission") {
		t.Error("cron で crontab を読めれば合格")
	}
	if !check(t, probeWithRun(t, false, crontab("no crontab for sitate", exitErr("no crontab for sitate"))), "timer_permission") {
		t.Error("crontab が空なだけ(許可はある)なら合格")
	}
	if check(t, probeWithRun(t, false, crontab("", exitErr("You (sitate) are not allowed to use this program (crontab)"))), "timer_permission") {
		t.Error("crontab が許されていなければ外れ")
	}
	if check(t, probeWithRun(t, false, crontab("", errors.New("crontab が無い"))), "timer_permission") {
		t.Error("crontab が無ければ外れ")
	}
}

// exitErr は終了コード 1 と標準エラーを持つ実行エラー。
func exitErr(stderr string) error {
	e := exec.Command("false").Run()
	var ee *exec.ExitError
	if errors.As(e, &ee) {
		ee.Stderr = []byte(stderr)
		return ee
	}
	return errors.New(stderr)
}

// ディレクトリがあっても systemctl が無ければ、登録(state.DetectScheduler)と同じく cron の環境として crontab で判定する(D-0161)。
func TestProbeTimerPermissionNoSystemctlIsCron(t *testing.T) {
	p := probeWithRun(t, false, func(name string, args ...string) ([]byte, error) {
		if name == "crontab" {
			return nil, nil
		}
		return nil, errors.New("使わない")
	})
	if err := os.MkdirAll(filepath.Join(p.Root, "run/systemd/system"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if !check(t, p, "timer_permission") {
		t.Error("systemctl が無い環境は crontab で判定する")
	}
}
