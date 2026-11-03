package cli

// spec 3.1.2 の「attest の段」と 3.7 bootstrap の段 (8): 署名を要さない 3 段は init の前も記録できる(init の前は state.json の項目だけを置く)。
// 署名を要する段は init の前は NOT_INITIALIZED のまま。

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// attestDeps は試験ビルドの核が要る共有の模擬時間を置いてから部品を作る(時計は作るときに決まる。製品ビルドはこの変数を読まない)。
func attestDeps(t *testing.T) *Deps {
	t.Helper()
	path, now := filepath.Join(t.TempDir(), "simtime"), time.Now()
	if err := contract.WriteSimTime(path, now, now); err != nil {
		t.Fatal(err)
	}
	t.Setenv(contract.EnvSimTimeFile, path)
	return DefaultDeps(t.TempDir())
}

func runAttest(t *testing.T, d *Deps, args ...string) (map[string]any, int) {
	t.Helper()
	var out bytes.Buffer
	code := Run(d, append([]string{"attest"}, args...), &out, &out)
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("封筒が読めない: %v %q", err, out.String())
	}
	return env, code
}

func TestAttestUnsignedStepsRecordBeforeInit(t *testing.T) {
	for item, step := range map[string]string{"admin_access_closed_at": "admin_access_closed", "admin_access_opened_at": "admin_access_opened"} {
		d := attestDeps(t)
		env, code := runAttest(t, d, "--step", step)
		if code != 0 {
			t.Fatalf("%s は init の前も記録できるはず: code=%d env=%v", step, code, env)
		}
		res, _ := env["result"].(map[string]any)
		if res["step"] != step || res["recorded"] != true || res["signed"] != false {
			t.Errorf("%s の結果が違う: %v", step, res)
		}
		v, err := d.State.Get(context.Background(), item)
		if err != nil || string(v) == "null" {
			t.Errorf("%s: %s が置かれていない: %s %v", step, item, v, err)
		}
		if v, _ := d.State.Get(context.Background(), "initialized"); string(v) == "true" {
			t.Errorf("attest が initialized を立てた")
		}
	}
}

func TestAttestSignedStepsStillNeedInit(t *testing.T) {
	for _, step := range []string{"R3", "R7", "C1"} {
		d := attestDeps(t)
		env, code := runAttest(t, d, "--step", step)
		if code == 0 {
			t.Fatalf("%s は init の前は失敗するはず: %v", step, env)
		}
		e, _ := env["error"].(map[string]any)
		if e["code"] != "NOT_INITIALIZED" {
			t.Errorf("%s: NOT_INITIALIZED のはず: %v", step, e)
		}
	}
}

func TestAttestUnknownStepBeforeInitIsArgInvalid(t *testing.T) {
	env, code := runAttest(t, DefaultDeps(t.TempDir()), "--step", "bogus")
	e, _ := env["error"].(map[string]any)
	if code == 0 || (e["code"] != "NOT_INITIALIZED" && e["code"] != "ARG_INVALID") {
		t.Fatalf("未知の段は失敗のはず: %v", env)
	}
}
