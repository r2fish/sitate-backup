package adapter

// WP 固有の試験の道具。アダプタは試験の中でシェルの実行ファイルとして作る(作業指示書 3.1 の組み方)。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

const secretVal = "s3cr3t-token-value-0001"

// spec は試験のアダプタの作り方。
type spec struct {
	kind, name string
	script     string                 // 実行ファイルの本体(#!/bin/sh の後)
	secrets    []string               // adapter.json の secrets[]
	mod        func(m map[string]any) // adapter.json を署名の前に書き換える
	verifier   contract.Verifier
}

type built struct {
	r      *Runner
	root   string
	dir    string
	marker string // アダプタが起動されたら作られる
}

func declFor(kind, name, exe string, secrets []string, sum string) map[string]any {
	if secrets == nil {
		secrets = []string{}
	}
	m := map[string]any{"kind": kind, "name": name, "version": "1.0.0", "contract": contract.AdapterContract[kind],
		"exe": exe, "secrets": secrets, "network": kind != "acquire" && kind != "check", "sha256": sum}
	return m
}

func build(t *testing.T, s spec) built {
	t.Helper()
	if s.name == "" {
		s.name = "webhook"
	}
	root := t.TempDir()
	dir := filepath.Join(root, s.kind, s.name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "started")
	body := "#!/bin/sh\ntouch " + marker + "\n" + s.script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "exe"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	m := declFor(s.kind, s.name, "exe", s.secrets, hex.EncodeToString(sum[:]))
	if s.mod != nil {
		s.mod(m)
	}
	decl, _ := json.Marshal(m)
	_ = os.WriteFile(filepath.Join(dir, "adapter.json"), decl, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "adapter.json.sig"), []byte("sig"), 0o644)
	v := s.verifier
	if v == nil {
		v = &fake.Verifier{Valid: true}
	}
	return built{r: &Runner{Root: root, Version: "1.0.0", Arch: "linux-x86_64", Verifier: v}, root: root, dir: dir, marker: marker}
}

func notifySpec(script string) spec {
	return spec{kind: "notify", name: "webhook", script: script, secrets: []string{"url"}}
}

func started(b built) bool { _, err := os.Stat(b.marker); return err == nil }

func runNotify(b built) (json.RawMessage, error) {
	return b.r.Run(context.Background(), contract.RunContext{RunID: "r-20261018T173000Z-3f9a"}, "notify", "webhook", request("sitate.notify/v1"), nil)
}

// errDetail は *contract.Error の原因コードを確かめ、detail が 3.1.3 のスキーマ(cli/detail.json)に合うことも確かめる。
func errDetail(t *testing.T, err error, code contract.Code) *contract.Error {
	t.Helper()
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != code {
		t.Fatalf("%s を期待したが %v", code, err)
	}
	if verr := contract.Validate("cli/detail.json#/$defs/"+string(code), ce.Detail); verr != nil {
		t.Errorf("%s の detail が 3.1.3 のスキーマに合わない: %s %v", code, ce.Detail, verr)
	}
	return ce
}

// internalWith は INTERNAL の detail が component: adapter・kind・name・reason の 5 項目の形で、reason が合うことを確かめる。
// reason が "request" のとき、detail の reason は null(列挙に無いため)。
func internalWith(t *testing.T, err error, reason string) {
	t.Helper()
	ce := errDetail(t, err, contract.CodeInternal)
	var d struct {
		Component string
		Reason    *string
		Kind      *string
		Name      *string
	}
	_ = json.Unmarshal(ce.Detail, &d)
	if d.Component != "adapter" || d.Kind == nil || d.Name == nil {
		t.Errorf("INTERNAL の detail は component: adapter と kind・name を持つ: %s", ce.Detail)
	}
	if reason == "request" {
		if d.Reason != nil {
			t.Errorf("reason は null を期待: %s", ce.Detail)
		}
	} else if d.Reason == nil || *d.Reason != reason {
		t.Errorf("reason: %s を期待したが %s", reason, ce.Detail)
	}
}

// okVerifier は常に合格の Verifier(別プロセスの核の試験用)。
type okVerifier struct{}

func (okVerifier) Verify(context.Context, []byte, []byte, contract.TrustedComment) (bool, error) {
	return true, nil
}
