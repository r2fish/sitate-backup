package approval

// propose(spec 3.1.2.9、3.6 の propose の行)。

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type proposeOut struct {
	ChangeID  string          `json:"change_id"`
	Plan      json.RawMessage `json:"plan"`
	Hash      string          `json:"canonical_hash"`
	NotBefore *string         `json:"not_before_if_approved_now"`
	Reused    bool            `json:"reused"`
	State     string          `json:"state"`
}

// Propose は contract.Approval を満たす。
func (a *Approval) Propose(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := a.ready(); err != nil {
		return contract.Reply{}, err
	}
	if len(c.Positional) != 1 {
		return contract.Reply{}, argErr("--kind", "missing")
	}
	if c.Positional[0] == "attest" {
		return a.Attest(ctx, c)
	}
	out, err := a.propose(ctx, c.Positional[0], c.Args, c.DryRun)
	if err != nil {
		return contract.Reply{}, err
	}
	b, err := json.Marshal(out)
	return contract.Reply{Result: b}, err
}

func (a *Approval) propose(ctx context.Context, kind string, in map[string][]string, dry bool) (proposeOut, error) {
	var out proposeOut
	if kind == "keep-days" || kind == "keep-days-extend" {
		return out, apiErr(contract.CodeNotImplemented, map[string]any{"feature": kind})
	}
	spec, ok := kinds[kind]
	if !ok {
		return out, argErr("--kind", "unknown")
	}
	if _, _, ce, err := a.store().events(); err != nil || ce != nil {
		return out, chainOrErr(ce, err)
	}
	args, err := normalizeArgs(spec, in)
	if err != nil {
		return out, err
	}
	plan := map[string]json.RawMessage{}
	if (kind == "notify-add" || kind == "notify-change") && a.TestNotifier == nil {
		return out, internalErr()
	}
	if err := a.checkKind(ctx, kind, spec, in, args, plan); err != nil {
		return out, err
	}
	pre, err := a.precondition(ctx, kind, args)
	if err != nil {
		return out, err
	}
	return a.persistProposal(ctx, kind, spec, args, plan, pre, dry, true)
}

func chainOrErr(ce *chainErr, err error) error {
	if ce != nil {
		return ce.toError(nil)
	}
	return err
}

// persistProposal は重複の確認、番号の割り当て、記録と事象の追記を state.lock の下で行い、通知を送る。
func (a *Approval) persistProposal(ctx context.Context, kind string, spec kindSpec, args, plan map[string]json.RawMessage, pre preState, dry, reuse bool) (proposeOut, error) {
	var out proposeOut
	now, err := a.now(ctx)
	if err != nil {
		return out, err
	}
	delay, err := a.delayFor(ctx, spec)
	if err != nil {
		return out, err
	}
	h, err := a.State.Lock(ctx)
	if err != nil {
		return out, err
	}
	released := false
	defer func() {
		if !released {
			h.Release()
		}
	}()
	s := a.store()
	evs, _, ce, err := s.events()
	if err != nil || ce != nil {
		return out, chainOrErr(ce, err)
	}
	order, states := replay(evs)
	if reuse {
		if dup, err := a.findDuplicate(s, order, states, kind, args, pre.hash); err != nil || dup != nil {
			return *dup2out(dup), err
		}
	}
	plan["conflicts"] = mustJSON(a.conflictsOf(s, order, states, kind))
	rec, err := a.buildRecord(s, kind, args, plan, pre, delay, now)
	if err != nil {
		return out, err
	}
	nb := fmtTime(now.Add(time.Duration(delay) * time.Hour))
	out = proposeOut{ChangeID: rec.ChangeID, Plan: rec.Plan, Hash: rec.CanonicalHash, NotBefore: &nb, State: evProposed}
	if dry {
		return out, nil
	}
	if err := a.writeProposal(ctx, h, s, rec); err != nil {
		return out, err
	}
	h.Release()
	released = true
	if kind == "notify-add" || kind == "notify-change" {
		if err := a.issueChangeProofs(ctx, s, rec); err != nil {
			return out, err
		}
	}
	a.emit(ctx, "change.proposed", map[string]any{"change_id": rec.ChangeID, "op": kind, "not_before": nil})
	return out, nil
}

func dup2out(d *proposeOut) *proposeOut {
	if d == nil {
		return &proposeOut{}
	}
	return d
}

func (a *Approval) buildRecord(s *pstore, kind string, args, plan map[string]json.RawMessage, pre preState, delay int, now time.Time) (changeRec, error) {
	id, err := s.nextChangeID(now.Format("20060102"))
	if err != nil {
		return changeRec{}, err
	}
	if _, ok := plan["reversible"]; !ok {
		plan["reversible"] = json.RawMessage(`true`)
	}
	plan["errors"] = json.RawMessage(`[]`)
	argsB, _ := json.Marshal(args)
	planB, _ := json.Marshal(plan)
	rec := changeRec{changeBody: changeBody{
		ChangeID: id, Op: kind, Args: argsB, Target: pre.target, PreconditionHash: pre.hash, Plan: planB,
		ProposedAt: fmtTime(now), DelayHours: delay, ExpiresAt: fmtTime(now.Add(contract.ProposalExpiry)),
	}}
	raw, err := json.Marshal(rec.changeBody)
	if err != nil {
		return rec, err
	}
	if rec.CanonicalHash, err = contract.CanonicalHash(raw); err != nil {
		return rec, err
	}
	return rec, nil
}

