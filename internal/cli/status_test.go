package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func putState(t *testing.T, d *Deps, item, v string) {
	t.Helper()
	h, err := d.State.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	if err := d.State.Update(context.Background(), h, item, json.RawMessage(v)); err != nil {
		t.Fatal(err)
	}
}

// 何も書かれていない状態でも、結果は status のスキーマに合う(記録を持たない新しいホスト)。
func TestStatusEmptyMatchesSchema(t *testing.T) {
	d, out := defDeps(t, "")
	Run(d, []string{"status"}, out, nil)
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	_ = json.Unmarshal(out.Bytes(), &env)
	if !env.OK {
		t.Fatalf("%s", out.String())
	}
	if err := contract.Validate("cli/results.json#/$defs/status", env.Result); err != nil {
		t.Fatalf("%v\n%s", err, env.Result)
	}
}

// 完了条件 23: チェックの無い世代は ok が true で checks_count が 0。pass でない世代・対象が ok でない世代は false。
func TestStatusLastBackupWithoutChecks(t *testing.T) {
	snap := func(id, at, health string, ok bool) string {
		return `{"ref":"m-` + strings.Repeat(id, 32) + `","created_at":"` + at + `","health":"` + health + `","targets_ok":` + map[bool]string{true: "true", false: "false"}[ok] + `}`
	}
	for _, c := range []struct {
		health string
		ok     bool
		want   bool
	}{{"pass", true, true}, {"incomplete", true, false}, {"pass", false, false}} {
		d, out := defDeps(t, "")
		putState(t, d, "initialized", "true")
		putState(t, d, "snapshots", "["+snap("1", "2026-11-01T02:00:00Z", "pass", true)+","+snap("2", "2026-11-02T02:00:00Z", c.health, c.ok)+"]")
		env, _ := runCLI(d, out, "status")
		lb := env["result"].(map[string]any)["last_backup"].(map[string]any)
		if lb["ok"] != c.want || lb["checks_count"] != float64(0) || lb["health"] != c.health {
			t.Errorf("%+v: %v", c, lb)
		}
	}
}
