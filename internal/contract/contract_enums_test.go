package contract_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// at は JSON 文書の JSON Pointer の位置の値を返す(スキーマの列挙の取り出し用)。
func at(t *testing.T, file, ptr string) any {
	t.Helper()
	b, err := os.ReadFile("schema/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, k := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		switch x := v.(type) {
		case map[string]any:
			v = x[k]
		default:
			t.Fatalf("%s %s: %q に進めない", file, ptr, k)
		}
		if v == nil {
			t.Fatalf("%s %s: %q が無い", file, ptr, k)
		}
	}
	return v
}

func strs(t *testing.T, v any) []string {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("配列でない: %v", v)
	}
	var r []string
	for _, x := range l {
		r = append(r, x.(string))
	}
	return r
}

func sameSet(t *testing.T, name string, schema, goVals []string) {
	t.Helper()
	a, b := append([]string{}, schema...), append([]string{}, goVals...)
	sort.Strings(a)
	sort.Strings(b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("%s: schema=%v go=%v", name, a, b)
	}
}

// Go の列挙(定数・一覧)と JSON Schema の enum の照合(D-0097 の 7)。error.code と warnings[].code は contract_cli_test.go。
func TestGoEnumsMatchSchemaEnums(t *testing.T) {
	const R = "cli/results.json"
	sameSet(t, "doctor.stage", strs(t, at(t, R, "/$defs/doctor/properties/stage/enum")), contract.DoctorStages)
	const items = "/$defs/doctor/properties/checks/items/properties/"
	sameSet(t, "doctor.checks[].name", strs(t, at(t, R, items+"name/enum")), contract.DoctorCheckNames())
	sameSet(t, "doctor.checks[].severity", strs(t, at(t, R, items+"severity/enum")), []string{"required", "advisory"})
	for _, c := range contract.DoctorChecks {
		if (c.Severity == "advisory") != (c.Stage == "all") {
			t.Errorf("%s: advisory はどの段階でも検査する", c.Name)
		}
	}
	if contract.DoctorCheckImagesCount != 0 {
		t.Error("イテレーション 1 の check_images の count は 0")
	}
	sameSet(t, "apply.waiting[].reason", strs(t, at(t, R, "/$defs/apply/properties/waiting/items/properties/reason/enum")), contract.WaitingReasons)
	sameSet(t, "source_survey.dirs[].label", strs(t, at(t, R, "/$defs/dir/properties/label/enum")), []string{contract.DirLabelHome, contract.DirLabelSrv, contract.DirLabelDBMount})
	sameSet(t, "source_survey.mounts[].class", strs(t, at(t, R, "/$defs/mount/properties/class/enum")),
		[]string{contract.MountClassCandidate, contract.MountClassParent, contract.MountClassDBData, contract.MountClassUnsupported})
	sameSet(t, "source_survey.prereq_missing", strs(t, at(t, R, "/$defs/source_survey/properties/prereq_missing/items/enum")),
		strs(t, at(t, "receiver/survey.json", "/$defs/source_survey/properties/prereq_missing/items/enum")))
	sameSet(t, "survey.scope", strs(t, at(t, "receiver/survey.json", "/$defs/source_survey/properties/scope/enum")), []string{contract.SurveyScopeFull, contract.SurveyScopeDBOnly})

	const S = "adapter/stream.json"
	var frames []string
	for _, d := range []string{"file", "file_retry", "unchanged", "dir", "symlink", "dump", "data", "item_end", "end"} {
		frames = append(frames, at(t, S, "/$defs/"+d+"/properties/type/const").(string))
	}
	sameSet(t, "stream frame type", frames, []string{contract.FrameTypeFile, contract.FrameTypeRetry, contract.FrameTypeUnchg, contract.FrameTypeDir,
		contract.FrameTypeSymlink, contract.FrameTypeDump, contract.FrameTypeData, contract.FrameTypeItemEnd, contract.FrameTypeEnd})
	sameSet(t, "stream dump.format", strs(t, at(t, S, "/$defs/dump/properties/format/enum")), []string{contract.DumpFormatGlobals, contract.DumpFormatCustom})
	sameSet(t, "stream item_end.status", strs(t, at(t, S, "/$defs/item_end/properties/status/enum")), []string{contract.ItemOK, contract.ItemChanged, contract.ItemError})

	sameSet(t, "recvpipe op", strs(t, at(t, "receiver/pipe.json", "/$defs/request/properties/op/enum")), contract.RecvOps)
	sameSet(t, "uncovered.kind", strs(t, at(t, R, "/$defs/uncovered/properties/kind/enum")), contract.UncoveredKinds)

	// source.declined の種別のキーと除外 id の形(spec 4.1)。
	decl := at(t, "data/definition.json", "/$defs/declined/properties").(map[string]any)
	var keys []string
	for k := range decl {
		if k != "id" {
			keys = append(keys, k)
		}
	}
	sameSet(t, "declined keys", keys, []string{contract.DeclinedKeyMount, contract.DeclinedKeyCluster})
	if p := at(t, "cli/common.json", "/$defs/declined_id/pattern").(string); p != contract.DeclinedIDPattern {
		t.Errorf("declined_id: schema=%s go=%s", p, contract.DeclinedIDPattern)
	}
	if !regexp.MustCompile(contract.DeclinedIDPattern).MatchString("d12") {
		t.Error("除外 id の形")
	}
}

