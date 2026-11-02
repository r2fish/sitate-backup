package acquire

// 部品の型と依存の置き場(骨格の型を引き継ぐ)。

import (
	"github.com/r2fish/sitate-backup/internal/contract"
)

// Acquire は contract.Acquire の実物。
// 依存は公開フィールドで受ける(契約試験が偽物を差し込む。実装は他のフィールドを足してよいが、これらを消さない)。
type Acquire struct {
	Store    contract.ObjectStore
	State    contract.StateFiles
	Index    contract.Index
	Codec    contract.Codec
	KeyS     contract.KeyService
	Adapters contract.AdapterRunner
	Clock    contract.Clock
	// SSH は元サーバーの受け口(pull 用鍵、`sitate-ro`)と管理アクセスへの接続。Secrets は pull 用鍵(参照名 contract.SourcePullKeyRef)と管理用資格情報の取得・保存。
	SSH     contract.SSH
	Secrets contract.Secrets
	// Dir は状態ディレクトリ(index/files/<id> の置き場)。空なら対象ごとの前回の索引を持たない(毎回、初回として全部を取る)。
	// WorkDir は取得アダプタの容器へ読み取り専用で渡す作業ディレクトリ(previous_index の写しを置く。空なら <Dir>/work)。
	Dir     string
	WorkDir string
	// Container は取得アダプタの容器の設定(spec 3.4)。nil なら実行器が直接起動する(試験用)。
	Container *Container
	// From は source enroll が authorized_keys の from= に書くバックアップサーバーのアドレス(元サーバーから見えるもの)。空なら source enroll が元サーバーの SSH_CONNECTION から観測する
	// (受け口の中身 = receiver_hash には from= の行が入るため、cli が実際のアドレスを渡すこと)。
	From string
	// Retention は延長の目標 A の計算に使う保持の定数(nil なら製品の固定値)。
	Retention contract.RetentionParams
}

var _ contract.Acquire = (*Acquire)(nil)

// SourceEnroller は contract.Enroller の実物(導入時の source enroll と apply の enroll の要素)。Secrets は pull 用鍵の保存先(登録に合格したとき置く)。
type SourceEnroller struct {
	SSH     contract.SSH
	Secrets contract.Secrets
	State   contract.StateFiles
	From    string // Acquire.From と同じ
}

var _ contract.Enroller = (*SourceEnroller)(nil)
