//go:build sitate_testbuild

package cli

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 試験ビルド(spec 3.3): 時計のオフセットは共有の値(EnvSimTimeFile)だけから取り、読むたびに読み直す。引数と定義ファイルの項目では受けない。
const buildName = "test"

// SimClock は contract.Clock と contract.Offsetter の試験ビルドの実装。共有の値が無いか壊れていれば誤りを返す(ARG_INVALID の format)。
type SimClock struct{ Path string }

var (
	_ contract.Clock     = SimClock{}
	_ contract.Offsetter = SimClock{}
)

// Offset は contract.Offsetter を満たす。
func (c SimClock) Offset() (time.Duration, error) {
	if c.Path == "" {
		return 0, argErr("--sim-time", "format")
	}
	s, err := contract.ReadSimTime(c.Path)
	if err != nil {
		return 0, argErr("--sim-time", "format")
	}
	return s.Offset()
}

// Now は contract.Clock を満たす。
func (c SimClock) Now(context.Context) (time.Time, error) {
	off, err := c.Offset()
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().Add(off).UTC().Truncate(time.Second), nil
}

func applyBuild(d *Deps) {
	d.Clock = SimClock{Path: os.Getenv(contract.EnvSimTimeFile)}
	d.Limits = limitsFromEnv(d.Limits)
}

// limitsFromEnv は実行の上限・猶予・記録の締め切り・state.lock の待ちを環境変数(秒)で短くする(試験ビルドだけ)。
func limitsFromEnv(l Limits) Limits {
	sec := func(env string, dst *time.Duration) {
		if v := os.Getenv(env); v != "" {
			if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
				*dst = time.Duration(n * float64(time.Second))
			}
		}
	}
	sec(contract.EnvTestRunMaxDurationS, &l.MaxDuration)
	sec(contract.EnvTestRunAbortGraceS, &l.AbortGrace)
	sec(contract.EnvTestRecordDeadlineS, &l.RecordDeadline)
	sec(contract.EnvTestStateLockWaitS, &l.StateLockWait)
	return l
}

// versionExtras は試験ビルドだけの項目: format_versions.test と、読んだ模擬時刻 sim_time(共有の値が読めなければ ARG_INVALID にせず null)。
func versionExtras(ctx context.Context, d *Deps, r *versionResult, fv map[string]any) {
	fv["test"] = true
	if now, err := d.Clock.Now(ctx); err == nil {
		s := now.Format(time.RFC3339)
		r.SimTime = &s
	}
}
