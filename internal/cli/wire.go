package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/state"
)

// TimersDirDefault は sitate ユーザーの systemd 単位の置き場(HOME の下の contract の systemd_user_dir。init が登録する Timers の Dir と同じ値)。
func TimersDirDefault() string {
	tab, err := contract.LoadTimerTable()
	home, herr := os.UserHomeDir()
	if err != nil || herr != nil {
		return ""
	}
	return filepath.Join(home, tab.SystemdUserDir)
}

// runCommand は Timers の外部コマンドの実行口(env を足し、stdin を渡す)。
func runCommand(ctx context.Context, env []string, stdin, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Env = append(os.Environ(), env...)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	return c.Run()
}

// Limits は実行の上限・猶予・記録の締め切り・state.lock の待ち(定数表。製品ビルドは固定)。
type Limits struct {
	MaxDuration, AbortGrace, RecordDeadline, StateLockWait time.Duration
	// NolockDeadline はロックを要しない操作の締め切り(0 は定数表の値)。
	NolockDeadline time.Duration
}

// DefaultLimits は定数表の値。
func DefaultLimits() Limits {
	return Limits{MaxDuration: contract.RunMaxDuration, AbortGrace: contract.RunAbortGrace, RecordDeadline: contract.RecordDeadline, StateLockWait: contract.StateLockWait}
}

// DefaultDeps は実物を組む。cli と state が持つ部品だけを実物にし、他の WP の部品と継ぎ目は nil のまま
// (完了条件 24 の結線は、受け入れ済みの WP の分をここで埋める。nil のままの入口は NOT_IMPLEMENTED)。
func DefaultDeps(stateDir string) *Deps {
	d := &Deps{StateDir: stateDir, Stdin: os.Stdin, Limits: DefaultLimits(), Retention: contract.FixedRetention{}}
	applyBuild(d) // 製品ビルドは実時計、試験ビルドは共有の値のオフセット(環境変数の短い上限もここで読む)
	d.Lease = &Lease{Dir: stateDir, MaxDuration: d.Limits.MaxDuration}
	d.Secrets = &Secrets{Dir: stateDir}
	d.SSH = SSHClient{}
	d.Bundles = &DirBundles{}
	d.StoreOpener = NewStoreOpener(d)
	d.State = &state.StateFiles{Dir: stateDir, Clock: d.Clock, LockWait: d.Limits.StateLockWait, Refs: d.Secrets.(*Secrets).Exists}
	d.OSProbe = &HostProbe{StateDir: stateDir, TimersDir: TimersDirDefault(), State: d.State}
	wireParts(d, stateDir) // 受け入れ済みの部品の実物(wire_parts.go)
	return d
}
