//go:build !sitate_testbuild

package cli

import (
	"context"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 製品ビルド(spec 3.3): 時計を変える設定・環境変数・引数を持たない。test-tick は無い。
const buildName = "product"

func applyBuild(d *Deps) { d.Clock = SystemClock{} }

func versionExtras(context.Context, *Deps, *versionResult, map[string]any) {}

// testTick は製品ビルドでは使えない(未知の引数と同じ ARG_INVALID)。
func testTick(_ context.Context, _ *Deps, _ []string) (contract.Reply, error) {
	return contract.Reply{}, argErr("--command", "unknown")
}

// SystemClock は contract.Clock の製品ビルドの実装(実時計。UTC、秒まで)。
type SystemClock struct{}

var _ contract.Clock = SystemClock{}

// Now は contract.Clock を満たす。
func (SystemClock) Now(context.Context) (time.Time, error) {
	return time.Now().UTC().Truncate(time.Second), nil
}
