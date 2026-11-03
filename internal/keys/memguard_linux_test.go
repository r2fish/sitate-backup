//go:build linux

package keys

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const probeEnv = "SITATE_MEMGUARD_PROBE"

// TestMemguardProbeChild は子として動くときだけ働く(環境変数があるとき)。単独では何もしない。
func TestMemguardProbeChild(t *testing.T) {
	if v := os.Getenv(probeEnv); v != "" {
		probeChild(v)
	}
}

// probeChild は子として HardenMemory を呼び、結果を 1 行の JSON で出す。v は MEMLOCK の soft 上限("keep" は変えない)。
func probeChild(v string) {
	if v != "keep" {
		n, _ := strconv.ParseUint(v, 10, 64)
		_ = syscall.Setrlimit(8, &syscall.Rlimit{Cur: n, Max: n})
	}
	ws := HardenMemory()
	lck := vmLck()
	b, _ := json.Marshal(map[string]any{"warnings": ws, "vmlck_kb": lck, "dumpable": Dumpable()})
	fmt.Println("PROBE:" + string(b))
}

func vmLck() int {
	b, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmLck:") {
			f := strings.Fields(l)
			n, _ := strconv.Atoi(f[1])
			return n
		}
	}
	return -1
}

type probeOut struct {
	Warnings []struct {
		Code   string
		Detail map[string]string
	}
	VmlckKB  int `json:"vmlck_kb"`
	Dumpable int
}

func runProbe(t *testing.T, limit string) probeOut {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMemguardProbeChild$")
	cmd.Env = append(os.Environ(), probeEnv+"="+limit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("子が失敗: %v\n%s", err, out)
	}
	var p probeOut
	for _, l := range strings.Split(string(out), "\n") {
		if j, ok := strings.CutPrefix(l, "PROBE:"); ok {
			if err := json.Unmarshal([]byte(j), &p); err != nil {
				t.Fatalf("子の出力が JSON でない: %q", j)
			}
			return p
		}
	}
	t.Fatalf("子の出力に PROBE が無い: %q", out)
	return p
}

func TestHardenMemoryFailureWarnsAndKeepsGoing(t *testing.T) {
	p := runProbe(t, "0") // 上限 0 では固定できない
	if len(p.Warnings) != 1 || p.Warnings[0].Code != "MLOCK_FAILED" {
		t.Fatalf("MLOCK_FAILED が 1 件のはずが %+v", p.Warnings)
	}
	if r := p.Warnings[0].Detail["reason"]; r != "enomem" && r != "eperm" && r != "eagain" {
		t.Fatalf("reason=%q", r)
	}
	if p.Dumpable != 0 {
		t.Fatalf("mlock が失敗しても dumpable は 0 のはずが %d", p.Dumpable)
	}
}

func TestHardenMemoryLocksProcess(t *testing.T) {
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(8, &rl); err != nil || rl.Max != ^uint64(0) {
		// カーネルは mlockall(MCL_CURRENT) を、仮想サイズが上限を超えるだけで拒む。上限が無制限の環境(systemd の LimitMEMLOCK=infinity と
		// 同じ。docker run --ulimit memlock=-1)でだけ成功の道を確かめられる。
		t.Skip("RLIMIT_MEMLOCK が無制限でない環境では固定の成功を確かめられない(失敗の道は別の試験が確かめる)")
	}
	p := runProbe(t, "keep")
	t.Logf("%+v", p)
	if p.Dumpable != 0 {
		t.Fatalf("dumpable=%d", p.Dumpable)
	}
	if len(p.Warnings) != 0 || p.VmlckKB <= 0 {
		t.Fatalf("無制限の上限で固定できるはずが warnings=%+v VmLck=%d", p.Warnings, p.VmlckKB)
	}
}
