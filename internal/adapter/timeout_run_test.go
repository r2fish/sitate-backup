package adapter

// 完了条件 4(kind ごとの時限と実行の残りの早い方)。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestLimitForKinds(t *testing.T) {
	r := &Runner{}
	want := map[string]time.Duration{
		"acquire": contract.AdapterTimeoutAcquire, "restore-dest": contract.AdapterTimeoutRestoreDest,
		"notify": contract.AdapterTimeoutNotify, "provision": contract.AdapterTimeoutProvision, "check": contract.CheckTimeout,
	}
	for k, d := range want {
		if got := r.limitFor(k); got != d {
			t.Errorf("%s の時限 %v (期待 %v)", k, got, d)
		}
	}
}

// SIGTERM を無視するアダプタは猶予の後に SIGKILL で止まり、ADAPTER_TIMEOUT になる。
func TestKindTimeoutKillsAfterGrace(t *testing.T) {
	b := build(t, notifySpec("trap '' TERM; cat >/dev/null; while :; do sleep 1; done"))
	b.r.timeouts = map[string]time.Duration{"notify": 300 * time.Millisecond}
	b.r.grace = 300 * time.Millisecond
	start := time.Now()
	_, err := runNotify(b)
	el := time.Since(start)
	ce := errDetail(t, err, contract.CodeAdapterTimeout)
	var d struct {
		Kind, Name string
		TimeoutS   int64 `json:"timeout_s"`
	}
	_ = json.Unmarshal(ce.Detail, &d)
	if d.Kind != "notify" || d.Name != "webhook" || d.TimeoutS != 1 {
		t.Errorf("detail: %s", ce.Detail)
	}
	if el < 550*time.Millisecond || el > 5*time.Second {
		t.Errorf("時限 + 猶予の後に止まるはず: %v", el)
	}
}

func TestKindTimeoutSigtermStops(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; sleep 30"))
	b.r.timeouts = map[string]time.Duration{"notify": 200 * time.Millisecond}
	b.r.grace = 10 * time.Second
	start := time.Now()
	_, err := runNotify(b)
	errDetail(t, err, contract.CodeAdapterTimeout)
	if time.Since(start) > 5*time.Second {
		t.Errorf("SIGTERM で猶予を待たずに止まるはず: %v", time.Since(start))
	}
}

// 実行の残り(RunContext.Deadline)が kind の時限より早ければ RUN_ABORTED。
func TestRunDeadlineEarlierThanKindLimit(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; sleep 30"))
	rc := contract.RunContext{Deadline: time.Now().Add(200 * time.Millisecond)}
	start := time.Now()
	_, err := b.r.Run(ctx, rc, "notify", "webhook", request("sitate.notify/v1"), nil)
	ce := errDetail(t, err, contract.CodeRunAborted)
	if string(ce.Detail) != `{"reason":"max_duration"}` || time.Since(start) > 5*time.Second {
		t.Errorf("RUN_ABORTED: %s %v", ce.Detail, time.Since(start))
	}
}

func TestRunContextCancelled(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; sleep 30"))
	c, cancel := context.WithCancel(ctx)
	time.AfterFunc(200*time.Millisecond, cancel)
	_, err := b.r.Run(c, contract.RunContext{}, "notify", "webhook", request("sitate.notify/v1"), nil)
	errDetail(t, err, contract.CodeRunAborted)
}

// kind の時限が実行の残りより早ければ ADAPTER_TIMEOUT。
func TestKindLimitEarlierThanDeadline(t *testing.T) {
	b := build(t, notifySpec("cat >/dev/null; sleep 30"))
	b.r.timeouts = map[string]time.Duration{"notify": 200 * time.Millisecond}
	rc := contract.RunContext{Deadline: time.Now().Add(time.Hour)}
	_, err := b.r.Run(ctx, rc, "notify", "webhook", request("sitate.notify/v1"), nil)
	errDetail(t, err, contract.CodeAdapterTimeout)
}
