package cli

// 継ぎ目 OSProbe の実物(spec 3.1.2 の「doctor の検査項目」の表。D-0157)。実測の材料は /proc・/etc・状態ディレクトリと、外部コマンド(podman・loginctl・systemctl)の出力。
// 判定(ok)だけを返し、秘密になり得る値は detail に入れない。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/state"
)

// ServiceUser は専用ユーザー名(spec 3.1.2 の service_user)。
const ServiceUser = "sitate"

// HostProbe は contract.OSProbe の実物。Root は試験が差し替える根("" は実機の /)。StateDir は状態ディレクトリ、TimersDir は contract.Timers の置き場。
// Run は外部コマンドの実行口(nil なら os/exec)。State は state.json の項目(admin_access_*_at・timers)を読む。
type HostProbe struct {
	Root      string
	StateDir  string
	TimersDir string
	State     contract.StateFiles
	Run       func(ctx context.Context, name string, args ...string) ([]byte, error)
	// LibexecUID は /usr/libexec/sitate/ の下の期待する所有者の uid(実機は 0 = root。試験だけが差し替える)。
	LibexecUID int
}

var _ contract.OSProbe = (*HostProbe)(nil)

func (p *HostProbe) path(s string) string { return filepath.Join(p.Root, s) }

func (p *HostProbe) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if p.Run != nil {
		return p.Run(ctx, name, args...)
	}
	c := exec.CommandContext(ctx, name, args...)
	if os.Getenv("XDG_RUNTIME_DIR") == "" { // sitate の --user の単位を引くのに要る
		c.Env = append(os.Environ(), fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", os.Getuid()))
	}
	return c.Output()
}

// FSType は contract.OSProbe を満たす。
func (p *HostProbe) FSType(_ context.Context, path string) (string, error) { return fsTypeOf(path) }

// Check は contract.OSProbe を満たす。state_dir_fs は FSType の担当なので、ここでは INTERNAL(呼び方の誤り)。
func (p *HostProbe) Check(ctx context.Context, name string) (bool, json.RawMessage, error) {
	var ok bool
	var detail map[string]any
	switch name {
	case "service_user":
		ok = p.serviceUser()
	case "container_runtime":
		out, err := p.run(ctx, "podman", "info", "--format", "{{.Host.Security.Rootless}}")
		ok = err == nil && strings.TrimSpace(string(out)) == "true"
	case "check_images":
		ok, detail = true, map[string]any{"count": contract.DoctorCheckImagesCount}
	case "timer_permission":
		ok = p.timerPermission(ctx)
	case "adapter_dir_modes":
		ok = p.adapterDirModes()
	case "listening_ports":
		ok, detail = p.listeningPorts()
	case "directory_join":
		ok = p.directoryJoin()
	case "admin_access_closed":
		ok = p.adminAccessClosed(ctx)
	case "timers_registered":
		ok = p.timersRegistered(ctx)
	case "file_modes":
		ok = p.fileModes()
	case "disk_encryption":
		ok = diskEncrypted(p.StateDir)
	case "swap_disabled":
		ok = p.swapDisabled()
	default:
		return false, nil, internalErr()
	}
	var raw json.RawMessage
	if detail != nil {
		raw, _ = json.Marshal(detail)
	}
	return ok, raw, nil
}

func (p *HostProbe) serviceUser() bool {
	u, err := user.Lookup(ServiceUser)
	return err == nil && strconv.Itoa(os.Getuid()) == u.Uid
}

// timerPermission は sitate が OS のスケジューラにタイマーを登録できるか(spec 3.1.2 の表)。
// systemd の環境ではそのユーザーの単位を常駐させられる(linger が有効)こと、cron の環境では sitate の crontab が許されていること。
// どちらの環境かは state.json の timers.backend、無ければ state.DetectScheduler で決め、片方が通れば合格にはしない。
func (p *HostProbe) timerPermission(ctx context.Context) bool {
	if p.timerBackend(ctx) == "cron" {
		return p.crontabAllowed(ctx)
	}
	out, err := p.run(ctx, "loginctl", "show-user", ServiceUser, "-p", "Linger")
	return err == nil && strings.TrimSpace(string(out)) == "Linger=yes"
}

