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
