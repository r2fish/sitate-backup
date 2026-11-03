package approval

// 遅延中の通知の配送の記録(spec 3.6「遅延中の通知の配送」、4.5 の delays[])。

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// updateDelay は delays[] のその変更の要素を state.lock の下で書き換える。要素が無ければ何もしない。
func (a *Approval) updateDelay(ctx context.Context, id string, fn func(*delayRec)) error {
	h, err := a.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	ds, err := a.readDelays(ctx)
	if err != nil {
		return err
	}
	d := findDelay(ds, id)
	if d == nil {
		return nil
	}
	fn(d)
	return a.putItem(ctx, h, "delays", ds)
}

func deliveriesOf(res []contract.DeliveryResult) []deliveryRec {
	out := make([]deliveryRec, 0, len(res))
	for _, r := range res {
		d := deliveryRec{To: r.To, Channel: r.Channel, Delivered: r.Delivered, Attempts: r.Attempts}
		if r.Reason != "" {
			reason := r.Reason
			d.Reason = &reason
		}
		if r.AcceptedAt != nil {
			t := fmtTime(*r.AcceptedAt)
			d.AcceptedAt = &t
		}
		out = append(out, d)
	}
	return out
}

// noteDeliveries は送った実行の配送の結果を delays[] に書く。approved は、人向けの宛先のどれかが最初に受理した時刻を first_accepted_at に置く(1 回だけ)。
func (a *Approval) noteDeliveries(ctx context.Context, id, typ string, res []contract.DeliveryResult, approved bool) {
	_ = a.updateDelay(ctx, id, func(d *delayRec) {
		rows := deliveriesOf(res)
		d.Deliveries = append(d.Deliveries, deliveryGroup{Type: typ, Delivery: rows})
		if !approved || d.FirstAcceptedAt != nil {
			return
		}
		var first *string
		for _, r := range rows {
			if r.Delivered && r.AcceptedAt != nil && (first == nil || *r.AcceptedAt < *first) {
				first = r.AcceptedAt
			}
		}
		d.FirstAcceptedAt = first
	})
}

// delivered は遅延中の通知の配送成功(3.6 の条件 (3)): 承認の受理から not_before までの accepted_at を持つ宛先が 1 つ以上あること。
func delivered(d *delayRec, received, notBefore string) bool {
	if d == nil {
		return false
	}
	return slices.ContainsFunc(d.Deliveries, func(g deliveryGroup) bool {
		return slices.ContainsFunc(g.Delivery, func(r deliveryRec) bool {
			return r.Delivered && r.AcceptedAt != nil && *r.AcceptedAt >= received && *r.AcceptedAt <= notBefore
		})
	})
}

// editNotifyProofs は state.json の notify の change_proofs[] を fn で書き換える(他の項目はそのまま)。
func (a *Approval) editNotifyProofs(ctx context.Context, h contract.StateLockHandle, fn func([]proofRow) []proofRow) error {
	m := map[string]json.RawMessage{}
	if raw, err := a.State.Get(ctx, "notify"); err != nil {
		return err
	} else if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
	}
	var rows []proofRow
	_ = json.Unmarshal(m["change_proofs"], &rows)
	rows = fn(rows)
	if rows == nil {
		rows = []proofRow{}
	}
	m["change_proofs"] = mustJSON(rows)
	return a.putItem(ctx, h, "notify", m)
}

// issueChangeProofs は notify-add・notify-change の提案の作成時に、足した宛先へ確認語付きの試験通知を送り、確認語のハッシュを change_proofs[] に記録する
// (確認語そのものは記録しない。3.6)。試験通知が送れなければ提案を取り消す。TestNotifier の返す値は "<word_salt>|<word_hash>"(契約の確認が要る)。
func (a *Approval) issueChangeProofs(ctx context.Context, s *pstore, rec changeRec) error {
	var plan struct {
		Required []string `json:"proof_required"`
	}
	_ = json.Unmarshal(rec.Plan, &plan)
	if len(plan.Required) == 0 {
		return nil
	}
	var hashes map[string]string
	var err error
	if tf, ok := a.TestNotifier.(interface {
		TestFor(ctx context.Context, changeID string, to []string) (map[string]string, error)
	}); ok { // 提案の change_id を facts.change_id に載せられる実装(state.TestNotifier)。契約の Test は change_id を受けない
		hashes, err = tf.TestFor(ctx, rec.ChangeID, plan.Required)
	} else {
		hashes, err = a.TestNotifier.Test(ctx, plan.Required)
	}
	var rows []proofRow
	for to, v := range hashes {
		salt, hash, ok := strings.Cut(v, "|")
		if !ok {
			err = internalErr()
			break
		}
		rows = append(rows, proofRow{ChangeID: rec.ChangeID, To: to, Salt: salt, Hash: hash})
	}
	if err == nil && len(rows) != len(plan.Required) {
		err = internalErr()
	}
	h, lerr := a.State.Lock(ctx)
	if lerr != nil {
		return lerr
	}
	defer h.Release()
	if err != nil {
		if ev, aerr := s.appendEvent(logEvent{Event: evCancelled, ChangeID: rec.ChangeID}); aerr == nil {
			_ = a.record(ctx, h, ev, nil)
		}
		return err
	}
	return a.editNotifyProofs(ctx, h, func(old []proofRow) []proofRow { return append(old, rows...) })
}

// dropChangeProofs は承認の受理で、その変更の change_proofs[] を消す。
func (a *Approval) dropChangeProofs(ctx context.Context, h contract.StateLockHandle, id string) error {
	return a.editNotifyProofs(ctx, h, func(old []proofRow) []proofRow {
		var out []proofRow
		for _, r := range old {
			if r.ChangeID != id {
				out = append(out, r)
			}
		}
		return out
	})
}
