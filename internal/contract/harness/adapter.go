package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// runExe は実行ファイルを `run` で呼び、標準入力に req を渡す。終了コードと標準出力を返す。
func runExe(t *testing.T, exe, req string) (int, []byte) {
	t.Helper()
	cmd := exec.Command(exe, "run")
	cmd.Stdin = strings.NewReader(req)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("起動できない: %v", err)
	}
	return code, out.Bytes()
}

// RunAdapter はアダプタ(adapters/<kind>/<name>/)を make build と同じ工程(tools/build.sh のアダプタの工程。自前の別の工程は持たない)で
// ビルドし、ビルド後の adapter.json の写しと実行ファイルを外から run で呼んで契約を照らす(作業指示書 3.2、spec 3.2.1)。
//   - 工程の終了コード 0、写しが kind ごとのスキーマに合い、kind/name が置き場と一致し、sha256 が実行ファイルと一致する。ソースに sha256 を書かない。
//   - 試験ベクトルの要求(testdata/adapter/valid/<kind>-request*.json)を通し、終了 0 で標準出力が改行で終わる JSON 1 行、kind の応答スキーマに合う。
//     終了 1 で error.code が NOT_IMPLEMENTED なら未実装の印で失敗させる(骨格)。
//   - contract の版が違う要求は終了 1・INTERNAL(detail.component: adapter, reason: contract)。
func RunAdapter(t *testing.T, dir string) {
	t.Helper()
	root := repoRoot()
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	rel, err := filepath.Rel(filepath.Join(root, "adapters"), dir)
	if err != nil {
		t.Fatal(err)
	}
	rel = filepath.ToSlash(rel)
	kind, name := filepath.Base(filepath.Dir(dir)), filepath.Base(dir)
	src, err := os.ReadFile(filepath.Join(dir, "adapter.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sj map[string]any
	_ = json.Unmarshal(src, &sj)
	if _, has := sj["sha256"]; has {
		t.Error("ソースの adapter.json に sha256 を書いてはいけない(ビルドが写しに書く)")
	}
	out := t.TempDir()
	build := exec.Command("bash", filepath.Join(root, "tools", "build.sh"), "adapter", rel, runtime.GOARCH, out)
	build.Dir = root
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("make build のアダプタの工程が失敗: %v\n%s", err, b)
	}
	copyRaw, err := os.ReadFile(filepath.Join(out, "adapter.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate("adapter/adapter-json.json", copyRaw); err != nil {
		t.Errorf("adapter.json の写しが kind ごとのスキーマに合わない: %v", err)
	}
	var cj struct{ Kind, Name, Exe, Sha256 string }
	_ = json.Unmarshal(copyRaw, &cj)
	if cj.Kind != kind || cj.Name != name {
		t.Errorf("adapter.json の kind/name が置き場と違う: %s/%s", cj.Kind, cj.Name)
	}
	exe := filepath.Join(out, cj.Exe)
	eb, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(eb); hex.EncodeToString(sum[:]) != cj.Sha256 {
		t.Errorf("sha256 が実行ファイルと一致しない")
	}
	checkRequests(t, exe, kind, name)
}

func checkRequests(t *testing.T, exe, kind, name string) {
	t.Helper()
	reqs, _ := filepath.Glob(filepath.Join(VectorPath("adapter/valid"), kind+"-request*.json"))
	sort.Strings(reqs)
	if len(reqs) == 0 {
		t.Fatalf("kind %s の要求の試験ベクトルが無い", kind)
	}
	req, _ := os.ReadFile(reqs[0])
	code, stdout := runExe(t, exe, string(compactJSON(req))+"\n")
	var res struct {
		Contract string `json:"contract"`
		Error    *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if !bytes.HasSuffix(stdout, []byte("\n")) || bytes.Count(bytes.TrimRight(stdout, "\n"), []byte("\n")) != 0 || json.Unmarshal(stdout, &res) != nil {
		t.Fatalf("標準出力が改行で終わる JSON 1 行でない: %q", stdout)
	}
	if res.Error != nil && res.Error.Code == "NOT_IMPLEMENTED" {
		if code != 1 {
			t.Errorf("未実装の終了コード %d(1 が必要)", code)
		}
		t.Fatalf("%s %s/%s", NotImplementedMarker, kind, name)
	}
	if code != 0 && code != 1 {
		t.Errorf("終了コード %d", code)
	}
	if err := contract.Validate("adapter/"+kind+".json#/$defs/response", stdout); err != nil {
		t.Errorf("結果が %s の応答のスキーマに合わない: %v\n%s", kind, err, stdout)
	}
	if kind == "check" && code == 0 {
		decl, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), "adapter.json"))
		for _, p := range CheckKeys(decl, req, stdout) {
			t.Error(p)
		}
	}
	mismatch := bytes.Replace(compactJSON(req), []byte(`"sitate.`+kind+`/v1"`), []byte(`"sitate.`+kind+`/v0"`), 1)
	code, stdout = runExe(t, exe, string(mismatch)+"\n")
	var me struct {
		Error struct {
			Code   string                             `json:"code"`
			Detail struct{ Component, Reason string } `json:"detail"`
		} `json:"error"`
	}
	if code != 1 || json.Unmarshal(stdout, &me) != nil || me.Error.Code != "INTERNAL" || me.Error.Detail.Component != "adapter" || me.Error.Detail.Reason != "contract" {
		t.Errorf("contract の版が違う要求は終了 1・INTERNAL(component: adapter, reason: contract): exit=%d %s", code, stdout)
	}
}

func compactJSON(b []byte) []byte {
	var buf bytes.Buffer
	if json.Compact(&buf, b) != nil {
		return b
	}
	return buf.Bytes()
}

// CheckKeys はチェックの adapter.json(decl)・要求・結果の 3 つを照合し、食い違いの一覧を返す(作業指示書 3.2、D-0115 の 2):
// 要求の params のキーは decl.params[] と一致し、結果の metrics のキーは decl.metrics[] に、findings[].code は decl.finding_codes[] に含まれる。
func CheckKeys(decl, req, res []byte) []string {
	var d struct {
		Params       []string `json:"params"`
		Metrics      []string `json:"metrics"`
		FindingCodes []string `json:"finding_codes"`
	}
	var rq struct {
		Params map[string]json.RawMessage `json:"params"`
	}
	var rs struct {
		Metrics  map[string]json.RawMessage `json:"metrics"`
		Findings []struct {
			Code string `json:"code"`
		} `json:"findings"`
	}
	var bad []string
	if json.Unmarshal(decl, &d) != nil || json.Unmarshal(req, &rq) != nil || json.Unmarshal(res, &rs) != nil {
		return []string{"JSON を読めない"}
	}
	has := func(set []string, k string) bool {
		for _, s := range set {
			if s == k {
				return true
			}
		}
		return false
	}
	for k := range rq.Params {
		if !has(d.Params, k) {
			bad = append(bad, "要求の params のキー "+k+" が adapter.json の params[] に無い")
		}
	}
	for _, p := range d.Params {
		if _, ok := rq.Params[p]; !ok {
			bad = append(bad, "adapter.json の params[] の "+p+" が要求の params に無い")
		}
	}
	for k := range rs.Metrics {
		if !has(d.Metrics, k) {
			bad = append(bad, "結果の metrics のキー "+k+" が adapter.json の metrics[] に無い")
		}
	}
	for _, f := range rs.Findings {
		if !has(d.FindingCodes, f.Code) {
			bad = append(bad, "findings[].code "+f.Code+" が adapter.json の finding_codes[] に無い")
		}
	}
	sort.Strings(bad)
	return bad
}