// 汚染疑いの引き継ぎで置く checks[] の要素(spec 4.4 の checks[] の行): inherited: true の要素は result: fail、
// findings: []・findings_total: 0 で inherited_from を持つ。マニフェストでは引き継いだ要素だけが inherited を持つ。
// backup の結果では inherited を全要素に出す(引き継ぎでなければ false で inherited_from を持たない)。
func TestInheritedCheckElements(t *testing.T) {
	const mf = "data/manifest.json#/$defs/manifest_check"
	const rs = "cli/results.json#/$defs/check"
	mfBase := `{"name":"entropy-ratio","version":"1","phase":"ingest","result":"%s","findings":%s,"findings_total":%s%s}`
	cases := []struct {
		name, schema, doc string
		ok                bool
	}{
		{"manifest-inherited", mf, fmt.Sprintf(mfBase, "fail", "[]", "0", `,"inherited":true,"inherited_from":"m-0123456789abcdef0123456789abcdef"`), true},
		{"manifest-inherited-with-findings", mf, fmt.Sprintf(mfBase, "fail", `[{"item_ref":"f:abababababababababababababababab","code":"x"}]`, "1", `,"inherited":true,"inherited_from":"m-0123456789abcdef0123456789abcdef"`), false},
		{"manifest-inherited-total-nonzero", mf, fmt.Sprintf(mfBase, "fail", "[]", "3", `,"inherited":true,"inherited_from":"m-0123456789abcdef0123456789abcdef"`), false},
		{"manifest-inherited-pass", mf, fmt.Sprintf(mfBase, "pass", "[]", "0", `,"inherited":true,"inherited_from":"m-0123456789abcdef0123456789abcdef"`), false},
		{"manifest-inherited-false-not-allowed", mf, fmt.Sprintf(mfBase, "fail", "[]", "0", `,"inherited":false`), false},
		{"manifest-inherited-from-without-inherited", mf, fmt.Sprintf(mfBase, "fail", "[]", "0", `,"inherited_from":"m-0123456789abcdef0123456789abcdef"`), false},
		{"manifest-plain-with-findings", mf, fmt.Sprintf(mfBase, "warn", `[{"item_ref":"f:abababababababababababababababab","code":"x"}]`, "1", ``), true},
		{"manifest-plain", mf, fmt.Sprintf(mfBase, "pass", "[]", "0", ``), true},
		{"result-inherited-true", rs, `{"name":"c","version":"1","phase":"ingest","result":"fail","findings":[],"inherited":true,"inherited_from":"m-0123456789abcdef0123456789abcdef"}`, true},
		{"result-inherited-false", rs, `{"name":"c","version":"1","phase":"ingest","result":"pass","findings":[],"inherited":false}`, true},
		{"result-inherited-true-without-from", rs, `{"name":"c","version":"1","phase":"ingest","result":"fail","findings":[],"inherited":true}`, false},
		{"result-inherited-false-with-from", rs, `{"name":"c","version":"1","phase":"ingest","result":"fail","findings":[],"inherited":false,"inherited_from":"m-0123456789abcdef0123456789abcdef"}`, false},
	}
	for _, c := range cases {
		if err := contract.Validate(c.schema, []byte(c.doc)); (err == nil) != c.ok {
			t.Errorf("%s: ok=%v err=%v", c.name, c.ok, err)
		}
	}
}
