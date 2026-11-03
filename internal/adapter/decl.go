package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var reName = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// loaded は署名を検証した adapter.json と、その置き場。
type loaded struct {
	dir     string
	raw     []byte
	exe     string
	sha256  string
	secrets []string
	decl    contract.Declaration
}

// load は <Root>/<kind>/<name>/adapter.json を読み、署名と trusted comment(Verifier に期待値を渡す)、
// kind ごとのスキーマ、置き場との一致、contract の版を確かめる。合わなければ使わない(spec 3.2.1)。
func (r *Runner) load(ctx context.Context, kind, name string) (*loaded, error) {
	if _, ok := contract.AdapterContract[kind]; !ok || !reName.MatchString(name) {
		return nil, internalErr(kind, name, "request")
	}
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return nil, internalErr(kind, name, "request")
	}
	dir := filepath.Join(root, kind, name)
	raw, err := os.ReadFile(filepath.Join(dir, "adapter.json"))
	if err != nil {
		return nil, internalErr(kind, name, "request")
	}
	sig, err := os.ReadFile(filepath.Join(dir, "adapter.json.sig"))
	if err != nil {
		return nil, internalErr(kind, name, "signature") // 署名が無い物は署名が合わない物として扱う(spec 3.2.1。D-0159)
	}
	want := contract.TrustedComment{Version: r.Version, Arch: r.Arch, Path: r.Arch + "/adapters/" + kind + "/" + name + "/adapter.json"}
	if ok, verr := r.Verifier.Verify(ctx, raw, sig, want); verr != nil || !ok {
		return nil, internalErr(kind, name, "signature") // 署名が合わない、または確かめられない(spec 3.2.1。D-0159)
	}
	return parseDecl(dir, kind, name, raw)
}

// parseDecl は署名を検証済みの adapter.json の内容を確かめる(スキーマ・置き場との一致・contract)。
func parseDecl(dir, kind, name string, raw []byte) (*loaded, error) {
	var d struct {
		Kind, Name, Version, Contract, Exe, SHA256 string
		Secrets                                    []string
	}
	if json.Unmarshal(raw, &d) != nil || d.Contract != contract.AdapterContract[kind] {
		return nil, internalErr(kind, name, "contract")
	}
	if contract.Validate("adapter/adapter-json.json#/$defs/adapter", raw) != nil || d.Kind != kind || d.Name != name {
		return nil, internalErr(kind, name, "contract")
	}
	if d.Exe == "." || d.Exe == ".." || strings.ContainsAny(d.Exe, "/\x00") {
		return nil, internalErr(kind, name, "contract")
	}
	return &loaded{dir: dir, raw: raw, exe: filepath.Join(dir, d.Exe), sha256: d.SHA256, secrets: d.Secrets,
		decl: contract.Declaration{Kind: kind, Name: name, Version: d.Version, Raw: json.RawMessage(raw)}}, nil
}

// checkExe は実行ファイルの SHA-256 が adapter.json の sha256 と一致することを確かめる。sha256 が無ければ使わない。
func (l *loaded) checkExe() error {
	if fi, err := os.Lstat(l.exe); err != nil || !fi.Mode().IsRegular() {
		return internalErr(l.decl.Kind, l.decl.Name, "contract") // シンボリックリンクなどは起動しない
	}
	b, err := os.ReadFile(l.exe)
	if err != nil {
		return internalErr(l.decl.Kind, l.decl.Name, "request")
	}
	sum := sha256.Sum256(b)
	if l.sha256 == "" || hex.EncodeToString(sum[:]) != l.sha256 {
		return internalErr(l.decl.Kind, l.decl.Name, "signature") // sha256 が合わない(spec 3.2.1。D-0159)
	}
	return nil
}
