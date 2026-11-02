package approval

// 失効、遅延中の通知と延長、期限の来た変更の開始前の照合と applying の追記(spec 3.6)。

import (
	"sort"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 遅延中の通知の間隔(3.6: 適用の 6 時間前に change.applying_soon、配送できなければ 24 時間ずつ延ばす)。
const (
	applyingSoonLead = 6 * time.Hour
	delayExtendStep  = 24
)

type dueChange struct {
	rec changeRec
	ap  approvalRec
	nb  time.Time
}

func (r *applyRun) expire(rec changeRec, reason string) error {
	h, err := r.a.State.Lock(r.ctx)
	if err != nil {
		return err
	}
	ev, err := r.s.appendEvent(logEvent{Event: evExpired, ChangeID: rec.ChangeID})
	if err == nil {
		err = r.a.dropDelay(r.ctx, h, rec.ChangeID)
	}
	if err == nil {
		err = r.a.record(r.ctx, h, ev, nil)
	}
	h.Release()
	if err == nil {
		r.a.emit(r.ctx, "change.expired", map[string]any{"change_id": rec.ChangeID, "op": rec.Op, "not_before": nil, "expired_reason": reason})
	}
	return err
}

// expireAndRemind は期限を過ぎた変更を expired にし(未承認は expires_at、承認済みは not_before + APPROVED_EXPIRY、配送できない延長が UNDELIVERED_EXPIRY に達したもの)、
// 遅延中の変更の change.applying_soon の送信と not_before の延長を行う。適用の途中(applying)の変更には掛けない。
func (r *applyRun) expireAndRemind() error {
	for _, id := range append([]string(nil), r.order...) {
		st := r.states[id]
		if st.Last != evProposed && st.Last != evApproved {
			continue
		}
		rec, _, err := r.s.readChange(id)
		if err != nil {
			return err
		}
		if err := r.expireOrRemind(rec, st.Last); err != nil {
			return err
		}
	}
	return r.reload()
}

func (r *applyRun) expireOrRemind(rec changeRec, last string) error {
	if last == evProposed {
		if exp, err := parseTime(rec.ExpiresAt); err == nil && !r.now.Before(exp) {
			return r.expire(rec, "unapproved")
		}
		return nil
	}
	if rec.Op == "attest" { // 署名済みの attest は approved で終わる(失効させない。4.6)
		return nil
	}
	ap, ok, err := r.s.readApproval(rec.ChangeID)
	if err != nil || !ok || len(ap.Approvers) == 0 {
		return err
	}
	d := findDelay(r.ds, rec.ChangeID)
	nb, err := notBefore(ap, rec.DelayHours, d)
	if err != nil {
		return nil
	}
	if !r.now.Before(nb.Add(contract.ApprovedExpiry)) {
		return r.expire(rec, "unapplied")
	}
	if rec.DelayHours == 0 || d == nil {
		return nil
	}
	return r.remind(rec, ap, d, nb)
}

// remind は 6 時間前の change.applying_soon と、not_before に達しても配送できていないときの 24 時間の延長(合計が UNDELIVERED_EXPIRY に達していれば失効)。
func (r *applyRun) remind(rec changeRec, ap approvalRec, d *delayRec, nb time.Time) error {
	received := ap.Approvers[0].ReceivedAt
	if !r.now.Before(nb) && !delivered(d, received, fmtTime(nb)) {
		if time.Duration(d.DelayExtendedHours)*time.Hour >= contract.UndeliveredExpiry {
			return r.expire(rec, "undelivered")
		}
		if err := r.a.updateDelay(r.ctx, rec.ChangeID, func(x *delayRec) { x.DelayExtendedHours += delayExtendStep }); err != nil {
			return err
		}
		nb = nb.Add(delayExtendStep * time.Hour)
		return r.sendSoon(rec, nb)
	}
	if d.ApplyingSoonSentAt == nil && !r.now.Before(nb.Add(-applyingSoonLead)) {
		return r.sendSoon(rec, nb)
	}
	return nil
}

func (r *applyRun) sendSoon(rec changeRec, nb time.Time) error {
	res := r.a.emit(r.ctx, "change.applying_soon", map[string]any{"change_id": rec.ChangeID, "op": rec.Op, "not_before": fmtTime(nb)})
	now := fmtTime(r.now)
	err := r.a.updateDelay(r.ctx, rec.ChangeID, func(x *delayRec) {
		if x.ApplyingSoonSentAt == nil {
			x.ApplyingSoonSentAt = &now
		}
	})
	r.a.noteDeliveries(r.ctx, rec.ChangeID, "change.applying_soon", res, false)
	return err
}

// beginDue は期限の来た変更を not_before の早い順(同じなら change_id の順)に扱う。DryRun は計画だけを返す(追記も通知もしない)。
func (r *applyRun) beginDue() error {
	for _, dc := range r.collectDue() {
		if r.c.DryRun {
			r.plan.Changes = append(r.plan.Changes, contract.ChangeOps{ChangeID: dc.rec.ChangeID, Op: dc.rec.Op, Steps: fullSteps(dc.rec)})
			continue
		}
		if err := r.beginOne(dc); err != nil {
			return err
		}
	}
	return nil
}

// checkOnlyID は --id で指定した変更の状態を確かめる: 署名のない(未承認の)変更は SIGNATURE_INVALID(変更は failed にしない)、
// 未知や終わった変更は CHANGE_STATE_INVALID。
func (r *applyRun) checkOnlyID() error {
	if r.onlyID == "" {
		return nil
	}
	st := r.states[r.onlyID]
	switch {
	case st == nil:
		return stateErr(r.onlyID, nil)
	case st.Last == evProposed:
		return sigErr("bad_signature", nil)
	case st.Last != evApproved && st.Last != evApplying:
		return stateErr(r.onlyID, &st.Last)
	}
	return nil
}

// collectDue は期限の来た変更(承認済みで not_before を過ぎ、遅延のあるものは配送成功を満たす)。
func (r *applyRun) collectDue() []dueChange {
	var due []dueChange
	for _, id := range r.order {
		if r.onlyID != "" && id != r.onlyID || r.states[id].Last != evApproved {
			continue
		}
		rec, _, err := r.s.readChange(id)
		if err != nil || rec.Op == "attest" {
			continue
		}
		ap, ok, err := r.s.readApproval(id)
		if err != nil || !ok || len(ap.Approvers) == 0 {
			continue
		}
		d := findDelay(r.ds, id)
		nb, err := notBefore(ap, rec.DelayHours, d)
		if err != nil {
			continue
		}
		if r.now.Before(nb) || rec.DelayHours > 0 && !delivered(d, ap.Approvers[0].ReceivedAt, fmtTime(nb)) {
			if r.onlyID != "" {
				r.wait(id, "DELAY_NOT_ELAPSED")
			}
			continue
		}
		due = append(due, dueChange{rec, ap, nb})
	}
	sort.SliceStable(due, func(i, j int) bool {
		if !due[i].nb.Equal(due[j].nb) {
			return due[i].nb.Before(due[j].nb)
		}
		return due[i].rec.ChangeID < due[j].rec.ChangeID
	})
	return due
}

func (r *applyRun) anyApplying() bool {
	for _, st := range r.states {
		if st.Last == evApplying {
			return true
		}
	}
	return false
}

// beginOne は 1 件の開始: 記録の照合と前提の再計算(applying の追記より前)→ 秘密の有無 → applying の追記。
func (r *applyRun) beginOne(dc dueChange) error {
	rec := dc.rec
	if r.anyApplying() {
		r.wait(rec.ChangeID, "applying_in_progress")
		return nil
	}
	raw, _, rerr := r.s.b.read(changeName(rec.ChangeID))
	if rerr != nil {
		return rerr
	}
	if err := r.a.verifyStored(r.ctx, raw, rec, dc.ap); err != nil {
		return r.failOnMismatch(rec, err, contract.CodeSignatureInvalid)
	}
	if reject, err := r.preconditionMismatch(rec); err != nil || reject {
		return err
	}
	if rec.Op != "seal" && !r.a.sealed(r.ctx) {
		r.unsealed = true
		return nil
	}
	if reason := r.blocked(rec); reason != "" {
		r.wait(rec.ChangeID, reason)
		return nil
	}
	return r.startApplying(rec)
}

func (r *applyRun) failOnMismatch(rec changeRec, err error, code contract.Code) error {
	var ce *contract.Error
	if asContractErr(err, &ce) && ce.Code == contract.CodeSignatureInvalid {
		return r.fail(rec, code)
	}
	return err
}

func (r *applyRun) preconditionMismatch(rec changeRec) (bool, error) {
	var args map[string]jsonRaw
	if err := unmarshalJSON(rec.Args, &args); err != nil {
		return true, r.fail(rec, contract.CodeSignatureInvalid)
	}
	pre, err := r.a.precondition(r.ctx, rec.Op, args)
	if err != nil {
		return false, err
	}
	if pre.hash != rec.PreconditionHash {
		return true, r.fail(rec, contract.CodePreconditionChanged)
	}
	return false, nil
}

func (a *Approval) sealed(ctx ctxT) bool {
	var s bool
	_, _ = a.getItem(ctx, "sealed", &s)
	return s
}

// blocked は applying の追記の前に待たせる理由(restore-change の teardown_pending[]、秘密の欠け)。
func (r *applyRun) blocked(rec changeRec) string {
	if rec.Op == "restore-change" {
		var tp []jsonRaw
		if ok, _ := r.a.getItem(r.ctx, "teardown_pending", &tp); ok && len(tp) > 0 {
			return string(contract.CodeTeardownPending)
		}
	}
	return missingSecret(rec, r.c)
}

// startApplying は state.lock の下で applying(instance 付き)を追記する。取り消されていたら(遷移の検査)追記せずその変更を飛ばす。
func (r *applyRun) startApplying(rec changeRec) error {
	h, err := r.a.State.Lock(r.ctx)
	if err != nil {
		return err
	}
	ev, err := r.s.appendEvent(logEvent{Event: evApplying, ChangeID: rec.ChangeID, Instance: r.instance})
	if err == nil {
		err = r.a.record(r.ctx, h, ev, nil)
	}
	h.Release()
	var ce *contract.Error
	if asContractErr(err, &ce) && ce.Code == contract.CodeChangeStateInvalid {
		return nil // 取り消し・失効と競合した
	}
	if err != nil {
		return err
	}
	steps := fullSteps(rec)
	r.setSteps(rec.ChangeID, steps)
	r.plan.Changes = append(r.plan.Changes, contract.ChangeOps{ChangeID: rec.ChangeID, Op: rec.Op, Steps: steps})
	return r.reload()
}
