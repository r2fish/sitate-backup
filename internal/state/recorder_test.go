package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func realRecorder(t *testing.T, n contract.Notifier, clk *fake.Clock) (*Recorder, *StateFiles, []string) {
	t.Helper()
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	codec := &harness.RefCodec{}
	sf := &StateFiles{Dir: t.TempDir(), Codec: codec, KeyS: fake.KeyService{}, Clock: clk}
	return &Recorder{State: sf, Store: store, Codec: codec, KeyS: fake.KeyService{}, Notifier: n, Clock: clk}, sf, nil
}

// 連鎖: 2 件目の prev_hash は 1 件目の entry_hash で、seq は 1 ずつ増え、状態ディレクトリと保管先に同じキーで置かれる。
func TestRecorderChain(t *testing.T) {
	clk := &fake.Clock{T: fake.StartTime}
	r, _, _ := realRecorder(t, &fake.Notifier{}, clk)
	p1, err := r.Record(ctx, []byte(`{"event":"a"}`))
	if err != nil || p1 != "" {
		t.Fatalf("1 件目: %q %v", p1, err)
	}
	p2, err := r.Record(ctx, []byte(`{"event":"b"}`))
	if err != nil || len(p2) != 64 {
		t.Fatalf("2 件目: %q %v", p2, err)
	}
	names, _ := r.listLocal()
	if len(names) != 2 || !strings.HasPrefix(names[0], "000000000001-") || !strings.HasPrefix(names[1], "000000000002-") || names[0][13:] != p2 {
		t.Fatalf("項目: %v prev=%s", names, p2)
	}
	key, _ := fake.KeyService{}.DerivedKey("journal")
	b, _ := r.readLocal(names[1])
	plain, err := r.Codec.Decode("SITJ", seqID(2), b, key)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(plain, &m)
	if m["seq"] != float64(2) || m["prev_hash"] != p2 {
		t.Errorf("平文: %v", m)
	}
	vs, _ := r.Store.ListVersions(ctx, "journal/")
	if len(vs) != 2 {
		t.Errorf("保管先の項目: %d", len(vs))
	}
	if err := r.SyncRemote(ctx); err != nil {
		t.Fatal(err)
	}
	if vs, _ = r.Store.ListVersions(ctx, "journal/"); len(vs) != 2 {
		t.Errorf("SyncRemote は冪等のはず: %d", len(vs))
	}
}

// state.lock を持たない追記は拒む。持っていれば追記でき、再入すると LOCKED。
func TestRecorderAppendLockedNeedsLock(t *testing.T) {
	r, sf, _ := realRecorder(t, &fake.Notifier{}, &fake.Clock{T: fake.StartTime})
	if _, err := r.AppendLocked(ctx, nil, []byte(`{"event":"x"}`)); err == nil {
		t.Fatal("ロック無しで追記できた")
	}
	h, err := sf.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	name, err := r.AppendLocked(ctx, h, []byte(`{"event":"x"}`))
	if err != nil || !journalItemRe.MatchString(name) {
		t.Fatalf("%q %v", name, err)
	}
	if vs, _ := r.Store.ListVersions(ctx, "journal/"); len(vs) != 0 {
		t.Error("AppendLocked が保管先へ書いた(放した後に書く)")
	}
}

func accepted(clk *fake.Clock) []contract.DeliveryResult {
	at := clk.T
	return []contract.DeliveryResult{{To: "a@example.invalid", Channel: "email", Delivered: true, Attempts: 1, AcceptedAt: &at}}
}

// 同じ種類の warn は 24 時間に 1 回にまとめ、24 時間の後の最初のものに送らなかった数を入れる。
func TestEmitMergeWarn(t *testing.T) {
	clk := &fake.Clock{T: fake.StartTime}
	n := &fake.Notifier{Result: accepted(clk)}
	r, _, _ := realRecorder(t, n, clk)
	emit := func() (contract.Event, int) {
		before := deliveries(n)
		ev, _, err := r.Emit(ctx, "phrase.unconfirmed", []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		return ev, deliveries(n) - before
	}
	if _, d := emit(); d != 1 {
		t.Fatal("1 回目は送る")
	}
	if _, d := emit(); d != 0 {
		t.Fatal("24 時間の内の 2 回目は送らない")
	}
	clk.T = clk.T.Add(25 * time.Hour)
	n.Result = accepted(clk)
	if ev, d := emit(); d != 1 || ev.SuppressedCount != 1 {
		t.Fatalf("24 時間の後: 送信=%d suppressed=%d", d, ev.SuppressedCount)
	}
}

// change.failed の state: applying は warn で、failed とは別にまとめる。critical はまとめない。
func TestEmitChangeFailedMergeKeys(t *testing.T) {
	clk := &fake.Clock{T: fake.StartTime}
	n := &fake.Notifier{Result: accepted(clk)}
	r, _, _ := realRecorder(t, n, clk)
	send := func(f string) (contract.Event, int) {
		b := deliveries(n)
		ev, _, err := r.Emit(ctx, "change.failed", []byte(f))
		if err != nil {
			t.Fatal(err)
		}
		return ev, deliveries(n) - b
	}
	app := `{"change_id":"c1","op":"def-change","error_code":"DEF_FILE_WRITE_FAILED","state":"applying"}`
	if ev, d := send(app); d != 1 || ev.Severity != "warn" {
		t.Fatalf("applying 1 回目: %d %s", d, ev.Severity)
	}
	if _, d := send(app); d != 0 {
		t.Error("applying の 2 回目はまとめる")
	}
	if _, d := send(`{"change_id":"c1","op":"def-change","error_code":"RUN_INTERRUPTED","state":"failed"}`); d != 1 {
		t.Error("failed は applying と別にまとめる")
	}
	crit := `{"change_id":"c2","op":"x","error_code":"SIGNATURE_INVALID","state":"failed"}`
	if ev, d := send(crit); d != 1 || ev.Severity != "critical" {
		t.Fatal("critical は送る")
	}
	if _, d := send(crit); d != 1 {
		t.Error("critical はまとめない")
	}
}

func deliveries(n *fake.Notifier) int {
	c := 0
	for _, x := range n.Calls() {
		if x == "Deliver" {
			c++
		}
	}
	return c
}
