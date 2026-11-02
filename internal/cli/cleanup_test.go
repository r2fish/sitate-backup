package cli

import (
	"context"
	"os"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type cleanupRec struct {
	calls [][2]string
	order *[]string
}

func (c *cleanupRec) Cleanup(_ context.Context, own, interrupted string) ([]contract.Resource, []contract.Resource, error) {
	c.calls = append(c.calls, [2]string{own, interrupted})
	return nil, nil, nil
}

const prevMarker = `{"run_id":"r-20261018T170000Z-aaaa","command":["verify"],"pid":1,"pid_start_ticks":1,"boot_id":"x","started_at":"2026-10-18T17:00:00Z","deadline_at":"2026-10-19T17:00:00Z"}`

// 規則 13・spec 7.5: ロックを取った実行の始めに RunCleanup を毎回呼ぶ。印が無い・読めない・完了済みでも呼び、読めた未完了の印の run_id だけ第 3 引数に渡す。
func TestRunCleanupCalledEveryLockedRun(t *testing.T) {
	cases := []struct {
		name   string
		marker string
		ended  bool
		want   string
	}{
		{"印なし", "", false, ""},
		{"読めない印", "{", false, ""},
		{"未完了の印", prevMarker, false, "r-20261018T170000Z-aaaa"},
		{"完了済みの印", prevMarker, true, ""},
	}
	for _, tc := range cases {
		f := newFlow(t)
		rc := &cleanupRec{}
		f.d.RunCleanup = []contract.RunCleanup{rc}
		if tc.ended {
			f.d.recordItem(bg, "run_end", "r-20261018T170000Z-aaaa", map[string]any{"command": []string{"verify"}, "ok": true, "code": nil, "envelope": map[string]any{}})
		}
		if tc.marker != "" {
			_ = os.MkdirAll(f.d.StateDir+"/run", 0o700)
			_ = os.WriteFile(markerPath(f.d.StateDir), []byte(tc.marker), 0o600)
		}
		f.run(t, []string{"backup"}, okEntry)
		if len(rc.calls) != 1 || rc.calls[0][0] != runA || rc.calls[0][1] != tc.want {
			t.Errorf("%s: %v (own=%s want interrupted=%q)", tc.name, rc.calls, runA, tc.want)
		}
	}
}

// RUN_ABORTED で終わった実行の後(印が消えている)でも、次の実行は RunCleanup を呼ぶ。
func TestRunCleanupAfterAbortedRun(t *testing.T) {
	f := newFlow(t)
	rc := &cleanupRec{}
	f.d.RunCleanup = []contract.RunCleanup{rc}
	f.run(t, []string{"backup"}, okEntry)
	f.run(t, []string{"backup"}, okEntry)
	if len(rc.calls) != 2 {
		t.Errorf("実行ごとに呼ぶ: %v", rc.calls)
	}
}
