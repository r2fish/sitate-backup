package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

func backupFlow(t *testing.T, run string, selftestErr error) (*flowEnv, outcome) {
	t.Helper()
	f := newFlow(t)
	f.d.Acquire = &fake.Acquire{Fn: map[string]func() (contract.Reply, error){
		"Selftest": func() (contract.Reply, error) { return contract.Reply{}, selftestErr },
		"Run":      func() (contract.Reply, error) { return contract.Reply{Result: json.RawMessage(run)}, nil },
	}}
	return f, f.run(t, []string{"backup"}, backupEntry)
}

func runEndTargets(t *testing.T, f *flowEnv) []map[string]any {
	t.Helper()
	e := f.ends(t, "run_end")
	if len(e) != 1 {
		t.Fatalf("run_end: %d", len(e))
	}
	var b struct{ Targets []map[string]any }
	if err := json.Unmarshal(e[0].Body, &b); err != nil {
		t.Fatal(err)
	}
	return b.Targets
}

// 完了条件 17: 対象ごとの errors[]・warnings[] は run_end に載り(各 ACQUIRE_ERRORS_MAX まで)、結果には件数だけが出る。path は出ない。
func TestBackupRunEndTargetsRecordsErrors(t *testing.T) {
	var errs []string
	for i := 0; i < contract.AcquireErrorsMax+3; i++ {
		errs = append(errs, `{"code":"PERMISSION","item_ref":"i-1","path_b64":"c2VjcmV0"}`)
	}
	run := `{"snapshot_ref":"m-` + strings.Repeat("a", 32) + `","targets":[{"id":"t1","status":"partial","bytes":1,"files":1,"changed_during_read":0,"errors":[` +
		strings.Join(errs, ",") + `],"warnings":[{"code":"NOT_FOUND","item_ref":null}],"errors_omitted":2,"warnings_omitted":0,"error_code":null}],"checks":[]}`
	f, o := backupFlow(t, run, nil)
	if o.err != nil {
		t.Fatal(o.err)
	}
	tg := runEndTargets(t, f)
	if len(tg) != 1 || len(tg[0]["errors"].([]any)) != contract.AcquireErrorsMax || len(tg[0]["warnings"].([]any)) != 1 {
		t.Fatalf("run_end targets: %v", tg)
	}
	env, _ := o.render()
	s := string(env)
	if strings.Contains(s, "path_b64") || strings.Contains(s, "c2VjcmV0") || strings.Contains(s, `"errors":[`) ||
		!strings.Contains(s, `"errors_omitted":5`) || !strings.Contains(s, `"warnings_omitted":0`) {
		t.Fatalf("結果: %s", s)
	}
	if strings.Contains(string(f.ends(t, "run_end")[0].Body), "path_b64") {
		t.Fatal("run_end に path_b64")
	}
}

// 完了条件 19: 一部の対象の失敗は backup.partial、全部の失敗は ALL_TARGETS_FAILED。どちらも run_end の targets[] に error_code。
func TestBackupPartialAndAllFailed(t *testing.T) {
	ok := `{"id":"t1","status":"ok","error_code":null}`
	bad := `{"id":"t2","status":"failed","error_code":"STORAGE_UNREACHABLE"}`
	f, o := backupFlow(t, `{"snapshot_ref":null,"targets":[`+ok+`,`+bad+`],"checks":[]}`, nil)
	if o.err != nil || len(f.ntf.Calls()) == 0 {
		t.Fatalf("err=%v sent=%d", o.err, len(f.ntf.Calls()))
	}
	if tg := runEndTargets(t, f); tg[1]["error_code"] != "STORAGE_UNREACHABLE" || tg[0]["error_code"] != nil {
		t.Fatalf("%v", tg)
	}
	env, _ := o.render()
	if !strings.Contains(string(env), `"error_code":"STORAGE_UNREACHABLE"`) {
		t.Fatalf("結果の error_code: %s", env)
	}
	f2, o2 := backupFlow(t, `{"snapshot_ref":null,"targets":[`+bad+`],"checks":[]}`, nil)
	if o2.err == nil || o2.err.Code != contract.CodeAllTargetsFailed {
		t.Fatalf("err=%v", o2.err)
	}
	if tg := runEndTargets(t, f2); tg[0]["error_code"] != "STORAGE_UNREACHABLE" {
		t.Fatalf("%v", tg)
	}
	// 自己診断で届かなければ実行の STORAGE_UNREACHABLE(取得に入らない)
	_, o3 := backupFlow(t, `{}`, contract.ErrNotImplemented("x"))
	if o3.err == nil {
		t.Fatal("自己診断の失敗が伝わらない")
	}
}

