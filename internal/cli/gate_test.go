package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// フレーズの秘密のレコード(24 語)。
func phraseRecord() []byte {
	w := make([]string, 24)
	for i := range w {
		w[i] = "abandon"
	}
	b, _ := json.Marshal(map[string]any{"schema": "sitate.secret-string/v1", "kind": "recovery_phrase", "words": w, "keyring_version": 1})
	return b
}

type gateEnv struct {
	d    *Deps
	out  *bytes.Buffer
	fd   *bytes.Buffer
	seen *fake.Approval
}

func newGate(t *testing.T, line string, secretOut string, withPhrase bool) *gateEnv {
	t.Helper()
	d, out := defDeps(t, "")
	ap := &fake.Approval{Allow: map[string]bool{"init": true, "status": true}}
	keys := &fake.Keys{Fn: map[string]func() (contract.Reply, error){"Init": func() (contract.Reply, error) {
		r := contract.Reply{Result: json.RawMessage(`{"committed":false}`)}
		if withPhrase {
			r.Phrase = contract.NewSecret(phraseRecord())
		}
		return r, nil
	}}}
	d.Keys = keys
	d.getenv = func(string) string { return line }
	g := &gateEnv{d: d, out: out, fd: &bytes.Buffer{}, seen: ap}
	d.Approval = &gateApproval{Approval: ap, secretOut: secretOut}
	d.fdOpen = func(int) io.WriteCloser { return nopCloser{g.fd} }
	return g
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// 判定が --secret-fd を stdout の先頭レコードに読み替えて返す偽物(引数列から --secret-fd <n> を除く)。
type gateApproval struct {
	*fake.Approval
	secretOut string
}

func (g *gateApproval) Gate(ctx context.Context, line string) (contract.GateDecision, error) {
	dec, err := g.Approval.Gate(ctx, line)
	if err != nil || !dec.Allowed {
		return dec, err
	}
	var argv []string
	for i := 0; i < len(dec.Argv); i++ {
		if dec.Argv[i] == "--secret-fd" {
			i++
			continue
		}
		argv = append(argv, dec.Argv[i])
	}
	dec.Argv, dec.SecretOut = argv, g.secretOut
	return dec, nil
}

func lines(b *bytes.Buffer) []json.RawMessage {
	var out []json.RawMessage
	for _, l := range strings.Split(strings.TrimRight(b.String(), "\n"), "\n") {
		out = append(out, json.RawMessage(l))
	}
	return out
}

// 完了条件 6: gate が --secret-fd を読み替えたとき、標準出力は [秘密, 封筒] の 2 レコードで、アプリの判定(harness)を通る。
func TestGateSecretFDToFirstRecord(t *testing.T) {
	g := newGate(t, "init --approval-pubkey AAAA --secret-fd 3", secretStdoutName, true)
	code := Run(g.d, []string{"gate"}, g.out, nil)
	recs := lines(g.out)
	if code != 0 || len(recs) != 2 {
		t.Fatalf("code=%d out=%s", code, g.out.String())
	}
	if c, has, err := harness.ClassifyStdout(recs); c != "" || !has || err != nil {
		t.Fatalf("アプリの判定: %q %v %v", c, has, err)
	}
	var env map[string]any
	_ = json.Unmarshal(recs[1], &env)
	if env["phrase_emitted"] != true || strings.Contains(string(recs[1]), "abandon") {
		t.Errorf("封筒: %s", recs[1])
	}
	if g.fd.Len() != 0 {
		t.Error("gate 経由で記述子へ書いた")
	}
}

// 秘密を出さない呼び出し(フレーズが無い)は封筒 1 レコード。
func TestGateNoSecretOneRecord(t *testing.T) {
	g := newGate(t, "init --approval-pubkey AAAA --secret-fd 3", secretStdoutName, false)
	Run(g.d, []string{"gate"}, g.out, nil)
	recs := lines(g.out)
	if len(recs) != 1 || strings.Contains(g.out.String(), "phrase_emitted") {
		t.Fatalf("%s", g.out.String())
	}
}

// 拒否: command は字句規則 3 で決めたサブコマンド、決まらなければ ["gate"]。detail に拒否した語を入れない。
func TestGateDenied(t *testing.T) {
	for line, want := range map[string]string{"restore --to /x": `["gate"]`, "status --x": `["status"]`, " status": `["gate"]`, "": `["gate"]`} {
		g := newGate(t, line, "", false)
		g.seen.Allow = map[string]bool{}
		code := Run(g.d, []string{"gate"}, g.out, nil)
		var env struct {
			Command json.RawMessage `json:"command"`
			Error   struct{ Code string }
		}
		_ = json.Unmarshal(g.out.Bytes(), &env)
		if code != 2 || env.Error.Code != "GATE_DENIED" {
			t.Errorf("%q: %d %s", line, code, g.out.String())
		}
		if line == "status --x" && string(env.Command) != want {
			t.Errorf("%q: command %s", line, env.Command)
		}
		if line != "status --x" && string(env.Command) != want && line != "restore --to /x" {
			t.Errorf("%q: command %s", line, env.Command)
		}
	}
}

// 直接の呼び出し: --secret-fd の記述子へ書き、標準出力は封筒 1 行(phrase_emitted)。書けなければ INTERNAL で phrase_emitted なし。
func TestDirectSecretFD(t *testing.T) {
	g := newGate(t, "", "", true)
	code := Run(g.d, []string{"init", "--approval-pubkey", "AAAA", "--secret-fd", "3"}, g.out, nil)
	if code != 0 || !strings.Contains(g.out.String(), `"phrase_emitted":true`) || strings.Contains(g.out.String(), "abandon") {
		t.Fatalf("%d %s", code, g.out.String())
	}
	if !bytes.Equal(bytes.TrimSpace(g.fd.Bytes()), phraseRecord()) {
		t.Errorf("記述子の内容: %s", g.fd.String())
	}
	g = newGate(t, "", "", true)
	g.d.fdOpen = func(int) io.WriteCloser { return nil }
	code = Run(g.d, []string{"init", "--approval-pubkey", "AAAA", "--secret-fd", "3"}, g.out, nil)
	if code == 0 || strings.Contains(g.out.String(), "phrase_emitted") {
		t.Fatalf("%d %s", code, g.out.String())
	}
}

// --secret-fd が無いのにフレーズが返ったら出さずに INTERNAL(秘密を出力先なしで漏らさない)。
func TestPhraseWithoutOutputIsInternal(t *testing.T) {
	g := newGate(t, "", "", true)
	code := Run(g.d, []string{"init", "--approval-pubkey", "AAAA"}, g.out, nil)
	if code == 0 || strings.Contains(g.out.String(), "abandon") || g.fd.Len() != 0 {
		t.Fatalf("%d %s", code, g.out.String())
	}
}

// 標準入力: --secrets-stdin の 1 行目の後に --file - の定義が続く。秘密の行は定義として読まれない。
func TestSecretsStdinThenFileDash(t *testing.T) {
	d, out := defDeps(t, `{"storage":"x"}`+"\n"+defYAML)
	env, code := runCLI(d, out, "def", "validate", "--file", "-", "--secrets-stdin")
	if code != 0 || !strings.Contains(string(mustJSON(env["result"])), "app01.example.internal") || strings.Contains(string(mustJSON(env["result"])), `"x"`) {
		t.Fatalf("%v", env)
	}
	d, out = defDeps(t, defYAML) // 秘密の行が無い(定義の 1 行目が秘密として読まれ、形が合わない)
	env, code = runCLI(d, out, "def", "validate", "--file", "-", "--secrets-stdin")
	if code != 2 {
		t.Fatalf("%v", env)
	}
}

var _ = os.Stdout
