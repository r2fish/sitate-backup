package restore

// restore-test の契約試験(作業指示書 5.6、spec 3.1.2.6・3.5)。保管先は偽の保管先、復元先アダプタは偽の AdapterRunner。
// 復元先へ核と鍵を送って復元し照合する段(3.5 の表の 6〜8)は、Restore に継ぎ目が無く(報告の「契約の不足」)、
// 合格経路(proof)はここでは駆動できない。ここでは、実行を始める前に決まる分岐(送り・拒否・世代の選択・復元先への最初の要求)を判定する。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const rtRun = "r-20270301T000000Z-3f9a"

// rtWorld は定義が ssh-host の復元先で、健全な世代 A と、より新しい復元できなかった印の世代 B を持つ。
// rtWorld は健全なキーリングと config の副本を持つ世界(表の 1 が先に行われるため、どの経路の試験もこの世界から始める)。
func rtWorld(t *testing.T) *env { return keyringWorld(t, defSSH, worldOpts{}) }

// rtWorldBody は世代 A・B のマニフェストの本体を指定できる(空なら仮の文)。
func rtWorldBody(t *testing.T, bodyA, bodyB string) *env {
	t.Helper()
	e := newEnv()
	e.St.Def = []byte(defSSH)
	snaps := []snap{
		{Ref: refA, Class: "monthly", Health: "pass", Created: e.Clk.T, TargetsOK: true, RestoreVerified: bptr(true), Body: bodyA},
		{Ref: refB, Class: "daily", Health: "pass", Created: e.Clk.T, TargetsOK: true, RestoreFailed: true, Body: bodyB},
	}
	seed(t, e.St, e.Clk, e.Core, snaps, 24*time.Hour)
	return e
}

func rtCall(force bool) contract.Call {
	c := contract.Call{Words: []string{"restore-test"}, RunID: rtRun}
	if force {
		c.Args = map[string][]string{"force": {}}
	}
	return c
}

// 前の破棄の残存が片付いていない間は、新しい資源を作らずに次回へ送る(タイマーも --force も同じ。予約は消費しない)。
func TestContractRestoreTestDeferredTeardownPending(t *testing.T) {
	for _, force := range []bool{false, true} {
		e := rtWorld(t)
		e.St.Ledger = []json.RawMessage{ledgerSession(t, "r-20270227T000000Z-aaaa", "r-20270227T000000Z-aaaa", true)}
		rep, err := e.RestoreTest(ctx, rtCall(force))
		harness.Shape(t, []string{"restore-test"}, rep, err)
		m := resultOf(t, rep, "restore_test")
		wantRaw(t, m, "deferred", "true")
		wantRaw(t, m, "deferred_reason", `"teardown_pending"`)
		for _, k := range []string{"backup_ref", "restore_ok", "keyring_ok", "config_copy_ok", "def_copy_ok", "duration_s", "egress_bytes", "teardown", "proof"} {
			wantRaw(t, m, k, "null")
		}
		wantRaw(t, m, "checks", "[]")
		if n := len(e.AR.Calls); n != 0 {
			t.Errorf("force=%v: 送った実行が復元先アダプタを %d 回呼んだ", force, n)
		}
		if len(rep.Warnings) != 1 || rep.Warnings[0].Code != contract.CodeTeardownUnverified {
			t.Fatalf("force=%v: 警告 TEARDOWN_UNVERIFIED が 1 つのはず: %+v", force, rep.Warnings)
		}
		var d struct {
			Sessions []struct {
				SessionID string              `json:"session_id"`
				Remaining []map[string]string `json:"remaining"`
			} `json:"sessions"`
		}
		_ = json.Unmarshal(rep.Warnings[0].Detail, &d)
		if len(d.Sessions) != 1 || d.Sessions[0].SessionID != "r-20270227T000000Z-aaaa" || len(d.Sessions[0].Remaining) != 1 {
			t.Errorf("force=%v: detail.sessions[] に残存したセッションと資源が無い: %s", force, rep.Warnings[0].Detail)
		}
	}
}

