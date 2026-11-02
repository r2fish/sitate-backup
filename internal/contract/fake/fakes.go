package fake

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 以下は contract の各インターフェースの偽物。既定は成功の零値を返し、関数の欄に差し替えると台本になる。
// 呼び出しは Calls に記録する。同時に使うため mu で守る。

// Rec は呼び出しの記録(偽物の共通部品)。
type Rec struct {
	mu    sync.Mutex
	calls []string
}

func (r *Rec) rec(s string) { r.mu.Lock(); r.calls = append(r.calls, s); r.mu.Unlock() }

// Calls は記録した呼び出しの名前を返す。
func (r *Rec) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// Clock は contract.Clock の偽物(制御できる時計)。
type Clock struct{ T time.Time }

// Now は contract.Clock を満たす。
func (c *Clock) Now(context.Context) (time.Time, error) { return c.T, nil }

// Advance は時計を進める。
func (c *Clock) Advance(d time.Duration) { c.T = c.T.Add(d) }

var (
	_ contract.Clock           = (*Clock)(nil)
	_ contract.RetentionParams = Params{}
)

// Params は contract.RetentionParams の偽物(保持日数を変えられる)。
type Params struct{ Days, Months int }

// DailyDays は contract.RetentionParams を満たす。
func (p Params) DailyDays() int { return p.Days }

// MonthlyMonths は contract.RetentionParams を満たす。
func (p Params) MonthlyMonths() int { return p.Months }

// Lease は contract.Lease の偽物。
type Lease struct {
	Rec
	Busy   bool
	Marker []byte
}

// Acquire は contract.Lease を満たす(Busy なら取れない)。
func (l *Lease) Acquire(context.Context) (bool, error) { l.rec("Acquire"); return !l.Busy, nil }

// WriteMarker は contract.Lease を満たす。
func (l *Lease) WriteMarker(_ context.Context, m []byte) ([]byte, error) {
	l.rec("WriteMarker")
	prev := l.Marker
	l.Marker = m
	return prev, nil
}

// Deadline は contract.Lease を満たす。
func (l *Lease) Deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, contract.RunMaxDuration)
}

// StateLock は取得済みの排他の偽物。
type StateLock struct{ Released bool }

// Release は contract.StateLockHandle を満たす。
func (s *StateLock) Release() { s.Released = true }

// StateFiles は contract.StateFiles の偽物(メモリ上の state.json)。
type StateFiles struct {
	Rec
	Items  map[string]json.RawMessage
	Def    []byte
	Ledger []json.RawMessage
	Locked bool
}

// Lock は contract.StateFiles を満たす。再入すると LOCKED を返す。
func (s *StateFiles) Lock(context.Context) (contract.StateLockHandle, error) {
	s.rec("Lock")
	if s.Locked {
		return nil, &contract.Error{Code: contract.CodeLocked, Class: "conflict", Retryable: true, Detail: json.RawMessage(`{}`), Next: []contract.Next{}}
	}
	s.Locked = true
	return &lockRel{s: s}, nil
}

type lockRel struct{ s *StateFiles }

func (l *lockRel) Release() { l.s.Locked = false }

// ReadDef は contract.StateFiles を満たす。
func (s *StateFiles) ReadDef(context.Context) ([]byte, string, error) { return s.Def, "", nil }

// ReadCandidate は contract.StateFiles を満たす。
func (s *StateFiles) ReadCandidate(context.Context) ([]byte, string, bool, error) {
	return nil, "", false, nil
}

// ValidateDef は contract.StateFiles を満たす。
func (s *StateFiles) ValidateDef(_ context.Context, d []byte, _ bool) ([]byte, string, json.RawMessage, error) {
	return d, "", json.RawMessage(`[]`), nil
}

// StageDef は contract.StateFiles を満たす。
func (s *StateFiles) StageDef(context.Context, []byte) error { s.rec("StageDef"); return nil }

// Get は contract.StateFiles を満たす。
func (s *StateFiles) Get(_ context.Context, item string) (json.RawMessage, error) {
	return s.Items[item], nil
}

// Update は contract.StateFiles を満たす。
func (s *StateFiles) Update(_ context.Context, _ contract.StateLockHandle, item string, v json.RawMessage) error {
	if s.Items == nil {
		s.Items = map[string]json.RawMessage{}
	}
	s.Items[item] = v
	return nil
}

// SwitchDef は contract.StateFiles を満たす。
func (s *StateFiles) SwitchDef(_ context.Context, in contract.SwitchInput) error {
	s.rec("SwitchDef")
	s.Def = in.Normalized
	return nil
}

// LedgerAppend は contract.StateFiles を満たす。
func (s *StateFiles) LedgerAppend(_ context.Context, _ contract.StateLockHandle, r json.RawMessage) error {
	s.Ledger = append(s.Ledger, r)
	return nil
}

// LedgerList は contract.StateFiles を満たす。
func (s *StateFiles) LedgerList(context.Context) ([]json.RawMessage, error) { return s.Ledger, nil }

// RecordReadStart は contract.StateFiles を満たす。
func (s *StateFiles) RecordReadStart(context.Context, contract.ReadStart) error { return nil }

// LedgerDelete は contract.StateFiles を満たす。record_id が一致する記録を台帳から消す。
func (s *StateFiles) LedgerDelete(_ context.Context, _ contract.StateLockHandle, id string) error {
	out := s.Ledger[:0:0]
	for _, r := range s.Ledger {
		var h struct {
			RecordID string `json:"record_id"`
		}
		_ = json.Unmarshal(r, &h)
		if h.RecordID != id {
			out = append(out, r)
		}
	}
	s.Ledger = out
	return nil
}

// SettleInterrupted は contract.StateFiles を満たす。
func (s *StateFiles) SettleInterrupted(context.Context, string) error { return nil }

// SecretStore は contract.Secrets の偽物。
type SecretStore struct{ M map[string]json.RawMessage }

// Get は contract.Secrets を満たす。無ければ SECRET_MISSING。
func (s *SecretStore) Get(_ context.Context, ref string) (json.RawMessage, error) {
	v, ok := s.M[ref]
	if !ok {
		return nil, &contract.Error{Code: contract.CodeSecretMissing, Class: "precondition", Detail: json.RawMessage(`{}`), Next: []contract.Next{}}
	}
	return v, nil
}

// Put は contract.Secrets を満たす。
func (s *SecretStore) Put(_ context.Context, ref string, v json.RawMessage) error {
	if s.M == nil {
		s.M = map[string]json.RawMessage{}
	}
	s.M[ref] = v
	return nil
}

// Delete は contract.Secrets を満たす。
func (s *SecretStore) Delete(_ context.Context, ref string) error { delete(s.M, ref); return nil }

var (
	_ contract.Lease      = (*Lease)(nil)
	_ contract.StateFiles = (*StateFiles)(nil)
	_ contract.Secrets    = (*SecretStore)(nil)
)
