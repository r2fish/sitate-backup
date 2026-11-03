package recv

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 完了条件(作業指示書 5.8 の 1): 段 prereq の道具の不足を prereq_missing[] / missing[] で返し(調査は失敗にしない)、
// OS が Linux でないとき調査の処理は標準出力に何も出さず終了 77、source-setup は unsupported_os で止まる(spec 3.3 の「失敗の判定」)。
// 道具の不足と OS は、スクリプトの PATH の行を、道具を外した一時ディレクトリに差し替えて作る(元サーバーのどこにも書かない)。

const scriptPath = "PATH=/usr/sbin:/usr/bin:/sbin:/bin"

// toolDir は /usr/bin 等の道具へのリンクを並べ、omit を外す。uname が空でなければ `uname -s` がその値を返す偽物にする。
func toolDir(t *testing.T, omit []string, uname string) string {
	t.Helper()
	dir := t.TempDir()
	for _, src := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		ents, _ := os.ReadDir(src)
		for _, e := range ents {
			p := filepath.Join(dir, e.Name())
			if _, err := os.Lstat(p); err == nil {
				continue
			}
			_ = os.Symlink(filepath.Join(src, e.Name()), p)
		}
	}
	for _, n := range omit {
		_ = os.Remove(filepath.Join(dir, n))
		if _, err := os.Lstat(filepath.Join(dir, n)); err == nil {
			t.Fatalf("%s を外せない", n)
		}
	}
	if uname != "" {
		p := filepath.Join(dir, "uname")
		_ = os.Remove(p)
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho "+uname+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runWith は台本 name の先頭に代入 pre を置き、PATH の行を tools に差し替えて /bin/sh -s で流す。
func runWith(t *testing.T, name, pre, tools string) (stdout string, code int) {
	t.Helper()
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), scriptPath+"\n") {
		t.Fatalf("%s の PATH の行が想定と違う", name)
	}
	s := strings.Replace(string(body), scriptPath+"\n", "PATH="+tools+"\n", 1)
	c := exec.Command("/bin/sh", "-s")
	c.Stdin = strings.NewReader(pre + s)
	c.Env = []string{"PATH=/usr/bin:/bin", "TMPDIR=" + t.TempDir()}
	var o bytes.Buffer
	c.Stdout = &o
	err = c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return o.String(), code
}

func setupPre(targets string) string {
	return "SITATE_FROM=" + sq("203.0.113.9") + "\nSITATE_PULL_KEY=" + sq(testKey) + "\nSITATE_RECEIVER=''\nSITATE_TARGETS=" + sq(targets) + "\nSITATE_CLUSTERS=''\n"
}

const (
	filesTarget = "t1\tfiles\t/srv/sitate-prereq-none\t\"/srv/sitate-prereq-none\""
	pgTarget    = "t2\tpostgresql\tapp\t/run/postgresql\t5432\t\"app\"\t\"/run/postgresql\"\t0"
)

func stepLines(out string) []map[string]any {
	var l []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		if strings.HasPrefix(ln, "{") && json.Unmarshal([]byte(ln), &m) == nil {
			l = append(l, m)
		}
	}
	return l
}

