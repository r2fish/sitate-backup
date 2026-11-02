//go:build sitate_testbuild

package cli

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var daysRe = regexp.MustCompile(`^[1-9][0-9]{0,5}$`)

// testTick は試験ビルドだけのサブコマンド `test-tick`(spec 3.1.2、作業指示書 3.3)。
// `--set-start <RFC 3339>` は共有の値を置くだけ(単位は起動しない)。`--days N` は模擬時間を進めながらタイマーの単位を子プロセスで起動する。
// 共通の引数(--run-id・--dry-run など)は受けない。共有の値の場所が無いときは ARG_INVALID(format)。
func testTick(ctx context.Context, d *Deps, rest []string) (contract.Reply, error) {
	if len(rest) != 2 || (rest[0] != contract.TestTickFlagDays && rest[0] != contract.TestTickFlagSetStart) {
		flag := "--command"
		if len(rest) > 0 && len(rest[0]) > 2 && rest[0][:2] == "--" {
			flag = rest[0]
		}
		return contract.Reply{}, argErr(flag, "unknown")
	}
	flag, val := rest[0], rest[1]
	path := os.Getenv(contract.EnvSimTimeFile)
	if flag == contract.TestTickFlagSetStart {
		return setStart(path, flag, val)
	}
	if !daysRe.MatchString(val) {
		return contract.Reply{}, argErr(flag, "format")
	}
	if _, err := contract.ReadSimTime(path); err != nil {
		return contract.Reply{}, argErr(flag, "format")
	}
	n, _ := strconv.Atoi(val)
	return tickDays(ctx, d, path, n)
}

func setStart(path, flag, val string) (contract.Reply, error) {
	t, err := time.Parse(time.RFC3339, val)
	if err != nil || path == "" {
		return contract.Reply{}, argErr(flag, "format")
	}
	if cur, err := contract.ReadSimTime(path); err == nil {
		if now, err := cur.Now(time.Now()); err == nil && t.Before(now) {
			return contract.Reply{}, argErr(flag, "mismatch") // 戻さない(test-tick-args.json は reason: range とするが、ARG_INVALID の detail の値域に無い。報告)
		}
	}
	if err := contract.WriteSimTime(path, t, time.Now()); err != nil {
		return contract.Reply{}, internalErr()
	}
	b, _ := json.Marshal(map[string]any{"days": 0, "now": t.UTC().Format(time.RFC3339)})
	return contract.Reply{Result: b}, nil
}
