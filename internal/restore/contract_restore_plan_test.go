package restore

// ReadPlan の契約試験(spec 3.5 の読み出し量の規則): 計画の量は latest の世代が参照するデータのチャンクの保管先の上の大きさの和(同じチャンクは 1 回)、
// 累計は状態の storage_reads の今の現地の暦月の値、予約は計画の復元テストの分が消費されずに残っているか。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const defPlan = `{"version":1,"timezone":"Asia/Tokyo","restore_test":"monthly"}`

func hex64(c string) string { return strings.Repeat(c, 64) }

// planWorld は latest の世代 A(データのチャンク 100 B と 50 B。100 B は 2 回参照)と、より新しい復元できなかった印の世代 B(別の 999 B のチャンクを指す)を置く。
// 期待する計画の量は 150。
func planWorld(t *testing.T) (*ReadPlan, *fake.StateFiles) {
	t.Helper()
	e := newEnv()
	e.St.Def = []byte(defPlan)
	ws := &fake.ObjectStore{Core: e.Core, Bucket: "b", Role: fake.RoleWrite, Params: contract.FixedRetention{}}
	put := func(key string, n int) string {
		o, err := ws.Put(ctx, key, strings.NewReader(strings.Repeat("x", n)), contract.RetentionDaily)
		if err != nil {
			t.Fatal(err)
		}
		return o.VersionID
	}
	d1, d2, dz, kt := hex64("1"), hex64("2"), hex64("9"), hex64("3")
	v1, v2 := put("chunks/11/"+d1, 100), put("chunks/22/"+d2, 50)
	put("chunks/99/"+dz, 999)
	line := func(id, v string) string { return fmt.Sprintf(`{"id":%q,"v":%q,"k":"AAAA"}`+"\n", id, v) }
	kto, err := ws.Put(ctx, "chunks/33/"+kt, strings.NewReader("fake:"+line(d1, v1)+line(d2, v2)+line(d1, v1)), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(harness.VectorPath("data/valid/manifest-full.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["manifest_id"] = refA
	m["chunk_keys"] = map[string]any{"format": "sitate.chunkkeys/v1", "count": 3, "chunks": []any{map[string]any{"id": kt, "version_id": kto.VersionID, "key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}}}
	body, _ := json.Marshal(m)
	snaps := []snap{
		{Ref: refA, Class: "monthly", Health: "pass", Created: e.Clk.T, TargetsOK: true, Body: "fake:" + string(body)},
		{Ref: refB, Class: "daily", Health: "pass", Created: e.Clk.T, TargetsOK: true, RestoreFailed: true, Body: "fake:{}"},
	}
	seed(t, e.St, e.Clk, e.Core, snaps, time.Hour)
	return &ReadPlan{Store: e.Store, State: e.St, Adapters: e.AR, Codec: e.Codec, KeyS: e.KeyS, Crypto: e.Crypto, Recorder: e.Rec, Clock: e.Restore.Clock}, e.St
}

func planOf(t *testing.T, p *ReadPlan) contract.PlanAmount {
	t.Helper()
	a, err := p.Plan(ctx)
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestContractReadPlan(t *testing.T) {
	p, st := planWorld(t)
	st.Items["storage_reads"] = json.RawMessage(`{"month":"2027-03","read_bytes":7000,"reserved_used":0}`)
	a := planOf(t, p)
	if a.PlannedBytes != 150 || a.MonthToDateBytes != 7000 || !a.Reserved {
		t.Errorf("計画 150・累計 7000・予約が残っている を期待: %+v", a)
	}
	st.Items["storage_reads"] = json.RawMessage(`{"month":"2027-03","read_bytes":7000,"reserved_used":1}`)
	if a = planOf(t, p); a.PlannedBytes != 150 || a.MonthToDateBytes != 7000 || a.Reserved {
		t.Errorf("予約を消費済みなら Reserved は偽: %+v", a)
	}
	// 現地(Asia/Tokyo)の暦月が替わったら、前の月の累計を持ち越さず予約は新しい月の分が残る。
	st.Items["storage_reads"] = json.RawMessage(`{"month":"2027-02","read_bytes":7000,"reserved_used":1}`)
	if a = planOf(t, p); a.MonthToDateBytes != 0 || !a.Reserved {
		t.Errorf("月が替わったら累計 0・予約あり: %+v", a)
	}
}

// 世代が無ければ計画の量は 0、読み出しも無く、累計 0。
func TestContractReadPlanNoGeneration(t *testing.T) {
	e := newEnv()
	e.St.Def = []byte(defPlan)
	a := planOf(t, &ReadPlan{Store: e.Store, State: e.St, Clock: e.Restore.Clock})
	if a.PlannedBytes != 0 || a.MonthToDateBytes != 0 {
		t.Errorf("世代も読み出しも無い状態の計画: %+v", a)
	}
}
