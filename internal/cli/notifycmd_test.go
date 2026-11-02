package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/state"
)

func notifyDeps(t *testing.T, stdin string, steps ...fake.Step) (*Deps, *fake.AdapterRunner) {
	d, _ := defDeps(t, stdin)
	d.Secrets = &Secrets{Dir: d.StateDir}
	ar := fake.NewAdapterRunner()
	ar.Script("notify", steps...)
	n := &state.Notifier{State: d.State, Adapters: ar, Clock: d.Clock, Secrets: d.Secrets}
	d.TestNotifier = &state.TestNotifier{State: d.State, Adapters: ar, Clock: d.Clock, Secrets: d.Secrets, Notifier: n}
	return d, ar
}

// notify set: 値は --secrets-stdin のキー <secret-ref> で受け、秘密ファイル(kind: token、0600)に置く。結果に値を出さない。
func TestNotifySet(t *testing.T) {
	d, _ := notifyDeps(t, `{"hook-a":"https://hooks.example.test/x"}`+"\n")
	o, code := runCLI(d, newBuf(), "notify", "set", "--to", "hook-a", "--secret-ref", "hook-a", "--secrets-stdin")
	if code != 0 {
		t.Fatalf("%v", o)
	}
	r := o["result"].(map[string]any)
	if r["to"] != "hook-a" || r["secret_ref"] != "hook-a" || r["channel"] != "webhook" || strings.Contains(string(mustJSON(o)), "hooks.example.test") {
		t.Errorf("%v", o)
	}
	f := filepath.Join(d.StateDir, "secrets", "hook-a.json")
	b, _ := os.ReadFile(f)
	st, _ := os.Stat(f)
	if !strings.Contains(string(b), `"kind":"token"`) || st.Mode().Perm() != 0o600 {
		t.Errorf("%s %v", b, st.Mode())
	}
	// Webhook の宛先と参照名が違うと、配送が宛先の名前で引けないので受けない。値が無いと ARG_INVALID。
	d, _ = notifyDeps(t, `{"other":"x"}`+"\n")
	if o, code = runCLI(d, newBuf(), "notify", "set", "--to", "hook-a", "--secret-ref", "other", "--secrets-stdin"); code != 2 {
		t.Errorf("参照名の不一致: %v", o)
	}
	d, _ = notifyDeps(t, "{}\n")
	if o, code = runCLI(d, newBuf(), "notify", "set", "--to", "ops@example.jp", "--secret-ref", "smtp", "--secrets-stdin"); code != 2 {
		t.Errorf("値が無い: %v", o)
	}
}

// notify test: 全通知先に 1 通ずつ送り、配送の結果を宛先ごとに返し、確認語のハッシュだけを状態に残す。
func TestNotifyTest(t *testing.T) {
	ok := fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.notify/v1","delivered":true,"channel":"email"}`)}
	d, _ := notifyDeps(t, defYAML, ok)
	b := newBuf()
	if _, code := runCLI(d, b, "def", "validate", "--file", "-"); code != 0 {
		t.Fatal("def validate")
	}
	d.Stdin = strings.NewReader("")
	d.stdinBuf = nil
	o, code := runCLI(d, b, "notify", "test")
	if code != 0 {
		t.Fatalf("%v", o)
	}
	res, _ := json.Marshal(o["result"])
	if err := contract.Validate("cli/results.json#/$defs/notify_test", res); err != nil {
		t.Fatalf("%v\n%s", err, res)
	}
	r := o["result"].(map[string]any)
	if r["all_delivered"] != true || len(r["delivered"].([]any)) != 1 {
		t.Errorf("%v", r)
	}
	raw, _ := os.ReadFile(filepath.Join(d.StateDir, "state.json"))
	if !strings.Contains(string(raw), "word_hash") || strings.Contains(string(raw), "confirmation_word") {
		t.Errorf("状態: %s", raw)
	}
}

// 配送が受理されなければ all_delivered が false で reason が出る(再試行の間隔は短くする)。
func TestNotifyTestRejected(t *testing.T) {
	bad := fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.notify/v1","delivered":false,"channel":"email","reason":"rejected"}`)}
	d, _ := notifyDeps(t, defYAML, bad, bad, bad, bad, bad, bad)
	d.TestNotifier.(*state.TestNotifier).Notifier.Backoff = 1
	b := newBuf()
	runCLI(d, b, "def", "validate", "--file", "-")
	d.Stdin = strings.NewReader("")
	d.stdinBuf = nil
	o, code := runCLI(d, b, "notify", "test")
	r, _ := o["result"].(map[string]any)
	if code != 0 || r["all_delivered"] != false || r["delivered"].([]any)[0].(map[string]any)["reason"] != "rejected" {
		t.Fatalf("%v", o)
	}
}

func newBuf() *bytes.Buffer { return &bytes.Buffer{} }
