//go:build sitate_testbuild

package cli

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/state"
)

// 完了条件 4: 31 日の月の月曜日かつ 1 日の 00:00 から 35 模擬日。期待回数は spec 3.1.2 のタイマー列から。
func TestTestTickDays35Counts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sim")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	start := time.Date(2029, 1, 1, 0, 0, 0, 0, tokyo) // 月曜日・31 日の月
	if err := contract.WriteSimTime(path, start, time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(contract.EnvSimTimeFile, path)
	dir := t.TempDir()
	var mu sync.Mutex
	counts := map[string]int{}
	var lastArgv []string
	d := &Deps{StateDir: dir, Clock: SimClock{Path: path}, Limits: DefaultLimits()}
	d.State = &state.StateFiles{Dir: dir}
	d.spawn = func(_ context.Context, argv []string) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		if n := len(argv); n < 2 || argv[n-2] != "--invoked-by" || argv[n-1] != "timer" {
			t.Errorf("--invoked-by timer が無い: %v", argv)
		}
		counts[strings.Join(argv[:len(argv)-2], " ")]++
		lastArgv = argv
		return 0, nil
	}
	rep, err := testTick(bg, d, []string{"--days", "35"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"backup": 35, "verify": 35, "bucket check": 5, "extend": 5, "prune": 5, "restore-test": 35, "report": 2, "report --weekly": 5, "apply": 840}
	for k, w := range want {
		if counts[k] != w {
			t.Errorf("%s: %d 回、期待 %d", k, counts[k], w)
		}
	}
	if len(counts) != len(want) {
		t.Errorf("想定外の起動: %v (最後 %v)", counts, lastArgv)
	}
	if !strings.Contains(string(rep.Result), `"days":35`) {
		t.Errorf("結果: %s", rep.Result)
	}
	if now, _ := d.Clock.Now(bg); now.Sub(start.AddDate(0, 0, 35)).Abs() > 5*time.Second {
		t.Errorf("共有の値が 35 日後に進んでいない: %v", now)
	}
}
