// Package restore は復元と復元テスト(spec 3.5、3.1.2.3〜3.1.2.6)を実装する。
package restore

import (
	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Restore は contract.Restore の実装。依存は公開フィールドで受ける(契約試験が偽物を差し込む)。
type Restore struct {
	Store    contract.ObjectStore
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Codec    contract.Codec
	KeyS     contract.KeyService
	Crypto   contract.Crypto
	Recorder contract.Recorder
	Clock    contract.Clock
	// SSH は復元先への SSH の実行(spec 3.5 の表の 5〜8)。Bundle は配布物(表の 6)、Secrets は復元先用の鍵と保管先の読み取り鍵の取得(表の 4・7)。
	SSH     contract.SSH
	Bundle  contract.BundleSource
	Secrets contract.Secrets
	// OpenStore は (a)(--endpoint・--bucket・--prefix)で引数の保管先を開く。状態を持たない復元先で Store の代わりに使う。
	OpenStore contract.StoreOpener
	// Retention は復元できなかった世代の削除停止(HoldSet)に使う。nil なら印だけを付ける。
	Retention contract.Retention
}

var _ contract.Restore = (*Restore)(nil)

// ReadPlan は contract.ReadPlan の実装(読み出し量の計画。spec 3.5)。
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

// RunCleanup は contract.RunCleanup の実装(台帳の復元先の資源の片付け。spec 3.5「中止の後の破棄」)。
// Secrets は復元先用の鍵(RestoreKeyRef)の取得に使う。nil なら鍵なしで要求する(片付けは確かめられず残存として扱う)。
type RunCleanup struct {
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Secrets  contract.Secrets
}

var _ contract.RunCleanup = (*RunCleanup)(nil)

// RestoreDestEnroller は contract.Enroller の実装。Secrets は復元先用の公開鍵の取得(無ければ生成)に使う。
type RestoreDestEnroller struct {
	Store    contract.ObjectStore
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Codec    contract.Codec
	KeyS     contract.KeyService
	Crypto   contract.Crypto
	Recorder contract.Recorder
	Clock    contract.Clock
	Secrets  contract.Secrets
}

var _ contract.Enroller = (*RestoreDestEnroller)(nil)

// JournalVerify は後のイテレーション(イテレーション 2)の機能。
func (*Restore) JournalVerify(ctx context.Context, c contract.Call) (contract.Reply, error) {
	return contract.Reply{}, contract.ErrNotImplemented("restore.Restore.JournalVerify")
}

// coreMissing は必須の依存が nil のとき INTERNAL(component: restore)を返す。
func (r *Restore) coreMissing() error {
	if r == nil || r.Store == nil || r.State == nil || r.Adapters == nil || r.Codec == nil || r.KeyS == nil || r.Crypto == nil || r.Recorder == nil || r.Clock == nil {
		return internalErr("")
	}
	return nil
}
