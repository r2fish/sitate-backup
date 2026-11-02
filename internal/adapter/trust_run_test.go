package adapter

// 完了条件 1(sha256 と署名)、8(adapter.json の contract とスキーマ)、9(trusted comment の期待値)、2(秘密の枠)。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// wantVerifier は期待値(trusted comment)が合うときだけ合格にする Verifier の偽物。
type wantVerifier struct {
	want  contract.TrustedComment
	calls []contract.TrustedComment
}

func (v *wantVerifier) Verify(_ context.Context, _, _ []byte, want contract.TrustedComment) (bool, error) {
	v.calls = append(v.calls, want)
	return want == v.want, nil
}

func TestRunSha256MismatchNotStarted(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; echo '"+okResult+"'"))
	// 署名後に実行ファイルを差し替える(adapter.json の sha256 は元のまま)
	if err := os.WriteFile(filepath.Join(b.dir, "exe"), []byte("#!/bin/sh\ntouch "+b.marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := runNotify(b)
	internalWith(t, err, "contract")
	if started(b) {
		t.Error("sha256 の合わない実行ファイルを起動した")
	}
}

func TestRunSha256MissingNotStarted(t *testing.T) {
	s := notifySpec("cat >/dev/null; echo '" + okResult + "'")
	s.mod = func(m map[string]any) { delete(m, "sha256") }
	b := build(t, s)
	_, err := runNotify(b)
	internalWith(t, err, "contract")
	if started(b) {
		t.Error("sha256 の無い adapter.json のアダプタを起動した")
	}
}

func TestRunSignatureRejectedNotStarted(t *testing.T) {
	s := notifySpec("cat >/dev/null; echo '" + okResult + "'")
	s.verifier = &wantVerifier{} // 何を渡しても合わない
	b := build(t, s)
	_, err := runNotify(b)
	internalWith(t, err, "contract")
	if started(b) {
		t.Error("署名の合わないアダプタを起動した")
	}
}

func TestRunTrustedCommentExpectation(t *testing.T) {
	want := contract.TrustedComment{Version: "1.0.0", Arch: "linux-x86_64", Path: "linux-x86_64/adapters/notify/webhook/adapter.json"}
	v := &wantVerifier{want: want}
	s := notifySpec("cat >/dev/null; echo '" + okResult + "'")
	s.verifier = v
	b := build(t, s)
	if _, err := runNotify(b); err != nil {
		t.Fatalf("期待値どおりなのに起動できない: %v", err)
	}
	if len(v.calls) == 0 || v.calls[0] != want {
		t.Errorf("Verifier に渡した期待値 %+v", v.calls)
	}
	for _, mod := range []func(r *Runner){
		func(r *Runner) { r.Version = "9.9.9" },
		func(r *Runner) { r.Arch = "linux-arm64" },
	} {
		b2 := build(t, s)
		b2.r.Verifier = v
		mod(b2.r)
		if _, err := runNotify(b2); err == nil || started(b2) {
			t.Errorf("trusted comment の期待値が合わないのに起動した: %v", err)
		}
	}
}

func TestRunDeclContractAndSchemaNotStarted(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"版が違う":         func(m map[string]any) { m["contract"] = "sitate.notify/v2" },
		"他の kind の項目":  func(m map[string]any) { m["inputs"] = []string{"tree"} },
		"必須の欠け(exe)":   func(m map[string]any) { delete(m, "exe") },
		"表に無い枠":        func(m map[string]any) { m["secrets"] = []string{"url", "admin"} },
		"kind が置き場と違う": func(m map[string]any) { m["kind"] = "provision" },
		"name が置き場と違う": func(m map[string]any) { m["name"] = "other" },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			s := notifySpec("cat >/dev/null; echo '" + okResult + "'")
			s.mod = mod
			b := build(t, s)
			_, err := runNotify(b)
			internalWith(t, err, "contract")
			if started(b) {
				t.Error("合わない adapter.json のアダプタを起動した")
			}
			if _, derr := b.r.Declaration(ctx, "notify", "webhook"); derr == nil {
				t.Error("合わない adapter.json の宣言を返した")
			}
		})
	}
}

func TestRunSecretsOutsideDeclaredSlotsDropped(t *testing.T) {
	b := build(t, notifySpec(`cat > "$0.stdin"`))
	var m map[string]json.RawMessage
	_ = json.Unmarshal(request("sitate.notify/v1"), &m)
	m["secrets"] = json.RawMessage(`{"url":"https://hooks.example.invalid/x","smtp":"smtp-pass-should-not-pass"}`)
	req, _ := json.Marshal(m)
	if _, err := b.r.Run(ctx, contract.RunContext{}, "notify", "webhook", req, nil); err == nil {
		t.Fatal("結果を返さないアダプタが成功した")
	}
	got, err := os.ReadFile(filepath.Join(b.dir, "exe.stdin"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, "smtp") || strings.Contains(s, "smtp-pass-should-not-pass") {
		t.Errorf("secrets[] に無い枠が渡った: %s", s)
	}
	if !strings.Contains(s, `"url":"https://hooks.example.invalid/x"`) || strings.Count(s, "\n") != 1 || !strings.HasSuffix(s, "\n") {
		t.Errorf("要求は secrets[] の枠を残した 1 行のはず: %q", s)
	}
}

// 要求のスキーマに合わない要求ではアダプタを起動しない(INTERNAL、reason: request)。版の違いは reason: contract が先。
func TestRunRequestSchemaInvalidNotStarted(t *testing.T) {
	cases := map[string]string{
		"event が通知の形でない": `{"contract":"sitate.notify/v1","to":"ops@example.invalid","event":{"schema":"x"},"secrets":{"url":"https://hooks.example.invalid/x"}}`,
		"必須の to が無い":     `{"contract":"sitate.notify/v1","event":{},"secrets":{}}`,
		"表に無い項目":         strings.Replace(string(request("sitate.notify/v1")), `"to"`, `"extra":1,"to"`, 1),
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			b := build(t, notifySpec("cat >/dev/null; echo '"+okResult+"'"))
			_, err := b.r.Run(ctx, contract.RunContext{}, "notify", "webhook", json.RawMessage(req), nil)
			internalWith(t, err, "request")
			if started(b) {
				t.Error("スキーマに合わない要求でアダプタを起動した")
			}
		})
	}
	b := build(t, notifySpec("cat >/dev/null; echo '"+okResult+"'"))
	_, err := b.r.Run(ctx, contract.RunContext{}, "notify", "webhook", json.RawMessage(`{"contract":"sitate.notify/v0","event":{}}`), nil)
	internalWith(t, err, "contract")
}

// 置き場の外を指すシンボリックリンクの実行ファイルは起動しない(中身の sha256 が合っても)。
func TestRunSymlinkExeNotStarted(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; echo '"+okResult+"'"))
	real := filepath.Join(b.root, "real-exe")
	if err := os.Rename(filepath.Join(b.dir, "exe"), real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(b.dir, "exe")); err != nil {
		t.Fatal(err)
	}
	_, err := runNotify(b)
	internalWith(t, err, "contract")
	if started(b) {
		t.Error("シンボリックリンクの実行ファイルを起動した")
	}
}
