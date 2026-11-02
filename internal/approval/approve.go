package approval

// approve(spec 3.1.2.10、3.6 の approve の行、4.6)。署名は登録済みの承認鍵の署名だけを受ける。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type approveOut struct {
	ApprovedAt string `json:"approved_at"`
	NotBefore  string `json:"not_before"`
	State      string `json:"state"`
	ProofOK    *bool  `json:"notify_proof_ok"`
}

func one(c contract.Call, key string) string {
	if v := c.Args[key]; len(v) == 1 {
		return v[0]
	}
	return ""
}

// Approve は contract.Approval を満たす。
func (a *Approval) Approve(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := a.ready(); err != nil {
		return contract.Reply{}, err
	}
	id, sig, signedAt := one(c, "id"), one(c, "signature"), one(c, "signed-at")
	for k, v := range map[string]string{"--id": id, "--signature": sig, "--signed-at": signedAt} {
		if v == "" {
			return contract.Reply{}, argErr(k, "missing")
		}
	}
	if !timeRe.MatchString(signedAt) {
		return contract.Reply{}, argErr("--signed-at", "format")
	}
	out, err := a.approve(ctx, id, sig, signedAt, one(c, "notify-proof-stdin"))
	if err != nil {
		return contract.Reply{}, err
	}
	b, err := json.Marshal(out)
	return contract.Reply{Result: b}, err
}

func (a *Approval) approve(ctx context.Context, id, sig, signedAt, proofIn string) (approveOut, error) {
	s := a.store()
	evs, _, ce, err := s.events()
	if err != nil || ce != nil {
		return approveOut{}, chainOrErr(ce, err)
	}
	_, states := replay(evs)
	st := states[id]
	if st == nil {
		return approveOut{}, stateErr(id, nil)
	}
	if st.Last == evApproved {
		return a.sameApproval(s, id, sig, signedAt)
	}
	if st.Last != evProposed {
		return approveOut{}, stateErr(id, &st.Last)
	}
	rec, raw, err := s.readChange(id)
	if err != nil {
		return approveOut{}, err
	}
	now, err := a.now(ctx)
	if err != nil {
		return approveOut{}, err
	}
	if exp, perr := parseTime(rec.ExpiresAt); perr != nil || !now.Before(exp) {
		return approveOut{}, a.expireUnapproved(ctx, s, rec)
	}
	ap, ok, err := a.verifyNew(ctx, raw, rec, sig, signedAt, now)
	if err != nil || !ok {
		return approveOut{}, err
	}
	var proof *bool
	if needsProof(rec.Op) {
		if proof, err = a.checkProofs(ctx, rec, proofIn); err != nil {
			return approveOut{}, err
		}
		ap.NotifyProofOK = proof != nil && *proof
	}
	return a.commitApproval(ctx, s, rec, ap, proof, now)
}

// verifyNew は新しい署名の検証(3.1.2.10 の 3・4): 計算し直した canonical_hash が記録の値と一致し、その値で組んだ署名の対象が登録済みの承認鍵で検証でき、
// signed_at と受理時刻の差が許容内であること。
func (a *Approval) verifyNew(ctx context.Context, raw []byte, rec changeRec, sig, signedAt string, now time.Time) (approvalRec, bool, error) {
	var ap approvalRec
	got, err := hashOfRaw(raw)
	if err != nil || got != rec.CanonicalHash {
		return ap, false, sigErr("hash_mismatch", nil)
	}
	k, err := a.verifySignature(ctx, got, signedAt, sig)
	if err != nil {
		return ap, false, err
	}
	rcv := fmtTime(now)
	if err := checkSkew(signedAt, rcv); err != nil {
		return ap, false, err
	}
	ap = approvalRec{ChangeID: rec.ChangeID, CanonicalHash: got, Approvers: []approverRec{{KeyFP: k.KeyFP, KeyType: k.KeyType, SignedAt: signedAt, Sig: sig, ReceivedAt: rcv}}}
	return ap, true, nil
}

