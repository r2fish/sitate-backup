package state

// 契約試験(作業指示書 3.2): 一時ディレクトリを状態ディレクトリにした実物と、偽物(U0 の fake)を差し込んだ外形。
// 骨格は NOT_IMPLEMENTED で落ち、nil を返すだけの実装は値の比較で落ち、仕様どおりの実装は panic しない。WP は骨格を実装し、この試験は変えない。
// 状態ファイルの暗号化の形式(SITS)は internal/contract/harness の参照 Codec で照らす。

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var ctx = context.Background()

func newFiles(t *testing.T) *StateFiles {
	return &StateFiles{Dir: t.TempDir(), Codec: &harness.RefCodec{}, KeyS: fake.KeyService{}, Clock: &fake.Clock{T: fake.StartTime}}
}

func mustLock(t *testing.T, s *StateFiles) contract.StateLockHandle {
	t.Helper()
	h, err := s.Lock(ctx)
	harness.Implemented(t, err)
	if err != nil || h == nil {
		t.Fatalf("Lock: %v %v", h, err)
	}
	return h
}

// 排他: 保持中の Lock は LOCKED(再試行できる)、解放すれば取れる。ロックの外では書けない。
func TestContractStateLock(t *testing.T) {
	s := newFiles(t)
	h, err := s.Lock(ctx)
	harness.Implemented(t, err)
	if err != nil || h == nil {
		t.Fatalf("Lock: %v %v", h, err)
	}
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_, err = s.Lock(short)
	if err == nil {
		t.Fatal("保持中の Lock が再び取れた")
	}
	h.Release()
	h2, err := s.Lock(ctx)
	if err != nil {
		t.Fatalf("解放後の Lock: %v", err)
	}
	h2.Release()
}

func TestContractStateUpdateGet(t *testing.T) {
	s := newFiles(t)
	h := mustLock(t, s)
	defer h.Release()
	if err := s.Update(ctx, h, "phrase_confirmed", json.RawMessage(`true`)); err != nil {
		t.Fatal(err)
	}
	got, err := (&StateFiles{Dir: s.Dir, Codec: s.Codec, KeyS: s.KeyS, Clock: s.Clock}).Get(ctx, "phrase_confirmed")
	if err != nil || string(got) != "true" {
		t.Errorf("別のプロセス(新しい StateFiles)から読む: %s %v", got, err)
	}
	if err := s.Update(ctx, nil, "phrase_confirmed", json.RawMessage(`false`)); err == nil {
		t.Error("ロックを持たない更新が成功した")
	}
}

func TestContractStateLedger(t *testing.T) {
	s := newFiles(t)
	h := mustLock(t, s)
	defer h.Release()
	rec := json.RawMessage(`{"run_id":"r-20261018T173000Z-3f9a","resource":"x"}`)
	if err := s.LedgerAppend(ctx, h, rec); err != nil {
		t.Fatal(err)
	}
	l, err := s.LedgerList(ctx)
	if err != nil || len(l) != 1 {
		t.Fatalf("台帳: %v %v", l, err)
	}
	var a, b map[string]any
	_ = json.Unmarshal(l[0], &a)
	_ = json.Unmarshal(rec, &b)
	if a["run_id"] != b["run_id"] {
		t.Errorf("台帳の内容: %s", l[0])
	}
}

func TestContractStateValidateDef(t *testing.T) {
	s := newFiles(t)
	_, _, issues, err := s.ValidateDef(ctx, []byte(`{"nosuchkey": 1`), false)
	harness.Implemented(t, err)
	if err == nil && (len(issues) == 0 || string(issues) == "[]" || string(issues) == "null") {
		t.Error("壊れた定義に指摘が無い")
	}
}

