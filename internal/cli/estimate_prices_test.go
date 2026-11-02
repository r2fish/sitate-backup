package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 価格表の読みと見積り(spec 3.1.2.8、6.1.1 の (3)、7.2 の規則 3・5・6・7。作業指示書 5.17 の追加の完了条件。D-0159)。

func pricesJSON(skipped, rows string) string {
	return `{"schema":"sitate.prices/v1","fx_jpy":{"USD":"150"},"rows":[` + rows + `],"skipped":[` + skipped + `]}`
}

const (
	cStorage = `{"item":"storage","amount":"0.01","currency":"USD","unit":"gb_month","per":"1","tier_upto":null,"free":null}`
	cEgress  = `{"item":"egress","amount":"0.01","currency":"USD","unit":"gb","per":"1","tier_upto":null,"free":null}`
	cPut     = `{"item":"request","ops":["PUT","LIST"],"amount":"0.005","currency":"USD","unit":"request","per":"1000","tier_upto":null,"free":null}`
	cGet     = `{"item":"request","ops":["GET"],"amount":"0.004","currency":"USD","unit":"request","per":"10000","tier_upto":null,"free":null}`
	cAll     = `{"item":"request","ops":["all"],"amount":"0.005","currency":"USD","unit":"request","per":"1000","tier_upto":null,"free":null}`
)

func rowWith(id string, idx int, host string, charges ...string) string {
	return `{"row_id":"` + id + `","kind":"storage","provider":"p q","plan":"x y","src":{"section":"storage","index":` + itoaT(idx) + `},"endpoint_patterns":["` + host + `"],"endpoint_default":true,"checked_on":"2026-11-02","charges":[` + strings.Join(charges, ",") + `]}`
}

func itoaT(n int) string { return string(rune('0' + n)) }

func runWith(t *testing.T, prices string, args map[string][]string) (map[string]any, []contract.Warning) {
	t.Helper()
	f := estimateEnv(t)
	f.d.Prices = func(context.Context) ([]byte, error) { return []byte(prices), nil }
	out, warns, err := estimateRun(t, f, args)
	if err != nil {
		t.Fatal(err)
	}
	return out, warns
}

var hostArgs = map[string][]string{"endpoint": {"https://s3.example.test"}, "size-gb": {"100"}, "daily-gb": {"1"}, "dump-gb": {"5"}}

func hasUnknown(w []contract.Warning) bool {
	for _, x := range w {
		if x.Code == contract.CodePriceUnknown {
			return true
		}
	}
	return false
}

// 26-a: 要る区分(保管量・転送・書き込み側・読み出し側)に当てはまる費目が行に無いときは 0 にせず PRICE_UNKNOWN。
func TestEstimateMissingClassIsPriceUnknown(t *testing.T) {
	full := []string{cStorage, cEgress, cPut, cGet}
	out, w := runWith(t, pricesJSON("", rowWith("a", 0, "s3.example.test", full...)), hostArgs)
	if hasUnknown(w) || y1(out, "total") == nil {
		t.Fatalf("全区分がある行は月額が出る: %v %v", w, out)
	}
	cases := map[string][]string{
		"読み出し側が無い": {cStorage, cEgress, cPut}, "書き込み側が無い": {cStorage, cEgress, cGet},
		"保管量が無い": {cEgress, cPut, cGet}, "転送が無い": {cStorage, cPut, cGet},
		"写像に無い語だけ": {cStorage, cEgress, strings.Replace(cPut, `"PUT","LIST"`, `"class_d"`, 1), cGet},
	}
	for name, cs := range cases {
		args := map[string][]string{"endpoint": hostArgs["endpoint"], "size-gb": {"100"}}
		out, w := runWith(t, pricesJSON("", rowWith("a", 0, "s3.example.test", cs...)), args)
		if !hasUnknown(w) || y1(out, "total") != nil || out["over_ceiling"] != nil {
			t.Errorf("%s: PRICE_UNKNOWN で total / over_ceiling が null になるはず: %v %v", name, w, out)
		}
	}
}

