package approval

// 中断後の再開(spec 3.1.2.11「中断後の再開」。自分が追記していない applying を中断したものとして扱う)。

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func (r *applyRun) resumeInterrupted() error {
	for _, id := range append([]string(nil), r.order...) {
		st := r.states[id]
		if st.Last != evApplying || st.Instance == r.instance {
			continue
		}
		rec, _, err := r.s.readChange(id)
		if err != nil {
			return err
		}
		if err := r.resumeOne(rec); err != nil {
			return err
		}
	}
	return r.reload()
}

// resumeOne は中断した変更 1 件を再開の規則で扱う: 定義を切り替える変更(登録を伴う)は節のハッシュで場合分け、
// define だけの列の変更は常に define から、ほかは failed(RUN_INTERRUPTED)。
func (r *applyRun) resumeOne(rec changeRec) error {
	if err := r.verifyResumed(rec); err != nil {
		return r.failOnMismatch(rec, err, contract.CodeSignatureInvalid)
	}
	switch {
	case kinds[rec.Op].section != "" && needsEnroll(rec) && !isDefineOnly(rec):
		return r.resumeSwitch(rec)
	case isDefineOnly(rec):
		r.resume(rec, fullSteps(rec))
		return nil
	}
	return r.fail(rec, contract.CodeRunInterrupted)
}

func (r *applyRun) resume(rec changeRec, steps []contract.OpStep) {
	r.setSteps(rec.ChangeID, steps)
	r.plan.Changes = append(r.plan.Changes, contract.ChangeOps{ChangeID: rec.ChangeID, Op: rec.Op, Steps: steps})
}

// resumeSwitch は変える節の承認済みの定義での現在のハッシュで場合分けする: (ii) 提案の節の値なら登録を省いて define を 1 つ(両方に当たるときも (ii))、
// (i) 提案の変更前の値なら登録からやり直す(秘密が無ければ failed の HUMAN_SECRET_REQUIRED)、(iii) どちらでもなければ failed の PRECONDITION_CHANGED。
func (r *applyRun) resumeSwitch(rec changeRec) error {
	norm, _, err := r.a.State.ReadDef(r.ctx)
	if err != nil {
		return err
	}
	cur, err := sectionHash(norm, kinds[rec.Op].section)
	if err != nil {
		return err
	}
	var tgt struct {
		Before string `json:"section_hash_before"`
	}
	_ = json.Unmarshal(rec.Target, &tgt)
	var plan struct {
		After json.RawMessage `json:"after"`
	}
	_ = json.Unmarshal(rec.Plan, &plan)
	proposed := ""
	if len(plan.After) > 0 {
		proposed, _ = contract.CanonicalHash(plan.After)
	}
	switch {
	case proposed != "" && cur == proposed:
		r.resume(rec, []contract.OpStep{step(stepDefine, "define", rec.Args)})
		return nil
	case cur == tgt.Before:
		if missingSecret(rec, r.callFor(rec.ChangeID)) != "" {
			return r.fail(rec, contract.CodeHumanSecretRequired)
		}
		r.resume(rec, fullSteps(rec))
		return nil
	}
	return r.fail(rec, contract.CodePreconditionChanged)
}

// verifyResumed は再開でも記録の照合(3.6 の apply の (a))を行う。承認の記録が無いものは SIGNATURE_INVALID。
func (r *applyRun) verifyResumed(rec changeRec) error {
	raw, _, err := r.s.b.read(changeName(rec.ChangeID))
	if err != nil {
		return err
	}
	ap, ok, err := r.s.readApproval(rec.ChangeID)
	if err != nil {
		return err
	}
	if !ok {
		return sigErr("hash_mismatch", nil)
	}
	return r.a.verifyStored(r.ctx, raw, rec, ap)
}

// callFor は変更のための apply の呼び出し: 秘密は --id と一致する変更にだけ渡し、ほかの再開は秘密なしとして扱う。
func (r *applyRun) callFor(id string) contract.Call {
	if r.onlyID == id {
		return r.c
	}
	c := r.c
	c.Secrets, c.Args = nil, map[string][]string{}
	for k, v := range r.c.Args {
		switch k {
		case "admin-ref", "source-admin-ref", "host-ref", "secrets-stdin":
		default:
			c.Args[k] = v
		}
	}
	return c
}
