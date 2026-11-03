package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func statusRun(t *testing.T, f *flowEnv) map[string]any {
	t.Helper()
	rep, err := statusEntry(f.d, context.Background(), contract.Call{Words: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate("cli/results.json#/$defs/status", rep.Result); err != nil {
		t.Fatalf("%v\n%s", err, rep.Result)
	}
	var out map[string]any
	_ = json.Unmarshal(rep.Result, &out)
	return out
}

func setDef(t *testing.T, f *flowEnv, endpoint, extra string) {
	t.Helper()
	d := strings.Replace(applyDef, `"notify"`, extra+`"notify"`, 1)
	d = strings.Replace(d, "https://s3.example-provider.com", endpoint, 1)
	if err := f.d.State.SwitchDef(bg, contract.SwitchInput{Normalized: mustNorm(t, f, d)}); err != nil {
		t.Fatal(err)
	}
}

// spec 4.1: cost_ceiling_yen の既定は 3,000(定義に書かないとき)。価格表に無い保管先は PRICE_UNKNOWN で月額と over_ceiling が null、警告が出る。
func TestStatusEstimateDefaultCeilingAndPriceUnknown(t *testing.T) {
	f := estimateEnv(t)
	setDef(t, f, "https://s3.unknown.example.org", "") // cost_ceiling_yen を書かない
	out := statusRun(t, f)
	est := out["estimate"].(map[string]any)
	if est["cost_ceiling_yen"] != 3000.0 || est["monthly_jpy"] != nil || est["over_ceiling"] != nil {
		t.Errorf("estimate: %v", est)
	}
	ws := out["warnings"].([]any)
	if len(ws) != 1 || ws[0].(map[string]any)["code"] != "PRICE_UNKNOWN" {
		t.Fatalf("warnings: %v", ws)
	}
	if d, _ := json.Marshal(ws[0].(map[string]any)["detail"]); string(d) != `{"missing":["storage"]}` {
		t.Errorf("detail: %s", d)
	}
}

// 定義の値があればそれ。価格表の行が引ける保管先では PRICE_UNKNOWN の警告を出さない。
func TestStatusEstimateKnownRowNoWarning(t *testing.T) {
	f := estimateEnv(t)
	setDef(t, f, "https://s3.us-west-004.example-b2.test", `"cost_ceiling_yen":1234,`)
	out := statusRun(t, f)
	if est := out["estimate"].(map[string]any); est["cost_ceiling_yen"] != 1234.0 {
		t.Errorf("estimate: %v", est)
	}
	if ws := out["warnings"].([]any); len(ws) != 0 {
		t.Errorf("warnings: %v", ws)
	}
}

// estimate: 上限は定義に書かなくても 3,000 と比べる。月額が出る(行がある)間は over_ceiling は null にならない(null は PRICE_UNKNOWN のときだけ)。
func TestEstimateDefaultCeilingIsCompared(t *testing.T) {
	f := estimateEnv(t)
	setDef(t, f, "https://s3.us-west-004.example-b2.test", "")
	out, _, err := estimateRun(t, f, map[string][]string{"size-gb": {"100"}})
	if err != nil {
		t.Fatal(err)
	}
	if out["cost_ceiling_yen"] != 3000.0 || out["over_ceiling"] != false {
		t.Errorf("既定の上限 3,000 と比べて超えない: ceiling=%v over=%v", out["cost_ceiling_yen"], out["over_ceiling"])
	}
	out, _, _ = estimateRun(t, f, map[string][]string{"size-gb": {"20000"}}) // 無料枠を引いても 3,000 円を超える大きさ
	if out["over_ceiling"] != true {
		t.Errorf("大きい入力は超える: %v", out["over_ceiling"])
	}
}
