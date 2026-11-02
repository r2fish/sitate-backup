package contract

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestAllSchemasCompile(t *testing.T) {
	paths, err := SchemaPaths()
	if err != nil || len(paths) == 0 {
		t.Fatalf("schema paths: %v %v", paths, err)
	}
	for _, p := range paths {
		if _, err := loadCompiler(); err != nil {
			t.Fatal(err)
		}
		c, _ := loadCompiler()
		if _, err := c.Compile(SchemaBase + p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestCLIVectors(t *testing.T) {
	for _, tc := range []struct {
		dir  string
		want bool
	}{{"testdata/cli/valid", true}, {"testdata/cli/invalid", false}} {
		files, _ := filepath.Glob(filepath.Join(tc.dir, "*.json"))
		if len(files) == 0 {
			t.Fatalf("%s: 試験ベクトルが無い", tc.dir)
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			err = Validate("cli/envelope.json", b)
			if (err == nil) != tc.want {
				t.Errorf("%s: 検証の結果が期待と違う: %v", f, err)
			}
		}
	}
}

// Go の列挙と JSON Schema の enum の照合(D-0097 の 7)。
func TestEnumsMatchSchema(t *testing.T) {
	b, err := os.ReadFile("schema/cli/envelope.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := jsonUnmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	same := func(name string, a []string, b []Code) {
		var bs []string
		for _, c := range b {
			bs = append(bs, string(c))
		}
		sort.Strings(a)
		if len(a) != len(bs) {
			t.Errorf("%s: 数が違う schema=%d go=%d", name, len(a), len(bs))
			return
		}
		for i := range a {
			if a[i] != bs[i] {
				t.Errorf("%s: %s != %s", name, a[i], bs[i])
			}
		}
	}
	same("error.code", doc.Defs["error"].Properties["code"].Enum, ErrorCodes())
	same("warnings[].code", doc.Defs["warning"].Properties["code"].Enum, WarningCodes())
}

func TestSubcommandTable(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range SubcommandTable {
		if s.Public == "" || len(s.Words) == 0 {
			t.Errorf("公開区分の印が無い: %v", s.Words)
		}
		k := ""
		for _, w := range s.Words {
			k += w + " "
		}
		if seen[k] {
			t.Errorf("重複: %s", k)
		}
		seen[k] = true
	}
	for _, need := range []string{"bootstrap ", "bootstrap-restore ", "test-tick "} {
		if !seen[need] {
			t.Errorf("表に無い: %s", need)
		}
	}
}

func TestSubcommandsJSONMatchesTable(t *testing.T) {
	b, err := os.ReadFile("schema/cli/subcommands.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Subcommands []struct {
			Command []string `json:"command"`
			Public  string   `json:"public"`
			Later   string   `json:"later"`
			DryRun  *bool    `json:"dry_run"`
		} `json:"subcommands"`
	}
	if err := jsonUnmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Subcommands) != len(SubcommandTable) {
		t.Fatalf("件数 json=%d go=%d", len(doc.Subcommands), len(SubcommandTable))
	}
	for i, s := range SubcommandTable {
		d := doc.Subcommands[i]
		if string(s.Public) != d.Public || len(s.Words) != len(d.Command) {
			t.Errorf("行 %d: %v %s != %v %s", i, s.Words, s.Public, d.Command, d.Public)
		}
		boot := s.Words[0] == "bootstrap" || s.Words[0] == "bootstrap-restore"
		if boot != (d.DryRun == nil) || (d.DryRun != nil && *d.DryRun != s.DryRun) {
			t.Errorf("行 %d %v: dry_run が json と表で違う(bootstrap 系だけ列を持たない)", i, s.Words)
		}
	}
	// 後のイテレーションの印が無い tool 行で dry_run が真になるのは次の 16 件(監督の決定)。
	want := "def validate|init-storage|init|source enroll|restore-dest enroll|notify set|backup|restore|restore-test|propose|apply|cancel|attest|hold set|prune|extend"
	var got []string
	for i, s := range SubcommandTable {
		if s.Public == PublicTool && doc.Subcommands[i].Later == "" && s.DryRun {
			got = append(got, strings.Join(s.Words, " "))
		}
	}
	sort.Strings(got)
	w := strings.Split(want, "|")
	sort.Strings(w)
	if strings.Join(got, "|") != strings.Join(w, "|") {
		t.Errorf("dry_run が真の tool 行(後の印なし): %v", got)
	}
}

// 送る欄の表のデータが U0 のスキーマに合う(完了条件 15)。spec 3.7 の表との一致は監督の受け入れの照合。
func TestSendFieldsDataMatchesSchema(t *testing.T) {
	b, err := os.ReadFile("../../app/src/data/send-fields.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("app/send-fields.json", b); err != nil {
		t.Fatal(err)
	}
	bad := []byte(`{"format":"sitate.send-fields/v1","rows":[{"where":"x","fields":[{"pointer":"result/no-slash"}]}]}`)
	if Validate("app/send-fields.json", bad) == nil {
		t.Error("不正な pointer を拒否しない")
	}
}
