package store

// 骨格(作業指示書 5.1): 契約のインターフェースを満たし、NOT_IMPLEMENTED を返す。

import (
	"context"
	"io"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ObjectStore は contract.ObjectStore の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が差し込む)。Offset は試験ビルドの「Date に足すオフセット」(製品ビルドでは nil)。
type ObjectStore struct {
	Endpoint, Bucket, AccessKey, SecretKey string
	// Prefix は spec 4.2 の `<p>`(既定 "sitate/")。実物が全操作のキーに付け、ListVersions の Key から外す(contract/storage.go の注記)。
	Prefix string
	Params contract.RetentionParams
	Offset contract.Offsetter
}

var _ contract.ObjectStore = (*ObjectStore)(nil)

// Put は contract.ObjectStore を満たす。
func (*ObjectStore) Put(ctx context.Context, key string, body io.Reader, class contract.RetentionClass) (contract.PutResult, error) {
	var r0 contract.PutResult
	return r0, contract.ErrNotImplemented("store.ObjectStore.Put")
}

// Get は contract.ObjectStore を満たす。
func (*ObjectStore) Get(ctx context.Context, key, versionID string) (io.ReadCloser, error) {
	var r0 io.ReadCloser
	return r0, contract.ErrNotImplemented("store.ObjectStore.Get")
}

// ListVersions は contract.ObjectStore を満たす。
func (*ObjectStore) ListVersions(ctx context.Context, prefix string) ([]contract.VersionInfo, error) {
	var r0 []contract.VersionInfo
	return r0, contract.ErrNotImplemented("store.ObjectStore.ListVersions")
}

// Extend は contract.ObjectStore を満たす。
func (*ObjectStore) Extend(ctx context.Context, key, versionID string, retainUntil time.Time) error {
	return contract.ErrNotImplemented("store.ObjectStore.Extend")
}

// DeleteVersion は contract.ObjectStore を満たす。
func (*ObjectStore) DeleteVersion(ctx context.Context, key, versionID string) error {
	return contract.ErrNotImplemented("store.ObjectStore.DeleteVersion")
}

// BucketConfig は contract.ObjectStore を満たす。
func (*ObjectStore) BucketConfig(ctx context.Context) (contract.BucketConfig, error) {
	var r0 contract.BucketConfig
	return r0, contract.ErrNotImplemented("store.ObjectStore.BucketConfig")
}

// RetainUntil は contract.ObjectStore を満たす。
func (*ObjectStore) RetainUntil(ctx context.Context, key, versionID string) (time.Time, error) {
	return time.Time{}, contract.ErrNotImplemented("store.ObjectStore.RetainUntil")
}

// CheckSkew は contract.ObjectStore を満たす。
func (*ObjectStore) CheckSkew(ctx context.Context) error {
	return contract.ErrNotImplemented("store.ObjectStore.CheckSkew")
}

// StoreTime は contract.ObjectStore を満たす。
func (*ObjectStore) StoreTime(ctx context.Context) (time.Time, error) {
	var r0 time.Time
	return r0, contract.ErrNotImplemented("store.ObjectStore.StoreTime")
}

// Retention は contract.Retention の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type Retention struct {
	// Index はチャンクの記録の読み書き(Records、SetRetain、Remove)。
	Index contract.Index
	// Intents は書き込み予定のキーの一覧(journal の write_intent。結合で cli が注入する)。中断した書き込みを UNEXPECTED_VERSION にしない。
	Intents func(ctx context.Context) ([]string, error)
	Store contract.ObjectStore
	State contract.StateFiles
	Codec contract.Codec
	KeyS  contract.KeyService
	Clock contract.Clock
	// Offset は保管先の Date に足す模擬時間のオフセット(試験ビルドだけ。nil はオフセット 0)。prune・extend の「今」は StoreTime + Offset で判定する(作業指示書 3.3)。
	Offset contract.Offsetter
}

var _ contract.Retention = (*Retention)(nil)

// Verify は contract.Retention を満たす。
func (*Retention) Verify(ctx context.Context, recorded []contract.VersionRef) (contract.VerifyResult, error) {
	var r0 contract.VerifyResult
	return r0, contract.ErrNotImplemented("store.Retention.Verify")
}

// Extend は contract.Retention を満たす。
func (*Retention) Extend(ctx context.Context, target string) (contract.ExtendResult, error) {
	var r0 contract.ExtendResult
	return r0, contract.ErrNotImplemented("store.Retention.Extend")
}

// Prune は contract.Retention を満たす。
func (*Retention) Prune(ctx context.Context) (contract.PruneResult, error) {
	var r0 contract.PruneResult
	return r0, contract.ErrNotImplemented("store.Retention.Prune")
}

// HoldSet は contract.Retention を満たす。
func (*Retention) HoldSet(ctx context.Context, in contract.HoldInput) (time.Time, error) {
	var r0 time.Time
	return r0, contract.ErrNotImplemented("store.Retention.HoldSet")
}

// Class は contract.Retention を満たす。
func (*Retention) Class(in contract.ClassInput) (contract.RetentionClass, error) {
	var r0 contract.RetentionClass
	return r0, contract.ErrNotImplemented("store.Retention.Class")
}

// MissingRefs は contract.Retention を満たす。
func (*Retention) MissingRefs(ctx context.Context, refs []contract.VersionRef) ([]contract.VersionRef, error) {
	var r0 []contract.VersionRef
	return r0, contract.ErrNotImplemented("store.Retention.MissingRefs")
}

// StoreProbe は contract.StoreProbe の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// Probe は probe/ の版の書き込みと各役割の鍵での操作の試行。Keys は役割(admin / write / read)ごとの ObjectStore。
type StoreProbe struct {
	Keys map[string]contract.ObjectStore
}

var _ contract.StoreProbe = (*StoreProbe)(nil)

// PutProbe は contract.StoreProbe を満たす。
func (*StoreProbe) PutProbe(ctx context.Context, key string, body []byte) (versionID string, err error) {
	var r0 string
	return r0, contract.ErrNotImplemented("store.StoreProbe.PutProbe")
}

// Try は contract.StoreProbe を満たす。
func (*StoreProbe) Try(ctx context.Context, role, op string) (allowed bool, err error) {
	var r0 bool
	return r0, contract.ErrNotImplemented("store.StoreProbe.Try")
}
