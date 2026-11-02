//go:build mainpath

package mainpath

// 第 2 段のタイマー: init が登録した実物の systemd --user のタイマーと、sitate ユーザーの linger を確かめ、単位を実際に起動する。

import (
	"encoding/json"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func (w *s2) userSystemctl(args string) string {
	return w.bak.root(`U=$(id -u sitate); runuser -u sitate -- env XDG_RUNTIME_DIR=/run/user/$U systemctl --user ` + args)
}

func (w *s2) lastRuns() []struct {
	Command []string `json:"command"`
	OK      bool     `json:"ok"`
	RunID   string   `json:"run_id"`
} {
	var st struct {
		LastRuns []struct {
			Command []string `json:"command"`
			OK      bool     `json:"ok"`
			RunID   string   `json:"run_id"`
		} `json:"last_runs"`
	}
	if err := json.Unmarshal([]byte(w.bak.root("cat "+stateDir+"/state.json")), &st); err != nil {
		w.t.Fatalf("state.json: %v", err)
	}
	return st.LastRuns
}

// verifyTimers は contract の定期実行の表のとおりのタイマーが systemd --user に登録され、次の起動の時刻が付いていることと、
// 単位を起動すると核が --invoked-by timer で実行され last_runs[] に残ることを確かめる。実物の systemd のみ(cron の経路は未検証)。
func (w *s2) verifyTimers() {
	t := w.t
	if got := w.bak.root("loginctl show-user sitate -p Linger"); !strings.Contains(got, "Linger=yes") {
		t.Fatalf("sitate ユーザーの linger が有効でない: %s", got)
	}
	tab, err := contract.LoadTimerTable()
	if err != nil {
		t.Fatal(err)
	}
	list := w.userSystemctl("list-timers --all --no-pager")
	for _, u := range tab.Units {
		name := tab.UnitPrefix + u.Name + ".timer"
		var line string
		for _, l := range strings.Split(list, "\n") {
			if strings.Contains(l, name) {
				line = l
			}
		}
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "-") {
			t.Fatalf("タイマー %s が登録されていない/次の起動の時刻が無い:\n%s", name, list)
		}
	}
	t.Logf("systemd --user のタイマー(linger 有効):\n%s", list)
	svc := tab.UnitPrefix + "apply.service"
	if unit := w.bak.root("cat " + stateDir + "/.config/systemd/user/" + svc); !strings.Contains(unit, "--invoked-by timer") {
		t.Fatalf("単位 %s が --invoked-by timer を含まない:\n%s", svc, unit)
	}
	before := len(w.lastRuns())
	w.userSystemctl("start " + svc)
	runs := w.lastRuns()
	if len(runs) <= before || runs[len(runs)-1].Command[0] != "apply" || !runs[len(runs)-1].OK {
		t.Fatalf("単位 %s を起動しても apply の実行が last_runs[] に残らない: %+v", svc, runs)
	}
	t.Logf("単位 %s を systemd が起動し、核の apply が実行された(last_runs[] %d 件目 %s)", svc, len(runs), runs[len(runs)-1].RunID)
}
