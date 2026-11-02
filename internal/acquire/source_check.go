package acquire

// source check と取得の前の自己診断(spec 3.3「source check の自己診断」、3.1.2 source check)。読み取りだけで、状態ファイルを変えない。

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// checkOut は自己診断の結果の全体。
type checkOut struct {
	Items      []selfItem
	Uncovered  []uncovered
	Actionable []uncovered
	NewSince   []uncovered
}

func (o checkOut) reply() contract.Reply {
	un := func(u []uncovered) []uncovered {
		if u == nil {
			return []uncovered{}
		}
		return u
	}
	b, _ := json.Marshal(map[string]any{"selftest": o.Items, "uncovered": un(o.Uncovered), "actionable": un(o.Actionable), "new_since_last": un(o.NewSince)})
	return contract.Reply{Result: b}
}

// Selftest は contract.Acquire を満たす。取得の前の自己診断で、結果は source check と同じ形。受け口に届かなければ STORAGE_UNREACHABLE。
func (a *Acquire) Selftest(ctx context.Context) (contract.Reply, error) {
	return a.SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}})
}

// SourceCheck は contract.Acquire を満たす。外れた項目は誤りでなく selftest[] の ok: false と code で返す。
func (a *Acquire) SourceCheck(ctx context.Context, c contract.Call) (contract.Reply, error) {
	o, err := a.check(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	return o.reply(), nil
}

func (a *Acquire) check(ctx context.Context) (checkOut, error) {
	if err := need(a.State, a.SSH, a.Secrets); err != nil {
		return checkOut{}, err
	}
	if e := ctxErr(ctx); e != nil {
		return checkOut{}, e
	}
	raw, err := readDef(ctx, a.State)
	if err != nil {
		return checkOut{}, err
	}
	def, err := parseCheckDef(raw)
	if err != nil {
		return checkOut{}, err
	}
	key, err := loadPullKey(ctx, a.Secrets)
	if err != nil {
		return checkOut{}, err
	}
	pin, err := a.pullPin(ctx, def.Host, key)
	if err != nil {
		return checkOut{}, err
	}
	conn := pullConn(def.Host, key, pin)
	doc, err := a.pullSelftest(ctx, conn)
	if err != nil {
		return checkOut{}, err
	}
	want := ""
	if rb, _ := a.State.Get(ctx, "receiver_hash"); len(rb) > 0 {
		_ = json.Unmarshal(rb, &want)
	}
	o := checkOut{Items: evalSelftest(doc, want, def)}
	return a.uncoveredOf(ctx, conn, def, doc, o)
}

// pullPin は pull 接続で照らす指紋(固定値、無ければ source_seen)。どちらも無ければ見えた指紋だけを取って SOURCE_HOSTKEY_MISMATCH(expected_fp: null)。
func (a *Acquire) pullPin(ctx context.Context, host string, key pullKey) (string, error) {
	fixed, seen, err := sourcePins(ctx, a.State, host)
	if err != nil {
		return "", err
	}
	if len(fixed) > 0 {
		return fixed[0], nil
	}
	if len(seen) > 0 {
		return seen[0], nil
	}
	d := enrollDeps{SSH: a.SSH}
	e := d.noRecord(ctx, mkErr(contract.CodeSourceHostkeyMismatch, map[string]any{"expected_fp": nil, "session_id": nil}), pullConn(host, key, ""))
	return "", e
}

// pullSelftest は受け口の selftest を実行して読む。受け口に届かなければ STORAGE_UNREACHABLE。
func (a *Acquire) pullSelftest(ctx context.Context, conn contract.SSHConn) (selfDoc, error) {
	o, err := pullRun(ctx, a.SSH, conn, nil, "selftest")
	if err != nil {
		return selfDoc{}, pullErr(err)
	}
	if o.Exit == contract.RecvExitUnreachable {
		return selfDoc{}, errUnreachable()
	}
	if o.Exit != 0 {
		return selfDoc{}, errInternal("exit_code")
	}
	return parseSelfDoc(o.Out)
}

// uncoveredOf は受け口の survey と source_clusters から uncovered[]・actionable[]・new_since_last[] を作る。
func (a *Acquire) uncoveredOf(ctx context.Context, conn contract.SSHConn, def checkDef, doc selfDoc, o checkOut) (checkOut, error) {
	so, err := pullRun(ctx, a.SSH, conn, nil, "survey")
	if err != nil {
		return o, pullErr(err)
	}
	out := bytes.TrimSpace(so.Out)
	if so.Exit != 0 || contract.Validate("receiver/survey.json#/$defs/recv_survey", out) != nil {
		return o, errInternal("output")
	}
	var rs recvSurvey
	_ = json.Unmarshal(out, &rs)
	sc, err := stateClusters(ctx, a.State)
	if err != nil {
		return o, err
	}
	u := judgeCheck(rs, sc, def)
	for _, t := range doc.Targets {
		if t.Kind == "postgresql" && t.LargeObjects != nil && *t.LargeObjects && inDef(def, t.ID) {
			id := t.ID
			u = append(u, uncovered{Kind: "large_object", TargetID: &id})
		}
	}
	o.Uncovered, o.Actionable = u, actionableOf(u)
	prev, err := getList[uncovered](ctx, a.State, "uncovered_seen")
	if err != nil {
		return o, err
	}
	o.NewSince = newSince(u, prev)
	return o, nil
}

func stateClusters(ctx context.Context, st contract.StateFiles) ([]stateCluster, error) {
	raw, err := st.Get(ctx, "source_clusters")
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var s struct {
		Clusters []stateCluster `json:"clusters"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil, errInternal("")
	}
	return s.Clusters, nil
}

func ukey(u uncovered) string {
	b, _ := json.Marshal(u)
	return string(b)
}

// newSince は前回の記録に無い要素。
func newSince(cur, prev []uncovered) []uncovered {
	seen := map[string]bool{}
	for _, p := range prev {
		seen[ukey(p)] = true
	}
	r := []uncovered{}
	for _, u := range cur {
		if !seen[ukey(u)] {
			r = append(r, u)
		}
	}
	return r
}