func (p *HostProbe) timerBackend(ctx context.Context) string {
	if p.State != nil {
		if raw, err := p.State.Get(ctx, "timers"); err == nil {
			var t struct {
				Backend string `json:"backend"`
			}
			if json.Unmarshal(raw, &t) == nil && t.Backend != "" {
				return t.Backend
			}
		}
	}
	return state.DetectScheduler(p.path("/run/systemd/system")) // 登録(wire_parts.go)と同じ判定
}

// crontabAllowed は sitate の crontab を読める(`crontab -l`)か。crontab が空なだけ("no crontab for")は許されている。
func (p *HostProbe) crontabAllowed(ctx context.Context) bool {
	out, err := p.run(ctx, "crontab", "-l")
	if err == nil {
		return true
	}
	msg := string(out)
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		msg += string(ee.Stderr)
	}
	return strings.Contains(msg, "no crontab for")
}

// adapterDirModes は /usr/libexec/sitate/ の下が spec 3.2.1 のとおりか: 所有者 root、ディレクトリは 0755、ファイルは root 以外が書けない(0o022 が立たない)。
// 置き場が無い、シンボリックリンクがある、読めないときは外れ。
func (p *HostProbe) adapterDirModes() bool {
	root := p.path("/usr/libexec/sitate")
	if fi, err := os.Lstat(root); err != nil || !fi.IsDir() {
		return false
	}
	good := true
	_ = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		switch {
		case err != nil || fileOwner(fi) != p.LibexecUID:
			good = false
		case fi.IsDir():
			good = good && fi.Mode().Perm() == 0o755
		case !fi.Mode().IsRegular():
			good = false
		default:
			good = good && fi.Mode().Perm()&0o022 == 0
		}
		return nil
	})
	return good
}

// listeningPorts は /proc/net/tcp{,6} の LISTEN のうち、ループバック以外のアドレスのポートが sshd の Port だけか。
func (p *HostProbe) listeningPorts() (bool, map[string]any) {
	ssh := p.sshdPorts()
	var extra []int
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(p.path(f))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Scan() // 見出し
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) < 4 || fs[3] != "0A" {
				continue
			}
			addr, port, ok := strings.Cut(fs[1], ":")
			n, err := strconv.ParseInt(port, 16, 32)
			if !ok || err != nil || isLoopbackHex(addr) || ssh[int(n)] {
				continue
			}
			extra = append(extra, int(n))
		}
	}
	if len(extra) == 0 {
		return true, nil
	}
	return false, map[string]any{"ports": extra}
}

// isLoopbackHex は /proc/net/tcp の 16 進のアドレス(IPv4 は 8 桁、IPv6 は 32 桁)がループバックか。
func isLoopbackHex(a string) bool {
	switch len(a) {
	case 8:
		return strings.HasSuffix(a, "7F") // リトルエンディアンの先頭バイトが 127
	case 32:
		return a == "00000000000000000000000001000000" || strings.HasPrefix(a, "0000000000000000FFFF00007F")
	}
	return false
}

func (p *HostProbe) sshdPorts() map[int]bool {
	ports := map[int]bool{}
	files := []string{p.path("/etc/ssh/sshd_config")}
	if m, err := filepath.Glob(p.path("/etc/ssh/sshd_config.d/*.conf")); err == nil {
		files = append(files, m...)
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		for _, l := range strings.Split(string(b), "\n") {
			fs := strings.Fields(l)
			if len(fs) == 2 && strings.EqualFold(fs[0], "Port") {
				if n, err := strconv.Atoi(fs[1]); err == nil {
					ports[n] = true
				}
			}
		}
	}
	if len(ports) == 0 {
		ports[22] = true
	}
	return ports
}