type emitted struct {
	typ   string
	facts string
}

// spyRecorder は Emit の呼び出し(型と事実)を記録して本物へ渡す。
type spyRecorder struct {
	contract.Recorder
	got *[]emitted
}

func (s spyRecorder) Emit(ctx context.Context, typ string, facts []byte) (contract.Event, []contract.DeliveryResult, error) {
	*s.got = append(*s.got, emitted{typ, string(facts)})
	return s.Recorder.Emit(ctx, typ, facts)
}

func backupFlowSpy(t *testing.T, run string, selftestErr error) (*[]emitted, outcome) {
	t.Helper()
	f := newFlow(t)
	var got []emitted
	f.d.Recorder = spyRecorder{f.d.Recorder, &got}
	f.d.Acquire = &fake.Acquire{Fn: map[string]func() (contract.Reply, error){
		"Selftest": func() (contract.Reply, error) { return contract.Reply{}, selftestErr },
		"Run":      func() (contract.Reply, error) { return contract.Reply{Result: json.RawMessage(run)}, nil },
	}}
	return &got, f.run(t, []string{"backup"}, backupEntry)
}

func emittedOf(got *[]emitted, typ string) []string {
	var out []string
	for _, e := range *got {
		if e.typ == typ {
			out = append(out, e.facts)
		}
	}
	return out
}

// 規則(spec 4.8): 実行の失敗はすべて backup.failed で知らせる(自己診断の STORAGE_UNREACHABLE も)。ALL_TARGETS_FAILED は 1 回だけ。
func TestBackupFailedOnEveryRunFailure(t *testing.T) {
	got, o := backupFlowSpy(t, `{}`, newErr(contract.CodeStorageUnreachable, map[string]any{"target": "source"}))
	if o.err == nil || o.err.Code != contract.CodeStorageUnreachable {
		t.Fatalf("err=%v", o.err)
	}
	n := emittedOf(got, "backup.failed")
	if len(n) != 1 || !strings.Contains(n[0], `"error_code":"STORAGE_UNREACHABLE"`) {
		t.Errorf("backup.failed: %v", n)
	}
	bad := `{"id":"t2","status":"failed","error_code":"STORAGE_UNREACHABLE"}`
	got2, o2 := backupFlowSpy(t, `{"snapshot_ref":null,"targets":[`+bad+`],"checks":[]}`, nil)
	if o2.err == nil || len(emittedOf(got2, "backup.failed")) != 1 {
		t.Errorf("ALL_TARGETS_FAILED は 1 回: %v", emittedOf(got2, "backup.failed"))
	}
}

// 飛ばした世代があれば全対象 ok でも backup.partial(reason: index_rebuild、件数つき)。型付きの配列も数える。
func TestBackupPartialOnSkippedAndLenOf(t *testing.T) {
	ok := `{"id":"t1","status":"ok","error_code":null}`
	got, o := backupFlowSpy(t, `{"snapshot_ref":null,"skipped_backups":[{"ref":"m-x","reason":"tag"}],"targets":[`+ok+`],"checks":[]}`, nil)
	if o.err != nil {
		t.Fatal(o.err)
	}
	n := emittedOf(got, "backup.partial")
	if len(n) != 1 {
		t.Fatalf("backup.partial: %v", n)
	}
	s := n[0]
	if !strings.Contains(s, `"index_rebuild"`) || !strings.Contains(s, `"skipped_backups_count":1`) {
		t.Errorf("%s", s)
	}
	if lenOf([]SkippedRef{{}, {}}) != 2 || lenOf(nil) != 0 {
		t.Error("lenOf")
	}
}
