package contract_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func TestSurveySchemaVectors(t *testing.T) {
	var d struct {
		Valid, Invalid []struct {
			Schema, Name string
			Doc          json.RawMessage
		}
	}
	loadVec(t, "testdata/survey/schema-vectors.json", &d)
	for ok, l := range map[bool]int{true: len(d.Valid), false: len(d.Invalid)} {
		if l == 0 {
			t.Fatalf("ok=%v: ベクトルが無い", ok)
		}
	}
	run := func(want bool, vs []struct {
		Schema, Name string
		Doc          json.RawMessage
	}) {
		for _, v := range vs {
			err := contract.Validate("receiver/survey.json#/$defs/"+v.Schema, v.Doc)
			if (err == nil) != want {
				t.Errorf("%s/%s: want ok=%v err=%v", v.Schema, v.Name, want, err)
			}
		}
	}
	run(true, d.Valid)
	run(false, d.Invalid)
}

// 判定の入力(材料)は source-survey のスキーマに、各ベクトルの mounts などの要素が合うことは rules の読み込みで確かめる。
func TestSurveyRules(t *testing.T) {
	var d struct {
		Survey []struct {
			Name   string
			In     harness.SourceSurveyIn
			Expect harness.SurveyOut
		}
		Check []struct {
			Name   string
			In     harness.CheckIn
			Expect []harness.Uncovered
		}
	}
	loadVec(t, "testdata/survey/rules.json", &d)
	if len(d.Survey) < 6 || len(d.Check) < 11 {
		t.Fatalf("ベクトルの数: %d %d", len(d.Survey), len(d.Check))
	}
	for _, v := range d.Survey {
		if got := harness.JudgeSurvey(v.In); !reflect.DeepEqual(got, v.Expect) {
			gj, _ := json.Marshal(got)
			ej, _ := json.Marshal(v.Expect)
			t.Errorf("survey/%s:\n got  %s\n want %s", v.Name, gj, ej)
		}
	}
	for _, v := range d.Check {
		want := append([]harness.Uncovered{}, v.Expect...)
		harness.SortUncovered(want)
		got := harness.JudgeCheck(v.In)
		if !reflect.DeepEqual(got, want) {
			gj, _ := json.Marshal(got)
			ej, _ := json.Marshal(want)
			t.Errorf("check/%s:\n got  %s\n want %s", v.Name, gj, ej)
		}
		for _, u := range got {
			ok := false
			for _, k := range contract.UncoveredKinds {
				ok = ok || k == u.Kind
			}
			if !ok {
				t.Errorf("check/%s: 未知の kind %s", v.Name, u.Kind)
			}
		}
	}
}

func TestSurveyEmbedAndRoots(t *testing.T) {
	var d struct {
		Survey []struct {
			Name, Scope string
			DuTimeoutS  int `json:"du_timeout_s"`
			Want        *string
		}
		Setup []struct {
			Name     string
			Clusters []harness.StateCluster
			Want     *string
		}
		Forbidden []struct{ Path, Want string } `json:"forbidden_roots"`
	}
	loadVec(t, "testdata/survey/embed.json", &d)
	for _, v := range d.Survey {
		got, err := harness.SurveyPreamble(v.Scope, v.DuTimeoutS)
		if (v.Want == nil) != (err != nil) || (err == nil && got != *v.Want) {
			t.Errorf("survey %s: %q %v", v.Name, got, err)
		}
	}
	if p, err := harness.SurveyPreamble(contract.SurveyScopeFull, contract.SurveyDuTimeoutS); err != nil || p == "" {
		t.Errorf("定数の既定値で埋め込めない: %v", err)
	}
	for _, v := range d.Setup {
		got, err := harness.SetupPreamble(v.Clusters)
		if (v.Want == nil) != (err != nil) || (err == nil && got != *v.Want) {
			t.Errorf("setup %s: %q %v", v.Name, got, err)
		}
	}
	for _, v := range d.Forbidden {
		if got := harness.RootForbidden(v.Path); got != v.Want {
			t.Errorf("root %q: %s want %s", v.Path, got, v.Want)
		}
	}
}

// SOURCE_SURVEY_FAILED の detail は (step, reason) の 4 つの組だけを受け、それ以外の組を退ける。
func TestContractSurveyFailedDetailPairs(t *testing.T) {
	for _, p := range contract.SurveyFailPairs {
		b := []byte(`{"step":"` + p[0] + `","reason":"` + p[1] + `"}`)
		if err := contract.Validate("cli/detail.json#/$defs/SOURCE_SURVEY_FAILED", b); err != nil {
			t.Errorf("%v を受けない: %v", p, err)
		}
	}
	for _, bad := range []string{`{"step":"connect","reason":"unsupported_os"}`, `{"step":"sudo","reason":"auth"}`, `{"step":"prereq","reason":"tool_missing"}`, `{"step":"other","reason":"auth"}`} {
		if contract.Validate("cli/detail.json#/$defs/SOURCE_SURVEY_FAILED", []byte(bad)) == nil {
			t.Errorf("%s を受けた", bad)
		}
	}
}
