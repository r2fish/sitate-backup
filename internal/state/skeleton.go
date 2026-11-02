package state

// 骨格(作業指示書 5.1): 契約のインターフェースを満たし、NOT_IMPLEMENTED を返す。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// StateFiles は contract.StateFiles の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が差し込む。実装は他のフィールドを足してよいが、これらを消さない)。Dir は状態ディレクトリ(spec 4.5)。
type StateFiles struct {
	Dir   string
	Codec contract.Codec
	KeyS  contract.KeyService
	Clock contract.Clock
}

var _ contract.StateFiles = (*StateFiles)(nil)

// Lock は contract.StateFiles を満たす。
func (*StateFiles) Lock(ctx context.Context) (contract.StateLockHandle, error) {
	var r0 contract.StateLockHandle
	return r0, contract.ErrNotImplemented("state.StateFiles.Lock")
}

// ReadDef は contract.StateFiles を満たす。
func (*StateFiles) ReadDef(ctx context.Context) (normalized []byte, defHash string, err error) {
	var r0 []byte
	var r1 string
	return r0, r1, contract.ErrNotImplemented("state.StateFiles.ReadDef")
}

// ReadCandidate は contract.StateFiles を満たす。
func (*StateFiles) ReadCandidate(ctx context.Context) (normalized []byte, defHash string, found bool, err error) {
	var r0 []byte
	var r1 string
	var r2 bool
	return r0, r1, r2, contract.ErrNotImplemented("state.StateFiles.ReadCandidate")
}

// ValidateDef は contract.StateFiles を満たす。
func (*StateFiles) ValidateDef(ctx context.Context, def []byte, skipRefs bool) (normalized []byte, defHash string, issues json.RawMessage, err error) {
	var r0 []byte
	var r1 string
	var r2 json.RawMessage
	return r0, r1, r2, contract.ErrNotImplemented("state.StateFiles.ValidateDef")
}

// StageDef は contract.StateFiles を満たす。
func (*StateFiles) StageDef(ctx context.Context, normalized []byte) error {
	return contract.ErrNotImplemented("state.StateFiles.StageDef")
}

// Get は contract.StateFiles を満たす。
func (*StateFiles) Get(ctx context.Context, item string) (json.RawMessage, error) {
	var r0 json.RawMessage
	return r0, contract.ErrNotImplemented("state.StateFiles.Get")
}

// Update は contract.StateFiles を満たす。
func (*StateFiles) Update(ctx context.Context, held contract.StateLockHandle, item string, v json.RawMessage) error {
	return contract.ErrNotImplemented("state.StateFiles.Update")
}

// SwitchDef は contract.StateFiles を満たす。
func (*StateFiles) SwitchDef(ctx context.Context, in contract.SwitchInput) error {
	return contract.ErrNotImplemented("state.StateFiles.SwitchDef")
}

// LedgerAppend は contract.StateFiles を満たす。
func (*StateFiles) LedgerAppend(ctx context.Context, held contract.StateLockHandle, rec json.RawMessage) error {
	return contract.ErrNotImplemented("state.StateFiles.LedgerAppend")
}

// LedgerList は contract.StateFiles を満たす。
func (*StateFiles) LedgerList(ctx context.Context) ([]json.RawMessage, error) {
	var r0 []json.RawMessage
	return r0, contract.ErrNotImplemented("state.StateFiles.LedgerList")
}

// RecordReadStart は contract.StateFiles を満たす。
func (*StateFiles) RecordReadStart(ctx context.Context, s contract.ReadStart) error {
	return contract.ErrNotImplemented("state.StateFiles.RecordReadStart")
}

// LedgerDelete は contract.StateFiles を満たす。
func (*StateFiles) LedgerDelete(ctx context.Context, held contract.StateLockHandle, recordID string) error {
	return contract.ErrNotImplemented("state.StateFiles.LedgerDelete")
}

// SettleInterrupted は contract.StateFiles を満たす。
func (*StateFiles) SettleInterrupted(ctx context.Context, runID string) error {
	return contract.ErrNotImplemented("state.StateFiles.SettleInterrupted")
}

// Recorder は contract.Recorder の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type Recorder struct {
	State    contract.StateFiles
	Store    contract.ObjectStore
	Codec    contract.Codec
	KeyS     contract.KeyService
	Notifier contract.Notifier
	Clock    contract.Clock
}

var _ contract.Recorder = (*Recorder)(nil)

// Record は contract.Recorder を満たす。
func (*Recorder) Record(ctx context.Context, entry []byte) (prevHash string, err error) {
	var r0 string
	return r0, contract.ErrNotImplemented("state.Recorder.Record")
}

// AppendLocked は contract.Recorder を満たす。
func (*Recorder) AppendLocked(ctx context.Context, held contract.StateLockHandle, entry []byte) (string, error) {
	var r0 string
	return r0, contract.ErrNotImplemented("state.Recorder.AppendLocked")
}

// WriteRemote は contract.Recorder を満たす。
func (*Recorder) WriteRemote(ctx context.Context, item []byte) error {
	return contract.ErrNotImplemented("state.Recorder.WriteRemote")
}

// Emit は contract.Recorder を満たす。
func (*Recorder) Emit(ctx context.Context, typ string, facts []byte) (contract.Event, []contract.DeliveryResult, error) {
	var r0 contract.Event
	var r1 []contract.DeliveryResult
	return r0, r1, contract.ErrNotImplemented("state.Recorder.Emit")
}

// Notifier は contract.Notifier の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type Notifier struct {
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Clock    contract.Clock
}

var _ contract.Notifier = (*Notifier)(nil)

// Deliver は contract.Notifier を満たす。
func (*Notifier) Deliver(ctx context.Context, ev contract.Event, to []string) ([]contract.DeliveryResult, error) {
	var r0 []contract.DeliveryResult
	return r0, contract.ErrNotImplemented("state.Notifier.Deliver")
}

// Resend は contract.Notifier を満たす。
func (*Notifier) Resend(ctx context.Context) ([]contract.DeliveryResult, error) {
	var r0 []contract.DeliveryResult
	return r0, contract.ErrNotImplemented("state.Notifier.Resend")
}

// TestNotifier は contract.TestNotifier の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type TestNotifier struct {
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Clock    contract.Clock
}

var _ contract.TestNotifier = (*TestNotifier)(nil)

// Test は contract.TestNotifier を満たす。
func (*TestNotifier) Test(ctx context.Context, to []string) (proofHashes map[string]string, err error) {
	var r0 map[string]string
	return r0, contract.ErrNotImplemented("state.TestNotifier.Test")
}

// Timers は contract.Timers の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// Dir はタイマー単位の定義の置き場(systemd と cron の形は contract/timers の表による)。
type Timers struct{ Dir string }

var _ contract.Timers = (*Timers)(nil)

// Register は contract.Timers を満たす。
func (*Timers) Register(ctx context.Context, units []contract.TimerUnit) error {
	return contract.ErrNotImplemented("state.Timers.Register")
}

// SetOuterLimit は contract.Timers を満たす。
func (*Timers) SetOuterLimit(ctx context.Context, limit time.Duration) error {
	return contract.ErrNotImplemented("state.Timers.SetOuterLimit")
}
