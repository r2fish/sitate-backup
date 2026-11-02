package keys

// 構造体の定義と、後のイテレーションの操作(NOT_IMPLEMENTED を返す)。実装は init.go・phrase.go・bucket.go・initstorage.go・kit.go・doctor.go・keysvc.go・keyring.go・issue.go・verifier.go。

import (
	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Keys は contract.Keys の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が偽物を差し込む。実装は他のフィールドを足してよいが、これらを消さない)。
type Keys struct {
	// HostKeyPubPath は kit_version の入力のホスト鍵の公開鍵の場所(空なら既定の場所)。試験が差し替える。
	HostKeyPubPath string
	Store          contract.ObjectStore
	Probe          contract.StoreProbe
	State          contract.StateFiles
	KeyS           contract.KeyService
	Codec          contract.Codec
	Provision      contract.AdapterRunner
	Clock          contract.Clock
	// Secrets は保管先の鍵・管理用鍵の取得と保存(参照名 --admin-ref、storage[0].key_ref)。Dir は状態ディレクトリの位置で、keys/(マスター鍵・保留・確定)は
	// keys が自分で読み書きする(マスター鍵の値は keys の外へ出ない。contract.KeysMasterFile ほか)。Verifier は bin/ に書く配布物の署名の検証、
	// Bundle はその配布物、OS は doctor の実測(statfs を含む)。Recorder は書き込み予定(write_intent)を実行の記録に残す(spec 4.3。全部品が持つ)。実物は cli が結合で注入する。
	Secrets  contract.Secrets
	Dir      string
	Recorder contract.Recorder
	Verifier contract.Verifier
	Bundle   contract.BundleSource
	OS       contract.OSProbe
	// Timers は init が定期実行を登録する口(実物は cli が結合で注入する。nil なら登録しない)。
	Timers contract.Timers
	// ProvisionName は保管先準備アダプタの名前(kind は provision)。--adapter の引数が優先し、どちらも無ければ同梱の b2。
	ProvisionName string
}

var _ contract.Keys = (*Keys)(nil)

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

// Verifier は contract.Verifier の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// Anchors は信頼の起点(minisign の公開鍵 1 行ずつ)。nil なら contract.TrustAnchors()。
type Verifier struct{ Anchors []string }

var _ contract.Verifier = (*Verifier)(nil)
