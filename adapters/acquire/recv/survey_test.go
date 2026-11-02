package recv

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func runSurvey(t *testing.T, scope, du, tmp string) ([]byte, int) {
	t.Helper()
	body, err := os.ReadFile("source-survey")
	if err != nil {
		t.Fatal(err)
	}
	pre := "SITATE_SCOPE=" + sq(scope) + "\nSITATE_DU_TIMEOUT_S=" + sq(du) + "\n"
	c := exec.Command("sh", "-s")
	c.Stdin = strings.NewReader(pre + string(body))
	c.Env = append(os.Environ(), "TMPDIR="+tmp)
	out, err := c.Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return out, code
}

// 調査の処理: 出力が sitate.source-survey/v1 に合い、一時ディレクトリに何も残さない。範囲 db_only は clusters だけ。
func TestSurveyOutputAndNoResidue(t *testing.T) {
	tmp := t.TempDir() // 調査専用の一時ディレクトリ(並行の試験の書き込みに影響されない)
	for _, scope := range []string{"full", "db_only"} {
		out, code := runSurvey(t, scope, "30", tmp)
		if code != 0 {
			t.Fatalf("%s: 終了 %d", scope, code)
		}
		if err := contract.Validate("receiver/survey.json#/$defs/source_survey", out); err != nil {
			t.Errorf("%s: スキーマに合わない: %v\n%.300s", scope, err, out)
		}
		var d struct{ Mounts, Dirs []json.RawMessage }
		_ = json.Unmarshal(out, &d)
		if (scope == "db_only") != (len(d.Mounts) == 0 && len(d.Dirs) == 0) {
			t.Errorf("%s: mounts/dirs の有無", scope)
		}
	}
	if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
		t.Errorf("一時ディレクトリに残した: %d 件", len(ents))
	}
}

// du の打ち切り: /srv の直下のディレクトリは、短い打ち切りで bytes が null になる(root の容器だけ。/srv を作る)。
func TestSurveyDuTimeout(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("/srv に作る(root の使い捨て容器)。未検証")
	}
	base := "/srv/sitate-du-test"
	if err := os.MkdirAll(base+"/d", 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	for i := 0; i < 3000; i++ {
		_ = os.WriteFile(fmt.Sprintf("%s/d/file-%05d", base, i), []byte("z"), 0o644)
	}
	find := func(scale string) (*int64, bool) {
		out, _ := runSurvey(t, "full", scale, t.TempDir())
		var d struct {
			Dirs []struct {
				Path  string
				Bytes *int64
			}
		}
		_ = json.Unmarshal(out, &d)
		for _, x := range d.Dirs {
			if x.Path == base {
				return x.Bytes, true
			}
		}
		return nil, false
	}
	if b, ok := find("0.001"); !ok || b != nil {
		t.Errorf("打ち切ったら bytes は null: %v %v", b, ok)
	}
	if b, ok := find("60"); !ok || b == nil {
		t.Errorf("十分な時間では bytes が出る: %v %v", b, ok)
	}
}
