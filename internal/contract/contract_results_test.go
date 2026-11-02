package contract

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 結果と detail の試験ベクトル(testdata/cli/*-samples.json)が封筒のスキーマに合う。
func envelope(command []string, ok bool, result any, warnings []any, errObj any) []byte {
	m := map[string]any{"schema": "sitate.out/v1", "command": command, "ok": ok, "dry_run": false,
		"run_id": "r-20261018T173000Z-3f9a", "result": result, "warnings": warnings, "error": errObj}
	if warnings == nil {
		m["warnings"] = []any{}
	}
	b, _ := json.Marshal(m)
	return b
}

func defName(words []string) string {
	return strings.ReplaceAll(strings.Join(words, "_"), "-", "_")
}

func TestResultSamples(t *testing.T) {
	b, err := os.ReadFile("testdata/cli/results-samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Results map[string]json.RawMessage `json:"results"`
		Extra   sampleSet                  `json:"extra"`
		Reject  sampleSet                  `json:"reject"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	words := map[string][]string{}
	for _, s := range SubcommandTable {
		words[defName(s.Words)] = s.Words
	}
	for name, w := range words {
		if _, ok := doc.Results[name]; !ok {
			t.Errorf("結果の試験ベクトルが無い: %s", name)
			continue
		}
		var res any
		_ = json.Unmarshal(doc.Results[name], &res)
		if err := Validate("cli/envelope.json", envelope(w, true, res, nil, nil)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		// 項目を 1 つ足した結果は拒否される(additionalProperties:false)
		if m, ok := res.(map[string]any); ok {
			m["__extra"] = 1
			if Validate("cli/envelope.json", envelope(w, true, m, nil, nil)) == nil {
				t.Errorf("%s: 未知の項目を拒否しない", name)
			}
		}
	}
	checkSampleSet(t, doc.Reject, words, false)
	checkSampleSet(t, doc.Extra, words, true)
}

type sampleSet map[string]struct {
	Def    string          `json:"def"`
	Result json.RawMessage `json:"result"`
}

// checkSampleSet は extra(合格)か reject(拒否)の例を、定義名から引いた語の封筒で検証する。
func checkSampleSet(t *testing.T, set sampleSet, words map[string][]string, accept bool) {
	t.Helper()
	for name, e := range set {
		var res any
		_ = json.Unmarshal(e.Result, &res)
		w, ok := words[e.Def]
		if !ok {
			t.Fatalf("%s: 定義 %s が表に無い", name, e.Def)
		}
		err := Validate("cli/envelope.json", envelope(w, true, res, nil, nil))
		if accept && err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if !accept && err == nil {
			t.Errorf("%s: 拒否すべき結果を通した", name)
		}
	}
}

func TestDetailSamples(t *testing.T) {
	b, err := os.ReadFile("testdata/cli/detail-samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var det map[string]any
	if err := json.Unmarshal(b, &det); err != nil {
		t.Fatal(err)
	}
	for code, info := range CodeTable {
		d, ok := det[string(code)]
		if !ok {
			t.Errorf("detail の試験ベクトルが無い: %s", code)
			continue
		}
		next := []any{}
		if info.Error || info.Failed {
			e := map[string]any{"code": string(code), "class": ExitClass(info.Exit), "retryable": info.Retryable, "detail": d, "next": next}
			if info.Exit == 0 { // 警告としても現れるコードの error 側(終了は別のサブコマンドで決まる)
				e["class"] = "precondition"
			}
			if err := Validate("cli/envelope.json", envelope([]string{"backup"}, false, nil, nil, e)); err != nil {
				t.Errorf("error %s: %v", code, err)
			}
		}
		if info.Warning {
			w := map[string]any{"code": string(code), "detail": d, "next": next}
			var res any
			_ = json.Unmarshal(sampleResult(t, "backup"), &res)
			if err := Validate("cli/envelope.json", envelope([]string{"backup"}, true, res, []any{w}, nil)); err != nil {
				t.Errorf("warning %s: %v", code, err)
			}
		}
	}
}

// 文字列の項目は列挙型・形式(pattern/format/const/enum)・「データ」の印(x-sitate-data)のどれかを持つ。
// 型の無い {} の項目も置かない。
func TestSchemaStringsTyped(t *testing.T) {
	paths, _ := SchemaPaths()
	for _, p := range paths {
		b, _ := os.ReadFile("schema/" + p)
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		walkSchema(t, p, "", doc)
	}
}

func hasType(m map[string]any, typ string) bool {
	switch v := m["type"].(type) {
	case string:
		return v == typ
	case []any:
		for _, x := range v {
			if x == typ {
				return true
			}
		}
	}
	return false
}

func walkSchema(t *testing.T, file, at string, v any) {
	switch x := v.(type) {
	case []any:
		for i, e := range x {
			walkSchema(t, file, at+"/"+string(rune('0'+i%10)), e)
		}
	case map[string]any:
		if len(x) == 0 && !strings.Contains(at, "/enum") && !strings.Contains(at, "/const") {
			t.Errorf("%s%s: 型の無い {} がある", file, at)
		}
		if hasType(x, "string") {
			_, e1 := x["enum"]
			_, e2 := x["pattern"]
			_, e3 := x["format"]
			_, e4 := x["const"]
			_, e5 := x["x-sitate-data"]
			if !(e1 || e2 || e3 || e4 || e5) {
				t.Errorf("%s%s: 文字列の項目に列挙・形式・データの印のどれも無い", file, at)
			}
		}
		for k, e := range x {
			if k == "enum" || k == "const" || k == "required" {
				continue
			}
			walkSchema(t, file, at+"/"+k, e)
		}
	}
}

func sampleResult(t *testing.T, name string) json.RawMessage {
	b, err := os.ReadFile("testdata/cli/results-samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Results map[string]json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Results[name]
}

// データ(状態・記録)のスキーマの試験ベクトル。ファイル名の接頭辞でスキーマを選ぶ。
var dataVectorSchemas = map[string]string{
	"attest-":      "data/change.json#/$defs/attest_record",
	"changeerror-": "data/change.json#/$defs/error",
	"kit-result":   "cli/envelope.json",
	"manifest-":    "data/manifest.json",
	"entry-":       "data/manifest.json#/$defs/entry",
	"event-":       "data/event.json",
	"ledger-":      "data/state.json#/$defs/ledger",
	"marker-":      "data/state.json#/$defs/run_marker",
	"state-":       "data/state.json",
	"prices-":      "data/prices.json",
}

func TestDataVectors(t *testing.T) {
	for _, tc := range []struct {
		dir  string
		want bool
	}{{"testdata/data/valid", true}, {"testdata/data/invalid", false}} {
		ents, _ := os.ReadDir(tc.dir)
		if len(ents) == 0 {
			t.Fatalf("%s: 試験ベクトルが無い", tc.dir)
		}
		for _, e := range ents {
			b, _ := os.ReadFile(tc.dir + "/" + e.Name())
			target := ""
			for pre, s := range dataVectorSchemas {
				if strings.HasPrefix(e.Name(), pre) {
					target = s
				}
			}
			if target == "" {
				t.Fatalf("%s: 対応するスキーマが無い", e.Name())
			}
			if e.Name() == "kit-result.json" {
				var res any
				_ = json.Unmarshal(b, &res)
				b = envelope([]string{"kit"}, true, res, nil, nil)
			}
			err := Validate(target, b)
			if (err == nil) != tc.want {
				t.Errorf("%s: 期待と違う: %v", e.Name(), err)
			}
		}
	}
}

// attest の署名を要さない 3 段は Go の定数とスキーマで一致し、署名を付ける段の値域に無い。
func TestAttestSteps(t *testing.T) {
	b, _ := os.ReadFile("schema/data/change.json")
	var doc struct {
		Defs struct {
			U struct {
				Enum []string `json:"enum"`
			} `json:"attest_step_unsigned"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if strings.Join(doc.Defs.U.Enum, ",") != strings.Join(AttestUnsignedSteps, ",") {
		t.Errorf("schema=%v go=%v", doc.Defs.U.Enum, AttestUnsignedSteps)
	}
}
