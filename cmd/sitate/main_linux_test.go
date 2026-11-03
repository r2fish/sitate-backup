//go:build linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 実物のバイナリを、MEMLOCK の上限 0 で起動すると、封筒の warnings[] に MLOCK_FAILED が出て処理は続く(spec 3.1.1「鍵のメモリ」)。
func TestBinaryWarnsMlockFailedInEnvelope(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "sitate")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command("sh", "-c", `ulimit -S -l 0 && exec "$0" status`, bin)
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, _ := cmd.Output() // 状態ディレクトリが無く失敗してよい。封筒が出ることだけを見る
	line := strings.TrimSpace(string(out))
	var env struct {
		Schema   string
		Warnings []struct {
			Code   string
			Detail map[string]string
		}
	}
	if err := json.Unmarshal([]byte(line), &env); err != nil || env.Schema != "sitate.out/v1" {
		t.Fatalf("封筒でない: %q (%v)", line, err)
	}
	n := 0
	for _, w := range env.Warnings {
		if w.Code == "MLOCK_FAILED" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("MLOCK_FAILED が 1 件のはずが %d: %s", n, line)
	}
}