// writeProposal は変更の記録(以後書き換えない)を書いてから proposed を追記する(4.5)。
func (a *Approval) writeProposal(ctx context.Context, h contract.StateLockHandle, s *pstore, rec changeRec) error {
	raw, err := marshalCanonical(rec)
	if err != nil {
		return err
	}
	if err := s.b.write(changeName(rec.ChangeID), raw); err != nil {
		return err
	}
	ev, err := s.appendEvent(logEvent{Event: evProposed, ChangeID: rec.ChangeID})
	if err != nil {
		return err
	}
	return a.record(ctx, h, ev, raw)
}

// findDuplicate は同じ内容の保留中(最後の事象が proposed か approved)の提案を返す(3.1.2.9 の冪等性)。
// 同じ内容は {"op","args"} のハッシュが一致し、前提のハッシュが今の値と一致すること。
func (a *Approval) findDuplicate(s *pstore, order []string, states map[string]*chgState, kind string, args map[string]json.RawMessage, pre string) (*proposeOut, error) {
	want, err := opArgsHash(kind, args)
	if err != nil {
		return nil, err
	}
	for _, id := range order {
		if st := states[id]; st.Last != evProposed && st.Last != evApproved {
			continue
		}
		rec, _, err := s.readChange(id)
		if err != nil || rec.Op != kind || rec.PreconditionHash != pre {
			continue
		}
		var got map[string]json.RawMessage
		if json.Unmarshal(rec.Args, &got) != nil {
			continue
		}
		if h, err := opArgsHash(rec.Op, got); err == nil && h == want {
			return &proposeOut{ChangeID: id, Plan: rec.Plan, Hash: rec.CanonicalHash, Reused: true, State: states[id].Last}, nil
		}
	}
	return nil, nil
}

func opArgsHash(op string, args map[string]json.RawMessage) (string, error) {
	b, err := json.Marshal(map[string]any{"op": op, "args": args})
	if err != nil {
		return "", err
	}
	return contract.CanonicalHash(b)
}

// conflictsOf は、保留中の提案のうち先に適用されると新しい提案の前提が変わるもの、または新しい提案が先に適用されると前提が変わるもの。
func (a *Approval) conflictsOf(s *pstore, order []string, states map[string]*chgState, kind string) []string {
	out := []string{}
	for _, id := range order {
		if st := states[id]; st.Last != evProposed && st.Last != evApproved {
			continue
		}
		if rec, _, err := s.readChange(id); err == nil && conflict(rec.Op, kind) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// checkKind は種別ごとの検査と、args・plan への追加(定義を受ける種別は定義の内容を args.definition に置く)。
// Planner は種別ごとの検査の後に呼ぶ(定義を受ける種別では args.definition が入った状態で渡す)。
func (a *Approval) checkKind(ctx context.Context, kind string, spec kindSpec, in map[string][]string, args, plan map[string]json.RawMessage) error {
	var err error
	switch {
	case kind == "delay-change":
		err = checkDelayHours(args, plan)
	case kind == "approval-key-change":
		err = a.checkPubkey(ctx, args, plan)
	case kind == "restore-test-increase" || kind == "restore-test-reduce":
		err = a.checkFrequency(ctx, kind, args, plan)
	case kind == "notify-add" || kind == "notify-change":
		err = a.checkNotify(ctx, kind, args, plan)
	case spec.defFile:
		err = a.checkDefinition(ctx, kind, in["file"], args, plan)
	case kind == "restore-elsewhere" || kind == "storage-read-extra" || kind == "restore-session-extend":
		err = checkTo(kind, args)
	}
	if err != nil {
		return err
	}
	if a.Planner != nil {
		extra, _, perr := a.Planner(ctx, kind, args)
		if perr != nil {
			return perr
		}
		for k, v := range extra {
			if k != "proof_required" { // 確認語を入れさせる宛先は核が計算する(Planner に任せない)
				plan[k] = v
			}
		}
		return pinHostKey(kind, args, plan)
	}
	return nil
}

// pinHostKey は plan.host_key_fp(取得した指紋。カードに示す)を args の節にも入れ、適用で照合する指紋をカードの指紋と同じ値にする(3.1.2.9)。
// 節に host_key_fp があって plan の値と違えば提案を作らない。
func pinHostKey(kind string, args, plan map[string]json.RawMessage) error {
	key := map[string]string{"source-change": "source", "restore-change": "restore"}[kind]
	var fp *string
	if key == "" || json.Unmarshal(plan["host_key_fp"], &fp) != nil || fp == nil {
		return nil
	}
	sec := map[string]json.RawMessage{}
	if json.Unmarshal(args[key], &sec) != nil {
		return argErr("--"+key, "format")
	}
	if cur, ok := sec["host_key_fp"]; ok && string(cur) != "null" && string(cur) != string(mustJSON(*fp)) {
		return hostkeyErr(*fp, strings.Trim(string(cur), `"`))
	}
	sec["host_key_fp"] = mustJSON(*fp)
	b, _ := json.Marshal(sec)
	args[key], _ = contract.CanonicalJSON(b)
	return nil
}
