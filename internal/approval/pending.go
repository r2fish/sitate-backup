package approval

// pending(保留中の変更の一覧)と PendingList。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// views は変更の記録に state と not_before を足した change_view の並び(変更の最初の出現の順)。onlyID が空なら終わっていない変更(proposed・approved・applying)だけ。
func views(ctx context.Context, s *pstore, state contract.StateFiles, onlyID string) ([]json.RawMessage, error) {
	evs, _, ce, err := s.events()
	if err != nil || ce != nil {
		return nil, chainOrErr(ce, err)
	}
	order, states := replay(evs)
	var ds []delayRec
	if raw, gerr := state.Get(ctx, "delays"); gerr == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &ds)
	}
	out := []json.RawMessage{}
	for _, id := range order {
		st := states[id]
		if onlyID != "" && id != onlyID || onlyID == "" && st.Last != evProposed && st.Last != evApproved && st.Last != evApplying {
			continue
		}
		v, err := view(s, st, ds)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func view(s *pstore, st *chgState, ds []delayRec) (json.RawMessage, error) {
	rec, raw, err := s.readChange(st.ID)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["state"] = mustJSON(st.Last)
	m["not_before"] = json.RawMessage(`null`)
	if ap, ok, aerr := s.readApproval(st.ID); aerr == nil && ok {
		if nb, nerr := notBefore(ap, rec.DelayHours, findDelay(ds, st.ID)); nerr == nil {
			m["not_before"] = mustJSON(fmtTime(nb))
		}
	}
	return json.Marshal(m)
}

// Pending は contract.Approval を満たす。--id はその変更を状態によらず返し、無ければ CHANGE_STATE_INVALID。
func (a *Approval) Pending(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := a.ready(); err != nil {
		return contract.Reply{}, err
	}
	id := one(c, "id")
	if _, ok := c.Args["id"]; ok && id == "" {
		return contract.Reply{}, argErr("--id", "format")
	}
	vs, err := views(ctx, a.store(), a.State, id)
	if err != nil {
		return contract.Reply{}, err
	}
	if id != "" && len(vs) == 0 {
		return contract.Reply{}, stateErr(id, nil)
	}
	b, err := json.Marshal(map[string]any{"changes": vs})
	return contract.Reply{Result: b}, err
}

// PendingList は contract.PendingList の実装(保留中の変更の一覧。Dir は Approval と同じ状態ディレクトリ)。
type PendingList struct {
	State contract.StateFiles
	Dir   string
}

var _ contract.PendingList = (*PendingList)(nil)

// List は終わっていない変更の change_view を返す。
func (p *PendingList) List(ctx context.Context) ([]json.RawMessage, error) {
	if p == nil || p.State == nil || p.Dir == "" {
		return nil, internalErr()
	}
	a := &Approval{State: p.State, Dir: p.Dir}
	return views(ctx, a.store(), p.State, "")
}
