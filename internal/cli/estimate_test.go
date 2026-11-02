package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const testPrices = `{"schema":"sitate.prices/v1","fx_jpy":{"USD":"150"},"rows":[
 {"row_id":"b2-payg","kind":"storage","provider":"example-b2","plan":"Pay-as-you-go","endpoint_patterns":["s3.<region>.example-b2.test"],"endpoint_default":true,"checked_on":"2026-11-02",
  "charges":[
   {"item":"storage","amount":"0.006","currency":"USD","unit":"gb_month","per":"1","tier_upto":null,"free":{"quantity":"10","unit":"gb","period":"month","basis":"absolute"}},
   {"item":"egress","amount":"0.01","currency":"USD","unit":"gb","per":"1","tier_upto":null,"free":{"quantity":"3","unit":"gb","period":"month","basis":"stored_ratio"}}]},
 {"row_id":"s3-std","kind":"storage","provider":"example-s3","plan":"Standard","endpoint_patterns":["s3.<region>.example-s3.test"],"endpoint_default":true,"checked_on":"2026-10-17",
  "charges":[
   {"item":"storage","amount":"0.025","currency":"USD","unit":"gb_month","per":"1","tier_upto":{"quantity":"500","unit":"gb"},"free":null},
   {"item":"storage","amount":"0.020","currency":"USD","unit":"gb_month","per":"1","tier_upto":null,"free":null},
   {"item":"request","ops":["PUT","LIST"],"amount":"0.0047","currency":"USD","unit":"request","per":"1000","tier_upto":null,"free":null}]},
 {"row_id":"vps-2g","kind":"compute","provider":"example-vps","plan":"2GB","endpoint_patterns":[],"endpoint_default":false,"checked_on":"2026-10-17",
  "charges":[{"item":"instance","amount":"770","currency":"JPY","unit":"month","per":"1","tier_upto":null,"free":null}]}]}`

func estimateEnv(t *testing.T) *flowEnv {
	t.Helper()
	f, _ := applyEnv(t, &applyStub{})
	f.d.Prices = func(context.Context) ([]byte, error) { return []byte(testPrices), nil }
	return f
}

func estimateRun(t *testing.T, f *flowEnv, args map[string][]string) (map[string]any, []contract.Warning, error) {
	t.Helper()
	rep, err := estimateEntry(f.d, context.Background(), contract.Call{Words: []string{"estimate"}, Args: args})
	var out map[string]any
	if err == nil && len(rep.Result) > 0 {
		_ = json.Unmarshal(rep.Result, &out)
	}
	return out, rep.Warnings, err
}

func y1(out map[string]any, key string) any {
	return out["monthly_jpy"].([]any)[0].(map[string]any)["y1"].(map[string]any)[key]
}

// 作業指示書 5.17 の完了条件 8: 定義の無い状態で --endpoint が価格表の行に当たると、その行で月額が出て、バックアップサーバーと復元先は未計上。
func TestEstimateEndpointWithoutDefinition(t *testing.T) {
	f := estimateEnv(t)
	out, warns, err := estimateRun(t, f, map[string][]string{"endpoint": {"https://s3.us-west-004.example-b2.test"}, "size-gb": {"100"}})
	if err != nil || len(warns) != 0 {
		t.Fatalf("%v %v", err, warns)
	}
	if out["basis"] != "args" || out["price_table_date"] != "2026-11-02" {
		t.Errorf("%v %v", out["basis"], out["price_table_date"])
	}
	in := out["inputs"].(map[string]any)
	if in["size_gb"] != 100.0 || in["daily_gb"] != 1.0 || in["dump_gb"] != 5.0 {
		t.Errorf("省いた引数の目安(対象データの 1 パーセントと 5 パーセント): %v", in)
	}
	if cap := out["capacity_gb"].(map[string]any); cap["y1"].(float64) < 1028.4 || cap["y1"].(float64) > 1028.6 {
		t.Errorf("1 年目の累積容量(spec 6.1.2 は 1,028.5 GB): %v", cap["y1"])
	}
	// (1028.5 - 無料 10 GB) × 0.006 USD × 150 円 = 916.65 → 917
	if y1(out, "storage") != 917.0 || y1(out, "egress") != 0.0 || y1(out, "server") != nil || y1(out, "restore_dest") != nil {
		t.Errorf("月額: storage=%v egress=%v server=%v", y1(out, "storage"), y1(out, "egress"), y1(out, "server"))
	}
	if u := out["uncounted"].([]any); len(u) != 2 || u[0] != "server" || u[1] != "restore_dest" {
		t.Errorf("uncounted: %v", u)
	}
	if out["over_ceiling"] != nil {
		t.Errorf("定義が無ければ上限と比べない: %v", out["over_ceiling"])
	}
}

func TestEstimateTiersAndUnknownEndpoint(t *testing.T) {
	f := estimateEnv(t)
	out, _, err := estimateRun(t, f, map[string][]string{"endpoint": {"s3.ap-northeast-1.example-s3.test"}, "size-gb": {"100"}, "daily-gb": {"1"}, "dump-gb": {"5"}})
	if err != nil {
		t.Fatal(err)
	}
	// 500 GB までは 0.025、超過分は 0.020: (500×0.025 + 528.5×0.020) × 150
	want := float64(int64((500*0.025+528.5*0.020)*150 + 0.5))
	if y1(out, "storage") != want {
		t.Errorf("階層: %v 期待 %v", y1(out, "storage"), want)
	}
	out, warns, err := estimateRun(t, f, map[string][]string{"endpoint": {"s3.unknown.example.org"}, "size-gb": {"100"}})
	if err != nil || len(warns) != 1 || warns[0].Code != contract.CodePriceUnknown || !strings.Contains(string(warns[0].Detail), `"storage"`) {
		t.Fatalf("%v %v", warns, err)
	}
	if y1(out, "storage") != nil || y1(out, "total") != nil || out["price_table_date"] != nil {
		t.Errorf("価格表に無い事業者の月額は出さない: %v", out)
	}
}

