package state

import (
	"context"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

type candState struct {
	*fake.StateFiles
	cand []byte
}

func (c candState) ReadCandidate(context.Context) ([]byte, string, bool, error) {
	return c.cand, "", len(c.cand) > 0, nil
}

type toNotifier struct {
	*fake.Notifier
	to []string
}

func (n *toNotifier) Deliver(_ context.Context, _ contract.Event, to []string) ([]contract.DeliveryResult, error) {
	n.to = to
	return nil, nil
}

// seal の前(承認済みの定義が無い)でも、seal の提案・承認の通知は候補の定義の notify.to に届く
// (これが無いと change.proposed・change.approved がどこにも送られず、seal の遅延中の配送が成り立たない。主経路の第 3 段で見つかった)。
func TestEmitUsesCandidateRecipientsBeforeSeal(t *testing.T) {
	n := &toNotifier{Notifier: &fake.Notifier{}}
	r := newRecorder()
	r.State, r.Notifier = candState{&fake.StateFiles{}, []byte(`{"notify":{"to":["ops-hook"]}}`)}, n
	if _, _, err := r.Emit(ctx, "change.proposed", []byte(`{"change_id":"c-20261103-0001","op":"seal","not_before":null,"expired_reason":null}`)); err != nil {
		t.Fatal(err)
	}
	if len(n.to) != 1 || n.to[0] != "ops-hook" {
		t.Fatalf("候補の宛先に送られない: %v", n.to)
	}
}

// M5: 宛先を替える notify-change の change.applied は、変更前の宛先にも届く(spec 3.6 の apply の行)。EmitTo は今の宛先に extra を足した和集合へ送る。
func TestEmitToAddsPreviousRecipients(t *testing.T) {
	n := &toNotifier{Notifier: &fake.Notifier{}}
	r := newRecorder()
	r.State, r.Notifier = candState{&fake.StateFiles{}, []byte(`{"notify":{"to":["new-hook"]}}`)}, n
	facts := []byte(`{"change_id":"c-20261103-0001","op":"notify-change","not_before":null,"expired_reason":null}`)
	if _, _, err := r.EmitTo(ctx, "change.applied", facts, []string{"old-owner", "new-hook"}); err != nil {
		t.Fatal(err)
	}
	if len(n.to) != 2 || n.to[0] != "new-hook" || n.to[1] != "old-owner" {
		t.Fatalf("宛先 = %v(今の宛先 + 変更前の宛先、重複なし)", n.to)
	}
}

// D-0166: backup.partial は warning_codes に TARGET_EMPTY があれば critical、無ければ warn。
func TestBackupPartialSeverityWithTargetEmpty(t *testing.T) {
	for codes, want := range map[string]string{`["TARGET_EMPTY"]`: "critical", `[]`: "warn"} {
		r := newRecorder()
		r.State = candState{&fake.StateFiles{}, []byte(`{"notify":{"to":["ops-hook"]}}`)}
		ev, _, err := r.Emit(ctx, "backup.partial", []byte(`{"run_id":"r-1","error_code":null,"warning_codes":`+codes+`,"snapshot_ref":null,"targets_total":1,"targets_failed":0,"chunk_keys_missing":0,"reason":null,"skipped_backups_count":0}`))
		if err != nil {
			t.Fatal(err)
		}
		if ev.Severity != want {
			t.Errorf("warning_codes=%s: severity = %s(%s のはず)", codes, ev.Severity, want)
		}
	}
}
