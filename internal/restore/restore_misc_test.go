package restore

// U1e 自身の試験: 模擬時間のオフセットでの stored_at_mismatch(完了条件 6)、RunCleanup の資源の識別(別の資源を破棄しない)。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

type offsetClock struct {
	fake.Clock
	off time.Duration
}

func (o *offsetClock) Offset() (time.Duration, error) { return o.off, nil }

// 保管先の Date は実時刻で、マニフェストの created_at は模擬時間(30 日先)。オフセット 30 日では印が付かず、オフセットなしでは付く。
func TestRestoreListOffsetClock(t *testing.T) {
	const off = 30 * 24 * time.Hour
	for _, c := range []struct {
		off  time.Duration
		want bool
	}{{off, false}, {0, true}} {
		e := newEnv()
		snaps := []snap{{Ref: refA, Class: "daily", Health: "pass", Created: e.Clk.T.Add(off), TargetsOK: true}}
		seed(t, e.St, e.Clk, e.Core, snaps, time.Second)
		for i := range snaps {
			_ = i
		}
		e.Restore.Clock = &offsetClock{Clock: fake.Clock{T: fake.StartTime}, off: c.off}
		_, rows := callList(t, e)
		if len(rows) != 1 || rows[0].Mismatch != c.want {
			t.Errorf("offset=%v: stored_at_mismatch = %+v, want %v", c.off, rows, c.want)
		}
		// 模擬時間の版でも、保管先の書き込み時刻は実時刻のまま stored_at に出す。
		if rows[0].StoredAt == rows[0].At {
			t.Errorf("stored_at は保管先の時刻のまま(at とは別): %+v", rows[0])
		}
	}
}

type calls struct {
	*fake.AdapterRunner
}

func mkLedger(t *testing.T, rid, purpose, state, wd string) json.RawMessage {
	rec := map[string]any{}
	_ = json.Unmarshal(ledgerSession(t, rid, rid, true), &rec)
	rec["record_id"], rec["purpose"], rec["state"], rec["teardown"] = "l-"+wd, purpose, state, nil
	rec["resources"] = []map[string]string{{"type": "workdir", "id": wd}}
	b, _ := json.Marshal(rec)
	return b
}

// RunCleanup は台帳の記録ごとにその記録の資源だけを破棄の要求に入れる。開いている復元セッションには触れない。
func TestRunCleanupOnlyOwnResources(t *testing.T) {
	st := &fake.StateFiles{Ledger: []json.RawMessage{
		mkLedger(t, "r-20270227T000000Z-aaaa", "restore_test", "created", "0000000000000001"),
		mkLedger(t, "r-20270227T000000Z-bbbb", "restore_test", "creating", "0000000000000002"),
		mkLedger(t, "r-20270227T000000Z-cccc", "restore_session", "created", "0000000000000003"),
		mkLedger(t, "r-20270227T000000Z-dddd", "restore_test", "destroy_failed", "0000000000000004"),
	}}
	ar := fake.NewAdapterRunner()
	for range 8 {
		ar.Script("restore-dest", fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"done":true,"verified_by":"ssh","remaining":[],"resources":[],"state":"absent"}`)})
	}
	cl, un, err := (&RunCleanup{State: st, Adapters: ar}).Cleanup(ctx, "r-20270301T000000Z-eeee", "r-20270227T000000Z-aaaa")
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, c := range ar.Calls {
		var q struct {
			Op        string   `json:"op"`
			SessionID string   `json:"session_id"`
			Resources []resRef `json:"resources"`
		}
		_ = json.Unmarshal(c.Req, &q)
		if q.Op != "stop" {
			continue
		}
		if len(q.Resources) != 1 || q.Resources[0].ID == "" {
			t.Errorf("要求の資源が 1 つでない: %s", c.Req)
			continue
		}
		seen[q.SessionID] = q.Resources[0].ID
	}
	want := map[string]string{"r-20270227T000000Z-aaaa": "0000000000000001", "r-20270227T000000Z-bbbb": "0000000000000002"}
	if len(seen) != len(want) {
		t.Fatalf("破棄した session_id = %v (creating・created の 2 件だけ。開いた復元セッションと destroy_failed(中断の run_id が分かるとき)は対象外)", seen)
	}
	for sid, wd := range want {
		if seen[sid] != wd {
			t.Errorf("%s の破棄に別の資源 %q を入れた(%q のはず)", sid, seen[sid], wd)
		}
	}
	if len(cl) != 2 || len(un) != 0 {
		t.Errorf("片付けた資源 = %v, 残存 = %v", cl, un)
	}
	// 片付けを確かめられた記録だけ台帳から消え、開いた復元セッションと destroy_failed は残る。
	left := map[string]bool{}
	for _, r := range st.Ledger {
		var h struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(r, &h)
		left[h.SessionID] = true
	}
	if len(left) != 2 || !left["r-20270227T000000Z-cccc"] || !left["r-20270227T000000Z-dddd"] {
		t.Errorf("台帳に残る記録 = %v", left)
	}
}

// 中断の run_id が分からないとき(空)は、台帳を正とし destroy_failed の記録も対象にする(開いている復元セッションを除く)。
func TestRunCleanupUnknownInterruptedIncludesDestroyFailed(t *testing.T) {
	rec := map[string]any{}
	_ = json.Unmarshal(ledgerSession(t, "r-20270227T000000Z-dddd", "r-20270227T000000Z-dddd", true), &rec)
	b, _ := json.Marshal(rec)
	st := &fake.StateFiles{Ledger: []json.RawMessage{b}}
	ar := fake.NewAdapterRunner()
	ar.Script("restore-dest", fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"done":true,"verified_by":"ssh","remaining":[]}`)},
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"resources":[],"state":"absent"}`)})
	cl, _, err := (&RunCleanup{State: st, Adapters: ar}).Cleanup(ctx, "r-20270301T000000Z-eeee", "")
	harness.Implemented(t, err)
	if err != nil || len(cl) != 1 || len(st.Ledger) != 0 {
		t.Errorf("destroy_failed の記録を片付けていない: %v %v ledger=%d", cl, err, len(st.Ledger))
	}
}

var _ = context.Background
var _ = contract.CodeInternal
