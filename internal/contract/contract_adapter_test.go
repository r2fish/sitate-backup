package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// アダプタ契約の試験ベクトル(testdata/adapter/{valid,invalid})。ファイル名の接頭辞でスキーマを選ぶ。
// valid は Go の型へ未知の項目なしで読めること(スキーマと型の項目が揃っていること)も確かめる。
func adapterSchema(name string) (schema string, into func() any) {
	switch {
	case strings.HasPrefix(name, "decl-"):
		return "adapter/adapter-json.json", func() any { return new(map[string]any) }
	}
	kind := strings.SplitN(name, "-request", 2)[0]
	if strings.Contains(name, "-request") {
		file := kind
		if kind == "restore-dest" {
			file = "restore-dest"
		}
		var f func() any
		switch kind {
		case "acquire":
			f = func() any { return new(AcquireRequest) }
		case "check":
			f = func() any { return new(CheckRequest) }
		case "restore-dest":
			f = func() any { return new(RestoreDestRequest) }
		case "notify":
			f = func() any { return new(NotifyRequest) }
		case "provision":
			f = func() any { return new(ProvisionRequest) }
		}
		return "adapter/" + file + ".json#/$defs/request", f
	}
	kind = strings.SplitN(name, "-", 2)[0]
	if strings.HasPrefix(name, "restore-dest-") {
		kind = "restore-dest"
	}
	var f func() any
	switch {
	case strings.Contains(name, "-error"):
		f = func() any { return new(AdapterErrorResult) }
	case kind == "acquire":
		f = func() any { return new(AcquireResult) }
	case kind == "check":
		f = func() any { return new(CheckResult) }
	case kind == "notify":
		f = func() any { return new(NotifyResult) }
	default:
		f = func() any { return new(map[string]any) }
	}
	return "adapter/" + kind + ".json#/$defs/response", f
}

func TestAdapterVectors(t *testing.T) {
	for _, tc := range []struct {
		dir  string
		want bool
	}{{"testdata/adapter/valid", true}, {"testdata/adapter/invalid", false}} {
		files, _ := filepath.Glob(tc.dir + "/*.json")
		if len(files) == 0 {
			t.Fatalf("%s: 試験ベクトルが無い", tc.dir)
		}
		for _, f := range files {
			b, _ := os.ReadFile(f)
			name := strings.TrimSuffix(filepath.Base(f), ".json")
			schema, into := adapterSchema(name)
			err := Validate(schema, b)
			if (err == nil) != tc.want {
				t.Errorf("%s: 期待と違う: %v", name, err)
			}
			if tc.want {
				dec := json.NewDecoder(bytes.NewReader(b))
				dec.DisallowUnknownFields()
				if err := dec.Decode(into()); err != nil {
					t.Errorf("%s: Go の型へ読めない: %v", name, err)
				}
			}
		}
	}
}

// 子の環境は PATH と LC_ALL=C のほかは許可した名前だけ。プロキシ・核の環境は渡さない。
func TestAdapterEnv(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid")
	got := AdapterEnv(map[string]string{EnvRecv: "/run/r", "HTTPS_PROXY": "x", "HOME": "/root", EnvExtraCAFile: "/ca"})
	want := []string{"PATH=" + AdapterEnvPATH, "LC_ALL=C", "SITATE_RECV=/run/r", "SITATE_EXTRA_CA_FILE=/ca"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %v want %v", got, want)
	}
	if len(AdapterEnv(nil)) != 2 {
		t.Errorf("extra なしは 2 件")
	}
}

// 秘密の枠と contract の表は adapter-json.json と一致する。
func TestAdapterSlotsMatchSchema(t *testing.T) {
	b, _ := os.ReadFile("schema/adapter/adapter-json.json")
	var doc struct {
		Defs map[string]struct {
			Properties struct {
				Contract struct{ Const string }
				Secrets  struct{ Items struct{ Enum []string } }
			}
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for kind, c := range AdapterContract {
		d := doc.Defs[strings.ReplaceAll(kind, "-", "_")]
		if d.Properties.Contract.Const != c {
			t.Errorf("%s: contract schema=%q go=%q", kind, d.Properties.Contract.Const, c)
		}
		if strings.Join(d.Properties.Secrets.Items.Enum, ",") != strings.Join(AdapterSecretSlots[kind], ",") {
			t.Errorf("%s: secrets 枠 schema=%v go=%v", kind, d.Properties.Secrets.Items.Enum, AdapterSecretSlots[kind])
		}
	}
}

// 骨格の adapter.json(version 0.0.0)以外は kind のスキーマに合う。骨格は記入前のためここでは検査しない。
func TestShippedAdapterJSON(t *testing.T) {
	files, _ := filepath.Glob("../../adapters/*/*/adapter.json")
	if len(files) == 0 {
		t.Fatal("adapter.json が無い")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var m struct{ Version string }
		_ = json.Unmarshal(b, &m)
		if m.Version == "0.0.0" {
			continue
		}
		if err := Validate("adapter/adapter-json.json", b); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
