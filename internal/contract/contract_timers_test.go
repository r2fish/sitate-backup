package contract_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var updateGolden = flag.Bool("update-golden", false, "timers/golden を描画の結果で書き換える")

func TestTimerTemplatesGolden(t *testing.T) {
	tab, err := contract.LoadTimerTable()
	if err != nil {
		t.Fatal(err)
	}
	got, err := harness.RenderTimers(tab, "02:30", "Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2*len(tab.Units)+1 {
		t.Fatalf("描画の数: %d", len(got))
	}
	for name, body := range got {
		p := filepath.Join("timers", "golden", name)
		if *updateGolden {
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(p)
		if err != nil || string(want) != body {
			t.Errorf("%s: 描画が golden と違う(err=%v)\n%s", name, err, body)
		}
	}
	if contract.TimerSystemdTimeoutStart != contract.TimerCronTerm+contract.TimerCronKillAfter {
		t.Error("systemd の TimeoutStartSec と cron の timeout の合計が違う")
	}
	// 外側の上限は規則 12: 上限 + 猶予 + 記録の締め切り
	if contract.TimerSystemdTimeoutStart != 24*time.Hour+10*time.Minute+time.Minute {
		t.Errorf("TimeoutStartSec: %v", contract.TimerSystemdTimeoutStart)
	}
}

// 35 模擬日の起動回数は作業指示書 5.17 の完了条件 4 の期待回数(spec 3.1.2 のタイマー列から)と同じ。
func TestTimerCalls35Days(t *testing.T) {
	tab, _ := contract.LoadTimerTable()
	want := map[string]int{
		"backup": 35, "verify": 35, "bucket check": 5, "extend": 5, "prune": 5,
		"restore-test": 35, "report": 2, "report --weekly": 5, "apply": 840,
	}
	got := harness.TimerCalls(tab, 35)
	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %d want %d", k, got[k], v)
		}
	}
}

// 取り戻す(Persistent / @reboot)のは backup と続く verify だけ。
func TestTimerRetakeOnlyBackupAndVerify(t *testing.T) {
	tab, _ := contract.LoadTimerTable()
	for _, u := range tab.Units {
		for _, s := range u.Steps {
			name := s.Args[0]
			if s.Retake != (name == "backup" || name == "verify") || (s.Retake && !u.Persistent) {
				t.Errorf("%s: retake=%v persistent=%v", name, s.Retake, u.Persistent)
			}
		}
	}
}

func TestTestTickArgs(t *testing.T) {
	var d struct {
		Valid, Invalid []struct {
			Name string
			Argv []string
		}
	}
	loadVec(t, "testdata/cli/test-tick-args.json", &d)
	check := func(want bool, vs []struct {
		Name string
		Argv []string
	}) {
		for _, v := range vs {
			b, _ := json.Marshal(v.Argv)
			if err := contract.Validate("cli/test-tick-args.json", b); (err == nil) != want {
				t.Errorf("%s: want ok=%v err=%v", v.Name, want, err)
			}
		}
	}
	check(true, d.Valid)
	check(false, d.Invalid)
}
