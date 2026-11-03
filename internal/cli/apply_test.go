package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/state"
)

// applyStub は approval の偽物: 操作の列を台本で返し、cli が記録させた結果を取っておく。
type applyStub struct {
	fake.Approval
	changes []contract.ChangeOps
	waiting []json.RawMessage
	pending string // Pending の結果
	rec     []recorded
	enroll  contract.StepResult
	// instance は ApplyPlan が受けた実行の個体の識別子。
	instance string
}

type recorded struct {
	id  string
	pos int
	res contract.StepResult
}

func (a *applyStub) ApplyPlan(_ context.Context, _ contract.Call, instance string) (contract.ApplyPlan, error) {
	a.instance = instance
	return contract.ApplyPlan{Changes: a.changes, Waiting: a.waiting}, nil
}
func (a *applyStub) ApplyEnroll(context.Context, string, int) (contract.StepResult, error) {
	return a.enroll, nil
}
func (a *applyStub) ApplyRecord(_ context.Context, id string, pos int, r contract.StepResult) error {
	a.rec = append(a.rec, recorded{id, pos, r})
	return nil
}
func (a *applyStub) Pending(context.Context, contract.Call) (contract.Reply, error) {
	return contract.Reply{Result: json.RawMessage(a.pending)}, nil
}

const applyDef = `{"version":1,"source":{"host":"app01.example.internal","targets":[{"id":"t1","files":"/srv/app/data"}]},` +
	`"storage":[{"endpoint":"https://s3.example-provider.com","bucket":"example-sitate","key_ref":"storage"}],` +
	`"restore":{"dest":{"at":"ssh://restore@203.0.113.10:22","key_ref":"restore-init"}},"notify":{"to":["ops@example.jp"]}}`

