package approval

// 記録の連鎖が切れたときの回復(spec 4.6)。run.lock を持つ apply から呼ぶ(propose・approve は検知だけ)。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// recoverChain は state.lock の下で連鎖を確かめ直し、なお切れていれば切れた記録を pending_discarded として journal に写し、
// log.jsonl を空から始め、適用の途中だった変更を CHAIN_BROKEN で扱い、PENDING_CHAIN_BROKEN を返す。
func (a *Approval) recoverChain(ctx context.Context, s *pstore, first *chainErr) error {
	h, err := a.State.Lock(ctx)
	if err != nil {
		return err
	}
	rerr, notes := a.recoverLocked(ctx, h, s)
	h.Release() // 通知は state.lock を放してから送る(規則 17。Emit は排他を取り得る)
	for _, n := range notes {
		a.emit(ctx, n.typ, n.facts)
	}
	return rerr
}

type note struct {
	typ   string
	facts map[string]any
}

func (a *Approval) recoverLocked(ctx context.Context, h contract.StateLockHandle, s *pstore) (error, []note) {
	_, lines, ce, err := s.events()
	if err != nil {
		return err, nil
	}
	if ce == nil {
		return nil, nil // 別の実行が先に回復した
	}
	var evs []logEvent
	for _, l := range lines {
		var ev logEvent
		if json.Unmarshal(l, &ev) == nil && ev.ChangeID != "" {
			evs = append(evs, ev)
		}
	}
	order, states := replay(evs)
	discarded, failed := []string{}, []map[string]string{}
	var notes []note
	for _, id := range order {
		switch states[id].Last {
		case evProposed, evApproved:
			discarded = append(discarded, id)
		case evApplying:
			discarded = append(discarded, id)
			op := ""
			if rec, _, rerr := s.readChange(id); rerr == nil {
				op = rec.Op
			}
			failed = append(failed, map[string]string{"change_id": id, "op": op, "code": "CHAIN_BROKEN"})
			notes = append(notes, note{"change.failed", map[string]any{"change_id": id, "op": op, "error_code": "CHAIN_BROKEN", "state": evFailed}})
		}
	}
	ls := make([]string, len(lines))
	for i, l := range lines {
		ls[i] = string(l)
	}
	entry, err := json.Marshal(map[string]any{"kind": "pending_discarded", "body": map[string]any{
		"kind": ce.kind, "seq": ce.seqValue(), "lines": ls, "discarded": discarded, "failed": failed}})
	if err != nil {
		return err, nil
	}
	if _, err := a.Recorder.AppendLocked(ctx, h, entry); err != nil {
		return err, nil // 写し終えてから初期化する
	}
	if err := s.b.write(logName, nil); err != nil {
		return err, nil
	}
	for _, id := range discarded { // 捨てた変更の delays[] の要素も消す
		if err := a.dropDelay(ctx, h, id); err != nil {
			return err, nil
		}
	}
	notes = append(notes, note{"journal.chain_broken", map[string]any{"chain": "pending"}})
	return ce.toError(discarded), notes
}
