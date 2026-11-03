package cli

// spec 3.1.3 の NOT_SEALED の補足(D-0028 の n2): 導入中(seal の前)で前提(source enroll・restore-dest enroll の完了)が欠けている間は、
// タイマーの backup・restore-test を飛ばし、失敗にも通知にもしない。seal の前も取得は止めない(D-0025 の 5)ので、前提がそろえば走る。手で起動したものは飛ばさない。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

func setItem(t *testing.T, f *flowEnv, item string, v string) {
	t.Helper()
	h, err := f.d.State.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	if err := f.d.State.Update(context.Background(), h, item, json.RawMessage(v)); err != nil {
		t.Fatal(err)
	}
}

func timerBackup(t *testing.T, setup func(*flowEnv), timer bool) (called int, res map[string]any, err error) {
	t.Helper()
	f := newFlow(t)
	setup(f)
	f.d.Acquire = &fake.Acquire{Fn: map[string]func() (contract.Reply, error){
		"Selftest": func() (contract.Reply, error) { called++; return contract.Reply{}, nil },
		"Run": func() (contract.Reply, error) {
			called++
			return contract.Reply{Result: json.RawMessage(`{"targets":[]}`)}, nil
		},
	}}
	c := contract.Call{Words: []string{"backup"}, RunID: runA, Args: map[string][]string{}}
	if timer {
		c.Args["invoked-by"] = []string{"timer"}
	}
	rep, err := backupEntry(f.d, context.Background(), c)
	_ = json.Unmarshal(rep.Result, &res)
	return called, res, err
}

const hostKeysComplete = `[{"role":"source","host":"h","host_key_fp":"SHA256:a","session_id":null},{"role":"restore_dest","host":"d","host_key_fp":"SHA256:b","session_id":null}]`

func TestTimerBackupSkippedBeforeSealWhileSetupIncomplete(t *testing.T) {
	called, res, err := timerBackup(t, func(f *flowEnv) { setItem(t, f, "sealed", "false") }, true)
	if err != nil || called != 0 || res["skipped"] != true {
		t.Fatalf("前提が欠けるタイマーの backup は飛ばすはず: called=%d res=%v err=%v", called, res, err)
	}
	// restore_dest の登録の記録が無い(host_keys だけ)間も飛ばす。
	called, _, err = timerBackup(t, func(f *flowEnv) { setItem(t, f, "host_keys", hostKeysComplete) }, true)
	if err != nil || called != 0 {
		t.Fatalf("登録の記録が欠けるのに走った: called=%d err=%v", called, err)
	}
}

func TestTimerBackupRunsWhenSetupCompleteOrSealedOrManual(t *testing.T) {
	complete := func(f *flowEnv) {
		setItem(t, f, "host_keys", hostKeysComplete)
		setItem(t, f, "restore_dest_enrollment", `{"egress":["192.0.2.1"]}`)
	}
	if called, _, err := timerBackup(t, complete, true); err != nil || called == 0 {
		t.Errorf("前提がそろえば走るはず: called=%d err=%v", called, err)
	}
	if called, _, err := timerBackup(t, func(f *flowEnv) { setItem(t, f, "sealed", "true") }, true); err != nil || called == 0 {
		t.Errorf("seal の後は前提を見ずに走るはず: called=%d err=%v", called, err)
	}
	if called, _, err := timerBackup(t, func(f *flowEnv) { setItem(t, f, "sealed", "false") }, false); err != nil || called == 0 {
		t.Errorf("手で起動した backup は飛ばさないはず: called=%d err=%v", called, err)
	}
}

func TestTimerRestoreTestSkippedWhileSetupIncomplete(t *testing.T) {
	d := DefaultDeps(t.TempDir())
	rep, err := d.Restore.RestoreTest(context.Background(), contract.Call{Words: []string{"restore-test"}, Args: map[string][]string{"invoked-by": {"timer"}}})
	var res map[string]any
	_ = json.Unmarshal(rep.Result, &res)
	if err != nil || res["skipped"] != true {
		t.Fatalf("タイマーの restore-test は前提が欠けると飛ばすはず: %v %v", res, err)
	}
}