// 26-a: ops の all は書き込み側にも読み出し側にも当たる(分けて書いた同額の行と同じ額になる)。
func TestEstimateOpsAllCoversEveryOperation(t *testing.T) {
	sep := strings.Replace(cGet, `"amount":"0.004"`, `"amount":"0.005"`, 1)
	sep = strings.Replace(sep, `"per":"10000"`, `"per":"1000"`, 1)
	a, wa := runWith(t, pricesJSON("", rowWith("a", 0, "s3.example.test", cStorage, cEgress, cAll)), hostArgs)
	b, wb := runWith(t, pricesJSON("", rowWith("a", 0, "s3.example.test", cStorage, cEgress, cPut, sep)), hostArgs)
	if hasUnknown(wa) || hasUnknown(wb) || y1(a, "requests") == nil || y1(a, "requests").(float64) <= 0 || y1(a, "requests") != y1(b, "requests") {
		t.Fatalf("all=%v 分けた=%v %v %v", y1(a, "requests"), y1(b, "requests"), wa, wb)
	}
}

// 26-b: 桁が文字の無料枠(「100 万件」)は使わない。数の無料枠は引く。
func TestEstimateCharDigitFreeTierNotUsed(t *testing.T) {
	withFree := func(q string) string {
		c := strings.Replace(cAll, `"free":null`, `"free":{"quantity":"`+q+`","unit":"request","period":"month","basis":"absolute"}`, 1)
		out, w := runWith(t, pricesJSON("", rowWith("a", 0, "s3.example.test", cStorage, cEgress, c)), hostArgs)
		if hasUnknown(w) {
			t.Fatal(w)
		}
		return jsonOf(y1(out, "requests"))
	}
	none := withFree("0")
	if got := withFree("100万"); got != none {
		t.Errorf("桁が文字の無料枠を使った: %s != %s", got, none)
	}
	if got := withFree("100000000"); got == none {
		t.Errorf("数の無料枠が効いていない: %s", got)
	}
}

func jsonOf(v any) string { return fmt.Sprint(v) }

// 26-b: 月額の基本料金か最低料金がある行(同じ src の minimums・unmapped が skipped[] にある)は PRICE_UNKNOWN で over_ceiling が null。
// 最低保管期間だけの行(skipped に無い)や別の理由の skipped は当たらない。
func TestEstimateMinimumFeeRowIsPriceUnknown(t *testing.T) {
	row := rowWith("a", 0, "s3.example.test", cStorage, cEgress, cPut, cGet)
	unmapped := `{"src":{"section":"storage","index":0},"src_field":"minimums","reason":"unmapped"}`
	def := strings.Replace(applyDef, `"notify"`, `"cost_ceiling_yen":1,"notify"`, 1)
	for name, c := range map[string]struct {
		skipped string
		unknown bool
	}{
		"基本料金の行":     {unmapped, true},
		"別の src の項目": {strings.Replace(unmapped, `"index":0`, `"index":1`, 1), false},
		"理由が違う項目":    {strings.Replace(unmapped, "unmapped", "unconfirmed", 1), false},
		"欄が違う項目":     {strings.Replace(unmapped, "minimums", "request_price", 1), false},
		"項目なし":       {"", false},
	} {
		f := estimateEnv(t)
		f.d.Prices = func(context.Context) ([]byte, error) { return []byte(pricesJSON(c.skipped, row)), nil }
		d := strings.Replace(def, "https://s3.example-provider.com", "https://s3.example.test", 1)
		if err := f.d.State.SwitchDef(bg, contract.SwitchInput{Normalized: mustNorm(t, f, d)}); err != nil {
			t.Fatal(err)
		}
		out, w, err := estimateRun(t, f, map[string][]string{"size-gb": {"100"}})
		if err != nil {
			t.Fatal(err)
		}
		if hasUnknown(w) != c.unknown || (c.unknown && (out["over_ceiling"] != nil || y1(out, "total") != nil)) || (!c.unknown && out["over_ceiling"] != true) {
			t.Errorf("%s: warns=%v over_ceiling=%v total=%v", name, w, out["over_ceiling"], y1(out, "total"))
		}
	}
}

// 7.2 の規則 3: 量が最後の段の tier_upto を超える見積もりは PRICE_UNKNOWN。
func TestEstimateBeyondLastTierIsPriceUnknown(t *testing.T) {
	st := strings.Replace(cStorage, `"tier_upto":null`, `"tier_upto":{"quantity":"500","unit":"gb"}`, 1)
	_, w := runWith(t, pricesJSON("", rowWith("a", 0, "s3.example.test", st, cEgress, cPut, cGet)), hostArgs)
	if !hasUnknown(w) {
		t.Error("最後の段(500 GB)を超えても PRICE_UNKNOWN にならない")
	}
}

