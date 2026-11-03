package acquire

// source enroll の核の側(spec 3.3「source enroll の順」「ホスト鍵」、3.1.2 source enroll、7.5 Enroller)。
// (1) ホスト鍵の照合 → (2) Surveyor の db_only → (3) クラスタを埋め込んだ source-setup の生成と実行 → (4) 合格したら記録。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// enrollDeps は登録の依存(Acquire と SourceEnroller が同じ実装を使う)。
type enrollDeps struct {
	SSH     contract.SSH
	Secrets contract.Secrets
	State   contract.StateFiles
	Clock   contract.Clock
	From    string // authorized_keys の from= に書くバックアップサーバーのアドレス
}

// enrollOut は登録の結果の全体(Enroller は EnrollResult だけを返す)。
type enrollOut struct {
	FP, Hash string
	Items    []selfItem
	Targets  []map[string]any
	Clusters []stateCluster
	Host     string
}

func (o enrollOut) result() contract.EnrollResult {
	cl := o.Clusters
	if cl == nil {
		cl = []stateCluster{}
	}
	b, _ := json.Marshal(map[string]any{"host": o.Host, "clusters": cl})
	return contract.EnrollResult{ReceiverHash: o.Hash, SourceClusters: b}
}

// Enroll は contract.Enroller を満たす。
func (e *SourceEnroller) Enroll(ctx context.Context, in contract.EnrollInput) (contract.EnrollResult, error) {
	if err := need(e.SSH, e.State); err != nil {
		return contract.EnrollResult{}, err
	}
	d := enrollDeps{SSH: e.SSH, Secrets: e.Secrets, State: e.State, From: e.From}
	o, err := d.enroll(ctx, in)
	if err != nil {
		return contract.EnrollResult{}, err
	}
	return o.result(), nil
}

// parseSourceSection は定義の節(source 節か、それを source に持つ定義全体)を読む。
func parseSourceSection(raw []byte) (checkDef, error) {
	var w struct {
		Source json.RawMessage `json:"source"`
	}
	if json.Unmarshal(raw, &w) == nil && len(w.Source) > 0 {
		return parseCheckDef(raw)
	}
	return parseCheckDef(append(append([]byte(`{"source":`), raw...), '}'))
}

func (d enrollDeps) enroll(ctx context.Context, in contract.EnrollInput) (enrollOut, error) {
	if in.Kind != "source" {
		return enrollOut{}, errArg("--kind", "format")
	}
	if in.Rotate {
		return enrollOut{}, errNotImplemented("source-enroll-rotate")
	}
	h, perr := parseHostArg(in.Host)
	if perr != nil {
		return enrollOut{}, perr
	}
	def, err := parseSourceSection(in.Section)
	if err != nil {
		return enrollOut{}, err
	}
	if in.Admin == nil {
		return enrollOut{}, errArg("--"+contract.ArgAdminRef, "missing")
	}
	if e := ctxErr(ctx); e != nil {
		return enrollOut{}, e
	}
	timing := TimingEnroll
	if !in.WriteState {
		timing = TimingApply
	}
	ch, err := chooseKey(ctx, d.State, h, def.Host, timing, in.HostKeyFP)
	if err != nil {
		return enrollOut{}, err
	}
	conn := adminConn(h, in.AdminRef, in.Admin, ch.FP)
	if ch.Err != nil {
		return enrollOut{}, d.noRecord(ctx, ch.Err, conn)
	}
	return d.enrollConnected(ctx, in, h, def, conn)
}

// noRecord は照らす値が無いとき、何も実行せずに見えた指紋だけを取って SOURCE_HOSTKEY_MISMATCH(expected_fp: null)にする。
func (d enrollDeps) noRecord(ctx context.Context, e *contract.Error, conn contract.SSHConn) error {
	if e.Code != contract.CodeSourceHostkeyMismatch {
		return e
	}
	conn.PinnedFP = sentinelFP
	_, err := d.SSH.Run(ctx, contract.SSHRequest{SSHConn: conn})
	var hk *contract.SSHHostKeyError
	if errors.As(err, &hk) {
		return withSeen(e, hk.Seen)
	}
	if err != nil {
		return mapConnErr(err, func(r string) *contract.Error { return setupAuth(r) })
	}
	return errInternal("")
}

func setupAuth(reason string) *contract.Error {
	return errSetup(setupDetail{Step: "connect", Reason: "auth"})
}

