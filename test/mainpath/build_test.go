//go:build mainpath

package mainpath

// 取得アダプタと SITATE_RECV の実体を静的リンクで作り、使い捨ての鍵で署名して Runner の根に置く。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	harnessVersion = "0.0.0-mainpath"
	repoRoot       = "../.."
)

func archName() string {
	if runtime.GOARCH == "arm64" {
		return "linux-arm64"
	}
	return "linux-x86_64"
}

func goBuild(t *testing.T, out, pkg string) {
	t.Helper()
	c := exec.Command("go", "build", "-trimpath", "-tags", "mainpath", "-o", out, pkg)
	c.Dir = repoRoot
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
}

// installAdapter は取得アダプタ files を <root>/acquire/files/ に置く(adapter.json と署名つき)。recvExe は SITATE_RECV の実体のパス。
func installAdapter(t *testing.T, root string, sg *signer) (recvExe string) {
	t.Helper()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	recvExe = filepath.Join(bin, "recvhelper")
	goBuild(t, recvExe, "./test/mainpath/recvhelper")
	dir := filepath.Join(root, "acquire", "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	goBuild(t, filepath.Join(dir, "files"), "./adapters/acquire/files")
	exe, err := os.ReadFile(filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(exe)
	decl, _ := json.Marshal(map[string]any{"kind": "acquire", "name": "files", "version": harnessVersion, "contract": contract.AdapterContract["acquire"],
		"exe": "files", "secrets": []string{}, "network": false, "sha256": hex.EncodeToString(sum[:])})
	if err := os.WriteFile(filepath.Join(dir, "adapter.json"), decl, 0o644); err != nil {
		t.Fatal(err)
	}
	tc := "version=" + harnessVersion + " arch=" + archName() + " path=" + archName() + "/adapters/acquire/files/adapter.json"
	if err := os.WriteFile(filepath.Join(dir, "adapter.json.sig"), sg.sign(decl, tc), 0o644); err != nil {
		t.Fatal(err)
	}
	return recvExe
}
