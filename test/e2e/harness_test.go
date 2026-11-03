//go:build sitate_testbuild

package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// 手順書の読み込み: 手順 1〜13 のすべてに完了確認があり、期待する値が読めること(完了確認を試験の中に写さない)。
func TestInstallStepsLoadFromDoc(t *testing.T) {
	steps, err := LoadInstall("")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]Step{}
	for _, s := range steps {
		have[s.ID] = s
	}
	for _, id := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13"} {
		s, ok := have[id]
		if !ok {
			t.Fatalf("手順 %s が手順書に無い", id)
		}
		for _, c := range s.Checks {
			if len(c.Expect) == 0 || !strings.HasPrefix(c.Command, "sitate ") {
				t.Errorf("手順 %s: 完了確認が読めていない: %+v", id, c)
			}
		}
	}
}

// 判定の道具が期待する値を正しく通し、外れを落とすこと。
func TestJudge(t *testing.T) {
	env := []byte(`{"ok":true,"result":{"schedule":["a","b"],"permissions":[{"actual":"x","expected":"x"}],"signing_key_fp":"FP"},"warnings":[{"code":"W1"}]}`)
	marks := Marks{"DIST_KEY_FINGERPRINT": []byte(`"FP"`), "TIMER_NAMES": []byte(`["a"]`), "WARNING_ALLOWLIST": []byte(`["W1"]`)}
	ok := []Expect{
		{"/ok", "=", "true"},
		{"/result/signing_key_fp", "=", "{{DIST_KEY_FINGERPRINT}}"},
		{"/result/schedule", "⊇", "{{TIMER_NAMES}}"},
		{"/warnings/<i>/code", "⊆", "{{WARNING_ALLOWLIST}}"},
		{"/result/permissions/<i>/actual", "=", "/result/permissions/<i>/expected"},
	}
	for _, e := range ok {
		if err := Judge(env, e, marks); err != nil {
			t.Errorf("%+v が落ちた: %v", e, err)
		}
	}
	bad := []Expect{
		{"/ok", "=", "false"},
		{"/result/signing_key_fp", "=", "{{TIMER_NAMES}}"},
		{"/result/schedule", "⊇", "{{WARNING_ALLOWLIST}}"},
		{"/warnings/<i>/code", "⊆", "{{TIMER_NAMES}}"},
		{"/result/nothing", "=", "1"},
	}
	for _, e := range bad {
		if err := Judge(env, e, marks); err == nil {
			t.Errorf("%+v が通ってしまった", e)
		}
	}
}

// 結合: 環境で手順 1〜13 の完了確認コマンドを実行し、手順書の期待する値に照らす。
// 環境(SITATE_E2E_BIN: sitate を呼ぶ実行ファイル、SITATE_E2E_MARKS: 印の値の JSON)が無いときは未検証として飛ばす。
func TestInstallCompletionChecks(t *testing.T) {
	bin := os.Getenv("SITATE_E2E_BIN")
	if bin == "" {
		t.Skip("未検証: SITATE_E2E_BIN が無い(結合の環境は日 5。手順 1〜13 を実行済みの状態で走らせる)")
	}
	var marks Marks
	if p := os.Getenv("SITATE_E2E_MARKS"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := jsonUnmarshal(b, &marks); err != nil {
			t.Fatal(err)
		}
	}
	steps, err := LoadInstall("")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if len(s.ID) > 2 { // 付録の行は本体の通しの外
			continue
		}
		for _, c := range s.Checks {
			args := strings.Fields(strings.TrimPrefix(c.Command, "sitate "))
			out, err := exec.Command(bin, args...).Output() // 完了確認は読み取りの道具。結果が失敗の封筒でも JSON は出る
			if len(out) == 0 && err != nil {
				t.Errorf("手順 %s: %s: %v", s.ID, c.Command, err)
				continue
			}
			for _, e := range c.Expect {
				if err := Judge(out, e, marks); err != nil {
					t.Errorf("手順 %s: %s: %v", s.ID, c.Command, err)
				}
			}
		}
	}
}

// 手順 13 の右辺 <定義の schedule>: marks の DEF_SCHEDULE(直近に合格した def validate の normalized.schedule)、無ければ既定 02:30(D-0168)。
func TestJudgeDefScheduleRef(t *testing.T) {
	env := []byte(`{"ok":true,"result":{"schedule":"03:15"}}`)
	e := Expect{"/result/schedule", "=", DefScheduleRef}
	if err := Judge(env, e, Marks{"DEF_SCHEDULE": []byte(`"03:15"`)}); err != nil {
		t.Errorf("定義の schedule と一致するのに落ちた: %v", err)
	}
	if err := Judge(env, e, Marks{}); err == nil {
		t.Errorf("定義に schedule が無い(既定 02:30)のに 03:15 が通った")
	}
	if err := Judge([]byte(`{"result":{"schedule":"02:30"}}`), e, Marks{}); err != nil {
		t.Errorf("既定: %v", err)
	}
	if err := Judge(env, e, Marks{"DEF_SCHEDULE": []byte(`"04:00"`)}); err == nil {
		t.Errorf("違う schedule が通った")
	}
}