func applyEnv(t *testing.T, a *applyStub) (*flowEnv, string) {
	t.Helper()
	f := newFlow(t)
	f.d.Approval = a
	f.d.DefFile = filepath.Join(t.TempDir(), "sitate.yaml")
	if err := os.WriteFile(filepath.Join(f.d.StateDir, "state.json"), []byte(`{"initialized":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return f, f.d.DefFile
}

func runApply(t *testing.T, f *flowEnv, c contract.Call) (applyOut, error) {
	t.Helper()
	rep, err := applyEntry(f.d, context.Background(), c)
	var out applyOut
	if err == nil {
		if e := json.Unmarshal(rep.Result, &out); e != nil {
			t.Fatal(e)
		}
	}
	return out, err
}

func sealChange(def string) contract.ChangeOps {
	args, _ := json.Marshal(map[string]json.RawMessage{"definition": json.RawMessage(def)})
	return contract.ChangeOps{ChangeID: "c-1", Op: "seal", Steps: []contract.OpStep{{Kind: "define", Entry: "define", Args: args}}}
}

func TestApplySealDefineSwitchesAndWritesDefFile(t *testing.T) {
	a := &applyStub{}
	f, defFile := applyEnv(t, a)
	a.changes = []contract.ChangeOps{sealChange(applyDef)}
	out, err := runApply(t, f, contract.Call{RunID: runA})
	if err != nil || len(out.Applied) != 1 || out.Applied[0]["op"] != "seal" || out.Applied[0]["state"] != "applied" {
		t.Fatalf("%+v %v", out, err)
	}
	if len(a.rec) != 1 || a.rec[0].pos != 0 || a.rec[0].res.Err != nil {
		t.Fatalf("記録: %+v", a.rec)
	}
	norm, _, err := f.d.State.ReadDef(context.Background())
	if err != nil || !strings.Contains(string(norm), "app01.example.internal") {
		t.Fatalf("承認済みの定義: %s %v", norm, err)
	}
	if sealed, _ := f.d.State.Get(context.Background(), "sealed"); string(sealed) != "true" {
		t.Errorf("sealed: %s", sealed)
	}
	raw, err := os.ReadFile(defFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, issues, _ := f.d.State.ValidateDef(context.Background(), raw, true); string(issues) != "[]" {
		t.Errorf("書き戻した定義ファイルが通らない: %s", issues)
	}
	// 冪等: 同じ列をもう一度実行しても成功し、定義ファイルは変わらない。
	before, _ := os.Stat(defFile)
	if out, err = runApply(t, f, contract.Call{RunID: runA}); err != nil || len(out.Applied) != 1 {
		t.Fatalf("再実行: %+v %v", out, err)
	}
	after, _ := os.Stat(defFile)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("一致する定義ファイルを書き直した")
	}
}

func TestApplyDefFileWriteFailureKeepsApplying(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	f.d.DefFile = filepath.Join(t.TempDir(), "no-such-dir", "sitate.yaml")
	a.changes = []contract.ChangeOps{sealChange(applyDef)}
	out, err := runApply(t, f, contract.Call{RunID: runA})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.rec) != 1 || a.rec[0].res.Err == nil || a.rec[0].res.Err.Code != contract.CodeDefFileWriteFailed {
		t.Fatalf("記録: %+v", a.rec)
	}
	if len(out.Applied) != 0 || len(out.Failed) != 0 {
		t.Errorf("applying のまま残る変更は applied にも failed にも載せない: %+v", out)
	}
	// 置き換えは済んでいる(書き戻しの前に切り替える順)。
	if norm, _, e := f.d.State.ReadDef(context.Background()); e != nil || len(norm) == 0 {
		t.Errorf("承認済みの定義: %v", e)
	}
}

func TestApplyRestoreChangeEnrollThenDefineUpdatesHostKeys(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	ctx := context.Background()
	if err := f.d.State.SwitchDef(ctx, contract.SwitchInput{Normalized: mustNorm(t, f, applyDef)}); err != nil {
		t.Fatal(err)
	}
	section := `{"adapter":"ssh-host","dest":{"at":"ssh://restore@203.0.113.20:22","key_ref":"restore-init"},"host_key_fp":"SHA256:newfp"}`
	args := mustMarshal(map[string]json.RawMessage{"restore": json.RawMessage(section)})
	a.enroll = contract.StepResult{Result: mustMarshal(contract.EnrollResult{ReceiverHash: "sha256:" + strings.Repeat("a", 64)})}
	a.changes = []contract.ChangeOps{{ChangeID: "c-2", Op: "restore-change", Steps: []contract.OpStep{
		{Kind: "enroll", Entry: "restore-dest enroll", Args: args}, {Kind: "define", Entry: "define", Args: args}}}}
	out, err := runApply(t, f, contract.Call{RunID: runA})
	if err != nil || len(out.Applied) != 1 || len(a.rec) != 2 || a.rec[0].pos != 0 || a.rec[1].pos != 1 {
		t.Fatalf("%+v %+v %v", out, a.rec, err)
	}
	hk, _ := f.d.State.Get(ctx, "host_keys")
	if !strings.Contains(string(hk), "SHA256:newfp") || !strings.Contains(string(hk), "restore_dest") || !strings.Contains(string(hk), `"host":"203.0.113.20"`) {
		t.Errorf("host_keys: %s", hk)
	}
	norm, _, _ := f.d.State.ReadDef(ctx)
	if !strings.Contains(string(norm), "203.0.113.20") || strings.Contains(string(norm), "host_key_fp") {
		t.Errorf("節の置き換え(host_key_fp は定義に入れない): %s", norm)
	}
}

func mustNorm(t *testing.T, f *flowEnv, def string) []byte {
	t.Helper()
	n, _, issues, err := f.d.State.ValidateDef(context.Background(), []byte(def), true)
	if err != nil || string(issues) != "[]" {
		t.Fatalf("%s %v", issues, err)
	}
	return n
}

func TestApplyStepFailureIsRecordedAndListedAsFailed(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	a.changes = []contract.ChangeOps{{ChangeID: "c-3", Op: "storage-init", Steps: []contract.OpStep{{Kind: "run", Entry: "init-storage", Args: json.RawMessage(`{}`)}}}}
	out, err := runApply(t, f, contract.Call{RunID: runA}) // Keys が無い: NOT_IMPLEMENTED
	if err != nil || len(out.Failed) != 1 || !strings.Contains(string(out.Failed[0]), `"code":"NOT_IMPLEMENTED"`) {
		t.Fatalf("%+v %v", out, err)
	}
	if len(a.rec) != 1 || a.rec[0].res.Err == nil {
		t.Fatalf("%+v", a.rec)
	}
}

type argKeys struct {
	fake.Keys
	got contract.Call
}

func (k *argKeys) InitStorage(_ context.Context, c contract.Call) (contract.Reply, error) {
	k.got = c
	return contract.Reply{Result: json.RawMessage(`{"ok":1}`)}, nil
}

func TestApplyRunStepPassesArgsAndApplyCallSecrets(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	k := &argKeys{}
	f.d.Keys = k
	args := json.RawMessage(`{"endpoint":"https://s3.example-provider.com","key_ref":"k1","reissue":true,"revoke_key_ids":["a","b"]}`)
	a.changes = []contract.ChangeOps{{ChangeID: "c-4", Op: "storage-init", Steps: []contract.OpStep{{Kind: "run", Entry: "init-storage", Args: args}}}}
	out, err := runApply(t, f, contract.Call{RunID: runA, Args: map[string][]string{"admin-ref": {"admin"}}})
	if err != nil || len(out.Applied) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	g := k.got.Args
	if g["endpoint"][0] != "https://s3.example-provider.com" || g["key-ref"][0] != "k1" || g["reissue"][0] != "true" || len(g["revoke-key-ids"]) != 2 || g["admin-ref"][0] != "admin" {
		t.Errorf("引数: %+v", g)
	}
	if k.got.RunID != runA || strings.Join(k.got.Words, " ") != "init-storage" {
		t.Errorf("%+v", k.got)
	}
}

func TestApplyDryRunExecutesNothing(t *testing.T) {
	a := &applyStub{}
	f, defFile := applyEnv(t, a)
	a.changes = []contract.ChangeOps{sealChange(applyDef)}
	out, err := runApply(t, f, contract.Call{RunID: runA, DryRun: true})
	if err != nil || len(out.Applied) != 0 || len(a.rec) != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	if _, e := os.Stat(defFile); e == nil {
		t.Error("dry-run が定義ファイルを書いた")
	}
}

func TestApplyAbortedContextEndsWithRunAbortedAndKeepsApplying(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	a.changes = []contract.ChangeOps{sealChange(applyDef)}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errSignal)
	_, err := applyEntry(f.d, ctx, contract.Call{RunID: runA})
	ce, _ := err.(*contract.Error)
	if ce == nil || ce.Code != contract.CodeRunAborted || !strings.Contains(string(ce.Detail), "signal") {
		t.Fatalf("%v", err)
	}
	if len(a.rec) != 1 || a.rec[0].res.Err == nil || a.rec[0].res.Err.Code != contract.CodeRunAborted {
		t.Fatalf("記録: %+v", a.rec)
	}
}

func seedHold(t *testing.T, f *flowEnv, causes string) {
	t.Helper()
	h, err := f.d.State.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	v := `{"active":true,"since":"2026-11-01T00:00:00Z","auto":true,"reason":"check_failed","causes":` + causes + `,"suppressed":[]}`
	if err := f.d.State.Update(context.Background(), h, "hold", json.RawMessage(v)); err != nil {
		t.Fatal(err)
	}
}

func holdOfState(t *testing.T, f *flowEnv) (active bool, causes, suppressed []map[string]any) {
	t.Helper()
	raw, _ := f.d.State.Get(context.Background(), "hold")
	var h struct {
		Active     bool             `json:"active"`
		Causes     []map[string]any `json:"causes"`
		Suppressed []map[string]any `json:"suppressed"`
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}
	return h.Active, h.Causes, h.Suppressed
}

const causeCheck = `{"reason":"check_failed","code":"CHECK_FAILED","at":"2026-11-01T00:00:00Z","days":null,"refs":[]}`
const causeNoHealthy = `{"reason":"no_healthy_backup","code":null,"at":"2026-11-02T00:00:00Z","days":14,"refs":[]}`

func holdReleaseChange() contract.ChangeOps {
	return contract.ChangeOps{ChangeID: "c-5", Op: "hold-release", Steps: []contract.OpStep{{Kind: "run", Entry: "hold-release", Args: json.RawMessage(`{}`)}}}
}

func TestApplyHoldReleaseMovesOnlyPlannedCausesAndKeepsHoldWhenOthersRemain(t *testing.T) {
	a := &applyStub{pending: `{"changes":[{"plan":{"causes":[` + causeCheck + `]}}]}`}
	f, _ := applyEnv(t, a)
	seedHold(t, f, `[`+causeCheck+`,`+causeNoHealthy+`]`) // 承認の後に no_healthy_backup が足された
	a.changes = []contract.ChangeOps{holdReleaseChange()}
	out, err := runApply(t, f, contract.Call{RunID: runA})
	if err != nil || len(out.Applied) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	active, causes, sup := holdOfState(t, f)
	if !active || len(causes) != 1 || causes[0]["reason"] != "no_healthy_backup" || len(sup) != 1 || sup[0]["reason"] != "check_failed" || sup[0]["code"] != "CHECK_FAILED" {
		t.Fatalf("active=%v causes=%v suppressed=%v", active, causes, sup)
	}
	for _, n := range f.rec.Notifications() {
		if n["type"] == "hold.set" {
			t.Error("hold-release が hold.set を送った")
		}
	}
}

func TestApplyHoldReleaseReleasesWhenAllCausesPlanned(t *testing.T) {
	a := &applyStub{pending: `{"changes":[{"plan":{"causes":[` + causeCheck + `]}}]}`}
	f, _ := applyEnv(t, a)
	seedHold(t, f, `[`+causeCheck+`]`)
	a.changes = []contract.ChangeOps{holdReleaseChange()}
	if _, err := runApply(t, f, contract.Call{RunID: runA}); err != nil {
		t.Fatal(err)
	}
	active, causes, sup := holdOfState(t, f)
	if active || len(causes) != 0 || len(sup) != 1 {
		t.Fatalf("active=%v causes=%v suppressed=%v", active, causes, sup)
	}
}

func TestStoreClockUsesStoreTimePlusOffset(t *testing.T) {
	store, _, core := fake.NewTestStore(fake.RoleWrite)
	_ = core
	d := &Deps{Object: store, Clock: &fake.Clock{T: fake.StartTime}}
	got, err := NewApplyClock(d).Now(context.Background())
	if err != nil || got.IsZero() {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := NewApplyClock(&Deps{}).Now(context.Background()); err == nil {
		t.Error("保管先が無いのに時刻を返した")
	}
}

type timersStub struct{ regs, limits int }

func (s *timersStub) Register(context.Context, []contract.TimerUnit) error {
	s.regs++
	return nil
}
func (s *timersStub) SetOuterLimit(context.Context, time.Duration) error { s.limits++; return nil }

// H2 (d): 定義の切り替えのあと、定義の schedule・timezone でタイマーを登録し直す。
func TestApplyDefineReregistersTimers(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	ts := &timersStub{}
	f.d.Timers = ts
	a.changes = []contract.ChangeOps{sealChange(applyDef)}
	if _, err := runApply(t, f, contract.Call{RunID: runA}); err != nil {
		t.Fatal(err)
	}
	if ts.regs != 1 || ts.limits != 1 {
		t.Errorf("register=%d limit=%d", ts.regs, ts.limits)
	}
}

// 完了条件 18: 承認済みの定義の置き換えの後・定義ファイルの書き戻しの前で止まった適用の再実行は、define を冪等に再実行して定義ファイルを書き戻し、applied にする。
func TestApplyResumeAfterSwitchBeforeDefFileWrite(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	st, _, _ := fake.NewTestStore(fake.RoleWrite)
	f.d.Object, f.d.Codec, f.d.KeyS = st, f.rec.Codec, fake.KeyService{}
	good := f.d.DefFile
	f.d.DefFile = filepath.Join(t.TempDir(), "no-such-dir", "sitate.yaml") // 書き戻しで止まる(SIGKILL の代わり)
	a.changes = []contract.ChangeOps{sealChange(applyDef)}
	if _, err := runApply(t, f, contract.Call{RunID: runA}); err != nil {
		t.Fatal(err)
	}
	f.d.DefFile = good
	out, err := runApply(t, f, contract.Call{RunID: runA})
	if err != nil || len(out.Applied) != 1 {
		t.Fatalf("再実行で applied: %+v %v", out, err)
	}
	if b, e := os.ReadFile(good); e != nil || !strings.Contains(string(b), "app01.example.internal") {
		t.Errorf("定義ファイルを書き戻していない: %v", e)
	}
	objs, _ := state.ReadObjects(context.Background(), f.d.State)
	n := 0
	for _, o := range objs {
		if strings.HasPrefix(o.Key, "config/") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("config/ の再書き込み・二重の記録(冪等): %d", n)
	}
}

// define の前で止まった(保管先に書けず置き換えに進めなかった)適用の再実行は、define から行って applied にする。
func TestApplyResumeBeforeDefine(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	st, _, _ := fake.NewTestStore(fake.RoleWrite)
	failing := true
	f.d.Object, f.d.Codec, f.d.KeyS = &flakyStore{ObjectStore: st, fail: &failing}, f.rec.Codec, fake.KeyService{}
	a.changes = []contract.ChangeOps{sealChange(applyDef)}
	if _, err := runApply(t, f, contract.Call{RunID: runA}); err != nil {
		t.Fatal(err)
	}
	if _, _, e := f.d.State.ReadDef(context.Background()); e == nil {
		t.Fatal("config/ を書けないのに定義を置き換えた(書く順: config/ → 置き換え)")
	}
	failing = false
	out, err := runApply(t, f, contract.Call{RunID: runA})
	if err != nil || len(out.Applied) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
}
