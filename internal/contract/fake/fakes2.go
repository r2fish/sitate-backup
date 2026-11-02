package fake

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 続き。既定は成功の零値、関数の欄で台本にする。

// Recorder は contract.Recorder の偽物。
type Recorder struct {
	Rec
	Entries [][]byte
	Events  []contract.Event
}

func (r *Recorder) add(e []byte) string { r.Entries = append(r.Entries, e); return "h" }

// Record は contract.Recorder を満たす。
func (r *Recorder) Record(_ context.Context, e []byte) (string, error) {
	r.rec("Record")
	return r.add(e), nil
}

// AppendLocked は contract.Recorder を満たす。
func (r *Recorder) AppendLocked(_ context.Context, _ contract.StateLockHandle, e []byte) (string, error) {
	r.rec("AppendLocked")
	return r.add(e), nil
}

// WriteRemote は contract.Recorder を満たす。
func (r *Recorder) WriteRemote(context.Context, []byte) error { return nil }

// Emit は contract.Recorder を満たす。
func (r *Recorder) Emit(_ context.Context, typ string, facts []byte) (contract.Event, []contract.DeliveryResult, error) {
	ev := contract.Event{ID: "e-20270301T000000Z-0000-01", Type: typ, Facts: facts, Severity: "info", Next: []contract.Next{}}
	r.Events = append(r.Events, ev)
	return ev, nil, nil
}

// Notifier は contract.Notifier の偽物。
type Notifier struct {
	Rec
	Result []contract.DeliveryResult
}

// Deliver は contract.Notifier を満たす。
func (n *Notifier) Deliver(context.Context, contract.Event, []string) ([]contract.DeliveryResult, error) {
	n.rec("Deliver")
	return n.Result, nil
}

// Resend は contract.Notifier を満たす。
func (n *Notifier) Resend(context.Context) ([]contract.DeliveryResult, error) {
	n.rec("Resend")
	return n.Result, nil
}

// TestNotifier は contract.TestNotifier の偽物。
type TestNotifier struct{ Hashes map[string]string }

// Test は contract.TestNotifier を満たす。
func (t *TestNotifier) Test(context.Context, []string) (map[string]string, error) {
	return t.Hashes, nil
}

// Timers は contract.Timers の偽物。
type Timers struct {
	Rec
	Units []contract.TimerUnit
	Outer time.Duration
}

// Register は contract.Timers を満たす。
func (t *Timers) Register(_ context.Context, u []contract.TimerUnit) error { t.Units = u; return nil }

// SetOuterLimit は contract.Timers を満たす。
func (t *Timers) SetOuterLimit(_ context.Context, d time.Duration) error { t.Outer = d; return nil }

// Retention は contract.Retention の偽物。
type Retention struct {
	Rec
	VerifyFn func() (contract.VerifyResult, error)
	Held     bool
}

// Verify は contract.Retention を満たす。
func (r *Retention) Verify(context.Context, []contract.VersionRef) (contract.VerifyResult, error) {
	if r.VerifyFn != nil {
		return r.VerifyFn()
	}
	return contract.VerifyResult{}, nil
}

// Extend は contract.Retention を満たす。
func (r *Retention) Extend(context.Context, string) (contract.ExtendResult, error) {
	return contract.ExtendResult{}, nil
}

// Prune は contract.Retention を満たす。削除停止中は何もしない。
func (r *Retention) Prune(context.Context) (contract.PruneResult, error) {
	if r.Held {
		return contract.PruneResult{Held: true}, nil
	}
	return contract.PruneResult{}, nil
}

// HoldSet は contract.Retention を満たす。
func (r *Retention) HoldSet(context.Context, contract.HoldInput) (time.Time, error) {
	r.Held = true
	return time.Time{}, nil
}

// Class は contract.Retention を満たす(健全で、同じ現地の暦月に monthly が無ければ monthly)。
func (r *Retention) Class(in contract.ClassInput) (contract.RetentionClass, error) {
	loc, err := time.LoadLocation(in.Timezone)
	if err != nil {
		return "", err
	}
	month := func(t time.Time) string { return t.In(loc).Format("2006-01") }
	if !in.Healthy {
		return contract.RetentionDaily, nil
	}
	for _, s := range in.Snapshots {
		if s.RetentionClass == contract.RetentionMonthly && month(s.CreatedAt) == month(in.CreatedAt) {
			return contract.RetentionDaily, nil
		}
	}
	return contract.RetentionMonthly, nil
}

