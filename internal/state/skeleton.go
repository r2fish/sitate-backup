package state

// 骨格(作業指示書 5.1): Recorder の構造体の定義。操作は recorder.go・emit.go が持つ。

import (
	"sync"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Recorder は contract.Recorder の実物。依存は公開フィールドで受ける(契約試験が差し込む。消さない)。
// 追加の欄: Dir は状態ディレクトリ(空で State が *StateFiles ならその Dir。どちらも無いときは journal/ の写しをこのプロセスの記憶に置く。契約試験の偽物のため)、
// RunID は発行するイベントの id に使う実行の run_id(空なら発行の時刻から作る)、Recipients は通知の宛先と repo_id の取り出し口(空なら承認済みの定義の notify.to と state.json の repo_id)。
type Recorder struct {
	State    contract.StateFiles
	Store    contract.ObjectStore
	Codec    contract.Codec
	KeyS     contract.KeyService
	Notifier contract.Notifier
	Clock    contract.Clock

	Dir        string
	RunID      string
	Recipients func(ctx contextT) (to []string, repoID *string, err error)

	mu            sync.Mutex
	mem           map[string][]byte
	emitted       int
	notifications []map[string]any
}

var _ contract.Recorder = (*Recorder)(nil)