func TestContractStateStageDef(t *testing.T) {
	s := newFiles(t)
	norm := []byte(`{"timezone":"Asia/Tokyo"}`)
	err := s.StageDef(ctx, norm)
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	got, hash, found, err := s.ReadCandidate(ctx)
	want, _ := contract.CanonicalHash(norm)
	if err != nil || !found || string(got) != string(norm) || hash != want {
		t.Errorf("候補: %s %s %v %v(def_hash は %s)", got, hash, found, err, want)
	}
}

func TestContractStateReadDef(t *testing.T) {
	_, _, err := newFiles(t).ReadDef(ctx)
	harness.Implemented(t, err)
	if err == nil {
		t.Error("承認済みの定義が無いのに ReadDef が成功した")
	}
}

func TestContractStateSwitchDef(t *testing.T) {
	err := newFiles(t).SwitchDef(ctx, contract.SwitchInput{})
	harness.Implemented(t, err)
	if err == nil {
		t.Error("空の定義への切り替えが成功した")
	}
}

func TestContractStateRecordReadStart(t *testing.T) {
	harness.Later(t, 3, "StateFiles.RecordReadStart")
}

func TestContractStateSettleInterrupted(t *testing.T) {
	harness.Later(t, 3, "StateFiles.SettleInterrupted")
}

func newRecorder() *Recorder {
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	return &Recorder{State: &fake.StateFiles{}, Store: store, Codec: &harness.RefCodec{}, KeyS: fake.KeyService{}, Notifier: &fake.Notifier{}, Clock: &fake.Clock{T: fake.StartTime}}
}

// 記録は連鎖する: 2 件目の prevHash は空でなく、1 件目のものと違う。
func TestContractRecorderRecord(t *testing.T) {
	r := newRecorder()
	h1, err := r.Record(ctx, []byte(`{"event":"run_start"}`))
	harness.Implemented(t, err)
	h2, err2 := r.Record(ctx, []byte(`{"event":"run_end"}`))
	if err != nil || err2 != nil || h2 == "" || h2 == h1 {
		t.Errorf("連鎖: %q %q %v %v", h1, h2, err, err2)
	}
}

func TestContractRecorderAppendLocked(t *testing.T) {
	_, err := newRecorder().AppendLocked(ctx, nil, []byte(`{"event":"x"}`))
	harness.Implemented(t, err)
	if err == nil {
		t.Error("state.lock を持たない排他の中の追記が成功した")
	}
}

func TestContractRecorderWriteRemote(t *testing.T) {
	err := newRecorder().WriteRemote(ctx, []byte("not an item"))
	harness.Implemented(t, err)
	if err == nil {
		t.Error("形式に合わない項目を保管先へ書いた")
	}
}

func TestContractRecorderEmit(t *testing.T) {
	r := newRecorder()
	ev, _, err := r.Emit(ctx, "phrase.unconfirmed", []byte(`{}`))
	harness.Implemented(t, err)
	if err != nil || ev.Type != "phrase.unconfirmed" || ev.Time.IsZero() || ev.Next == nil || ev.SuppressedCount < 0 {
		t.Fatalf("イベント: %+v %v", ev, err)
	}
	// 通知アダプタへ渡す sitate.event/v1(schema/data/event.json)の必須項目を、Event が運べる値で満たす。
	if !regexp.MustCompile(`^e-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}-[0-9]{2,}$`).MatchString(ev.ID) || !slices.Contains([]string{"info", "warn", "critical"}, ev.Severity) {
		t.Errorf("イベントの id・severity の形: %q %q", ev.ID, ev.Severity)
	}
	if _, _, err := r.Emit(ctx, "no.such.event", []byte(`{}`)); err == nil {
		t.Error("通知イベントの種類に無い type が成功した")
	}
}

