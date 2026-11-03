//go:build mainpath

package mainpath

// 第 3 段: 核のメモリのピークと MLOCK_FAILED。ピークは実行中に /proc/<pid>/status の VmHWM(最大の常駐量)を 0.02 秒ごとに読み、
// プロセスごとの最後の値(VmHWM は単調)の最大を記録する。標本の間隔より短い山は VmHWM が覚えている。

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// peakRSS は fn の間、host で動く comm=sitate のプロセスの VmHWM(kB)を測り、最大を返す。
func (h *host) peakRSS(t *testing.T, label string, fn func()) int {
	t.Helper()
	log := "/dev/shm/hwm-" + label + ".log"
	script := "while :; do for p in $(ps -eo pid=,comm= | awk '$2==\"sitate\"{print $1}'); do echo \"$p $(grep VmHWM /proc/$p/status 2>/dev/null)\"; echo \"L $(grep -i 'locked memory' /proc/$p/limits 2>/dev/null)\"; done; sleep 0.02; done\n"
	h.root(`rm -f ` + log + `; echo ` + base64.StdEncoding.EncodeToString([]byte(script)) + ` | base64 -d > ` + log + `.sh; setsid nohup sh ` + log + `.sh >` + log + ` 2>/dev/null </dev/null & echo $! > ` + log + `.pid`)
	fn()
	h.root(`kill $(cat ` + log + `.pid) 2>/dev/null || true`)
	last := map[string]int{}
	for _, l := range strings.Split(h.root("cat "+log), "\n") {
		f := strings.Fields(l) // pid VmHWM: N kB
		if len(f) > 1 && f[0] == "L" {
			if h.lockLim == nil {
				h.lockLim = map[string]bool{}
			}
			h.lockLim[strings.Join(f[1:], " ")] = true // 実行中の核の Max locked memory(soft hard 単位)
			continue
		}
		if len(f) >= 4 {
			if n, err := strconv.Atoi(f[2]); err == nil {
				last[f[0]] = n
			}
		}
	}
	max := 0
	for _, n := range last {
		if n > max {
			max = n
		}
	}
	t.Logf("核のメモリのピーク(%s、%s): VmHWM 最大 %d kB(標本のあったプロセス %d 個)", label, h.role, max, len(last))
	return max
}

// checkMlock は gate 経由の全封筒に MLOCK_FAILED が載らなかったことを判定する。no-mlock(足す前の状態)では載った回数を記録するだけ。
func (w *s2) checkMlock() {
	t := w.t
	n := w.g.warns["MLOCK_FAILED"]
	t.Logf("実行中の核(gate 経由 / restore-test)の /proc/<pid>/limits の Max locked memory: %v", w.bak.lockLim)
	t.Logf("gate 経由の呼び出し %d 回の封筒の警告の集計: %v", w.g.calls, w.g.warns)
	if noMlock() {
		t.Logf("足す前の状態(limits.d を置かない): MLOCK_FAILED は %d / %d 回の封筒に載った", n, w.g.calls)
		return
	}
	if n != 0 {
		t.Errorf("limits.d に memlock unlimited を置いたのに、gate 経由の封筒に MLOCK_FAILED が %d / %d 回載った", n, w.g.calls)
	}
	t.Logf("limits.d: %s", strings.TrimSpace(w.bak.root(`cat /etc/security/limits.d/sitate.conf`)))
}

// noMlock は足す前の状態を再現する指定: test/env/.run/accept-<NAME>/no-mlock があれば limits.d を置かない。
func noMlock() bool {
	_, err := os.Stat(filepath.Join(repoRoot, "test/env/.run/accept-"+os.Getenv("SITATE_ACCEPT_NAME"), "no-mlock"))
	return err == nil
}