// ssh-host の復元先で復元セッションが開いているときは実行せず、警告 RESTORE_SESSION_OPEN で翌日へ送る。
func TestContractRestoreTestDeferredSessionOpen(t *testing.T) {
	e := rtWorld(t)
	rec := ledgerSession(t, "r-20270228T000000Z-bbbb", "r-20270228T000000Z-bbbb", false)
	var m0 map[string]any
	_ = json.Unmarshal(rec, &m0)
	m0["purpose"], m0["state"], m0["expires_at"] = "restore_session", "created", "2027-03-02T00:00:00Z"
	rec, _ = json.Marshal(m0)
	e.St.Ledger = []json.RawMessage{rec}
	rep, err := e.RestoreTest(ctx, rtCall(false))
	harness.Shape(t, []string{"restore-test"}, rep, err)
	m := resultOf(t, rep, "restore_test")
	wantRaw(t, m, "deferred", "true")
	wantRaw(t, m, "deferred_reason", `"session_open"`)
	wantRaw(t, m, "proof", "null")
	if len(rep.Warnings) != 1 || rep.Warnings[0].Code != contract.CodeRestoreSessionOpen {
		t.Errorf("警告 RESTORE_SESSION_OPEN が 1 つのはず: %+v", rep.Warnings)
	}
	if n := len(e.AR.Calls); n != 0 {
		t.Errorf("送った実行が復元先アダプタを %d 回呼んだ", n)
	}
}

// 対象にできる世代(健全・全対象 ok・復元できなかった印なし)が無ければ NO_HEALTHY_BACKUP で、復元先を作らない。
func TestContractRestoreTestNoHealthyBackup(t *testing.T) {
	e := keyringWorld(t, defSSH, worldOpts{})
	var arr []map[string]any
	_ = json.Unmarshal(e.St.Items["snapshots"], &arr)
	for _, s := range arr {
		s["restore_failed"] = true // 全世代が「復元できなかった」印つき
	}
	e.St.Items["snapshots"], _ = json.Marshal(arr)
	_, err := e.RestoreTest(ctx, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeNoHealthyBackup)
	if n := len(e.AR.Calls); n != 0 {
		t.Errorf("復元先アダプタを %d 回呼んだ", n)
	}
}

// ledgerSpy は復元先アダプタの呼び出しの瞬間の台帳を写す(「呼ぶ前に session_id を台帳に書く」を確かめる)。
type ledgerSpy struct {
	*fake.AdapterRunner
	st     *fake.StateFiles
	ledger map[string][]json.RawMessage
}

func (s *ledgerSpy) Run(c context.Context, rc contract.RunContext, kind, name string, req json.RawMessage, f func(contract.Frame) error) (json.RawMessage, error) {
	var h struct {
		Op string `json:"op"`
	}
	_ = json.Unmarshal(req, &h)
	s.ledger[h.Op] = append([]json.RawMessage(nil), s.st.Ledger...)
	return s.AdapterRunner.Run(c, rc, kind, name, req, f)
}

type adReq struct {
	Op        string `json:"op"`
	SessionID string `json:"session_id"`
	Dest      struct {
		At     string `json:"at"`
		KeyRef string `json:"key_ref"`
	} `json:"dest"`
}

// 復元先を用意する最初の要求(start)は、定義の復元先と run_id を session_id に持ち、呼ぶ前に台帳へ session_id が書かれている。
// 失敗した start(何も作らなかった)は ADAPTER_ERROR で返り、復元は読み出しを始めない。
func TestContractRestoreTestStartRequest(t *testing.T) {
	e := rtWorld(t)
	e.St.Items["host_keys"] = json.RawMessage(`[{"role":"restore_dest","host":"dest.example.invalid","host_key_fp":"` + fpDest + `"}]`) // 固定した指紋が無ければ start の前に止まる
	e.AR.Script("restore-dest",
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"resources":[],"state":"absent"}`)}, // 掃引の inspect
		fake.Step{Outcome: fake.OutcomeError, ErrCode: "ADAPTER_FAILED"})                                      // start
	spy := &ledgerSpy{AdapterRunner: e.AR, st: e.St, ledger: map[string][]json.RawMessage{}}
	e.Restore.Adapters = spy
	_, err := e.RestoreTest(ctx, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeAdapterError)
	var start *adReq
	var ops []string
	for _, c := range e.AR.Calls {
		var r adReq
		_ = json.Unmarshal(c.Req, &r)
		ops = append(ops, r.Op)
		if r.Op == "start" {
			start = &r
			if c.Kind != "restore-dest" || c.Name != "ssh-host" || c.RC.RunID != rtRun {
				t.Errorf("start の呼び出しの種別・名前・run_id が違う: %+v", c)
			}
		}
	}
	if len(ops) < 2 || ops[0] != "inspect" || start == nil {
		t.Fatalf("掃引(inspect)の後に start を呼ぶはず: %v", ops)
	}
	if start.SessionID != rtRun || start.Dest.At != destURL || start.Dest.KeyRef != "dest-key" {
		t.Errorf("start の要求が定義の復元先と run_id に合わない: %+v", start)
	}
	found := false
	for _, l := range spy.ledger["start"] {
		var r struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(l, &r)
		found = found || r.SessionID == rtRun
	}
	if !found {
		t.Errorf("start を呼ぶ前の台帳に session_id %s の記録が無い: %s", rtRun, spy.ledger["start"])
	}
}