func (p *HostProbe) directoryJoin() bool {
	b, err := os.ReadFile(p.path("/etc/nsswitch.conf"))
	if err != nil {
		return false
	}
	seen := 0
	for _, l := range strings.Split(string(b), "\n") {
		db, srcs, ok := strings.Cut(strings.TrimSpace(l), ":")
		if !ok || (db != "passwd" && db != "group") {
			continue
		}
		seen++
		for _, s := range strings.Fields(srcs) {
			if strings.HasPrefix(s, "[") { // [NOTFOUND=return] などの動作指定
				continue
			}
			if s != "files" && s != "systemd" {
				return false
			}
		}
	}
	return seen == 2
}

func (p *HostProbe) adminAccessClosed(ctx context.Context) bool {
	b, err := os.ReadFile(p.path("/etc/ssh/sshd_config.d/00-sitate.conf"))
	if err != nil {
		return false
	}
	var pw, root bool
	for _, l := range strings.Split(string(b), "\n") {
		fs := strings.Fields(l)
		if len(fs) != 2 {
			continue
		}
		switch {
		case strings.EqualFold(fs[0], "PasswordAuthentication") && fs[1] == "no":
			pw = true
		case strings.EqualFold(fs[0], "PermitRootLogin") && (fs[1] == "no" || fs[1] == "prohibit-password"):
			root = true
		case strings.EqualFold(fs[0], "AllowUsers"):
			return false
		}
	}
	if !pw || !root || p.State == nil {
		return false
	}
	closed, ok1 := p.stateTime(ctx, "admin_access_closed_at")
	opened, ok2 := p.stateTime(ctx, "admin_access_opened_at")
	return ok1 && (!ok2 || opened < closed) // RFC 3339(UTC)の文字列は辞書順で時刻順
}

// stateTime は state.json の項目が null でない文字列のときその値を返す。
func (p *HostProbe) stateTime(ctx context.Context, item string) (string, bool) {
	raw, err := p.State.Get(ctx, item)
	var s string
	if err != nil || json.Unmarshal(raw, &s) != nil || s == "" {
		return "", false
	}
	return s, true
}

func (p *HostProbe) timersRegistered(ctx context.Context) bool {
	backend := "systemd"
	if p.State != nil {
		raw, err := p.State.Get(ctx, "timers")
		if err != nil || strings.TrimSpace(string(raw)) == "null" || len(raw) == 0 {
			return false
		}
		var t struct {
			Backend string `json:"backend"`
		}
		if json.Unmarshal(raw, &t) == nil && t.Backend != "" {
			backend = t.Backend
		}
	}
	tab, err := contract.LoadTimerTable()
	if err != nil || len(tab.Units) == 0 {
		return false
	}
	if backend == "cron" {
		out, err := p.run(ctx, "crontab", "-l")
		return err == nil && strings.Count(string(out), "--invoked-by timer") >= len(tab.Units) // 単位ごとに 1 行以上(@reboot の行は余分に数える)
	}
	for _, u := range tab.Units { // 表の単位がすべて置かれ、有効であること(1 つでも欠ければ不合格)
		name := tab.UnitPrefix + u.Name + ".timer"
		if _, err := os.Stat(filepath.Join(p.TimersDir, name)); err != nil {
			return false
		}
		if out, err := p.run(ctx, "systemctl", "--user", "is-enabled", name); err != nil || strings.TrimSpace(string(out)) != "enabled" {
			return false
		}
	}
	return true
}

func (p *HostProbe) fileModes() bool {
	uid := os.Getuid()
	if u, err := user.Lookup(ServiceUser); err == nil {
		uid, _ = strconv.Atoi(u.Uid)
	}
	good := true
	for _, d := range []string{"keys", "secrets"} {
		_ = filepath.Walk(filepath.Join(p.StateDir, d), func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				good = false
				return nil
			}
			want := os.FileMode(0o600)
			if fi.IsDir() {
				want = 0o700
			}
			if fi.Mode().Perm() != want || fileOwner(fi) != uid {
				good = false
			}
			return nil
		})
	}
	return good
}

func (p *HostProbe) swapDisabled() bool {
	b, err := os.ReadFile(p.path("/proc/swaps"))
	return err == nil && len(strings.Split(strings.TrimSpace(string(b)), "\n")) <= 1
}
