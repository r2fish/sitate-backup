package adapter

// 契約試験(作業指示書 3.2): 偽の署名検証(fake.Verifier)と、試験の中で作る小さなアダプタ(シェルの実行ファイル)で、
// Runner の外形を照らす。骨格は NOT_IMPLEMENTED で落ち、nil を返すだけの実装は値の比較で落ちる。WP は骨格を実装し、この試験は変えない。
// 子プロセスの試験は 3.1 の組み方による(シェルが要る)。署名の検証そのもの(Verifier の中身)は U1g の持ち物。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var ctx = context.Background()

const okResult = `{"contract":"sitate.notify/v1","delivered":true,"channel":"webhook","reason":null,"http_status":200,"provider_ref":null}`

// newRunner は <root>/notify/webhook に exe と署名付きの adapter.json を置いた Runner を返す。marker はアダプタが起動されたら作られる。
func newRunner(t *testing.T, valid bool, script string) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	d := filepath.Join(root, "notify", "webhook")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "started")
	body := "#!/bin/sh\ntouch " + marker + "\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(d, "webhook"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	decl, _ := json.Marshal(map[string]any{"kind": "notify", "name": "webhook", "version": "1.0.0", "contract": "sitate.notify/v1",
		"exe": "webhook", "secrets": []string{"url"}, "network": true, "sha256": hex.EncodeToString(sum[:])})
	_ = os.WriteFile(filepath.Join(d, "adapter.json"), decl, 0o644)
	_ = os.WriteFile(filepath.Join(d, "adapter.json.sig"), []byte("sig"), 0o644)
	return &Runner{Root: root, Version: "1.0.0", Arch: "linux-x86_64", Verifier: &fake.Verifier{Valid: valid}}, marker
}

// request は通知の要求のスキーマに合う要求(試験ベクトル)の contract を差し替えたもの。
func request(contractVer string) json.RawMessage {
	b, err := os.ReadFile(harness.VectorPath("adapter/valid/notify-request.json"))
	if err != nil {
		panic(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	m["contract"], _ = json.Marshal(contractVer)
	out, _ := json.Marshal(m)
	return out
}

func TestContractRunnerDeclaration(t *testing.T) {
	r, _ := newRunner(t, true, "")
	d, err := r.Declaration(ctx, "notify", "webhook")
	harness.Implemented(t, err)
	if err != nil || d.Kind != "notify" || d.Name != "webhook" || d.Version != "1.0.0" || len(d.Raw) == 0 {
		t.Fatalf("宣言: %+v %v", d, err)
	}
	if _, err := r.Declaration(ctx, "notify", "nosuch"); err == nil {
		t.Error("無いアダプタの宣言が成功した")
	}
	bad, _ := newRunner(t, false, "")
	if _, err := bad.Declaration(ctx, "notify", "webhook"); err == nil {
		t.Error("署名の合わない adapter.json の宣言を返した(署名を検証した内容だけを返す)")
	}
}

func TestContractRunnerRunOK(t *testing.T) {
	r, marker := newRunner(t, true, "cat >/dev/null; echo '"+okResult+"'")
	res, err := r.Run(ctx, contract.RunContext{RunID: "r-20261018T173000Z-3f9a"}, "notify", "webhook", request("sitate.notify/v1"), nil)
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(marker); serr != nil {
		t.Error("アダプタが起動されていない")
	}
	if err := contract.Validate("adapter/notify.json#/$defs/response", res); err != nil || !strings.Contains(string(res), `"delivered":true`) {
		t.Errorf("結果: %s %v", res, err)
	}
}

func TestContractRunnerRunUnsignedNotStarted(t *testing.T) {
	r, marker := newRunner(t, false, "echo '"+okResult+"'")
	_, err := r.Run(ctx, contract.RunContext{}, "notify", "webhook", request("sitate.notify/v1"), nil)
	harness.Implemented(t, err)
	if err == nil {
		t.Error("署名の合わないアダプタを実行して成功にした")
	}
	if _, serr := os.Stat(marker); serr == nil {
		t.Error("署名の合わないアダプタを起動した")
	}
}

func TestContractRunnerRunContractMismatch(t *testing.T) {
	r, marker := newRunner(t, true, "echo '"+okResult+"'")
	_, err := r.Run(ctx, contract.RunContext{}, "notify", "webhook", request("sitate.notify/v0"), nil)
	harness.Implemented(t, err)
	ce := harness.ExpectCode(t, err, contract.CodeInternal)
	if !strings.Contains(string(ce.Detail), `"contract"`) || !strings.Contains(string(ce.Detail), `"adapter"`) {
		t.Errorf("detail: %s", ce.Detail)
	}
	if _, serr := os.Stat(marker); serr == nil {
		t.Error("contract の版が違う要求でアダプタを起動した")
	}
}

func TestContractRunnerRunOutputInvalid(t *testing.T) {
	r, _ := newRunner(t, true, "cat >/dev/null; echo '{}'")
	_, err := r.Run(ctx, contract.RunContext{}, "notify", "webhook", request("sitate.notify/v1"), nil)
	harness.Implemented(t, err)
	ce := harness.ExpectCode(t, err, contract.CodeInternal)
	if !strings.Contains(string(ce.Detail), `"output"`) {
		t.Errorf("スキーマに合わない出力は reason: output: %s", ce.Detail)
	}
}

// 終了 1 の error は結果として返さず、ADAPTER_ERROR の誤りに error の全体を載せる(contract.AdapterRunner)。
// 結果として返すと、呼び出し側が失敗を成功と取り違える。
func TestContractRunnerRunExit1Error(t *testing.T) {
	line := `{"contract":"sitate.notify/v1","error":{"code":"adapter:webhook:BOOM","detail":{}}}`
	r, _ := newRunner(t, true, "cat >/dev/null; echo '"+line+"'; exit 1")
	res, err := r.Run(ctx, contract.RunContext{}, "notify", "webhook", request("sitate.notify/v1"), nil)
	harness.Implemented(t, err)
	if err == nil {
		t.Fatalf("終了 1 が成功に見える: %s", res)
	}
	ce := harness.ExpectCode(t, err, contract.CodeAdapterError)
	if ce.Adapter == nil || ce.Adapter.Code != "adapter:webhook:BOOM" {
		t.Errorf("Error.Adapter に error の全体が無い: %+v", ce.Adapter)
	}
	if !strings.Contains(string(ce.Detail), `"adapter_code":"adapter:webhook:BOOM"`) {
		t.Errorf("detail.adapter_code: %s", ce.Detail)
	}
	if verr := contract.Validate("cli/detail.json#/$defs/ADAPTER_ERROR", ce.Detail); verr != nil {
		t.Errorf("detail がスキーマに合わない: %v", verr)
	}
}
