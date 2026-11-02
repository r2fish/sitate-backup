package restore

// list の契約試験(作業指示書 5.6 の目的、spec 3.1.2.3): 世代の一覧を保管先の書き込み時刻と状態の世代の一覧から作る。

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

type listRow struct {
	Ref             string `json:"ref"`
	At              string `json:"at"`
	StoredAt        string `json:"stored_at"`
	Mismatch        bool   `json:"stored_at_mismatch"`
	Class           string `json:"retention_class"`
	Health          string `json:"health"`
	TargetsOK       bool   `json:"targets_ok"`
	RestoreVerified *bool  `json:"restore_verified"`
	RestoreFailed   bool   `json:"restore_failed"`
	Findings        []any  `json:"findings"`
}

// listWorld は 4 世代(A が最古)を置く。保管先の書き込み時刻は 2 日おき + 700ms(秒への切り捨ても確かめる)。
// A: 健全・復元確認済み / B: 復元できなかった印 / C: 健常性 suspect / D: 対象の一部が ok でなく、manifest の created_at が書き込み時刻より TIME_SKEW_MAX を超えて前。
func listWorld(t *testing.T) (*env, []snap) {
	t.Helper()
	e := newEnv()
	e.Clk.Advance(700 * time.Millisecond)
	base := e.Clk.T
	d := func(n int) time.Time { return base.Add(time.Duration(2*n) * 24 * time.Hour) }
	snaps := []snap{
		{Ref: refA, Class: "monthly", Health: "pass", Created: d(1), TargetsOK: true, RestoreVerified: bptr(true)},
		{Ref: refB, Class: "daily", Health: "pass", Created: d(2), TargetsOK: true, RestoreVerified: bptr(false), RestoreFailed: true},
		{Ref: refC, Class: "daily", Health: "suspect", Created: d(3), TargetsOK: true},
		{Ref: refD, Class: "daily", Health: "pass", Created: d(4).Add(-contract.TimeSkewMax - time.Minute), TargetsOK: false, RestoreVerified: bptr(false)},
	}
	seed(t, e.St, e.Clk, e.Core, snaps, 48*time.Hour)
	return e, snaps
}

func callList(t *testing.T, e *env) (map[string]json.RawMessage, []listRow) {
	t.Helper()
	rep, err := e.List(ctx, contract.Call{Words: []string{"list"}})
	harness.Shape(t, []string{"list"}, rep, err)
	m := resultOf(t, rep, "list")
	var rows []listRow
	if err := json.Unmarshal(m["backups"], &rows); err != nil {
		t.Fatal(err)
	}
	return m, rows
}

// 世代の一覧は at の新しい順で、stored_at は保管先の書き込み時刻(秒に切り捨て)、latest は健全で全対象 ok で復元できなかった印の無い最新の世代。
func TestContractRestoreList(t *testing.T) {
	e, snaps := listWorld(t)
	m, rows := callList(t, e)
	if len(rows) != 4 {
		t.Fatalf("世代は 4 つのはず: %d", len(rows))
	}
	for i, ref := range []string{refD, refC, refB, refA} {
		if rows[i].Ref != ref {
			t.Fatalf("at の新しい順(D C B A)でない: %d 番目は %s", i, rows[i].Ref)
		}
	}
	byRef := map[string]listRow{}
	for _, r := range rows {
		byRef[r.Ref] = r
	}
	for _, s := range snaps {
		r := byRef[s.Ref]
		if r.StoredAt != ts(s.Stored) || r.At != ts(s.Created) || r.Class != s.Class || r.Health != s.Health || r.TargetsOK != s.TargetsOK || r.RestoreFailed != s.RestoreFailed {
			t.Errorf("%s の行が世代の記録と合わない: %+v", s.Ref, r)
		}
		if r.Findings == nil {
			t.Errorf("%s: findings は配列(所見が無ければ [])", s.Ref)
		}
	}
	if v := byRef[refA].RestoreVerified; v == nil || !*v {
		t.Errorf("A の restore_verified は真のまま: %v", v)
	}
	if v := byRef[refC].RestoreVerified; v != nil {
		t.Errorf("C は確かめた記録が無く null のまま(偽にしない): %v", *v)
	}
	wantRaw(t, m, "latest", `"`+refA+`"`)
	if _, ok := m["anchor"]; ok {
		t.Error("--anchor が無いのに anchor がある")
	}
}

// マニフェストの created_at と書き込み時刻が TIME_SKEW_MAX を超えて違う世代だけに stored_at_mismatch の印が付く。
func TestContractRestoreListStoredAtMismatch(t *testing.T) {
	e, _ := listWorld(t)
	_, rows := callList(t, e)
	for _, r := range rows {
		if want := r.Ref == refD; r.Mismatch != want {
			t.Errorf("%s: stored_at_mismatch は %v を期待", r.Ref, want)
		}
	}
}

// 世代が無ければ backups は [](null でない)、latest は null。健全な世代が無いときも latest は null。
func TestContractRestoreListEmpty(t *testing.T) {
	e := newEnv()
	seed(t, e.St, e.Clk, e.Core, nil, time.Hour)
	m, rows := callList(t, e)
	if len(rows) != 0 || string(m["backups"]) != "[]" {
		t.Errorf("backups は []: %s", m["backups"])
	}
	wantRaw(t, m, "latest", "null")

	e, _ = listWorld(t)
	var arr []map[string]any
	_ = json.Unmarshal(e.St.Items["snapshots"], &arr)
	for _, a := range arr {
		a["health"] = "suspect"
	}
	e.St.Items["snapshots"], _ = json.Marshal(arr)
	m, _ = callList(t, e)
	wantRaw(t, m, "latest", "null")
}
