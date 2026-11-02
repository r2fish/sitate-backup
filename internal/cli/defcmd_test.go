package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/state"
)

const defYAML = `version: 1
source: {host: app01.example.internal, targets: [{id: t1, files: /srv/app/data}]}
storage: [{endpoint: "https://s3.example-provider.com", bucket: b, key_ref: storage}]
restore: {dest: {at: "ssh://restore@203.0.113.10:22", key_ref: restore-init}}
notify: {to: [ops@example.jp]}
`

func defDeps(t *testing.T, stdin string) (*Deps, *bytes.Buffer) {
	dir := t.TempDir()
	clk := &fake.Clock{T: fake.StartTime}
	d := &Deps{StateDir: dir, Clock: clk, Stdin: strings.NewReader(stdin), Limits: DefaultLimits()}
	d.State = &state.StateFiles{Dir: dir, Clock: clk}
	out := &bytes.Buffer{}
	return d, out
}

func runCLI(d *Deps, out *bytes.Buffer, args ...string) (map[string]any, int) {
	code := Run(d, args, out, nil)
	var env map[string]any
	_ = json.Unmarshal(out.Bytes(), &env)
	out.Reset()
	return env, code
}

// 完了条件 12: 標準入力と --file で同じ判定、通った内容を候補に保存、--dry-run は保存しない、seal の前の def show は候補を sealed:false で出す。
func TestDefValidateStdinFileDryRunShow(t *testing.T) {
	d, out := defDeps(t, defYAML)
	env, code := runCLI(d, out, "def", "validate", "--file", "-", "--dry-run")
	if code != 0 || env["ok"] != true {
		t.Fatalf("%v %d", env, code)
	}
	if _, _, found, _ := d.State.ReadCandidate(context.Background()); found {
		t.Fatal("--dry-run が候補を保存した")
	}
	hashStdin := env["result"].(map[string]any)["def_hash"]
	f := filepath.Join(t.TempDir(), "d.yaml")
	_ = os.WriteFile(f, []byte(defYAML), 0o600)
	d.Stdin = strings.NewReader("")
	d.stdinBuf = nil
	env, code = runCLI(d, out, "def", "validate", "--file", f)
	if code != 0 || env["result"] == nil || env["result"].(map[string]any)["def_hash"] != hashStdin {
		t.Fatalf("--file と標準入力で結果が違う: %v", env)
	}
	if _, _, found, _ := d.State.ReadCandidate(context.Background()); !found {
		t.Fatal("通った内容が候補に保存されない")
	}
	env, _ = runCLI(d, out, "def", "show")
	r := env["result"].(map[string]any)
	if r["sealed"] != false || r["def_hash"] != hashStdin || r["approved_at"] != nil {
		t.Errorf("def show: %v", r)
	}
}

// 不正な定義は終了 2 の DEF_INVALID で issues[] を返し、候補を置き換えない。製品ビルドで上限の項目を定義に書くと unknown_key。
func TestDefValidateInvalid(t *testing.T) {
	d, out := defDeps(t, defYAML+"run_max_duration_s: 5\n")
	env, code := runCLI(d, out, "def", "validate", "--file", "-")
	e := env["error"].(map[string]any)
	if code != 2 || e["code"] != "DEF_INVALID" || !strings.Contains(string(mustJSON(e["detail"])), "unknown_key") {
		t.Fatalf("%v %d", env, code)
	}
	if _, _, found, _ := d.State.ReadCandidate(context.Background()); found {
		t.Error("不正な定義が候補に入った")
	}
}

// --secrets-stdin の 1 行目の後に、続けて --file - の定義を読める(アプリの渡し方)。
func TestDefValidateAfterSecretsLine(t *testing.T) {
	d, out := defDeps(t, "{}\n"+defYAML)
	env, code := runCLI(d, out, "def", "validate", "--file", "-", "--secrets-stdin")
	if code != 0 {
		t.Fatalf("%v", env)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

var _ = time.Second
