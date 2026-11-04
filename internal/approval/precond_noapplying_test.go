package approval

import (
	"encoding/json"
	"strings"
	"testing"
)

// 完了条件 2: 前提の再計算と一致の確認は applying の追記より前。一致しなければ applying を書かない(PRECONDITION_CHANGED)。
func TestPreconditionChangedNeverAppendsApplying(t *testing.T) {
	count := func(e *env, id string) int {
		b, _, _ := e.a.store().b.read(logName)
		return strings.Count(string(b), `"applying"`)
	}
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.approveOK(p)
	e.st.Items["repo_id"] = json.RawMessage(`"rp-other"`)
	plan, _ := e.apply(nil)
	if len(plan.Failed) != 1 || !strings.Contains(string(plan.Failed[0]), "PRECONDITION_CHANGED") || lastState(t, e, p.ChangeID) != "failed" {
		t.Fatalf("前提が変わった変更: %+v", plan)
	}
	if n := count(e, p.ChangeID); n != 0 {
		t.Errorf("前提が合わないのに applying を %d 回書いた", n)
	}
	// 対照: 前提が合えば applying が 1 回書かれる(この数え方が空振りでないことの確認)。
	e2 := newEnv(t, true)
	q := e2.propose("restore-test-increase", incr)
	e2.approveOK(q)
	if plan, _ := e2.apply(nil); len(plan.Changes) != 1 || count(e2, q.ChangeID) != 1 {
		t.Fatalf("対照: %+v applying=%d", plan, count(e2, q.ChangeID))
	}
}
