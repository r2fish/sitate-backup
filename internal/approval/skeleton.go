package approval

// 骨格(作業指示書 5.1): 契約のインターフェースを満たし、NOT_IMPLEMENTED を返す。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Approval は contract.Approval の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が偽物を差し込む。実装は他のフィールドを足してよいが、これらを消さない)。
type Approval struct {
	// Dir は状態ディレクトリ(pending/ の置き場)。空なら動かない。
	Dir      string
	State    contract.StateFiles
	Enroller contract.Enroller
	Recorder contract.Recorder
	Clock    contract.Clock
}

var _ contract.Approval = (*Approval)(nil)

// Propose は contract.Approval を満たす。
func (*Approval) Propose(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("approval.Approval.Propose")
}

// Approve は contract.Approval を満たす。
func (*Approval) Approve(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("approval.Approval.Approve")
}

// ApplyPlan は contract.Approval を満たす。
func (*Approval) ApplyPlan(ctx context.Context, c contract.Call, instance string) (contract.ApplyPlan, error) {
	var r0 contract.ApplyPlan
	return r0, contract.ErrNotImplemented("approval.Approval.ApplyPlan")
}

// ApplyEnroll は contract.Approval を満たす。
func (*Approval) ApplyEnroll(ctx context.Context, changeID string, pos int) (contract.StepResult, error) {
	var r0 contract.StepResult
	return r0, contract.ErrNotImplemented("approval.Approval.ApplyEnroll")
}

// ApplyRecord は contract.Approval を満たす。
func (*Approval) ApplyRecord(ctx context.Context, changeID string, pos int, r contract.StepResult) error {
	return contract.ErrNotImplemented("approval.Approval.ApplyRecord")
}

// Cancel は contract.Approval を満たす。
func (*Approval) Cancel(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("approval.Approval.Cancel")
}

// Attest は contract.Approval を満たす。
func (*Approval) Attest(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("approval.Approval.Attest")
}

// Pending は contract.Approval を満たす。
func (*Approval) Pending(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("approval.Approval.Pending")
}

// Gate は contract.Approval を満たす。
func (*Approval) Gate(ctx context.Context, line string) (contract.GateDecision, error) {
	var r0 contract.GateDecision
	return r0, contract.ErrNotImplemented("approval.Approval.Gate")
}

// Proposer は contract.Proposer の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type Proposer struct{}

var _ contract.Proposer = (*Proposer)(nil)

// Propose は contract.Proposer を満たす。
func (*Proposer) Propose(ctx context.Context, kind string, content json.RawMessage) (changeID string, err error) {
	var r0 string
	return r0, contract.ErrNotImplemented("approval.Proposer.Propose")
}

// PendingList は contract.PendingList の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type PendingList struct {
	Dir   string
	State contract.StateFiles
}

var _ contract.PendingList = (*PendingList)(nil)

// List は contract.PendingList を満たす。
func (*PendingList) List(ctx context.Context) ([]json.RawMessage, error) {
	var r0 []json.RawMessage
	return r0, contract.ErrNotImplemented("approval.PendingList.List")
}