// sameApproval は承認済みの変更への再提出: 同じ署名なら変化なし、違う署名は受け付けない(4.6)。
func (a *Approval) sameApproval(s *pstore, id, sig, signedAt string) (approveOut, error) {
	ap, ok, err := s.readApproval(id)
	if err != nil {
		return approveOut{}, err
	}
	st := evApproved
	if !ok || len(ap.Approvers) != 1 || ap.Approvers[0].Sig != sig || ap.Approvers[0].SignedAt != signedAt {
		return approveOut{}, stateErr(id, &st)
	}
	rec, _, err := s.readChange(id)
	if err != nil {
		return approveOut{}, err
	}
	nb, err := notBefore(ap, rec.DelayHours, nil)
	if err != nil {
		return approveOut{}, err
	}
	var proof *bool
	if needsProof(rec.Op) && ap.NotifyProofOK {
		t := true
		proof = &t
	}
	return approveOut{ApprovedAt: ap.Approvers[0].ReceivedAt, NotBefore: fmtTime(nb), State: evApproved, ProofOK: proof}, nil
}

// expireUnapproved は期限を過ぎた未承認の提案を expired にして change.expired を送る。承認は受理しない(CHANGE_STATE_INVALID)。
func (a *Approval) expireUnapproved(ctx context.Context, s *pstore, rec changeRec) error {
	h, err := a.State.Lock(ctx)
	if err != nil {
		return err
	}
	ev, aerr := s.appendEvent(logEvent{Event: evExpired, ChangeID: rec.ChangeID})
	if aerr == nil {
		aerr = a.record(ctx, h, ev, nil)
	}
	h.Release()
	if aerr == nil {
		a.emit(ctx, "change.expired", map[string]any{"change_id": rec.ChangeID, "op": rec.Op, "not_before": nil, "expired_reason": "unapproved"})
	}
	st := evExpired
	return stateErr(rec.ChangeID, &st)
}

// commitApproval は state.lock の下で最後の事象が proposed のままであることを確かめ、承認の記録を書いて approved を追記する(4.5)。
func (a *Approval) commitApproval(ctx context.Context, s *pstore, rec changeRec, ap approvalRec, proof *bool, now time.Time) (approveOut, error) {
	h, err := a.State.Lock(ctx)
	if err != nil {
		return approveOut{}, err
	}
	released := false
	defer func() {
		if !released {
			h.Release()
		}
	}()
	raw, err := marshalCanonical(ap)
	if err != nil {
		return approveOut{}, err
	}
	if err := s.b.write(approvalName(rec.ChangeID), raw); err != nil {
		return approveOut{}, err
	}
	ev, err := s.appendEvent(logEvent{Event: evApproved, ChangeID: rec.ChangeID}) // 遷移の検査(取り消し・失効との競合)を含む
	if err != nil {
		return approveOut{}, err
	}
	if needsProof(rec.Op) && rec.Op != "seal" {
		if derr := a.dropChangeProofs(ctx, h, rec.ChangeID); derr != nil {
			return approveOut{}, derr
		}
	}
	if rec.DelayHours > 0 {
		ds, derr := a.readDelays(ctx)
		if derr != nil {
			return approveOut{}, derr
		}
		derr = a.putItem(ctx, h, "delays", append(ds, delayRec{ChangeID: rec.ChangeID, Deliveries: []deliveryGroup{}}))
		if derr != nil {
			return approveOut{}, derr
		}
	}
	if err := a.record(ctx, h, ev, raw); err != nil {
		return approveOut{}, err
	}
	nb, err := notBefore(ap, rec.DelayHours, nil)
	if err != nil {
		return approveOut{}, err
	}
	h.Release()
	released = true
	res := a.emit(ctx, "change.approved", map[string]any{"change_id": rec.ChangeID, "op": rec.Op, "not_before": fmtTime(nb)})
	if rec.DelayHours > 0 {
		a.noteDeliveries(ctx, rec.ChangeID, "change.approved", res, true)
	}
	return approveOut{ApprovedAt: fmtTime(now), NotBefore: fmtTime(nb), State: evApproved, ProofOK: proof}, nil
}
