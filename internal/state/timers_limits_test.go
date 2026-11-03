package state

// 主経路の第 2 段で見つけた誤り E5 の試験(spec 3.1.1「鍵のメモリ」): init が登録する systemd の単位には LimitCORE=0 と LimitMEMLOCK=infinity を置く。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTimersSystemdServiceHasMemoryLimits(t *testing.T) {
	rr := &runRec{}
	tm, _ := newTimers(t, "systemd", rr)
	if err := tm.Register(context.Background(), oneUnit); err != nil {
		t.Fatal(err)
	}
	units, _ := filepath.Glob(filepath.Join(tm.Dir, "sitate-*.service"))
	if len(units) == 0 {
		t.Fatal("service の単位が無い")
	}
	for _, u := range units {
		b, _ := os.ReadFile(u)
		svc := string(b)
		sec := svc[strings.Index(svc, "[Service]"):]
		for _, want := range []string{"LimitCORE=0\n", "LimitMEMLOCK=infinity\n"} {
			if !strings.Contains(sec, want) {
				t.Errorf("%s の [Service] に %q が無い:\n%s", filepath.Base(u), strings.TrimSpace(want), svc)
			}
		}
	}
}

// F8: 足す場所(TimeoutStartSec= の行)が無い単位は、黙って足さずに誤りにする(雛形が変わって制限が消えるのを見逃さない)。
func TestWithMemoryLimitsFailsWhenAnchorIsMissing(t *testing.T) {
	if _, err := withMemoryLimits("[Service]\nExecStart=/bin/true\n"); err == nil {
		t.Fatal("TimeoutStartSec= が無いのに黙って通った")
	}
	out, err := withMemoryLimits("[Service]\nTimeoutStartSec=1\nExecStart=/bin/true\n")
	if err != nil || !strings.Contains(out, "LimitCORE=0\nLimitMEMLOCK=infinity\n") {
		t.Fatalf("%q %v", out, err)
	}
}
