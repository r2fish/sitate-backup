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

type captureNotifier struct {
	fake.Notifier
	events []contract.Event
}

func (n *captureNotifier) Deliver(_ context.Context, ev contract.Event, _ []string) ([]contract.DeliveryResult, error) {
	n.events = append(n.events, ev)
	at := ev.Time
	return []contract.DeliveryResult{{To: "ops@example.jp", Channel: "email", Delivered: true, Attempts: 1, AcceptedAt: &at}}, nil
}

// 作業指示書 5.17 の完了条件 22: 除外のある定義で件数と除外 id が載り、根のパスと DB 名(クラスタのポート)が通知の本文に無い。
func TestReportWeeklyDigestCarriesDeclinedIDsNotNames(t *testing.T) {
	f, _ := applyEnv(t, &applyStub{})
	cap := &captureNotifier{}
	f.rec.Notifier = cap
	def := strings.Replace(applyDef, `"targets"`, `"declined":[{"id":"d1","mount":"/mnt/secret-volume"},{"id":"d2","postgresql_cluster":5499}],"targets"`, 1)
	if err := f.d.State.SwitchDef(bg, contract.SwitchInput{Normalized: mustNorm(t, f, def)}); err != nil {
		t.Fatal(err)
	}
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	f.clk.T = time.Date(2026, 11, 9, 9, 0, 0, 0, tokyo).UTC() // 月曜日 09:00
	inWeek := time.Date(2026, 11, 3, 2, 30, 0, 0, tokyo).UTC().Format(time.RFC3339)
	before := time.Date(2026, 11, 1, 2, 30, 0, 0, tokyo).UTC().Format(time.RFC3339)
	lk, _ := f.d.State.Lock(bg)
	_ = f.d.State.Update(bg, lk, "snapshots", json.RawMessage(`[{"ref":"m-old","created_at":"`+before+`","health":"pass","targets_ok":true,"size":1},{"ref":"m-new","created_at":"`+inWeek+`","health":"pass","targets_ok":true,"size":5}]`))
	lk.Release()

	rep, err := reportEntry(f.d, bg, contract.Call{Words: []string{"report"}, Args: map[string][]string{"weekly": {""}}})
	if err != nil || len(cap.events) != 1 {
		t.Fatalf("%v events=%d", err, len(cap.events))
	}
	ev := cap.events[0]
	if ev.Type != "digest.weekly" || ev.Severity != "info" {
		t.Fatalf("%+v", ev)
	}
	var facts struct {
		Start    string           `json:"period_start_at"`
		End      string           `json:"period_end_at"`
		Backups  []map[string]any `json:"backups"`
		Count    int              `json:"declined_count"`
		IDs      []string         `json:"declined_ids"`
		Uncov    *int             `json:"uncovered_count"`
		LastOKAt any              `json:"last_restore_ok_at"`
	}
	if err := json.Unmarshal(ev.Facts, &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Count != 2 || len(facts.IDs) != 2 || facts.IDs[0] != "d1" || facts.IDs[1] != "d2" || facts.Uncov == nil {
		t.Errorf("件数と除外 id(毎回載せる): %+v", facts)
	}
	if len(facts.Backups) != 1 || facts.Backups[0]["snapshot_ref"] != "m-new" {
		t.Errorf("前の 7 日の世代だけ: %+v", facts.Backups)
	}
	if facts.Start != time.Date(2026, 11, 2, 0, 0, 0, 0, tokyo).UTC().Format(time.RFC3339) || facts.End != time.Date(2026, 11, 9, 0, 0, 0, 0, tokyo).UTC().Format(time.RFC3339) {
		t.Errorf("期間(前の月曜日〜日曜日): %s %s", facts.Start, facts.End)
	}
	for _, secret := range []string{"secret-volume", "5499", "/mnt"} {
		if strings.Contains(string(ev.Facts), secret) || strings.Contains(string(rep.Result), secret) {
			t.Errorf("実名が通知か結果に出ている: %s", secret)
		}
	}
	checkDeliveredAndOncePerWeek(t, f, cap, rep.Result)
}

func checkDeliveredAndOncePerWeek(t *testing.T, f *flowEnv, cap *captureNotifier, result json.RawMessage) {
	t.Helper()
	var res struct {
		Delivered []map[string]any `json:"delivered"`
	}
	_ = json.Unmarshal(result, &res)
	if len(res.Delivered) != 1 || res.Delivered[0]["delivered"] != true {
		t.Errorf("delivered[]: %s", result)
	}
	n, _ := f.d.State.Get(bg, "notify")
	if !strings.Contains(string(n), "last_digest_at") || !strings.Contains(string(n), "last_digest_accepted_at") {
		t.Errorf("notify: %s", n)
	}
	// 同じ週の一覧は 1 回だけ通知する。
	if _, err := reportEntry(f.d, bg, contract.Call{Args: map[string][]string{"weekly": {""}}}); err != nil || len(cap.events) != 1 {
		t.Errorf("2 回目: %v events=%d", err, len(cap.events))
	}
	f.clk.T = f.clk.T.Add(7 * 24 * time.Hour)
	if _, err := reportEntry(f.d, bg, contract.Call{Args: map[string][]string{"weekly": {""}}}); err != nil || len(cap.events) != 2 {
		t.Errorf("翌週: %v events=%d", err, len(cap.events))
	}
}

func TestReportMonthlyIsNotImplementedYet(t *testing.T) {
	f, _ := applyEnv(t, &applyStub{})
	if _, err := reportEntry(f.d, bg, contract.Call{Args: map[string][]string{}}); err == nil || !strings.Contains(err.Error(), "NOT_IMPLEMENTED") {
		t.Errorf("%v", err)
	}
}