// MissingRefs は contract.Retention を満たす。
func (r *Retention) MissingRefs(context.Context, []contract.VersionRef) ([]contract.VersionRef, error) {
	return nil, nil
}

// StoreProbe は contract.StoreProbe の偽物。Allowed[role+"/"+op] が通ったか。
type StoreProbe struct{ Allowed map[string]bool }

// PutProbe は contract.StoreProbe を満たす。
func (s *StoreProbe) PutProbe(context.Context, string, []byte) (string, error) { return "v-probe", nil }

// Try は contract.StoreProbe を満たす。
func (s *StoreProbe) Try(_ context.Context, role, op string) (bool, error) {
	return s.Allowed[role+"/"+op], nil
}

// Index は contract.Index の偽物(メモリ上)。
type Index struct{ M map[string]contract.ChunkRef }

// Lookup は contract.Index を満たす。版 ID の無いものは参照しない(送り直し)。
func (i *Index) Lookup(_ context.Context, id string) (*contract.ChunkRef, error) {
	r, ok := i.M[id]
	if !ok || r.VersionID == "" {
		return nil, nil
	}
	return &r, nil
}

// Reserve は contract.Index を満たす。
func (i *Index) Reserve(_ context.Context, r contract.ChunkRef) error {
	if i.M == nil {
		i.M = map[string]contract.ChunkRef{}
	}
	i.M[r.ChunkID] = r
	return nil
}

// SetVersion は contract.Index を満たす。
func (i *Index) SetVersion(_ context.Context, id, v string) error {
	r := i.M[id]
	r.VersionID = v
	i.M[id] = r
	return nil
}

// Rebuild は contract.Index を満たす。
func (i *Index) Rebuild(context.Context, [][]byte, []byte) error { return nil }

// Remove は contract.Index を満たす。
func (i *Index) Remove(_ context.Context, v string) error {
	for k, r := range i.M {
		if r.VersionID == v {
			delete(i.M, k)
		}
	}
	return nil
}

// Verifier は contract.Verifier の偽物。Valid が真のときだけ合格。
type Verifier struct{ Valid bool }

// Verify は contract.Verifier を満たす。
func (v *Verifier) Verify(context.Context, []byte, []byte, contract.TrustedComment) (bool, error) {
	return v.Valid, nil
}

// Proposer は contract.Proposer の偽物(同じ内容の保留中の提案は同じ id)。
type Proposer struct{ ids map[string]string }

// Propose は contract.Proposer を満たす。
func (p *Proposer) Propose(_ context.Context, kind string, c json.RawMessage) (string, error) {
	if p.ids == nil {
		p.ids = map[string]string{}
	}
	k := kind + string(c)
	if id, ok := p.ids[k]; ok {
		return id, nil
	}
	id := "chg-" + kind + "-" + string(rune('a'+len(p.ids)))
	p.ids[k] = id
	return id, nil
}

// PendingList は contract.PendingList の偽物。
type PendingList struct{ Items []json.RawMessage }

// List は contract.PendingList を満たす。
func (p *PendingList) List(context.Context) ([]json.RawMessage, error) { return p.Items, nil }

// ReadPlan は contract.ReadPlan の偽物。
type ReadPlan struct{ Amount contract.PlanAmount }

// Plan は contract.ReadPlan を満たす。
func (r *ReadPlan) Plan(context.Context) (contract.PlanAmount, error) { return r.Amount, nil }

// Enroller は contract.Enroller の偽物。
type Enroller struct {
	Err    *contract.Error
	Result contract.EnrollResult
}

// Enroll は contract.Enroller を満たす。
func (e *Enroller) Enroll(context.Context, contract.EnrollInput) (contract.EnrollResult, error) {
	if e.Err != nil {
		return contract.EnrollResult{}, e.Err
	}
	return e.Result, nil
}

// RunCleanup は contract.RunCleanup の偽物。
type RunCleanup struct{ Cleaned, Unverified []contract.Resource }