func TestEstimateServerMonthlyAndEndpointOverridesDefinition(t *testing.T) {
	f := estimateEnv(t)
	d := strings.Replace(applyDef, `"notify"`, `"server_monthly_yen":770,"cost_ceiling_yen":1000,"notify"`, 1)
	d = strings.Replace(d, "https://s3.example-provider.com", "https://s3.us-west-004.example-b2.test", 1)
	if err := f.d.State.SwitchDef(bg, contract.SwitchInput{Normalized: mustNorm(t, f, d)}); err != nil {
		t.Fatal(err)
	}
	out, _, err := estimateRun(t, f, map[string][]string{"size-gb": {"100"}})
	if err != nil {
		t.Fatal(err)
	}
	if y1(out, "server") != 770.0 || y1(out, "total") != 917.0+770.0 || out["over_ceiling"] != true || out["cost_ceiling_yen"] != 1000.0 {
		t.Errorf("定義の保管先と server_monthly_yen: %v", out)
	}
	if u := out["uncounted"].([]any); len(u) != 1 || u[0] != "restore_dest" {
		t.Errorf("uncounted: %v", u)
	}
	out, _, _ = estimateRun(t, f, map[string][]string{"size-gb": {"100"}, "endpoint": {"s3.ap-northeast-1.example-s3.test"}})
	if y1(out, "storage") == 917.0 {
		t.Error("--endpoint が定義の値より優先されない")
	}
}

func TestEstimatePriceTableAndArgs(t *testing.T) {
	f := estimateEnv(t)
	out, _, err := estimateRun(t, f, map[string][]string{"price-table": {""}})
	rows := out["prices"].([]any)
	if err != nil || len(rows) != 3 || rows[2].(map[string]any)["monthly_jpy"] != 770.0 || rows[0].(map[string]any)["monthly_jpy"] != nil {
		t.Fatalf("%v %v", out, err)
	}
	if _, _, err := estimateRun(t, f, map[string][]string{"endpoint": {"x"}}); err == nil || !strings.Contains(err.Error(), "ARG_INVALID") {
		t.Errorf("--size-gb が無い: %v", err)
	}
	if _, _, err := estimateRun(t, f, map[string][]string{"size-gb": {"-1"}}); err == nil {
		t.Error("負の大きさ")
	}
	f.d.Prices = nil
	if _, _, err := estimateRun(t, f, map[string][]string{"size-gb": {"1"}}); err == nil || !strings.Contains(err.Error(), "NOT_IMPLEMENTED") {
		t.Errorf("価格表が結線されていない: %v", err)
	}
}

const gcsPrices = `{"schema":"sitate.prices/v1","fx_jpy":{"USD":"150"},"rows":[
 {"row_id":"gcs-std","kind":"storage","provider":"example-gcs","plan":"Standard","endpoint_patterns":["storage.example-gcs.test"],"endpoint_default":true,"checked_on":"2026-11-02",
  "charges":[
   {"item":"storage","amount":"0.02","currency":"USD","unit":"gb_month","per":"1","tier_upto":null,"free":null},
   {"item":"request","ops":["class_a"],"amount":"0.005","currency":"USD","unit":"request","per":"1000","tier_upto":null,"free":null},
   {"item":"request","ops":["class_b"],"amount":"0.0004","currency":"USD","unit":"request","per":"1000","tier_upto":null,"free":null},
   {"item":"request","ops":["class_d"],"amount":"0.0","currency":"USD","unit":"request","per":"1000","tier_upto":null,"free":null}]}]}`

// 価格表の review: GCS の class_a・class_b が核の操作の区分(書き込み・一覧 / 読み出し)に当たり、要求の月額が 0 円にならない(spec 6.1.1 の (3))。
func TestEstimateGCSRequestClasses(t *testing.T) {
	f := estimateEnv(t)
	f.d.Prices = func(context.Context) ([]byte, error) { return []byte(gcsPrices), nil }
	out, _, err := estimateRun(t, f, map[string][]string{"endpoint": {"https://storage.example-gcs.test"}, "size-gb": {"100"}, "daily-gb": {"1"}, "dump-gb": {"5"}})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := y1(out, "requests").(float64)
	if req <= 0 {
		t.Fatalf("GCS の要求が 0 円: %v", y1(out, "requests"))
	}
	// class_a だけの価格にすると小さくなる(class_b の分が消える)。class_a と class_b の両方が効いている。
	only := strings.Replace(gcsPrices, `"amount":"0.0004"`, `"amount":"0.0"`, 1)
	f.d.Prices = func(context.Context) ([]byte, error) { return []byte(only), nil }
	out2, _, _ := estimateRun(t, f, map[string][]string{"endpoint": {"https://storage.example-gcs.test"}, "size-gb": {"100"}, "daily-gb": {"1"}, "dump-gb": {"5"}})
	req2, _ := y1(out2, "requests").(float64)
	if req2 <= 0 || req2 >= req {
		t.Errorf("class_a=%v class_a+class_b=%v", req2, req)
	}
}