// source-setup の段 prereq: 不足の道具を missing[] に全部(段の順で)列挙して失敗し、後の段(アカウント・ACL)へ進まない。
// setfacl を必ず外すので、外し方が効かなければ試験が先へ進まず止まる前に落ちる(元サーバーを変えない)。
func TestSetupPrereqToolMissing(t *testing.T) {
	cases := []struct {
		name    string
		omit    []string
		targets string
		want    string
	}{
		{"2 つ", []string{"setfacl", "sha256sum"}, filesTarget, `["setfacl","sha256sum"]`},
		{"GNU の find が無い", []string{"find", "setfacl"}, filesTarget, `["findutils","setfacl"]`},
		{"tar が無い", []string{"tar", "setfacl"}, filesTarget, `["tar","setfacl"]`},
		{"DB の対象があれば runuser も", []string{"setfacl", "runuser"}, filesTarget + "\n" + pgTarget, `["setfacl","runuser"]`},
		{"ファイルの対象だけなら runuser は要らない", []string{"setfacl", "runuser"}, filesTarget, `["setfacl"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, code := runWith(t, "source-setup", setupPre(c.targets), toolDir(t, c.omit, ""))
			l := stepLines(out)
			if code != 1 || len(l) != 1 {
				t.Fatalf("終了 1 で 1 行だけ(後の段へ進まない): 終了 %d\n%s", code, out)
			}
			m, _ := json.Marshal(l[0]["missing"])
			if l[0]["step"] != "prereq" || l[0]["result"] != "failed" || l[0]["reason"] != "tool_missing" || string(m) != c.want {
				t.Errorf("段 prereq の tool_missing と missing=%s: %s", c.want, out)
			}
		})
	}
}

// OS が Linux でないとき source-setup は段 prereq の unsupported_os で止まる。
func TestSetupPrereqUnsupportedOS(t *testing.T) {
	out, code := runWith(t, "source-setup", setupPre(filesTarget), toolDir(t, []string{"setfacl"}, "Darwin"))
	l := stepLines(out)
	if code != 1 || len(l) != 1 || l[0]["step"] != "prereq" || l[0]["result"] != "failed" || l[0]["reason"] != "unsupported_os" || l[0]["missing"] != nil {
		t.Errorf("unsupported_os: 終了 %d\n%s", code, out)
	}
}

// 調査の処理: 道具の不足は失敗にせず、prereq_missing[] に載せて終了 0 で出力する(スキーマに合う)。不足が無ければ空の配列。
func TestSurveyPrereqMissing(t *testing.T) {
	pre := "SITATE_SCOPE='full'\nSITATE_DU_TIMEOUT_S='30'\n"
	for _, c := range []struct {
		omit []string
		want string
	}{
		{nil, `[]`},
		{[]string{"tar", "setfacl", "sha256sum"}, `["tar","setfacl","sha256sum"]`},
		{[]string{"setfacl"}, `["setfacl"]`},
	} {
		if c.omit == nil {
			if _, err := exec.LookPath("setfacl"); err != nil {
				continue // 道具が元から無い環境(開発用コンテナ)では「不足なし」の対照を取れない。PostgreSQL 入りの試験用の容器で走る
			}
		}
		out, code := runWith(t, "source-survey", pre, toolDir(t, c.omit, ""))
		if code != 0 {
			t.Fatalf("道具の不足は失敗にしない(終了 0): %v → %d", c.omit, code)
		}
		if err := contract.Validate("receiver/survey.json#/$defs/source_survey", []byte(out)); err != nil {
			t.Errorf("%v: スキーマに合わない: %v", c.omit, err)
		}
		var d struct {
			P json.RawMessage `json:"prereq_missing"`
		}
		_ = json.Unmarshal([]byte(out), &d)
		if string(d.P) != c.want {
			t.Errorf("%v: prereq_missing は %s: %s", c.omit, c.want, d.P)
		}
	}
}

// OS が Linux でないとき調査の処理は標準出力に何も出さず終了状態 77(範囲 full・db_only のどちらでも)。
func TestSurveyExit77NotLinux(t *testing.T) {
	for _, scope := range []string{"full", "db_only"} {
		pre := "SITATE_SCOPE=" + sq(scope) + "\nSITATE_DU_TIMEOUT_S='30'\n"
		out, code := runWith(t, "source-survey", pre, toolDir(t, nil, "Darwin"))
		if code != 77 || out != "" {
			t.Errorf("%s: 終了 77・標準出力なし: 終了 %d %q", scope, code, out)
		}
		// 対照: Linux なら 77 にならない(終了状態 77 を他の失敗と取り違えていない)
		if _, code := runWith(t, "source-survey", pre, toolDir(t, nil, "")); code != 0 {
			t.Errorf("%s: Linux では終了 0: %d", scope, code)
		}
	}
}
