package approval

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// acceptRec は配送を受理する Recorder(遅延中の通知の配送成功の条件 (3) を満たす)。
type acceptRec struct {
	*fake.Recorder
	clock  *fake.Clock
	accept bool
	lag    time.Duration // 受理の時刻 = 今 + lag
	st     *fake.StateFiles
	locked []string // Emit の時に state.lock を持っていた通知
}

func (r *acceptRec) Emit(ctx ctxT, typ string, facts []byte) (contract.Event, []contract.DeliveryResult, error) {
	ev, _, err := r.Recorder.Emit(ctx, typ, facts)
	if r.st != nil && r.st.Locked {
		r.locked = append(r.locked, typ)
	}
	if !r.accept {
		return ev, nil, err
	}
	t := r.clock.T.Add(r.lag)
	return ev, []contract.DeliveryResult{{To: "ops", Channel: "email", Delivered: true, Attempts: 1, AcceptedAt: &t}}, err
}

type env struct {
	t     *testing.T
	a     *Approval
	st    *fake.StateFiles
	clock *fake.Clock
	rec   *acceptRec
	priv  ed25519.PrivateKey
}

func pubLine(pub ed25519.PublicKey) (string, string) {
	sp, _ := ssh.NewPublicKey(pub)
	return string(ssh.MarshalAuthorizedKey(sp)), ssh.FingerprintSHA256(sp)
}

func newEnv(t *testing.T, accept bool) *env {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	line, fp := pubLine(pub)
	k, _ := json.Marshal(map[string]string{"key_fp": fp, "key_type": "software", "public_key": line})
	st := &fake.StateFiles{Items: map[string]json.RawMessage{"approval_key": k, "sealed": json.RawMessage(`true`)}}
	clock := &fake.Clock{T: fake.StartTime}
	rec := &acceptRec{Recorder: &fake.Recorder{}, clock: clock, accept: accept, st: st}
	return &env{t: t, a: &Approval{State: st, Recorder: rec, Clock: clock, Dir: t.TempDir()}, st: st, clock: clock, rec: rec, priv: priv}
}

func (e *env) propose(op string, args map[string][]string) proposeRes {
	e.t.Helper()
	r, err := e.a.Propose(ctx, contract.Call{Words: []string{"propose"}, Positional: []string{op}, Args: args})
	if err != nil {
		e.t.Fatalf("propose: %v", err)
	}
	var p proposeRes
	_ = json.Unmarshal(r.Result, &p)
	return p
}

func sign(priv ed25519.PrivateKey, hash, signedAt string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, signedText(hash, signedAt)))
}

func (e *env) approveCall(id, sig, signedAt string) (contract.Reply, error) {
	return e.a.Approve(ctx, contract.Call{Words: []string{"approve"}, Args: map[string][]string{"id": {id}, "signature": {sig}, "signed-at": {signedAt}}})
}

func (e *env) approveOK(p proposeRes) {
	e.t.Helper()
	at := fmtTime(e.clock.T)
	if _, err := e.approveCall(p.ChangeID, sign(e.priv, p.Hash, at), at); err != nil {
		e.t.Fatalf("approve: %v", err)
	}
}

func (e *env) apply(args map[string][]string) (contract.ApplyPlan, error) {
	return e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}, Args: args}, "i-1")
}

func codeOf(err error) contract.Code {
	var ce *contract.Error
	if asContractErr(err, &ce) {
		return ce.Code
	}
	return ""
}

func reasonOf(err error) string {
	var ce *contract.Error
	var d struct{ Reason string }
	if asContractErr(err, &ce) {
		_ = json.Unmarshal(ce.Detail, &d)
	}
	return d.Reason
}

var incr = map[string][]string{"to": {"weekly"}} // restore-test-increase(遅延 0)

func hours(h int) time.Duration { return time.Duration(h) * time.Hour }

func jsonUnmarshalDetail(b []byte, v any) error { return json.Unmarshal(b, v) }

// candState は ReadCandidate と ValidateDef の値を差し替える StateFiles。
type candState struct {
	*fake.StateFiles
	cand     []byte
	candHash string
	defHash  string
}

func (c *candState) ReadCandidate(ctx ctxT) ([]byte, string, bool, error) {
	return c.cand, c.candHash, c.cand != nil, nil
}

func (c *candState) ValidateDef(_ ctxT, d []byte, _ bool) ([]byte, string, json.RawMessage, error) {
	return d, c.defHash, json.RawMessage(`[]`), nil
}

// recEnroller は登録の入力を覚える。
type recEnroller struct{ in []contract.EnrollInput }

func (r *recEnroller) Enroll(_ ctxT, in contract.EnrollInput) (contract.EnrollResult, error) {
	r.in = append(r.in, in)
	return contract.EnrollResult{}, nil
}

// withDefs は定義を返す ReadDefinition を足す。
func (e *env) withDef(def string) {
	e.a.ReadDefinition = func(ctxT, string) ([]byte, error) { return []byte(def), nil }
}

func (e *env) setDef(def string) { e.st.Def = []byte(def) }

// deliver は遅延のある変更を承認し、遅延を過ぎるまで進める。
func (e *env) approveAndWait(p proposeRes, h int) {
	e.t.Helper()
	e.approveOK(p)
	e.clock.Advance(hours(h))
}

func (e *env) delayHoursOf(id string) int {
	e.t.Helper()
	rec, _, err := e.a.store().readChange(id)
	if err != nil {
		e.t.Fatal(err)
	}
	return rec.DelayHours
}

func (e *env) planOf(id string) string {
	e.t.Helper()
	rec, _, err := e.a.store().readChange(id)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(rec.Plan)
}