// Cleanup は contract.RunCleanup を満たす。
func (r *RunCleanup) Cleanup(context.Context, string, string) ([]contract.Resource, []contract.Resource, error) {
	return r.Cleaned, r.Unverified, nil
}

// Surveyor は contract.Surveyor の偽物。
type Surveyor struct{ Survey_ json.RawMessage }

// Survey は contract.Surveyor を満たす。
func (s *Surveyor) Survey(context.Context, string) (json.RawMessage, error) { return s.Survey_, nil }

// Judge は contract.Surveyor を満たす。
func (s *Surveyor) Judge(context.Context, json.RawMessage, []byte) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// SelectHostKey は contract.Surveyor を満たす。
func (s *Surveyor) SelectHostKey(context.Context, contract.HostKeyChoiceInput) (contract.HostKeyChoice, error) {
	return contract.HostKeyChoice{}, nil
}

var errFake = errors.New("fake: 台本が無い")

// Approval は contract.Approval の偽物。Gate は許可リストを持たず、Allow に載せた argv[0] だけ通す。
type Approval struct {
	Rec
	Allow map[string]bool
	Plan  contract.ApplyPlan
}

func rep() (contract.Reply, error) { return contract.Reply{Result: json.RawMessage(`{}`)}, nil }

// Propose は contract.Approval を満たす。
func (a *Approval) Propose(context.Context, contract.Call) (contract.Reply, error) {
	a.rec("Propose")
	return rep()
}

// Approve は contract.Approval を満たす。
func (a *Approval) Approve(context.Context, contract.Call) (contract.Reply, error) {
	a.rec("Approve")
	return rep()
}

// ApplyPlan は contract.Approval を満たす。
func (a *Approval) ApplyPlan(context.Context, contract.Call, string) (contract.ApplyPlan, error) {
	return a.Plan, nil
}

// ApplyEnroll は contract.Approval を満たす。
func (a *Approval) ApplyEnroll(context.Context, string, int) (contract.StepResult, error) {
	return contract.StepResult{}, nil
}

// ApplyRecord は contract.Approval を満たす。
func (a *Approval) ApplyRecord(context.Context, string, int, contract.StepResult) error { return nil }

// Cancel は contract.Approval を満たす。
func (a *Approval) Cancel(context.Context, contract.Call) (contract.Reply, error) { return rep() }

// Attest は contract.Approval を満たす。
func (a *Approval) Attest(context.Context, contract.Call) (contract.Reply, error) { return rep() }

// Pending は contract.Approval を満たす。
func (a *Approval) Pending(context.Context, contract.Call) (contract.Reply, error) { return rep() }

// Gate は contract.Approval を満たす(語の最初が Allow にあれば通す。拒否は契約の形: reason と index を持つ)。
func (a *Approval) Gate(_ context.Context, line string) (contract.GateDecision, error) {
	argv := strings.Split(line, " ")
	if line != "" && a.Allow[argv[0]] {
		return contract.GateDecision{Allowed: true, Argv: argv}, nil
	}
	return contract.GateDecision{Denied: &contract.Error{Code: contract.CodeGateDenied, Class: "input", Detail: json.RawMessage(`{"reason":"not_allowed","index":0}`), Next: []contract.Next{}}}, nil
}

var (
	_ contract.Recorder     = (*Recorder)(nil)
	_ contract.Notifier     = (*Notifier)(nil)
	_ contract.TestNotifier = (*TestNotifier)(nil)
	_ contract.Timers       = (*Timers)(nil)
	_ contract.Retention    = (*Retention)(nil)
	_ contract.StoreProbe   = (*StoreProbe)(nil)
	_ contract.Index        = (*Index)(nil)
	_ contract.Verifier     = (*Verifier)(nil)
	_ contract.Proposer     = (*Proposer)(nil)
	_ contract.PendingList  = (*PendingList)(nil)
	_ contract.ReadPlan     = (*ReadPlan)(nil)
	_ contract.Enroller     = (*Enroller)(nil)
	_ contract.RunCleanup   = (*RunCleanup)(nil)
	_ contract.Surveyor     = (*Surveyor)(nil)
	_ contract.Approval     = (*Approval)(nil)
	_                       = errFake
)
