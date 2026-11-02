package approval

// cancel(spec 3.1.2.11「cancel と適用の開始」、4.6 の取り消し)。取り消せるのは最後の事象が proposed・approved の変更だけ。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Cancel は contract.Approval を満たす。
func (a *Approval) Cancel(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := a.ready(); err != nil {
		return contract.Reply{}, err
	}
	id := one(c, "id")
	if id == "" {
		return contract.Reply{}, argErr("--id", "missing")
	}
	s := a.store()
	evs, _, ce, err := s.events()
	if err != nil || ce != nil {
		return contract.Reply{}, chainOrErr(ce, err)
	}
	_, states := replay(evs)
	st := states[id]
	if st == nil {
		return contract.Reply{}, stateErr(id, nil)
	}
	now, err := a.now(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	switch {
	case st.Last == evCancelled:
		// 取り消し済みは変化なし(取り消した時刻は journal の項目の at にあり、ここでは今の時刻を返す)。
	case st.Last != evProposed && st.Last != evApproved:
		return contract.Reply{}, stateErr(id, &st.Last) // applying の後は止めない
	case !c.DryRun:
		if err := a.appendCancel(ctx, s, id); err != nil {
			return contract.Reply{}, err
		}
	}
	b, err := json.Marshal(map[string]string{"cancelled_at": fmtTime(now)})
	return contract.Reply{Result: b}, err
}

// appendCancel は state.lock の下で cancelled を追記する(apply の applying の追記と同じ排他。遷移の検査が競合を防ぐ)。
func (a *Approval) appendCancel(ctx context.Context, s *pstore, id string) error {
	h, err := a.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	ev, err := s.appendEvent(logEvent{Event: evCancelled, ChangeID: id})
	if err != nil {
		return err
	}
	if err := a.dropDelay(ctx, h, id); err != nil {
		return err
	}
	return a.record(ctx, h, ev, nil)
}