func (d enrollDeps) enrollConnected(ctx context.Context, in contract.EnrollInput, h hostSpec, def checkDef, conn contract.SSHConn) (enrollOut, error) {
	sv := &Surveyor{SSH: d.SSH, Conn: conn, Sudo: h.User != "root"}
	raw, err := sv.Survey(ctx, contract.SurveyScopeDBOnly)
	if err != nil {
		return enrollOut{}, convSurveyErr(err)
	}
	clusters, err := trustedClusters(raw)
	if err != nil {
		return enrollOut{}, err
	}
	fp := sv.SeenFP()
	key, err := d.pullKey(ctx)
	if err != nil {
		return enrollOut{}, err
	}
	from := d.From
	if from == "" { // 元サーバーの SSH 接続から観測する(台本を流す前)
		if from, err = d.observeFrom(ctx, conn); err != nil {
			return enrollOut{}, err
		}
	}
	if from == "" || strings.ContainsAny(from, "\"\n\x00'") { // from= が空の受け口の行は作れない。台本を流さない
		return enrollOut{}, errInternal("")
	}
	script, hash, err := setupScript(def, clusters, key.Secret.PublicKey, from)
	if err != nil {
		return enrollOut{}, err
	}
	if err := d.runSetup(ctx, h, conn, script, def); err != nil {
		return enrollOut{}, err
	}
	if d.Secrets == nil {
		return enrollOut{}, errInternal("")
	}
	if err := key.put(ctx, d.Secrets); err != nil {
		return enrollOut{}, err
	}
	items, err := d.pullChecks(ctx, h, key, fp, hash, def)
	if err != nil {
		return enrollOut{}, err
	}
	sealed, _ := getBool(ctx, d.State, "sealed")
	if e := itemsError(items, sealed); e != nil {
		return enrollOut{}, e
	}
	out := enrollOut{FP: fp, Hash: hash, Items: items, Clusters: clusters, Host: h.Host, Targets: enrollTargets(def)}
	if in.WriteState {
		if err := d.writeRecords(ctx, out); err != nil {
			return enrollOut{}, err
		}
	}
	return out, nil
}

// observeFrom は元サーバーから見た核の送信元のアドレス(元サーバーの SSH の SSH_CONNECTION の先頭の語)を取る。
// 取れなければ SOURCE_SETUP_FAILED(step: connect、reason: null)で台本を流さない。sudo は環境を落とすので使わない。
func (d enrollDeps) observeFrom(ctx context.Context, conn contract.SSHConn) (string, error) {
	o, err := runSSH(ctx, d.SSH, contract.SSHRequest{SSHConn: conn, Script: []byte("printf '%s\\n' \"${SSH_CONNECTION%% *}\"\n")})
	if err != nil {
		return "", mapConnErr(err, setupAuth)
	}
	from := strings.TrimSpace(string(o.Out))
	if o.Exit != 0 || from == "" || strings.Trim(from, "0123456789abcdefABCDEF.:") != "" {
		return "", errSetup(setupDetail{Step: "connect"})
	}
	return from, nil
}

// convSurveyErr は調査(db_only)の失敗を source enroll の側の原因コードにする(調査の失敗は準備の失敗の step に読み替える)。
func convSurveyErr(err error) error {
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeSourceSurveyFailed {
		return err
	}
	var dt struct{ Step, Reason string }
	_ = json.Unmarshal(ce.Detail, &dt)
	return errSetup(setupDetail{Step: dt.Step, Reason: dt.Reason})
}

// trustedClusters は db_only の調査から信じるクラスタの一覧を作る。読めないクラスタが 1 つでもあれば SOURCE_SETUP_FAILED(prereq, cluster_unreadable)。
func trustedClusters(raw json.RawMessage) ([]stateCluster, error) {
	var doc srvDoc
	if json.Unmarshal(raw, &doc) != nil {
		return nil, errInternal("output")
	}
	out := []stateCluster{}
	for _, c := range doc.Clusters {
		if !c.Readable || c.Major == nil {
			return nil, errSetup(setupDetail{Step: "prereq", Reason: "cluster_unreadable"})
		}
		rp := c.RawPaths
		if rp == nil {
			rp = []string{}
		}
		out = append(out, stateCluster{SocketDir: c.SocketDir, Port: c.Port, Major: *c.Major, RawPaths: rp})
	}
	return out, nil
}

// pullKey は既にある pull 用鍵を再利用し(再実行で変えない)、無ければ作る。
func (d enrollDeps) pullKey(ctx context.Context) (pullKey, error) {
	if d.Secrets != nil {
		if k, err := loadPullKey(ctx, d.Secrets); err == nil {
			return k, nil
		}
	}
	return newPullKey()
}

func enrollTargets(def checkDef) []map[string]any {
	r := []map[string]any{}
	for _, t := range def.Targets {
		m := map[string]any{"id": t.ID, "kind": "files", "root": t.Files}
		if t.Postgresql != "" {
			m["kind"], m["root"] = "postgresql", nil
		}
		r = append(r, m)
	}
	return r
}

type setupLine struct {
	Step     string   `json:"step"`
	TargetID *string  `json:"target_id"`
	Result   string   `json:"result"`
	Reason   *string  `json:"reason"`
	Missing  []string `json:"missing"`
}

