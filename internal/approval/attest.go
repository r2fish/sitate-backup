package approval

// attest(spec 3.1.2 の attest と「attest の段」、4.6 の attest の記録)。署名を付ける段だけが記録を作り、署名は approve が付ける。適用するものは無い。

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var procStepRe = regexp.MustCompile(`^[A-Z][0-9]{1,2}$`)

type attestOut struct {
	ChangeID *string `json:"change_id"`
	Step     string  `json:"step"`
	Recorded bool    `json:"recorded"`
	Signed   bool    `json:"signed"`
	SignedAt *string `json:"signed_at"`
}

func unsignedStep(step string) bool {
	return step == "admin_access_closed" || step == "admin_access_opened" || step == "dest_disposed"
}

func signedStep(step string) bool {
	return step == "R3" || step == "R7" || procStepRe.MatchString(step)
}

// Attest は contract.Approval を満たす。
func (a *Approval) Attest(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := a.ready(); err != nil {
		return contract.Reply{}, err
	}
	step := one(c, "step")
	var out attestOut
	var err error
	switch {
	case step == "":
		return contract.Reply{}, argErr("--step", "missing")
	case unsignedStep(step):
		out, err = a.attestUnsigned(ctx, c, step)
	case signedStep(step):
		out, err = a.attestSigned(ctx, c, step)
	default:
		return contract.Reply{}, argErr("--step", "unknown")
	}
	if err != nil {
		return contract.Reply{}, err
	}
	b, err := json.Marshal(out)
	return contract.Reply{Result: b}, err
}

// attestUnsigned は署名を要さない段: 記録(4.6)を作らず state.json の項目を置く。--note は受けない。
func (a *Approval) attestUnsigned(ctx context.Context, c contract.Call, step string) (attestOut, error) {
	out := attestOut{Step: step, Recorded: true}
	if _, ok := c.Args["note"]; ok {
		return out, argErr("--note", "unknown")
	}
	if step == "dest_disposed" { // 復元セッションはイテレーション 4 以降(8.2 の D2-01)
		return out, apiErr(contract.CodeNotImplemented, map[string]any{"feature": "attest dest_disposed"})
	}
	now, err := a.now(ctx)
	if err != nil {
		return out, err
	}
	if c.DryRun {
		return out, nil
	}
	h, err := a.State.Lock(ctx)
	if err != nil {
		return out, err
	}
	defer h.Release()
	item := "admin_access_closed_at"
	if step == "admin_access_opened" {
		item = "admin_access_opened_at"
	}
	return out, a.putItem(ctx, h, item, fmtTime(now))
}

// attestSigned は署名を付ける段: 署名前の記録があれば作らずに返す。署名済みの記録は R3・R7 では返し、手順 ID では新しい記録を作る(付録は繰り返し行う)。
func (a *Approval) attestSigned(ctx context.Context, c contract.Call, step string) (attestOut, error) {
	args := map[string]json.RawMessage{"step": mustJSON(step), "note": json.RawMessage(`null`)}
	if v, ok := c.Args["note"]; ok {
		var note string
		if len(v) != 1 || json.Unmarshal([]byte(v[0]), &note) != nil {
			return attestOut{}, argErr("--note", "format")
		}
		args["note"] = mustJSON(note)
	}
	s := a.store()
	evs, _, ce, err := s.events()
	if err != nil || ce != nil {
		return attestOut{}, chainOrErr(ce, err)
	}
	order, states := replay(evs)
	if got, ok, err := a.existingAttest(s, order, states, step); err != nil || ok {
		return got, err
	}
	plan := map[string]json.RawMessage{"step": mustJSON(step)}
	if a.Planner != nil {
		extra, _, perr := a.Planner(ctx, "attest", args)
		if perr != nil {
			return attestOut{}, perr
		}
		for k, v := range extra {
			plan[k] = v
		}
	}
	pre, err := preAttest(step)
	if err != nil {
		return attestOut{}, err
	}
	res, err := a.persistProposal(ctx, "attest", kinds["attest"], args, plan, pre, c.DryRun, false)
	if err != nil {
		return attestOut{}, err
	}
	return attestOut{ChangeID: &res.ChangeID, Step: step, Recorded: true}, nil
}

func preAttest(step string) (preState, error) {
	b, _ := json.Marshal(map[string]string{"op": "attest", "step": step})
	h, err := contract.CanonicalHash(b)
	return preState{hash: h, target: json.RawMessage(`null`)}, err
}

func (a *Approval) existingAttest(s *pstore, order []string, states map[string]*chgState, step string) (attestOut, bool, error) {
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		st := states[id]
		if st.Last != evProposed && st.Last != evApproved {
			continue
		}
		rec, _, err := s.readChange(id)
		if err != nil || rec.Op != "attest" {
			continue
		}
		var args struct {
			Step string `json:"step"`
		}
		if json.Unmarshal(rec.Args, &args) != nil || args.Step != step {
			continue
		}
		out := attestOut{ChangeID: &id, Step: step, Recorded: true}
		if st.Last == evApproved {
			if step != "R3" && step != "R7" {
				continue
			}
			ap, ok, err := s.readApproval(id)
			if err != nil || !ok || len(ap.Approvers) != 1 {
				continue
			}
			out.Signed, out.SignedAt = true, &ap.Approvers[0].SignedAt
		}
		return out, true, nil
	}
	return attestOut{}, false, nil
}
