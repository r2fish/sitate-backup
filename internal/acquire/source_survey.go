package acquire

// source survey の入口(spec 3.1.2、3.3「元サーバーの調査」「ホスト鍵」)。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// adminConn は管理アクセスの接続の指定。
func adminConn(h hostSpec, ref string, cred *contract.Secret, pin string) contract.SSHConn {
	return contract.SSHConn{Target: contract.SSHTarget{Host: trimHost(h.Host), Port: h.Port, User: h.User}, CredRef: ref, Cred: cred, PinnedFP: pin}
}

// stateDefHost は読める定義の source.host(読めなければ空)。
func stateDefHost(ctx context.Context, st contract.StateFiles) string {
	d, err := readDef(ctx, st)
	if err != nil {
		return ""
	}
	cd, err := parseCheckDef(d)
	if err != nil {
		return ""
	}
	return cd.Host
}

// chooseKey は状態の記録から照らす値を選ぶ。
func chooseKey(ctx context.Context, st contract.StateFiles, h hostSpec, defHost, timing, proposalFP string) (contract.HostKeyChoice, error) {
	sealed, err := getBool(ctx, st, "sealed")
	if err != nil {
		return contract.HostKeyChoice{}, err
	}
	fixed, seen, err := sourcePins(ctx, st, h.Host)
	if err != nil {
		return contract.HostKeyChoice{}, err
	}
	in := contract.HostKeyChoiceInput{Host: h.Host, Timing: timing, DefHost: defHost, Sealed: sealed, SourceSeen: seen, HostKeys: fixed, ProposalFP: proposalFP}
	return (&Surveyor{}).SelectHostKey(ctx, in)
}

// SourceSurvey は contract.Acquire を満たす。
func (a *Acquire) SourceSurvey(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := need(a.State, a.SSH); err != nil {
		return contract.Reply{}, err
	}
	h, err := hostArg(c)
	if err != nil {
		return contract.Reply{}, err
	}
	if e := ctxErr(ctx); e != nil {
		return contract.Reply{}, e
	}
	ref, cred, err := adminSecret(ctx, c, a.Secrets)
	if err != nil {
		return contract.Reply{}, err
	}
	defer cred.Wipe()
	ch, err := chooseKey(ctx, a.State, h, stateDefHost(ctx, a.State), TimingSurvey, "")
	if err != nil {
		return contract.Reply{}, err
	}
	pin := ""
	if ch.Compare {
		pin = ch.FP
	}
	sv := &Surveyor{SSH: a.SSH, Conn: adminConn(h, ref, cred, pin), Sudo: h.User != "root"}
	raw, err := sv.Survey(ctx, contract.SurveyScopeFull)
	if err != nil {
		return contract.Reply{}, err
	}
	if err := putSourceSeen(ctx, a.State, a.Clock, h.Host, sv.SeenFP()); err != nil {
		return contract.Reply{}, err
	}
	return srvResult(sv, raw)
}

// srvResult は調査の値を source survey の結果の形に写す。
func srvResult(sv *Surveyor, raw json.RawMessage) (contract.Reply, error) {
	judged, err := sv.Judge(context.Background(), raw, nil)
	if err != nil {
		return contract.Reply{}, err
	}
	var doc srvDoc
	_ = json.Unmarshal(raw, &doc)
	var res map[string]json.RawMessage
	_ = json.Unmarshal(judged, &res)
	fp, _ := json.Marshal(sv.SeenFP())
	pm := doc.PrereqMissing
	if pm == nil {
		pm = []string{}
	}
	pmb, _ := json.Marshal(pm)
	res["host_key_fp"], res["prereq_missing"] = fp, pmb
	out, _ := json.Marshal(res)
	return contract.Reply{Result: out}, nil
}
