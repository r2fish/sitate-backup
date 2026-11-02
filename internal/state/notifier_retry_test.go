package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

var (
	notifyFail = fake.Step{Result: json.RawMessage(`{"contract":"sitate.notify/v1","delivered":false,"channel":"email","reason":"http_5xx","http_status":null,"provider_ref":null}`)}
	notifyOK   = fake.Step{Result: json.RawMessage(`{"contract":"sitate.notify/v1","delivered":true,"channel":"email","reason":null,"http_status":null,"provider_ref":null}`)}
)

func retryNotifier(steps ...fake.Step) *Notifier {
	ad := fake.NewAdapterRunner()
	ad.Script("notify", steps...)
	return &Notifier{State: &fake.StateFiles{}, Adapters: ad, Clock: &fake.Clock{T: fake.StartTime}, Backoff: time.Millisecond}
}

var retryEv = contract.Event{ID: "e-1", Type: "phrase.unconfirmed", Facts: []byte(`{}`)}

// 完了条件 13: 1 回目の失敗は同じ実行の中で再試行して配送され、NOTIFY_RETRY_MAX を超えた失敗は配送の失敗として残る。
func TestNotifierRetriesWithinRun(t *testing.T) {
	res, err := retryNotifier(notifyFail, notifyOK).Deliver(ctx, retryEv, []string{"a@example.invalid"})
	if err != nil || len(res) != 1 || !res[0].Delivered || res[0].Attempts != 2 {
		t.Fatalf("1 回目失敗・2 回目成功: %+v %v", res, err)
	}
	res, _ = retryNotifier(notifyFail, notifyFail, notifyFail, notifyFail, notifyOK).Deliver(ctx, retryEv, []string{"a@example.invalid"})
	if res[0].Delivered || res[0].Attempts != contract.NotifyRetryMax || res[0].Reason != "http_5xx" {
		t.Errorf("上限(%d 回)で止まる: %+v", contract.NotifyRetryMax, res[0])
	}
	n := retryNotifier(notifyFail, notifyFail, notifyFail, notifyOK)
	n.RetryMax = 2
	if res, _ = n.Deliver(ctx, retryEv, []string{"a@example.invalid"}); res[0].Attempts != 2 || res[0].Delivered {
		t.Errorf("RetryMax=2: %+v", res[0])
	}
}

// 1 つの宛先の失敗が他の宛先の配送を止めない。
func TestNotifierOneRecipientFailureDoesNotStopOthers(t *testing.T) {
	res, err := retryNotifier(notifyFail, notifyFail, notifyFail, notifyOK).Deliver(ctx, retryEv, []string{"a@example.invalid", "b@example.invalid"})
	if err != nil || len(res) != 2 || res[0].Delivered || !res[1].Delivered || res[1].To != "b@example.invalid" {
		t.Fatalf("%+v %v", res, err)
	}
}