// runSetup は source-setup を管理アクセスで流し、段の失敗を原因コードにする。
func (d enrollDeps) runSetup(ctx context.Context, h hostSpec, conn contract.SSHConn, script []byte, def checkDef) error {
	o, err := runSSH(ctx, d.SSH, contract.SSHRequest{SSHConn: conn, Script: script, Sudo: h.User != "root"})
	if err != nil {
		return mapConnErr(err, setupAuth)
	}
	var lines []setupLine
	for _, ln := range bytes.Split(bytes.TrimSpace(o.Out), []byte("\n")) {
		var l setupLine
		if json.Unmarshal(ln, &l) == nil && l.Step != "" {
			lines = append(lines, l)
		}
	}
	for _, l := range lines {
		if l.Result == "failed" {
			return setupFailure(l, def)
		}
	}
	switch {
	case o.Exit == contract.SurveyExitUnsupportedOS:
		return errSetup(setupDetail{Step: "prereq", Reason: "unsupported_os"})
	case h.User != "root" && len(lines) == 0 && (o.Exit == 1 || o.Exit == 127):
		return errSetup(setupDetail{Step: "sudo", Reason: "sudo_unavailable"})
	case o.Exit != 0 || len(lines) == 0:
		return errInternal("output")
	}
	return nil
}

// setupFailure は失敗した段を原因コードにする。段 roots の root_forbidden だけは DEF_INVALID(対象の根の pointer)。
func setupFailure(l setupLine, def checkDef) error {
	reason := ""
	if l.Reason != nil {
		reason = *l.Reason
	}
	if l.Step == "roots" && reason == "root_forbidden" {
		for i, t := range def.Targets {
			if l.TargetID != nil && t.ID == *l.TargetID {
				return errDefInvalid("/source/targets/"+itoa(i)+"/files", "root_forbidden")
			}
		}
		return errDefInvalid("/source/targets", "root_forbidden")
	}
	return errSetup(setupDetail{Step: l.Step, Reason: reason, TargetID: l.TargetID, Missing: l.Missing})
}

// writeRecords は (4) 合格したら記録を書く(host_keys の role: source、source_seen の消去、receiver_hash、source_clusters)。
func (d enrollDeps) writeRecords(ctx context.Context, o enrollOut) error {
	return withLock(ctx, d.State, func(h contract.StateLockHandle) error {
		hk, err := getList[hostKeyRec](ctx, d.State, "host_keys")
		if err != nil {
			return err
		}
		keep := []hostKeyRec{}
		for _, k := range hk {
			if !(k.Role == "source" && k.Host == o.Host) {
				keep = append(keep, k)
			}
		}
		fp := o.FP
		keep = append(keep, hostKeyRec{Role: "source", Host: o.Host, HostKeyFP: &fp})
		seen, err := getList[seenRec](ctx, d.State, "source_seen")
		if err != nil {
			return err
		}
		rest := []seenRec{}
		for _, s := range seen {
			if s.Host != o.Host {
				rest = append(rest, s)
			}
		}
		res := o.result()
		hb, _ := json.Marshal(res.ReceiverHash)
		for _, w := range []struct {
			item string
			v    any
		}{{"host_keys", keep}, {"source_seen", rest}} {
			b, _ := json.Marshal(w.v)
			if err := d.State.Update(ctx, h, w.item, b); err != nil {
				return err
			}
		}
		if err := d.State.Update(ctx, h, "receiver_hash", hb); err != nil {
			return err
		}
		return d.State.Update(ctx, h, "source_clusters", res.SourceClusters)
	})
}

// SourceEnroll は contract.Acquire を満たす。
func (a *Acquire) SourceEnroll(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := need(a.State, a.SSH); err != nil {
		return contract.Reply{}, err
	}
	// seal の後の source enroll(--rotate を含む)は承認を要する(propose source-change。spec 3.1.2)。直接実行は定義も SSH も使う前に拒否する。
	// apply の Enroller(SourceEnroller.Enroll)は承認の後に呼ばれるので止めない。
	if sealed, err := getBool(ctx, a.State, "sealed"); err != nil {
		return contract.Reply{}, err
	} else if sealed {
		e := mkErr(contract.CodeApprovalRequired, map[string]any{"kind": "source-change"})
		e.Next = []contract.Next{{Argv: []string{"sitate", "propose", "source-change"}, RequiresApproval: true}}
		return contract.Reply{}, e
	}
	if c.DryRun { // --dry-run は実行しない(spec 3.1.1)。SSH も定義の読みも状態の書き込みもしない
		return contract.Reply{}, contract.ErrNotImplemented("acquire.Acquire.SourceEnroll.dry-run")
	}
	h, err := hostArg(c)
	if err != nil {
		return contract.Reply{}, err
	}
	ref, admin, err := adminSecret(ctx, c, a.Secrets)
	if err != nil {
		return contract.Reply{}, err
	}
	defer admin.Wipe()
	def, err := readDef(ctx, a.State)
	if err != nil {
		return contract.Reply{}, err
	}
	d := enrollDeps{SSH: a.SSH, Secrets: a.Secrets, State: a.State, Clock: a.Clock, From: a.From}
	in := contract.EnrollInput{Kind: "source", Section: def, WriteState: true, Host: c.Args[contract.ArgHost][0], AdminRef: ref, Admin: admin}
	o, err := d.enroll(ctx, in)
	if err != nil {
		return contract.Reply{}, err
	}
	b, _ := json.Marshal(map[string]any{"host_key_fp": o.FP, "receiver_hash": o.Hash, "selftest": o.Items, "targets": o.Targets, "key_rotated": nil})
	_ = h
	return contract.Reply{Result: b}, nil
}
