package keys

// 骨格(作業指示書 5.1): 契約のインターフェースを満たし、NOT_IMPLEMENTED を返す。

import (
	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Keys は contract.Keys の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が偽物を差し込む。実装は他のフィールドを足してよいが、これらを消さない)。
type Keys struct {
	Store     contract.ObjectStore
	Probe     contract.StoreProbe
	State     contract.StateFiles
	KeyS      contract.KeyService
	Codec     contract.Codec
	Provision contract.AdapterRunner
	Clock     contract.Clock
	// Secrets は保管先の鍵・管理用鍵の取得と保存(参照名 --admin-ref、storage[0].key_ref)。Dir は状態ディレクトリの位置で、keys/(マスター鍵・保留・確定)は
	// keys が自分で読み書きする(マスター鍵の値は keys の外へ出ない。contract.KeysMasterFile ほか)。Verifier は bin/ に書く配布物の署名の検証、
	// Bundle はその配布物、OS は doctor の実測(statfs を含む)。Recorder は書き込み予定(write_intent)を実行の記録に残す(spec 4.3。全部品が持つ)。実物は cli が結合で注入する。
	Secrets  contract.Secrets
	Dir      string
	Recorder contract.Recorder
	Verifier contract.Verifier
	Bundle   contract.BundleSource
	OS       contract.OSProbe
}

var _ contract.Keys = (*Keys)(nil)

// Init は contract.Keys を満たす。
func (*Keys) Init(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.Init")
}

// InitStorage は contract.Keys を満たす。
func (*Keys) InitStorage(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.InitStorage")
}

// BucketCheck は contract.Keys を満たす。
func (*Keys) BucketCheck(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.BucketCheck")
}

// PhraseVerify は contract.Keys を満たす。
func (*Keys) PhraseVerify(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.PhraseVerify")
}

// StorageIssueReadKey は contract.Keys を満たす。
func (*Keys) StorageIssueReadKey(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.StorageIssueReadKey")
}

// StorageRevokeReadKey は contract.Keys を満たす。
func (*Keys) StorageRevokeReadKey(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.StorageRevokeReadKey")
}

// Kit は contract.Keys を満たす。
func (*Keys) Kit(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.Kit")
}

// KitConfirm は contract.Keys を満たす。
func (*Keys) KitConfirm(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.KitConfirm")
}

// Doctor は contract.Keys を満たす。
func (*Keys) Doctor(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.Doctor")
}

// SecretsSet は contract.Keys を満たす。
func (*Keys) SecretsSet(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("keys.Keys.SecretsSet")
}

// KeyService は contract.KeyService の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける。Dir は状態ディレクトリの位置で、マスター鍵(keys/master.json)は KeyService が自分で読む(外から値を受けない)。
// Provision は IssueKeys・RevokeKey が呼ぶ保管先準備アダプタ。
type KeyService struct {
	Dir       string
	Codec     contract.Codec
	Crypto    contract.Crypto
	Provision contract.AdapterRunner
	// ProvisionName は保管先準備アダプタの名前(kind は provision)。
	ProvisionName string
}

var _ contract.KeyService = (*KeyService)(nil)

// NewSnapshotKey は contract.KeyService を満たす。
func (*KeyService) NewSnapshotKey(ctx context.Context, manifestID string) (key, wrapped []byte, keyID string, err error) {
	var r0 []byte
	var r1 []byte
	var r2 string
	return r0, r1, r2, contract.ErrNotImplemented("keys.KeyService.NewSnapshotKey")
}

// OpenSnapshotKey は contract.KeyService を満たす。
func (*KeyService) OpenSnapshotKey(ctx context.Context, wrapped []byte, keyID, manifestID string) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("keys.KeyService.OpenSnapshotKey")
}

// OpenKeyring は contract.KeyService を満たす。
func (*KeyService) OpenKeyring(ctx context.Context, phrase *contract.Secret, keyring []byte) (bool, error) {
	var r0 bool
	return r0, contract.ErrNotImplemented("keys.KeyService.OpenKeyring")
}

// OpenedKeyring は contract.KeyService を満たす。
func (*KeyService) OpenedKeyring(ctx context.Context, phrase *contract.Secret, keyring []byte) (contract.KeyringInfo, error) {
	var r0 contract.KeyringInfo
	return r0, contract.ErrNotImplemented("keys.KeyService.OpenedKeyring")
}

// IssueKeys は contract.KeyService を満たす。
func (*KeyService) IssueKeys(ctx context.Context, req contract.IssueRequest) ([]contract.IssuedKey, []string, error) {
	var r0 []contract.IssuedKey
	var r1 []string
	return r0, r1, contract.ErrNotImplemented("keys.KeyService.IssueKeys")
}

// RevokeKey は contract.KeyService を満たす。
func (*KeyService) RevokeKey(ctx context.Context, req contract.RevokeRequest) ([]string, []string, error) {
	return nil, nil, contract.ErrNotImplemented("keys.KeyService.RevokeKey")
}

// ChunkID は contract.KeyService を満たす。
func (*KeyService) ChunkID(plain []byte) string {
	panic(contract.ErrNotImplemented("keys.KeyService.ChunkID"))
}

// EntryRef は contract.KeyService を満たす。
func (*KeyService) EntryRef(path string) string {
	panic(contract.ErrNotImplemented("keys.KeyService.EntryRef"))
}

// CandidateFingerprint は contract.KeyService を満たす。
func (*KeyService) CandidateFingerprint(kind, path string) string {
	panic(contract.ErrNotImplemented("keys.KeyService.CandidateFingerprint"))
}

// WrapChunkKey は contract.KeyService を満たす。
func (*KeyService) WrapChunkKey(chunkKey []byte, chunkID string) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("keys.KeyService.WrapChunkKey")
}

// UnwrapChunkKey は contract.KeyService を満たす。
func (*KeyService) UnwrapChunkKey(wrapped []byte, chunkID string) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("keys.KeyService.UnwrapChunkKey")
}

// DerivedKey は contract.KeyService を満たす。
func (*KeyService) DerivedKey(purpose string) ([]byte, error) {
	var r0 []byte
	return r0, contract.ErrNotImplemented("keys.KeyService.DerivedKey")
}

// Verifier は contract.Verifier の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// Anchors は信頼の起点(minisign の公開鍵 1 行ずつ)。nil なら contract.TrustAnchors()。
type Verifier struct{ Anchors []string }

var _ contract.Verifier = (*Verifier)(nil)

// Verify は contract.Verifier を満たす。
func (*Verifier) Verify(ctx context.Context, target, sig []byte, want contract.TrustedComment) (bool, error) {
	var r0 bool
	return r0, contract.ErrNotImplemented("keys.Verifier.Verify")
}
