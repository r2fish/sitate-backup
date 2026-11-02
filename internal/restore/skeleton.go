package restore

// 骨格(作業指示書 5.1): 契約のインターフェースを満たし、NOT_IMPLEMENTED を返す。

import (
	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Restore は contract.Restore の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が偽物を差し込む。実装は他のフィールドを足してよいが、これらを消さない)。
type Restore struct {
	Store    contract.ObjectStore
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Codec    contract.Codec
	KeyS     contract.KeyService
	Crypto   contract.Crypto
	Recorder contract.Recorder
	Clock    contract.Clock
	// SSH は復元先への SSH の実行(spec 3.5 の表の 6〜8: 核とチェックの配置、鍵を標準入力で渡す、`sitate restore` の実行)。Bundle は配布物(表の 6)、
	// Secrets は復元先用の鍵と保管先の読み取り鍵の取得(表の 4・7)。実物は cli が結合で注入する。
	SSH     contract.SSH
	Bundle  contract.BundleSource
	Secrets contract.Secrets
	// OpenStore は (a)(--endpoint・--bucket・--prefix)で引数の保管先を開く。状態を持たない復元先で Store の代わりに使う。実物は cli が注入する。
	OpenStore contract.StoreOpener
}

var _ contract.Restore = (*Restore)(nil)

// List は contract.Restore を満たす。
func (*Restore) List(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("restore.Restore.List")
}

// RestoreTest は contract.Restore を満たす。
func (*Restore) RestoreTest(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("restore.Restore.RestoreTest")
}

// Restore は contract.Restore を満たす。
func (*Restore) Restore(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("restore.Restore.Restore")
}

// RestoreDestEnroll は contract.Restore を満たす。
func (*Restore) RestoreDestEnroll(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("restore.Restore.RestoreDestEnroll")
}

// RestoreDestCheck は contract.Restore を満たす。
func (*Restore) RestoreDestCheck(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("restore.Restore.RestoreDestCheck")
}

// RestoreVerify は contract.Restore を満たす。
func (*Restore) RestoreVerify(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("restore.Restore.RestoreVerify")
}

// JournalVerify は contract.Restore を満たす。
func (*Restore) JournalVerify(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("restore.Restore.JournalVerify")
}

// ReadPlan は contract.ReadPlan の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が偽物を差し込む。実装は他のフィールドを足してよいが、これらを消さない)。
type ReadPlan struct {
	Store    contract.ObjectStore
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Codec    contract.Codec
	KeyS     contract.KeyService
	Crypto   contract.Crypto
	Recorder contract.Recorder
	Clock    contract.Clock
}

var _ contract.ReadPlan = (*ReadPlan)(nil)

// Plan は contract.ReadPlan を満たす。
func (*ReadPlan) Plan(ctx context.Context) (contract.PlanAmount, error) {
	var r0 contract.PlanAmount
	return r0, contract.ErrNotImplemented("restore.ReadPlan.Plan")
}

// RunCleanup は contract.RunCleanup の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type RunCleanup struct {
	State    contract.StateFiles
	Adapters contract.AdapterRunner
}

var _ contract.RunCleanup = (*RunCleanup)(nil)

// Cleanup は contract.RunCleanup を満たす。
func (*RunCleanup) Cleanup(ctx context.Context, own string, interrupted string) (cleaned, unverified []contract.Resource, err error) {
	var r0 []contract.Resource
	var r1 []contract.Resource
	return r0, r1, contract.ErrNotImplemented("restore.RunCleanup.Cleanup")
}

// RestoreDestEnroller は contract.Enroller の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が偽物を差し込む。実装は他のフィールドを足してよいが、これらを消さない)。
type RestoreDestEnroller struct {
	Store    contract.ObjectStore
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Codec    contract.Codec
	KeyS     contract.KeyService
	Crypto   contract.Crypto
	Recorder contract.Recorder
	Clock    contract.Clock
}

var _ contract.Enroller = (*RestoreDestEnroller)(nil)

// Enroll は contract.Enroller を満たす。
func (*RestoreDestEnroller) Enroll(ctx context.Context, in contract.EnrollInput) (contract.EnrollResult, error) {
	var r0 contract.EnrollResult
	return r0, contract.ErrNotImplemented("restore.RestoreDestEnroller.Enroll")
}