// 配送の結果は通知アダプタの結果から宛先ごとに作る(channel は結果の channel)。台本が尽きた(アダプタの失敗)ときは delivered=false・reason=adapter_failed。
func TestContractNotifierDeliver(t *testing.T) {
	ad := fake.NewAdapterRunner()
	ad.Script("notify", fake.Step{Result: json.RawMessage(`{"contract":"sitate.notify/v1","delivered":true,"channel":"email","reason":null,"http_status":null,"provider_ref":null}`)})
	n := &Notifier{State: &fake.StateFiles{}, Adapters: ad, Clock: &fake.Clock{T: fake.StartTime}}
	to := "ops@example.invalid"
	res, err := n.Deliver(ctx, contract.Event{ID: "e-1", Type: "phrase.unconfirmed", Facts: []byte(`{}`)}, []string{to, "second@example.invalid"})
	harness.Implemented(t, err)
	if err != nil || len(res) != 2 || res[0].To != to || res[0].Channel != "email" || !res[0].Delivered || res[0].AcceptedAt == nil || res[0].Attempts < 1 {
		t.Fatalf("宛先ごとの結果(受理): %+v %v", res, err)
	}
	if res[1].To != "second@example.invalid" || res[1].Delivered || res[1].Reason != "adapter_failed" || res[1].AcceptedAt != nil {
		t.Errorf("アダプタが失敗した宛先: %+v", res[1])
	}
}

func TestContractNotifierResend(t *testing.T) {
	n := &Notifier{State: &fake.StateFiles{}, Adapters: fake.NewAdapterRunner(), Clock: &fake.Clock{T: fake.StartTime}}
	res, err := n.Resend(ctx)
	harness.Implemented(t, err)
	if err != nil || res == nil {
		t.Errorf("再送するものが無くても結果は配列: %v %v", res, err)
	}
}

func TestContractTestNotifierTest(t *testing.T) {
	ad := fake.NewAdapterRunner()
	okRes := json.RawMessage(`{"contract":"sitate.notify/v1","delivered":true,"channel":"email","reason":null,"http_status":null,"provider_ref":null}`)
	ad.Script("notify", fake.Step{Result: okRes}, fake.Step{Result: okRes})
	n := &TestNotifier{State: &fake.StateFiles{}, Adapters: ad, Clock: &fake.Clock{T: fake.StartTime}}
	if _, err := n.Test(ctx, nil); err == nil {
		harness.Implemented(t, err)
		t.Error("宛先の無い notify test が成功した")
	}
	h, err := n.Test(ctx, []string{"a@example.invalid", "b@example.invalid"})
	harness.Implemented(t, err)
	// 通知先ごとに別の語で、残すのはハッシュだけ(宛先の数だけ、互いに違う)。
	if err != nil || len(h) != 2 || h["a@example.invalid"] == "" || h["a@example.invalid"] == h["b@example.invalid"] {
		t.Errorf("宛先ごとの確認語のハッシュ: %v %v", h, err)
	}
}

func TestContractTimersRegister(t *testing.T) {
	tm := &Timers{Dir: t.TempDir()}
	err := tm.Register(ctx, []contract.TimerUnit{{Words: []string{"backup"}, Period: "fortnightly"}})
	harness.Implemented(t, err)
	if err == nil {
		t.Error("周期が hourly/daily/weekly/monthly 以外のタイマーが登録できた")
	}
	if err := tm.Register(ctx, []contract.TimerUnit{{Words: []string{"backup"}, Period: "daily"}, {Words: []string{"extend"}, Period: "weekly"}}); err != nil {
		t.Fatalf("正しい単位の登録: %v", err)
	}
	if es, _ := os.ReadDir(tm.Dir); len(es) == 0 {
		t.Error("登録したタイマーの定義が Dir に出ていない")
	}
}

func TestContractTimersSetOuterLimit(t *testing.T) {
	tm := &Timers{Dir: t.TempDir()}
	err := tm.SetOuterLimit(ctx, -time.Second)
	harness.Implemented(t, err)
	if err == nil {
		t.Error("負の上限が成功した")
	}
	if err := tm.SetOuterLimit(ctx, 8*time.Hour); err != nil {
		t.Errorf("正の上限: %v", err)
	}
}
