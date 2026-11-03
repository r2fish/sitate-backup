//go:build mainpath

package mainpath

// 第 3 段の終わりで、手順書(docs/install.md)の手順 1〜13 の完了確認コマンドを実物の sitate(gate 経由)で実行し、手順書の期待する値に照らす
// (作業指示書 5.18 の完了条件 3。完了確認は手順書から読み、台本に写さない。test/e2e の LoadInstall と Judge を使う)。
// 引数に「...」を含む確認(手順 2・3 の dry-run)は、導入の途中の入力が要るので第 3 段では dryRunsBeforeDefinition が済ませており、ここでは飛ばす。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/test/e2e"
)

func e2eRoot() string {
	d, _ := os.Getwd()
	for ; d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	return "."
}

func lastLine(raw string) []byte {
	l := strings.Split(strings.TrimSpace(raw), "\n")
	return []byte(l[len(l)-1])
}

func timerNames(t interface{ Fatalf(string, ...any) }) json.RawMessage {
	tab, err := contract.LoadTimerTable()
	if err != nil {
		t.Fatalf("タイマー表: %v", err)
	}
	var names []string
	for _, u := range tab.Units {
		names = append(names, u.Name)
	}
	b, _ := json.Marshal(names)
	return b
}

func (w *s2) installMarks() e2e.Marks {
	t := w.t
	root := e2eRoot()
	pub, err := os.ReadFile(filepath.Join(root, "test/env/.run/keys/sitate-test.pub"))
	if err != nil {
		t.Fatalf("使い捨ての公開鍵: %v", err)
	}
	line := strings.Split(string(pub), "\n")[1]
	fp, err := exec.Command("bash", filepath.Join(root, "tools/fingerprint.sh"), line).Output()
	if err != nil {
		t.Fatalf("指紋: %v", err)
	}
	b, _ := json.Marshal(strings.TrimSpace(string(fp)))
	return e2e.Marks{"DIST_KEY_FINGERPRINT": b, "TIMER_NAMES": timerNames(t), "WARNING_ALLOWLIST": json.RawMessage(`[]`)}
}

type checkItem struct {
	step string
	c    e2e.Check
}

// orderedChecks は手順 1〜13 の完了確認を手順書の順に返す。notify test は新しい確認語を送り手順 10 の proof_confirmed を外すので最後に回す。
func orderedChecks(steps []e2e.Step) []checkItem {
	var items, late []checkItem
	for _, s := range steps {
		if len(s.ID) > 2 { // 付録の行は本体の通しの外
			continue
		}
		for _, c := range s.Checks {
			if c.Command == "sitate notify test" {
				late = append(late, checkItem{s.ID, c})
			} else {
				items = append(items, checkItem{s.ID, c})
			}
		}
	}
	return append(items, late...)
}

func (w *s2) installChecks() {
	t := w.t
	steps, err := e2e.LoadInstall(filepath.Join(e2eRoot(), "docs/install.md"))
	if err != nil {
		t.Fatal(err)
	}
	marks := w.installMarks()
	w.prepareForChecks()
	ran := 0
	for _, it := range orderedChecks(steps) {
		if strings.Contains(it.c.Command, "...") {
			t.Logf("手順 %s: %s は第 3 段の dry-run の確認で済み(ここでは飛ばす)", it.step, it.c.Command)
			continue
		}
		w.runCheck(it, marks)
		ran++
	}
	t.Logf("完了確認 %d 件を実行", ran)
}

func (w *s2) runCheck(it checkItem, marks e2e.Marks) {
	t, c := w.t, it.c
	stdin := ""
	if strings.HasSuffix(c.Command, "--file -") {
		stdin = w.definition() + "\n"
	}
	o := w.g.call(stdin, strings.Fields(strings.TrimPrefix(c.Command, "sitate "))...)
	if stdin != "" && o.Env.OK { // 直近に合格した def validate の normalized.schedule(手順 13 の右辺 <定義の schedule>)
		var r struct {
			N struct {
				Schedule *string `json:"schedule"`
			} `json:"normalized"`
		}
		if json.Unmarshal(o.Env.Result, &r) == nil && r.N.Schedule != nil {
			marks["DEF_SCHEDULE"], _ = json.Marshal(*r.N.Schedule)
		} else {
			delete(marks, "DEF_SCHEDULE")
		}
	}
	for _, e := range c.Expect {
		err := e2e.Judge(lastLine(o.Raw), e, marks)
		switch {
		case err == nil:
			t.Logf("手順 %s: %s: %s %s %s", it.step, c.Command, e.Pointer, e.Op, e.Value)
		case c.Command == "sitate doctor" && e.Pointer == "/result/passed" && onlyAdminAccessOpen(o.Env.Result):
			t.Logf("手順 %s: sitate doctor: 必須の項目で外れたのは admin_access_closed だけ(受入試験の環境は台本が管理アクセスを使い続けるので閉じない。実機では bootstrap が閉じる)", it.step)
		default:
			t.Errorf("手順 %s: %s: %v\n  結果: %s", it.step, c.Command, err, failingPart(o.Env.Result))
		}
	}
}

// prepareForChecks は第 3 段が通らない手順(復旧キットの保管の記録、最後の backup が成功の状態)を、手順 7・11 と同じ呼び出しで済ませる。
func (w *s2) prepareForChecks() {
	t := w.t
	k := w.g.call("", "kit").ok(t, "kit")
	var kr struct {
		Version string `json:"kit_version"`
	}
	_ = json.Unmarshal(k.Env.Result, &kr)
	w.g.call("", "kit", "confirm", "--kit-version", kr.Version, "--copies", "2").ok(t, "kit confirm")
	// 手順 11: kindEmptySource が元データを空にしたので、新しい内容を置いてから取る(最後の backup が健全な世代になる)。
	w.src.root("mkdir -p " + srcRoot + "/docs " + srcRoot + "/public && printf 'e2e check\\n' | tee " + srcRoot + "/docs/e2e-check.txt > " + srcRoot + "/public/e2e-check.css")
	b := w.g.call("", "backup").ok(t, "backup(手順 11)")
	t.Logf("backup(手順 11): %.800s warnings=%v", b.Env.Result, b.Env.Warnings)
}

// failingPart は結果のうち原因の手がかり(ok が偽の checks[]、notify、last_backup)だけを返す。
func failingPart(res json.RawMessage) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(res, &m) != nil {
		return string(res)
	}
	out := map[string]any{}
	var checks []map[string]any
	if json.Unmarshal(m["checks"], &checks) == nil {
		var bad []map[string]any
		for _, c := range checks {
			if c["ok"] == false {
				bad = append(bad, c)
			}
		}
		out["failed_checks"] = bad
	}
	for _, k := range []string{"notify", "last_backup", "actionable", "hold"} {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// onlyAdminAccessOpen は doctor の必須の項目のうち外れているのが admin_access_closed だけか。
func onlyAdminAccessOpen(res json.RawMessage) bool {
	var r struct {
		Checks []struct {
			Name     string `json:"name"`
			Severity string `json:"severity"`
			OK       bool   `json:"ok"`
		} `json:"checks"`
	}
	if json.Unmarshal(res, &r) != nil {
		return false
	}
	open := false
	for _, c := range r.Checks {
		if c.Severity == "required" && !c.OK {
			if c.Name != "admin_access_closed" {
				return false
			}
			open = true
		}
	}
	return open
}