// 7.2 の「照合」: <region>・<bucket> は英小文字・数字・- の 1 文字以上(. を含まない)、それ以外のプレースホルダは 1 ラベルの任意(_ を含む名前も)。
func TestEndpointPlaceholders(t *testing.T) {
	cases := []struct {
		pat, host string
		want      bool
	}{
		{"s3.<region>.backblazeb2.com", "s3.us-west-004.backblazeb2.com", true},
		{"s3.<region>.backblazeb2.com", "s3.a.b.backblazeb2.com", false},
		{"<bucket>.s3.<region>.backblazeb2.com", "my-bkt1.s3.us-west-004.backblazeb2.com", true},
		{"<bucket>.s3.<region>.backblazeb2.com", "a.b.s3.us-west-004.backblazeb2.com", false},
		{"<account_id>.r2.cloudflarestorage.com", "0123abcd.r2.cloudflarestorage.com", true},
		{"<account_id>.r2.cloudflarestorage.com", "a.b.r2.cloudflarestorage.com", false},
		{"<storage-account>.blob.core.windows.net", "acct.blob.core.windows.net", true},
		{"<location>.your-objectstorage.com", "fsn1.your-objectstorage.com", true},
	}
	for _, c := range cases {
		if got := endpointRe(c.pat).MatchString(c.host); got != c.want {
			t.Errorf("%s vs %s: %v", c.pat, c.host, got)
		}
	}
}

// 署名した実物の prices.json(署名の検証は結線の担当で data/prices の TestPricesSignature が署名の形を確かめる)と B2 のエンドポイント:
// PRICE_UNKNOWN が無く、over_ceiling が null でなく、月額が出る。基本料金の行(Wasabi・Hetzner・IDrive・Storj・さくら)は PRICE_UNKNOWN。
func TestEstimateRealPricesB2(t *testing.T) {
	raw, err := os.ReadFile("../../data/prices/prices.json")
	if err != nil {
		t.Fatal(err)
	}
	run := func(endpoint string) (map[string]any, []contract.Warning) {
		f := estimateEnv(t)
		f.d.Prices = func(context.Context) ([]byte, error) { return raw, nil }
		d := strings.Replace(applyDef, `"notify"`, `"cost_ceiling_yen":3000,"notify"`, 1)
		d = strings.Replace(d, "https://s3.example-provider.com", endpoint, 1)
		if err := f.d.State.SwitchDef(bg, contract.SwitchInput{Normalized: mustNorm(t, f, d)}); err != nil {
			t.Fatal(err)
		}
		out, w, err := estimateRun(t, f, map[string][]string{"size-gb": {"100"}})
		if err != nil {
			t.Fatal(err)
		}
		return out, w
	}
	out, w := run("https://s3.us-west-004.backblazeb2.com")
	if hasUnknown(w) || out["over_ceiling"] == nil || y1(out, "total") == nil || y1(out, "requests") != 0.0 {
		t.Fatalf("B2: warns=%v over_ceiling=%v monthly=%v", w, out["over_ceiling"], out["monthly_jpy"])
	}
	for _, ep := range []string{"https://s3.ap-northeast-1.wasabisys.com", "https://s3.isk01.sakurastorage.jp", "https://gateway.storjshare.io"} {
		out, w := run(ep)
		if !hasUnknown(w) || out["over_ceiling"] != nil {
			t.Errorf("%s: 基本料金か最低料金のある行は PRICE_UNKNOWN: %v %v", ep, w, out["over_ceiling"])
		}
	}
}

// --price-table の行は結果のスキーマ(cli/results.json の price_row)に通る。provider・plan は入力のまま(空白と日本語を含む。spec 7.2。D-0159)。
func TestPriceTableRowsPassResultsSchema(t *testing.T) {
	raw, err := os.ReadFile("../../data/prices/prices.json")
	if err != nil {
		t.Fatal(err)
	}
	f := estimateEnv(t)
	f.d.Prices = func(context.Context) ([]byte, error) { return raw, nil }
	out, _, err := estimateRun(t, f, map[string][]string{"price-table": {""}})
	if err != nil {
		t.Fatal(err)
	}
	rows := out["prices"].([]any)
	if len(rows) < 15 {
		t.Fatalf("価格表の行が少ない: %d", len(rows))
	}
	for _, r := range rows {
		b, _ := json.Marshal(r)
		if err := contract.Validate("cli/results.json#/$defs/price_row", b); err != nil {
			t.Errorf("%s: %v", b, err)
			break
		}
	}
}
