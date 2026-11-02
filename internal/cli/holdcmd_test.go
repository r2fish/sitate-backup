package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// holdStub は Retention の偽物: 掛けた原因を記録して、状態の hold を実物の Retention と同じ形で書く代わりに、固定の時刻を返す。
type holdStub struct {
	fake.Retention
	got   []contract.HoldInput
	since time.Time
}

func (h *holdStub) HoldSet(_ context.Context, in contract.HoldInput) (time.Time, error) {
	h.got = append(h.got, in)
	return h.since, nil
}

func holdEnv(t *testing.T) (*flowEnv, *holdStub) {
	t.Helper()
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	h := &holdStub{since: time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)}
	f.d.Store = h
	if err := f.d.State.SwitchDef(bg, contract.SwitchInput{Normalized: mustNorm(t, f, applyDef)}); err != nil {
		t.Fatal(err)
	}
	return f, h
}

func TestHoldSetReturnsHeldSinceAndNotifiesOnce(t *testing.T) {
	f, h := holdEnv(t)
	o := execute(bg, f.d, []string{"hold", "set"}, secretNone)
	if o.err != nil || !strings.Contains(string(o.result), `"held_since":"2026-11-03T01:00:00Z"`) {
		t.Fatalf("%v %s", o.err, o.result)
	}
	if len(h.got) != 1 || h.got[0].Reason != "manual" || h.got[0].Code != nil {
		t.Errorf("%+v", h.got)
	}
	n := 0
	for _, x := range f.rec.Notifications() {
		if x["type"] == "hold.set" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("hold.set の通知: %d", n)
	}
	if it := f.ends(t, "hold"); len(it) != 1 {
		t.Errorf("journal の hold: %d", len(it))
	}
}

// 作業指示書 5.17 の完了条件 5 の 14: LOCKED を返す経路でも、タイマーの実行の始めに自動の削除停止の条件を判定して掛ける。
func TestAutoHoldRunsOnLockedTimerRunOnly(t *testing.T) {
	f, h := holdEnv(t)
	holder := &Lease{Dir: f.d.StateDir}
	if ok, err := holder.Acquire(bg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	f.clk.T = f.clk.T.Add(20 * 24 * time.Hour) // seal から 20 日(健全な世代が無いので seal が基点)
	o := execute(bg, f.d, []string{"backup"}, secretNone)
	if o.err == nil || o.err.Code != contract.CodeLocked || len(h.got) != 0 {
		t.Fatalf("タイマーでない実行は判定しない: %v %+v", o.err, h.got)
	}
	o = execute(bg, f.d, []string{"backup", "--invoked-by", "timer"}, secretNone)
	if o.err == nil || o.err.Code != contract.CodeLocked {
		t.Fatalf("%v", o.err)
	}
	if len(h.got) != 1 || h.got[0].Reason != "no_healthy_backup" || h.got[0].Days == nil || *h.got[0].Days != 20 {
		t.Fatalf("%+v", h.got)
	}
}

func TestAutoHoldBelowThresholdDoesNothingAndHealthySnapshotIsTheBase(t *testing.T) {
	f, h := holdEnv(t)
	f.clk.T = f.clk.T.Add(13 * 24 * time.Hour)
	f.d.autoHold(bg, "")
	if len(h.got) != 0 {
		t.Fatalf("13 日では掛けない: %+v", h.got)
	}
	healthy := f.clk.T.Add(-2 * 24 * time.Hour).UTC().Format(time.RFC3339)
	lk, _ := f.d.State.Lock(bg)
	_ = f.d.State.Update(bg, lk, "snapshots", json.RawMessage(`[{"created_at":"`+healthy+`","health":"pass","targets_ok":true,"restore_failed":false}]`))
	lk.Release()
	f.clk.T = f.clk.T.Add(13 * 24 * time.Hour) // seal からは 26 日だが、健全な世代からは 15 日
	f.d.autoHold(bg, "")
	if len(h.got) != 1 || *h.got[0].Days != 15 {
		t.Fatalf("健全な世代が基点: %+v", h.got)
	}
}

func TestPlannerHoldReleaseAndNotifyProofStdin(t *testing.T) {
	f, _ := holdEnv(t)
	seedHold(t, f, `[`+causeCheck+`]`)
	plan, mat, err := f.d.Planner(bg, "hold-release", nil)
	if err != nil || !strings.Contains(string(plan["causes"]), "CHECK_FAILED") || string(plan["since"]) != `"2026-11-01T00:00:00Z"` || len(mat["hold"]) == 0 {
		t.Fatalf("%s %s %v", plan["causes"], plan["since"], err)
	}
	if p, m, e := f.d.Planner(bg, "delay-change", nil); p != nil || m != nil || e != nil {
		t.Error("hold-release 以外は追加なし")
	}
	f.d.Stdin = strings.NewReader("{\"ops@example.jp\":\"word-1\"}\nrest")
	p, _, perr := parseArgs([]string{"approve", "--id", "c-1", "--notify-proof-stdin"})
	if perr != nil {
		t.Fatal(perr)
	}
	c, cerr := buildCall(bg, f.d, p, nil)
	if cerr != nil || c.Args["notify-proof-stdin"][0] != `{"ops@example.jp":"word-1"}` {
		t.Fatalf("%v %+v", cerr, c.Args)
	}
	rest, _ := f.d.ReadDefinition(bg, "-")
	if string(rest) != "rest" {
		t.Errorf("続けて標準入力から定義を読む: %q", rest)
	}
}

func setHoldState(t *testing.T, f *flowEnv, v string) {
	t.Helper()
	lk, _ := f.d.State.Lock(bg)
	if err := f.d.State.Update(bg, lk, "hold", json.RawMessage(v)); err != nil {
		t.Fatal(err)
	}
	lk.Release()
}

// M1: hold-release の後の suppressed[] にある原因では再発動しない。停止中の同じ原因は重ねない。成功で suppressed[] を消す。
func TestAutoHoldHonoursSuppressedAndDoesNotRepeat(t *testing.T) {
	f, h := holdEnv(t)
	f.clk.T = f.clk.T.Add(20 * 24 * time.Hour)
	setHoldState(t, f, `{"active":false,"causes":[],"suppressed":[{"reason":"no_healthy_backup","code":null,"refs":[]}]}`)
	f.d.autoHold(bg, "")
	if len(h.got) != 0 {
		t.Fatalf("suppressed の原因で再発動した: %+v", h.got)
	}
	setHoldState(t, f, `{"active":true,"since":"2026-11-01T00:00:00Z","causes":[{"reason":"no_healthy_backup","code":null,"at":"2026-11-01T00:00:00Z","days":14,"refs":[]}],"suppressed":[]}`)
	f.d.autoHold(bg, "")
	f.d.autoHold(bg, "")
	if len(h.got) != 0 || len(f.ends(t, "hold")) != 0 {
		t.Fatalf("停止中の同じ原因を重ねた: %+v %d", h.got, len(f.ends(t, "hold")))
	}
	setHoldState(t, f, `{"active":false,"causes":[],"suppressed":[{"reason":"no_healthy_backup","code":null,"refs":[]}]}`)
	f.d.clearSuppressed(bg)
	v, _ := f.d.State.Get(bg, "hold")
	if !strings.Contains(string(v), `"suppressed":[]`) {
		t.Errorf("成功で suppressed[] を消す: %s", v)
	}
	f.d.autoHold(bg, "")
	if len(h.got) != 1 {
		t.Errorf("抑止を終えたら再び判定する: %+v", h.got)
	}
}
